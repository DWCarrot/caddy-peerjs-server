package caddy_peerjs_server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func TestCaddyfileConfiguration(t *testing.T) {
	var p PeerJSServer
	err := p.UnmarshalCaddyfile(caddyfile.NewTestDispenser(`peerjs_server {
 path /signal
 key secret
 expire_timeout 2s
 alive_timeout 30s
 concurrent_limit 10
 queue_limit 4
 allow_discovery false
 transmission_extend CUSTOM OTHER
 }`))
	if err != nil {
		t.Fatal(err)
	}
	if p.Path != "/signal" || p.Key != "secret" || p.ExpireTimeout != 2*time.Second || p.AliveTimeout != 30*time.Second || p.ConcurrentLimit != 10 || p.QueueLimit != 4 || p.AllowDiscovery || len(p.TransmissionExtend) != 2 {
		t.Fatalf("unexpected config: %+v", &p)
	}
	for _, line := range []string{"path", "key", "expire_timeout", "alive_timeout", "concurrent_limit", "queue_limit", "transmission_extend", "unknown value", "id_manager custom", "expire_timeout nope", "allow_discovery nope", "queue_limit -1", "path /x {\n key nested\n }"} {
		t.Run(line, func(t *testing.T) {
			var p PeerJSServer
			if err := p.UnmarshalCaddyfile(caddyfile.NewTestDispenser("peerjs_server {\n" + line + "\n}")); err == nil {
				t.Fatal("accepted invalid configuration")
			}
		})
	}
	for _, line := range []string{"path /x", "key x", "expire_timeout 1s", "alive_timeout 1s", "concurrent_limit 2", "queue_limit 2", "allow_discovery true"} {
		t.Run(line+" extra", func(t *testing.T) {
			var p PeerJSServer
			if err := p.UnmarshalCaddyfile(caddyfile.NewTestDispenser("peerjs_server {\n" + line + " extra\n}")); err == nil {
				t.Fatal("accepted excess argument")
			}
		})
	}
	var bare PeerJSServer
	if err := bare.UnmarshalCaddyfile(caddyfile.NewTestDispenser("peerjs_server {\n allow_discovery\n}")); err != nil || !bare.AllowDiscovery {
		t.Fatalf("bare discovery: %v", err)
	}
}

func loadPeer(t *testing.T, raw string) (*PeerJSServer, context.CancelFunc) {
	t.Helper()
	ctx, cancel := caddy.NewContext(caddy.Context{Context: context.Background()})
	t.Cleanup(cancel)
	mod, err := ctx.LoadModuleByID("http.handlers.peerjs_server", json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	return mod.(*PeerJSServer), cancel
}

func TestRegistrationAndReloadLifecycle(t *testing.T) {
	old, stopOld := loadPeer(t, `{}`)
	next, stopNext := loadPeer(t, `{"path":"/new","expire_timeout":1000000}`)
	if old.Path != "/" || old.Key != "peerjs" || old.ExpireTimeout != 5*time.Second || old.AliveTimeout != time.Minute || old.ConcurrentLimit != 64 || old.QueueLimit != 16 {
		t.Fatal("incorrect defaults")
	}
	if next.Path != "/new/" || old.instance == next.instance {
		t.Fatal("reload instances must be independent")
	}
	stopped := make(chan struct{})
	go func() { stopOld(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("cleanup blocked")
	}
	select {
	case <-old.expireDone:
	default:
		t.Fatal("old worker still running")
	}
	select {
	case <-next.expireDone:
		t.Fatal("new worker stopped with old configuration")
	default:
	}
	stopNext()
	select {
	case <-next.expireDone:
	default:
		t.Fatal("new worker still running")
	}
	if err := next.Cleanup(); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidJSONProvisioning(t *testing.T) {
	for _, raw := range []string{`{"expire_timeout":-1}`, `{"alive_timeout":-1}`, `{"path":"relative"}`, `{"client_id_manager":{"id_manager":"custom"}}`} {
		t.Run(raw, func(t *testing.T) {
			ctx, cancel := caddy.NewContext(caddy.Context{Context: context.Background()})
			defer cancel()
			if _, err := ctx.LoadModuleByID("http.handlers.peerjs_server", json.RawMessage(raw)); err == nil {
				t.Fatal("accepted invalid config")
			}
		})
	}
	var unprovisioned PeerJSServer
	if err := unprovisioned.Cleanup(); err != nil {
		t.Fatal(err)
	}
}

func TestCaddyReload(t *testing.T) {
	t.Cleanup(func() {
		if err := caddy.Stop(); err != nil {
			t.Error(err)
		}
	})
	adapt := func(path string) []byte {
		t.Helper()
		raw, _, err := (caddyfile.Adapter{ServerType: httpcaddyfile.ServerType{}}).Adapt([]byte("{\n admin off\n auto_https off\n}\nhttp://127.0.0.1:0 {\n route {\n peerjs_server {\n path "+path+"\n }\n }\n}"), nil)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), `"handler":"peerjs_server"`) {
			t.Fatal("directive not registered")
		}
		return raw
	}
	for _, path := range []string{"/old", "/new"} {
		if err := caddy.Load(adapt(path), true); err != nil {
			t.Fatal(err)
		}
	}
	if err := caddy.Load(adapt("invalid"), true); err == nil {
		t.Fatal("invalid reload succeeded")
	}
	if err := caddy.Load(adapt("/recovered"), true); err != nil {
		t.Fatal(err)
	}
}

func TestCleanupClosesWebSocket(t *testing.T) {
	p, stop := loadPeer(t, `{}`)
	handlerDone := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(handlerDone)
		if err := p.ServeHTTP(w, r, nil); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/peerjs?key=peerjs&id=test&token=test", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	var msg struct {
		Type string `json:"type"`
	}
	if err := wsjson.Read(ctx, conn, &msg); err != nil || msg.Type != "OPEN" {
		t.Fatalf("opening connection: %s, %v", msg.Type, err)
	}
	stop()
	if _, _, err := conn.Read(ctx); err == nil {
		t.Fatal("connection remained open")
	}
	select {
	case <-handlerDone:
	case <-time.After(3 * time.Second):
		t.Fatal("WebSocket handler did not exit after cleanup")
	}
}

func TestPeerJSSignalingRelay(t *testing.T) {
	p, stop := loadPeer(t, "{}")
	defer stop()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := p.ServeHTTP(&caddyhttp.ResponseWriterWrapper{ResponseWriter: w}, r, nil); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	dial := func(id string) *websocket.Conn {
		t.Helper()
		conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/peerjs?key=peerjs&id="+id+"&token=test", nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.CloseNow() })
		var msg map[string]any
		if err := wsjson.Read(ctx, conn, &msg); err != nil {
			t.Fatal(err)
		}
		if msg["type"] != "OPEN" {
			t.Fatalf("expected OPEN: %v", msg)
		}
		return conn
	}
	sender := dial("sender")
	receiver := dial("receiver")
	// Exercise a signaling payload larger than Coder's default read limit.
	payload := strings.Repeat("x", 64*1024)
	if err := wsjson.Write(ctx, sender, map[string]any{"type": "OFFER", "dst": "receiver", "src": "spoofed", "payload": payload}); err != nil {
		t.Fatal(err)
	}
	receiver.SetReadLimit(-1)
	var msg map[string]any
	if err := wsjson.Read(ctx, receiver, &msg); err != nil {
		t.Fatal(err)
	}
	if msg["type"] != "OFFER" || msg["src"] != "sender" || msg["payload"] != payload {
		t.Fatal("signaling message was not relayed intact")
	}
}
