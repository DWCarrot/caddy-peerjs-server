package utils

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/gorilla/websocket"
)

func TestHijackResponseWriterUpgrade(t *testing.T) {
	for _, tc := range []struct {
		name   string
		layers int
	}{
		{name: "direct"},
		{name: "caddy wrapper", layers: 1},
		{name: "nested caddy wrappers", layers: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := make(chan error, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var handlerErr error
				defer func() { result <- handlerErr }()
				for range tc.layers {
					w = &caddyhttp.ResponseWriterWrapper{ResponseWriter: w}
				}
				upgrader := websocket.Upgrader{}
				conn, err := upgrader.Upgrade(HijackResponseWriter{ResponseWriter: w}, r, nil)
				if err != nil {
					handlerErr = err
					return
				}
				defer conn.Close()
				if handlerErr = conn.SetReadDeadline(time.Now().Add(5 * time.Second)); handlerErr != nil {
					return
				}
				messageType, message, err := conn.ReadMessage()
				if err != nil {
					handlerErr = err
					return
				}
				if handlerErr = conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); handlerErr != nil {
					return
				}
				handlerErr = conn.WriteMessage(messageType, message)
			}))
			defer server.Close()

			dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
			conn, response, err := dialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
			if err != nil {
				if response != nil {
					response.Body.Close()
				}
				t.Fatalf("upgrade failed: %v", err)
			}
			defer conn.Close()
			if err := conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
				t.Fatal(err)
			}
			if err := conn.WriteMessage(websocket.TextMessage, []byte("hello")); err != nil {
				t.Fatal(err)
			}
			if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
				t.Fatal(err)
			}
			messageType, message, err := conn.ReadMessage()
			if err != nil {
				t.Fatal(err)
			}
			if messageType != websocket.TextMessage || string(message) != "hello" {
				t.Fatalf("unexpected echo: type=%d message=%q", messageType, message)
			}
			if err := <-result; err != nil {
				t.Fatalf("server failed: %v", err)
			}
		})
	}
}

func TestHijackResponseWriterUnsupported(t *testing.T) {
	w := HijackResponseWriter{ResponseWriter: &caddyhttp.ResponseWriterWrapper{
		ResponseWriter: httptest.NewRecorder(),
	}}
	_, _, err := w.Hijack()
	if !errors.Is(err, http.ErrNotSupported) {
		t.Fatalf("expected unsupported hijacking, got %v", err)
	}
}
