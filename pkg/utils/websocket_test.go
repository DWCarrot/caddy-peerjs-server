package utils

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/coder/websocket"
)

func TestWebSocketUpgrade(t *testing.T) {
	for _, layers := range []int{0, 1, 2} {
		t.Run(string(rune('0'+layers)), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			result := make(chan error, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for range layers {
					w = &caddyhttp.ResponseWriterWrapper{ResponseWriter: w}
				}
				conn, err := websocket.Accept(w, r, nil)
				if err != nil {
					result <- err
					return
				}
				defer conn.CloseNow()
				ty, data, err := conn.Read(ctx)
				if err == nil {
					err = conn.Write(ctx, ty, data)
				}
				result <- err
			}))
			defer server.Close()
			conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.CloseNow()
			if err := conn.Write(ctx, websocket.MessageText, []byte("hello")); err != nil {
				t.Fatal(err)
			}
			ty, data, err := conn.Read(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if ty != websocket.MessageText || string(data) != "hello" {
				t.Fatalf("unexpected echo: %v %q", ty, data)
			}
			if err := <-result; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestWebSocketUpgradeUnsupported(t *testing.T) {
	w := &caddyhttp.ResponseWriterWrapper{ResponseWriter: httptest.NewRecorder()}
	r := httptest.NewRequest(http.MethodGet, "http://localhost/", nil)
	r.Header.Set("Connection", "Upgrade")
	r.Header.Set("Upgrade", "websocket")
	r.Header.Set("Sec-WebSocket-Version", "13")
	r.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	if _, err := websocket.Accept(w, r, nil); err == nil {
		t.Fatal("expected unsupported hijacking")
	}
}
