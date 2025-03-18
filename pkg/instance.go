package peerjs_server

import (
	"fmt"
	"net"
	"time"

	"github.com/DWCarrot/caddy-peerjs-server/pkg/clients"
	"github.com/DWCarrot/caddy-peerjs-server/pkg/idprovider"
	"github.com/DWCarrot/caddy-peerjs-server/pkg/msgstorage"
	"github.com/DWCarrot/caddy-peerjs-server/pkg/protocol"
	"github.com/DWCarrot/caddy-peerjs-server/pkg/utils"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

type sctMsgTuple struct {
	Inner  *protocol.Message
	Expire time.Time
}

// GetExpireTime implements messagestorage.IMessage.
func (s *sctMsgTuple) GetExpireTime() time.Time {
	return s.Expire
}

// GetSource implements messagestorage.IMessage.
func (s *sctMsgTuple) GetSource() string {
	return s.Inner.Src
}

// GetTarget implements messagestorage.IMessage.
func (s *sctMsgTuple) GetTarget() string {
	return s.Inner.Dst
}

func transformToMessage(msg msgstorage.IMessage) *protocol.Message {
	return msg.(*sctMsgTuple).Inner
}

func transformToExpire(msg msgstorage.IMessage) *protocol.Message {
	return &protocol.Message{
		Type: protocol.EXPIRE,
		Src:  msg.GetTarget(),
		Dst:  msg.GetSource(),
	}
}

type MessageHandler func(msg *protocol.Message, t time.Time, self *PeerJSServerInstance, c *clients.Client) error

func DoTransmit(msg *protocol.Message, t time.Time, self *PeerJSServerInstance, c *clients.Client) error {
	err := c.UpdateTimeout(t.Add(self.ConnExpireTimeout))
	_ = err
	// TODO: Handle error?
	ok, err := self.clients.SendToClient(msg.Dst, msg)
	if err != nil {
		return err
	}
	if !ok {
		msgTuple := &sctMsgTuple{
			Inner:  msg,
			Expire: t.Add(self.MsgExpireTimeout),
		}
		overflown, e := self.storage.Store(msgTuple)
		if e != nil {
			return e
		}
		if overflownMsg, ok := overflown.(*sctMsgTuple); ok {
			// TODO: Send EXPIRE message to the sender?
			_ = overflownMsg
		}
	}
	return nil
}

func HandleHeartbeat(msg *protocol.Message, t time.Time, self *PeerJSServerInstance, c *clients.Client) error {
	err := c.UpdateTimeout(t.Add(self.ConnExpireTimeout))
	_ = err
	// TODO: Handle error?
	return nil
}

type InvalidDestinationError struct {
	Dst string
}

func (e InvalidDestinationError) Error() string {
	return fmt.Sprintf("InvalidDestinationError{ Dst: %s }", e.Dst)
}

type PeerJSServerInstance struct {
	MsgExpireTimeout  time.Duration
	ConnExpireTimeout time.Duration
	ClientIdValidator idprovider.IClientIdValidator
	handlers          map[string]MessageHandler
	clients           *clients.ClientManager
	storage           *msgstorage.MessageStorage
	logger            *zap.Logger
}

func NewInstance(
	msgExpireTimeout time.Duration,
	connExpireTimeout time.Duration,
	clientIdValidator idprovider.IClientIdValidator,
	handlers map[string]MessageHandler,
	maxClients uint,
	maxMessagesPerClient uint,
	logger *zap.Logger,
) *PeerJSServerInstance {
	return &PeerJSServerInstance{
		MsgExpireTimeout:  msgExpireTimeout,
		ConnExpireTimeout: connExpireTimeout,
		ClientIdValidator: clientIdValidator,
		handlers:          handlers,
		clients:           clients.NewClientManager(maxClients),
		storage:           msgstorage.NewMessageStorage(maxMessagesPerClient),
		logger:            logger,
	}
}

func (pjs *PeerJSServerInstance) StartSession(id string, token string, conn *websocket.Conn) (*clients.Client, error) {
	client := clients.NewClient(id, token, conn)
	err := pjs.clients.AddClient(client)
	if err != nil {
		if idTakenErr, ok := err.(*clients.IdTakenError); ok {
			pjs.logger.Debug("[PeerJSServerInstance::StartSession] AddClient: ID taken", zap.String("id", id))
			_ = idTakenErr
			msg := protocol.BuildIdTaken(id, token)
			err = client.SendMessageManually(msg)
			if err != nil {
				pjs.logger.Error("Failed to send ID-TAKEN error", zap.String("id", id), zap.Error(err))
			}
			return nil, client.CloseManually()
		}
		if tooManyClientErr, ok := err.(*clients.TooManyClientsError); ok {
			pjs.logger.Debug("[PeerJSServerInstance::StartSession] AddClient: Too many clients", zap.String("id", id), zap.Uint("limit", tooManyClientErr.Count))
			msg := protocol.BuildError(fmt.Sprintf("Too many clients, limit: %d", tooManyClientErr.Count))
			err = client.SendMessageManually(msg)
			if err != nil {
				pjs.logger.Error("Failed to send client limit error", zap.String("id", id), zap.Error(err))
			}
			return nil, client.CloseManually()
		}
		pjs.logger.Error("Failed to add client", zap.String("id", id), zap.Error(err))
		err0 := client.CloseManually()
		if err0 != nil {
			pjs.logger.Error("Failed to close client", zap.String("id", id), zap.Error(err0))
		}
		return nil, err
	}
	pjs.logger.Info("Client session started", zap.String("id", id))
	return client, nil
}

func (pjs *PeerJSServerInstance) EndSession(client *clients.Client) error {
	pjs.clients.RemoveClient(client)
	pjs.storage.Drop(client.GetId())
	pjs.logger.Info("Client session ended", zap.String("id", client.GetId()))
	return nil
}

func (pjs *PeerJSServerInstance) DoReceive(client *clients.Client, preHandle func(id string, msg *protocol.Message) error) error {
	msg, err := client.ReadMessage()
	t := time.Now()
	if err != nil {
		return err
	}
	id := client.GetId()
	if preHandle != nil {
		err = preHandle(id, msg)
		if err != nil {
			return err
		}
	}
	if pjs.ClientIdValidator != nil && msg.Dst != "" {
		valid, err := pjs.ClientIdValidator.ValidateClientId(msg.Dst)
		if err != nil {
			return err
		}
		if !valid {
			return &InvalidDestinationError{Dst: msg.Dst}
		}
	}
	ty := string(msg.Type)
	handler, ok := pjs.handlers[ty]
	if !ok {
		return &protocol.UnknownMessageError{Type: ty}
	}
	return handler(msg, t, pjs, client)
	// TODO: should "LEAVE" message give a signal to close the connection?
}

func (pjs *PeerJSServerInstance) DoExpireCheck(t time.Time) error {
	expired, err := pjs.storage.Check(t)
	if err != nil {
		return err
	}
	pjs.logger.Debug("Expire check", zap.Int("source.count", len(expired)))
	if len(expired) == 0 {
		return nil
	}
	transfered := make(map[string]utils.Iterable[*protocol.Message])
	for k, v := range expired {
		transfered[k] = &utils.IteratorTransform[msgstorage.IMessage, *protocol.Message]{
			Inner:     v,
			Transform: transformToExpire,
		}
	}
	results, err := pjs.clients.SendToMultiClientBatch(transfered)
	if err != nil {
		return err
	}
	_ = results
	return nil
}

func (pjs *PeerJSServerInstance) SendOpen(client *clients.Client) error {
	msg := protocol.BuildOpen(client.GetId())
	return client.SendMessageManually(msg)
}

func (pjs *PeerJSServerInstance) SendCachedMessages(client *clients.Client) error {
	id := client.GetId()
	messages, err := pjs.storage.Take(id)
	if err != nil {
		return err
	}
	transformed := &utils.IteratorTransform[msgstorage.IMessage, *protocol.Message]{
		Inner:     messages,
		Transform: transformToMessage,
	}
	ok, err := pjs.clients.SendToClientBatch(id, transformed)
	if err != nil {
		return err
	}
	if !ok {
		pjs.logger.Warn("Failed to send messages to client", zap.String("id", id))
		return nil
	}
	return nil
}

func (pjs *PeerJSServerInstance) Session(id string, token string, conn *websocket.Conn, preHandle func(id string, msg *protocol.Message) error) (inboundLoop func() error, outboundLoop func() error, err error) {
	client, err := pjs.StartSession(id, token, conn)
	if err != nil {
		return nil, nil, err
	}
	if client == nil {
		return nil, nil, nil
	}
	beforeClose := func(id string, conn *websocket.Conn) error {
		message := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")
		err0 := conn.WriteControl(websocket.CloseMessage, message, time.Now().Add(clients.WriteWait))
		if err0 != nil {
			pjs.logger.Error("Failed to send close message", zap.String("id", id), zap.Error(err0))
		}
		pjs.logger.Debug("Client message loop closing", zap.String("id", id))
		return nil
	}
	inboundLoop = func() error {
		defer pjs.EndSession(client)

		err0 := pjs.SendOpen(client)
		if err0 != nil {
			pjs.logger.Error("Failed to send OPEN message", zap.String("id", id), zap.Error(err0))
			return nil
		}

		err0 = pjs.SendCachedMessages(client)
		if err0 != nil {
			pjs.logger.Error("Failed to send cached messages", zap.String("id", id), zap.Error(err0))
		}

		for {
			err0 = pjs.DoReceive(client, preHandle)
			if err0 != nil {
				if unknownMsgErr, ok := err0.(*protocol.UnknownMessageError); ok {
					pjs.logger.Warn("Unknown message type", zap.String("id", id), zap.String("type", unknownMsgErr.Type))
					continue
				}
				if invalidDstErr, ok := err0.(*InvalidDestinationError); ok {
					pjs.logger.Warn("Invalid destination", zap.String("id", id), zap.String("dst", invalidDstErr.Dst))
					continue
				}
				if wsClose, ok := err0.(*websocket.CloseError); ok {
					pjs.logger.Debug("Client connection closed by websocket", zap.String("id", id), zap.Int("code", wsClose.Code), zap.String("reason", wsClose.Text))
					break
				}
				if netErr, ok := err0.(net.Error); ok {
					pjs.logger.Debug("Client connection closed", zap.String("id", id), zap.Error(netErr))
					break
				}
				pjs.logger.Error("Failed to handle message", zap.String("id", id), zap.Error(err0))
				break
			}
		}

		return nil
	}
	outboundLoop = func() error {
		err0 := client.StartMessageLoop(beforeClose)
		if err0 != nil {
			pjs.logger.Error("Client message loop error", zap.String("id", id), zap.Error(err0))
		}
		return nil
	}
	return inboundLoop, outboundLoop, nil
}

func (pjs *PeerJSServerInstance) ExpireCheck() (loop func() error, cancel func(), err error) {
	ticker := time.NewTicker(pjs.MsgExpireTimeout)
	loop = func() error {
		pjs.logger.Info("Expire check started")
		for t := range ticker.C {
			err0 := pjs.DoExpireCheck(t)
			if err0 != nil {
				pjs.logger.Error("Expire check error", zap.Error(err0))
			}
		}
		return nil
	}
	cancel = func() {
		ticker.Stop()
	}
	return loop, cancel, nil
}

func (pjs *PeerJSServerInstance) Clear() error {
	pjs.clients.Clear()
	pjs.storage.Clear()
	return nil
}

func (pjs *PeerJSServerInstance) ListPeers() []string {
	return pjs.clients.ListClients()
}

var (
	_ msgstorage.IMessage = (*sctMsgTuple)(nil)
	_ MessageHandler      = DoTransmit
	_ MessageHandler      = HandleHeartbeat

	_ error = (*InvalidDestinationError)(nil)
)
