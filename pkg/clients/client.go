package clients

import (
	"time"

	"github.com/DWCarrot/caddy-peerjs-server/pkg/protocol"
	"github.com/DWCarrot/caddy-peerjs-server/pkg/utils"
	"github.com/gorilla/websocket"
)

type OneOrMoreMsg struct {
	One  *protocol.Message
	More utils.Iterable[*protocol.Message]
}

const WriteWait = time.Second

// Client represents a connected client.
type Client struct {
	id       string            // Unique client ID
	token    string            // Token provided by client
	conn     *websocket.Conn   // WebSocket connection for the client
	closeSig chan struct{}     // Channel for signal of close
	msgChan  chan OneOrMoreMsg // Channel for message
}

// NewClient creates a new client with the given ID, token, and WebSocket connection.
func NewClient(id string, token string, conn *websocket.Conn) *Client {
	// handleClose := conn.CloseHandler()
	// handlePing := conn.PingHandler()
	client := &Client{
		id:       id,
		token:    token,
		conn:     conn,
		closeSig: nil,
		msgChan:  nil,
	}
	// handleCloseNew := func(code int, text string) error {
	// 	err := handleClose(code, text)
	// 	if err != nil {
	// 		return err
	// 	}
	// 	return &websocket.CloseError{Code: code, Text: text}
	// }
	// conn.SetCloseHandler(handleCloseNew)
	// handlePingNew := func(appData string) error {
	// 	client.lastActive = time.Now()
	// 	err := handlePing(appData)
	// 	if err != nil {
	// 		return err
	// 	}
	// 	return client.conn.SetReadDeadline(client.lastActive.Add(client.AliveTimeout))
	// }
	// conn.SetPingHandler(handlePingNew)
	return client
}

func (c *Client) GetId() string {
	return c.id
}

func (c *Client) GetToken() string {
	return c.token
}

type BeforeCloseCallback func(id string, conn *websocket.Conn) error

func (c *Client) StartMessageLoop(onClose BeforeCloseCallback) error {
	for {
		select {
		case <-c.closeSig:
			var err error
			if onClose != nil {
				err = onClose(c.id, c.conn)
				if err != nil {
					// TODO
				}
			}
			err = c.conn.Close()
			return err
		case msgs := <-c.msgChan:
			if msgs.One != nil {
				err := c.conn.WriteJSON(*msgs.One)
				if err != nil {
					if websocket.IsUnexpectedCloseError(err) {
						return err
					} else {
						// TODO
					}
				}
			} else {
				for m := range msgs.More.Iterator() {
					err := c.conn.WriteJSON(m)
					if err != nil {
						if websocket.IsUnexpectedCloseError(err) {
							return err
						} else {
							// TODO
						}
					}
				}
			}
		}
	}
}

func (c *Client) ReadMessage() (*protocol.Message, error) {
	var msg protocol.Message
	err := c.conn.ReadJSON(&msg)
	if err != nil {
		return nil, err
	}
	msg.Src = c.id
	return &msg, nil
}

func (c *Client) UpdateTimeout(deadline time.Time) error {
	return c.conn.SetReadDeadline(deadline)
}

func (c *Client) SendMessageManually(msg *protocol.Message) error {
	return c.conn.WriteJSON(msg)
}

func (c *Client) CloseManually() error {
	return c.conn.Close()
}
