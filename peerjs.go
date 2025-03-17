package caddy_peerjs_server

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/DWCarrot/caddy-peerjs-server/pkg/clients"
	"github.com/DWCarrot/caddy-peerjs-server/pkg/idprovider"
	messagestorage "github.com/DWCarrot/caddy-peerjs-server/pkg/msgstorage"
	"github.com/DWCarrot/caddy-peerjs-server/pkg/protocol"
	"github.com/DWCarrot/caddy-peerjs-server/pkg/utils"
	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
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

func transformToMessage(msg messagestorage.IMessage) *protocol.Message {
	return msg.(*sctMsgTuple).Inner
}

func transformToExpire(msg messagestorage.IMessage) *protocol.Message {
	return &protocol.Message{
		Type: protocol.EXPIRE,
		Src:  msg.GetTarget(),
		Dst:  msg.GetSource(),
	}
}

const WS_PATH string = "peerjs"
const ENABLE_TRACE bool = true

func init() {
	caddy.RegisterModule(PeerJSServer{})
}

type MessageHandler func(msg *protocol.Message, self *PeerJSServer, shouldExit *bool) error

type PeerJSServer struct {

	// Path (string). The server responds for requests to the root URL + path.
	// E.g. Set the path to /myapp and run server on 9000 port via peerjs --port 9000 --path /myapp Then open http://127.0.0.1:9000/myapp - you should see a JSON reponse.
	// Default: "/"
	Path string `json:"path,omitempty"`

	// Connection key (string). Client must provide it to call API methods.
	// Default: "peerjs"
	Key string `json:"key,omitempty"`

	// The amount of time after which a message sent will expire, the sender will then receive a EXPIRE message (milliseconds).
	// Default: 5000
	ExpireTimeout time.Duration `json:"expire_timeout,omitempty"`

	// Timeout for broken connection (milliseconds).
	// If the server doesn't receive any data from client (includes pong messages), the client's connection will be destroyed.
	// Default: 60000
	AliveTimeout time.Duration `json:"alive_timeout,omitempty"`

	// Maximum number of clients' connections to WebSocket server
	// Default: 64
	ConcurrentLimit uint `json:"concurrent_limit,omitempty"`

	// [Additional] Maximum number of messages in the queue for each client
	// Default: 16
	QueueLimit uint `json:"queue_limit,omitempty"`

	// Allow to use GET /peers http API method to get an array of ids of all connected clients
	AllowDiscovery bool `json:"allow_discovery,omitempty"`

	// [Additional] Other MesseageType (`type`) in protocol.Message that allowed to be transmitted
	TransmissionExtend []string `json:"transmission_extend,omitempty"`

	// [Additional] Allow to use GET /id http API method to get a new id
	ClientIdManagerRaw json.RawMessage `json:"client_id_manager,omitempty" caddy:"namespace=http.handlers.peerjs_server inline_key=id_manager"`

	expireTicker *time.Ticker
	clientIdPvd  idprovider.IClientIdProvider
	msgHandlers  map[string]MessageHandler
	clients      *clients.ClientManager
	storage      *messagestorage.MessageStorage
	logger       *zap.Logger
}

func (PeerJSServer) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID: "http.handlers.peerjs_server",
		New: func() caddy.Module {
			return new(PeerJSServer)
		},
	}
}

func (pjs *PeerJSServer) FillDefault() error {
	if pjs.Path == "" {
		pjs.Path = "/"
	} else if !strings.HasSuffix(pjs.Path, "/") { // Ensure path ends with "/"
		pjs.Path += "/"
	}
	if pjs.Key == "" {
		pjs.Key = "peerjs"
	}
	if pjs.ExpireTimeout == 0 {
		pjs.ExpireTimeout = 5000
	}
	if pjs.AliveTimeout == 0 {
		pjs.AliveTimeout = 60000
	}
	if pjs.ConcurrentLimit == 0 {
		pjs.ConcurrentLimit = 64
	}
	if pjs.QueueLimit == 0 {
		pjs.QueueLimit = 16
	}
	return nil
}

