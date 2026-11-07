package clients

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DWCarrot/caddy-peerjs-server/pkg/protocol"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func TestClientMessageDeadlineAndSource(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			result <- err
			return
		}
		defer conn.CloseNow()
		client := NewClient("actual", "token", conn)
		client.UpdateTimeout(time.Now().Add(time.Second))
		msg, err := client.ReadMessage()
		if err != nil {
			result <- err
			return
		}
		if msg.Src != "actual" {
			t.Errorf("source spoofing: %s", msg.Src)
		}
		if err := client.SendMessageManually(msg); err != nil {
			result <- err
			return
		}
		client.UpdateTimeout(time.Now().Add(50 * time.Millisecond))
		_, err = client.ReadMessage()
		result <- err
	}))
	defer server.Close()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	msg := &protocol.Message{Type: protocol.HEARTBEAT, Src: "spoofed"}
	if err := wsjson.Write(ctx, conn, msg); err != nil {
		t.Fatal(err)
	}
	if err := wsjson.Read(ctx, conn, msg); err != nil {
		t.Fatal(err)
	}
	if msg.Src != "actual" {
		t.Fatalf("source = %s", msg.Src)
	}
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("idle client did not expire")
		}
	case <-ctx.Done():
		t.Fatal("client read remained blocked")
	}
}
