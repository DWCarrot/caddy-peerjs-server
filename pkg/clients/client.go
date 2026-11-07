package clients

import (
	"context"
	"sync"
	"time"

	"github.com/DWCarrot/caddy-peerjs-server/pkg/protocol"
	"github.com/DWCarrot/caddy-peerjs-server/pkg/utils"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

type OneOrMoreMsg struct {
	One  *protocol.Message
	More utils.Iterable[*protocol.Message]
}

const WriteWait = time.Second

type Client struct {
	id           string
	token        string
	conn         *websocket.Conn
	closeSig     chan struct{}
	msgChan      chan OneOrMoreMsg
	deadlineMu   sync.Mutex
	readDeadline time.Time
}

func NewClient(id string, token string, conn *websocket.Conn) *Client {
	if conn != nil {
		conn.SetReadLimit(-1)
	}
	return &Client{id: id, token: token, conn: conn}
}

func (c *Client) GetId() string    { return c.id }
func (c *Client) GetToken() string { return c.token }

type BeforeCloseCallback func(id string, conn *websocket.Conn) error

func (c *Client) StartMessageLoop(onClose BeforeCloseCallback) error {
	defer c.conn.CloseNow()
	for {
		select {
		case <-c.closeSig:
			if onClose != nil {
				return onClose(c.id, c.conn)
			}
			return nil
		case msgs, ok := <-c.msgChan:
			if !ok {
				return nil
			}
			if msgs.One != nil {
				if err := c.SendMessageManually(msgs.One); err != nil {
					return err
				}
			} else {
				for m := range msgs.More.Iterator() {
					if err := c.SendMessageManually(m); err != nil {
						return err
					}
				}
			}
		}
	}
}

func (c *Client) ReadMessage() (*protocol.Message, error) {
	c.deadlineMu.Lock()
	deadline := c.readDeadline
	c.deadlineMu.Unlock()
	ctx := context.Background()
	if !deadline.IsZero() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, deadline)
		defer cancel()
	}
	var msg protocol.Message
	if err := wsjson.Read(ctx, c.conn, &msg); err != nil {
		return nil, err
	}
	msg.Src = c.id
	return &msg, nil
}

// UpdateTimeout sets the deadline for the next message. Signaling handlers call
// it after receiving a heartbeat or forwarded message, before the next read.
func (c *Client) UpdateTimeout(deadline time.Time) error {
	c.deadlineMu.Lock()
	c.readDeadline = deadline
	c.deadlineMu.Unlock()
	return nil
}

func (c *Client) SendMessageManually(msg *protocol.Message) error {
	ctx, cancel := context.WithTimeout(context.Background(), WriteWait)
	defer cancel()
	return wsjson.Write(ctx, c.conn, msg)
}

func (c *Client) CloseManually() error { return c.conn.CloseNow() }