// Provision implements caddy.Provisioner.
func (pjs *PeerJSServer) Provision(ctx caddy.Context) error {
	err := pjs.FillDefault()
	if err != nil {
		return err
	}

	pjs.logger = ctx.Logger()
	// initialize client id provider
	pjs.clientIdPvd = &idprovider.DefaultClientIdProvider{}
	// initialize message handlers
	pjs.msgHandlers = make(map[string]MessageHandler)
	pjs.msgHandlers[string(protocol.LEAVE)] = doTransmit
	pjs.msgHandlers[string(protocol.CANDIDATE)] = doTransmit
	pjs.msgHandlers[string(protocol.OFFER)] = doTransmit
	pjs.msgHandlers[string(protocol.ANSWER)] = doTransmit
	pjs.msgHandlers[string(protocol.EXPIRE)] = doTransmit
	if pjs.TransmissionExtend != nil {
		for _, t := range pjs.TransmissionExtend {
			pjs.msgHandlers[t] = doTransmit
		}
	}
	pjs.msgHandlers[string(protocol.HEARTBEAT)] = doHeartbeatRecord
	// initialize storage
	pjs.storage = messagestorage.NewMessageStorage(pjs.QueueLimit)
	// initialize client manager
	pjs.clients = clients.NewClientManager(pjs.ConcurrentLimit)
	// initialize ticker
	pjs.expireTicker = time.NewTicker(pjs.ExpireTimeout)
	go pjs.loopMessageExpireCheck()

	pjs.logger.Info("PeerJS server started up")
	return nil
}

// Cleanup implements caddy.CleanerUpper.
func (pjs *PeerJSServer) Cleanup() error {
	pjs.clients.Clear()
	pjs.storage.Clear()
	pjs.expireTicker.Stop()
	pjs.logger.Info("PeerJS server cleaned up")
	return nil
}

// Validate implements caddy.Validator.
func (pjs *PeerJSServer) Validate() error {
	return nil
}

// ServeHTTP implements caddyhttp.MiddlewareHandler.
func (pjs *PeerJSServer) ServeHTTP(w http.ResponseWriter, req *http.Request, h caddyhttp.Handler) error {
	path, ok := pjs.checkPath(req.URL.Path)
	if ok {
		pjs.traceRequest(req)
		if path == "" && req.Method == http.MethodGet {
			return pjs.handleRoot(w, req)
		}
		parts := strings.Split(path, "/")
		if len(parts) == 2 && parts[0] == pjs.Key {
			if parts[1] == "id" && pjs.clientIdPvd != nil && req.Method == http.MethodGet {
				return pjs.handleGetId(w, req)
			}
			if parts[1] == "peers" && req.Method == http.MethodGet {
				return pjs.handleGetPeers(w, req)
			}
		}
		if len(parts) == 1 && parts[0] == WS_PATH {
			// if req.ProtoMajor == 2 {
			// 	frame := http2.NewFramer(w, req.Body)
			// 	return frame.WriteGoAway(0, http2.ErrCodeHTTP11Required, []byte("HTTP/1.1 required"))
			// }
			return pjs.handleWS(w, req)
		}
		w.WriteHeader(http.StatusBadRequest)
		return nil
	}
	return h.ServeHTTP(w, req)
}

func (pjs *PeerJSServer) traceRequest(req *http.Request) {
	if ENABLE_TRACE {
		pjs.logger.Debug(
			"[trace] Request",
			zap.String("proto", req.Proto),
			zap.String("method", req.Method),
			zap.String("url", req.URL.String()),
			zap.String("remote", req.RemoteAddr),
			zap.Any("headers", req.Header),
		)
	}
}

func (pjs *PeerJSServer) traceMessage(msg *protocol.Message, from string) {
	if ENABLE_TRACE {
		pjs.logger.Debug(
			"[trace] Message",
			zap.String("@", from),
			zap.String("src", msg.Src),
			zap.String("dst", msg.Dst),
			zap.String("type", string(msg.Type)),
		)
	}
}

func (pjs *PeerJSServer) checkPath(path string) (string, bool) {
	if strings.HasPrefix(path, pjs.Path) {
		i := len(pjs.Path)
		return path[i:], true
	}
	return "", false
}

type sctServerRootResponse struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Website     string `json:"website"`
}

func (pjs *PeerJSServer) handleRoot(w http.ResponseWriter, req *http.Request) error {
	website := url.URL{
		Scheme: req.URL.Scheme,
		Host:   req.URL.Host,
		Path:   pjs.Path,
	}
	resp := &sctServerRootResponse{
		Name:        "Caddy PeerJS Server",
		Description: "A server side element to broker connections between PeerJS clients implement with caddy extension.",
		Website:     website.String(),
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(resp)
}

func (pjs *PeerJSServer) handleGetId(w http.ResponseWriter, req *http.Request) error {
	id, err := pjs.clientIdPvd.GenerateClientId(req.Header)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "plain/text")
	w.WriteHeader(http.StatusOK)
	_, err = w.Write([]byte(id))
	return err
}

func (pjs *PeerJSServer) handleGetPeers(w http.ResponseWriter, req *http.Request) error {
	_ = req
	if !pjs.AllowDiscovery {
		w.WriteHeader(http.StatusForbidden)
		return nil
	}
	peers := pjs.clients.ListClients()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(peers)
}

func (pjs *PeerJSServer) handleWS(w http.ResponseWriter, req *http.Request) error {
	query := req.URL.Query()
	key := query.Get("key")
	if key != pjs.Key {
		w.WriteHeader(http.StatusBadRequest)
		return nil
	}
	id := query.Get("id")
	if id == "" {
		w.WriteHeader(http.StatusBadRequest)
		return nil
	}
	token := query.Get("token")
	if token == "" {
		w.WriteHeader(http.StatusBadRequest)
		return nil
	}

	client, err := pjs.startSession(id, token, w, req)
	if err != nil {
		pjs.logger.Error("Failed to start session", zap.String("id", id), zap.Error(err))
		return nil
	}
	if client == nil {
		return nil
	}
	defer pjs.endSession(client)

	go pjs.loopMessagePumb(client, id)

	ok, err := pjs.clients.SendToClient(id, protocol.BuildOpen(id))
	if err != nil {
		pjs.logger.Error("Failed to send OPEN message", zap.String("id", id), zap.Error(err))
		return nil
	}
	if !ok {
		pjs.logger.Warn("Failed to send OPEN message", zap.String("id", id))
		return nil
	}

	messages, err := pjs.storage.Take(id)
	if err != nil {
		pjs.logger.Error("Failed to take messages from storage", zap.String("id", id), zap.Error(err))
	} else {
		transformed := &utils.IteratorTransform[messagestorage.IMessage, *protocol.Message]{
			Inner:     messages,
			Transform: transformToMessage,
		}
		ok, err := pjs.clients.SendToClientBatch(id, transformed)
		if err != nil {
			pjs.logger.Error("Failed to send messages to client", zap.String("id", id), zap.Error(err))
		}
		if !ok {
			pjs.logger.Warn("Failed to send messages to client", zap.String("id", id))
			return nil
		}
	}

	var message *protocol.Message = nil
	var shouldExit bool = false
	for message, err = client.ReadMessage(pjs.AliveTimeout); err == nil && !shouldExit; message, err = client.ReadMessage(pjs.AliveTimeout) {
		pjs.traceMessage(message, client.GetId())
		ty := string(message.Type)
		handler, exists := pjs.msgHandlers[ty]
		if !exists {
			pjs.logger.Warn("Unknown message type", zap.String("id", client.GetId()), zap.String("type", ty))
			// TODO: Send ERROR message to the sender ?
			continue
		}

		err = handler(message, pjs, &shouldExit)
		if err != nil {
			pjs.logger.Error("Message handler error", zap.String("id", client.GetId()), zap.Error(err))
		}
	}
	if err != nil {
		if closeErr, ok := err.(*websocket.CloseError); ok {
			pjs.logger.Info("Client connection closed", zap.String("id", client.GetId()), zap.Int("code", closeErr.Code), zap.String("reason", closeErr.Text))
		} else if netErr, ok := err.(net.Error); ok {
			pjs.logger.Info("Client connection error", zap.String("id", client.GetId()), zap.Error(netErr))
		} else {
			pjs.logger.Error("Client message read error", zap.String("id", client.GetId()), zap.Error(err))
		}
	}
	return nil
}

func (pjs *PeerJSServer) loopMessagePumb(client *clients.Client, id string) {
	beforeClose := func(id string, conn *websocket.Conn) error {
		message := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")
		err := conn.WriteControl(websocket.CloseMessage, message, time.Now().Add(clients.WriteWait))
		if err != nil {
			pjs.logger.Error("Failed to send close message", zap.String("id", id), zap.Error(err))
		}
		pjs.logger.Debug("Client message loop closing", zap.String("id", id))
		return nil
	}
	err := client.StartMessageLoop(beforeClose)
	if err != nil {
		pjs.logger.Error("Client message loop error", zap.String("id", id), zap.Error(err))
	}
}

func (pjs *PeerJSServer) startSession(id string, token string, w http.ResponseWriter, req *http.Request) (*clients.Client, error) {
	upgrader := websocket.Upgrader{}
	conn, err := upgrader.Upgrade(w, req, nil)
	if err != nil {
		return nil, err
	}
	client := clients.NewClient(id, token, conn, pjs.AliveTimeout)
	err = pjs.clients.AddClient(client)
	if err != nil {
		if idTakenErr, ok := err.(*clients.IdTakenError); ok {
			pjs.logger.Debug("ID taken", zap.String("id", id))
			_ = idTakenErr
			msg := protocol.BuildIdTaken(id, token)
			err = client.SendMessageManually(msg)
			if err != nil {
				pjs.logger.Error("Failed to send ID-TAKEN error", zap.String("id", id), zap.Error(err))
			}
			return nil, client.CloseManually()
		}
		if tooManyClientErr, ok := err.(*clients.TooManyClientsError); ok {
			pjs.logger.Debug("Too many clients", zap.String("id", id))
			msg := protocol.BuildError(fmt.Sprintf("Too many clients, limit: %d", tooManyClientErr.Count))
			err = client.SendMessageManually(msg)
			if err != nil {
				pjs.logger.Error("Failed to send client limit error", zap.String("id", id), zap.Error(err))
			}
			return nil, client.CloseManually()
		}
		pjs.logger.Error("Failed to add client", zap.String("id", id), zap.Error(err))
		return nil, client.CloseManually()
	}
	pjs.logger.Info("Client session started", zap.String("id", id))
	return client, nil
}

func (pjs *PeerJSServer) endSession(client *clients.Client) error {
	pjs.clients.RemoveClient(client)
	pjs.storage.Drop(client.GetId())
	pjs.logger.Info("Client session ended", zap.String("id", client.GetId()))
	return nil
}

func (pjs *PeerJSServer) doMessageExpireCheck(t time.Time) error {
	expired, err := pjs.storage.Check(t)
	if err != nil {
		return err
	}
	if len(expired) == 0 {
		return nil
	}
	transfered := make(map[string]utils.Iterable[*protocol.Message])
	for k, v := range expired {
		transfered[k] = &utils.IteratorTransform[messagestorage.IMessage, *protocol.Message]{
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

func (pjs *PeerJSServer) loopMessageExpireCheck() {
	pjs.logger.Debug("Message expire check loop started")
	for c := range pjs.expireTicker.C {
		err := pjs.doMessageExpireCheck(c)
		if err != nil {
			pjs.logger.Error("Message expire check error", zap.Error(err))
		}
	}
	pjs.logger.Debug("Message expire check loop stopped")
}

func doTransmit(msg *protocol.Message, pjs *PeerJSServer, shouldExit *bool) error {
	if msg.Type == protocol.EXPIRE {
		*shouldExit = true
	}
	ok, err := pjs.clients.SendToClient(msg.Dst, msg)
	if err != nil {
		return err
	}
	if !ok {
		msgTuple := &sctMsgTuple{
			Inner:  msg,
			Expire: time.Now().Add(pjs.ExpireTimeout),
		}
		overflown, e := pjs.storage.Store(msgTuple)
		if e != nil {
			return e
		}
		if overflownMsg, ok := overflown.(*sctMsgTuple); ok {
			// TODO: Send EXPIRE message to the sender ?
			_ = overflownMsg
		}
	}
	return nil
}

func doHeartbeatRecord(msg *protocol.Message, pjs *PeerJSServer, shouldExit *bool) error {
	return nil
}

// Interface guards
var (
	_ caddy.Provisioner           = (*PeerJSServer)(nil)
	_ caddy.CleanerUpper          = (*PeerJSServer)(nil)
	_ caddy.Validator             = (*PeerJSServer)(nil)
	_ caddyhttp.MiddlewareHandler = (*PeerJSServer)(nil)

	_ messagestorage.IMessage = (*sctMsgTuple)(nil)
)
