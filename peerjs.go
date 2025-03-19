package caddy_peerjs_server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	signaling "github.com/DWCarrot/caddy-peerjs-server/pkg"
	"github.com/DWCarrot/caddy-peerjs-server/pkg/idprovider"
	"github.com/DWCarrot/caddy-peerjs-server/pkg/msgstorage"
	"github.com/DWCarrot/caddy-peerjs-server/pkg/protocol"
	"github.com/DWCarrot/caddy-peerjs-server/pkg/utils"
	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

const WS_PATH string = "peerjs"
const ENABLE_TRACE bool = false

func init() {
	caddy.RegisterModule(PeerJSServer{})
}

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

	instance     *signaling.PeerJSServerInstance
	expireTicker *time.Ticker
	clientIdPvd  idprovider.IClientIdProvider
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
		pjs.ExpireTimeout = 5000 * time.Millisecond
	}
	if pjs.AliveTimeout == 0 {
		pjs.AliveTimeout = 60000 * time.Millisecond
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
	msgHandlers := make(map[string]signaling.MessageHandler)
	msgHandlers[string(protocol.LEAVE)] = signaling.DoTransmit
	msgHandlers[string(protocol.CANDIDATE)] = signaling.DoTransmit
	msgHandlers[string(protocol.OFFER)] = signaling.DoTransmit
	msgHandlers[string(protocol.ANSWER)] = signaling.DoTransmit
	msgHandlers[string(protocol.EXPIRE)] = signaling.DoTransmit
	if pjs.TransmissionExtend != nil {
		for _, t := range pjs.TransmissionExtend {
			msgHandlers[t] = signaling.DoTransmit
		}
	}
	msgHandlers[string(protocol.HEARTBEAT)] = signaling.HandleHeartbeat
	// initialize storage
	var vd idprovider.IClientIdValidator
	vd, ok := pjs.clientIdPvd.(idprovider.IClientIdValidator)
	if !ok {
		vd = nil
	}
	pjs.instance = signaling.NewInstance(
		pjs.ExpireTimeout,
		pjs.AliveTimeout,
		vd,
		msgHandlers,
		pjs.ConcurrentLimit,
		pjs.QueueLimit,
		&ZapLoggerWrapper{pjs},
	)
	// initialize ticker
	pjs.expireTicker = time.NewTicker(pjs.ExpireTimeout)
	go pjs.loopExpireCheck()

	pjs.logger.Info("PeerJS server started up")
	return nil
}

// Cleanup implements caddy.CleanerUpper.
func (pjs *PeerJSServer) Cleanup() error {
	pjs.expireTicker.Stop()
	pjs.instance.Clear()
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

func (pjs *PeerJSServer) loopExpireCheck() {
	pjs.logger.Info("Expire check started")
	for t := range pjs.expireTicker.C {
		err0 := pjs.instance.DoExpireCheck(t)
		if err0 != nil {
			pjs.logger.Error("Expire check error", zap.Error(err0))
		}
	}
	pjs.logger.Info("Expire check stopped")
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
	peers := pjs.instance.ListPeers()
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

	upgrader := websocket.Upgrader{}
	conn, err := upgrader.Upgrade(w, req, nil)
	if err != nil {
		return err
	}

	client, err := pjs.instance.StartSession(id, token, conn)
	if err != nil {
		return err
	}
	if client == nil {
		return nil
	}

	defer pjs.instance.EndSession(client)

	go pjs.instance.LoopSessionOutbound(client)

	err = pjs.instance.SendOpen(client)
	if err != nil {
		pjs.logger.Error("Failed to send OPEN message", zap.String("id", id), zap.Error(err))
		return nil
	}

	err = pjs.instance.SendCachedMessages(client)
	if err != nil {
		pjs.logger.Error("Failed to send cached messages", zap.String("id", id), zap.Error(err))
		return nil
	}

	return pjs.instance.LoopSessionInbound(client)
}

type ZapLoggerWrapper struct {
	pjs *PeerJSServer
}

// Debug implements peerjs_server.IFunctionalLogger.
func (z *ZapLoggerWrapper) Debug(msg string, id string) {
	z.pjs.logger.Debug(msg, zap.String("id", id))
}

// DebugWithError implements peerjs_server.IFunctionalLogger.
func (z *ZapLoggerWrapper) DebugWithError(msg string, id string, err error) {
	z.pjs.logger.Debug(msg, zap.String("id", id), zap.Error(err))
}

// Error implements peerjs_server.IFunctionalLogger.
func (z *ZapLoggerWrapper) Error(msg string, id string, err error) {
	z.pjs.logger.Error(msg, zap.String("id", id), zap.Error(err))
}

// Info implements peerjs_server.IFunctionalLogger.
func (z *ZapLoggerWrapper) Info(msg string, id string) {
	z.pjs.logger.Info(msg, zap.String("id", id))
}

// TraceExpireCheck implements peerjs_server.IFunctionalLogger.
func (z *ZapLoggerWrapper) TraceExpireCheck(msgs map[string]utils.Iterable[msgstorage.IMessage]) {
	if ENABLE_TRACE {
		z.pjs.logger.Debug("Expire check", zap.Int("count", len(msgs)))
	}
}

// TraceMessage implements peerjs_server.IFunctionalLogger.
func (z *ZapLoggerWrapper) TraceMessage(id string, msg *protocol.Message) {
	z.pjs.traceMessage(msg, id)
}

// Warn implements peerjs_server.IFunctionalLogger.
func (z *ZapLoggerWrapper) Warn(msg string, id string, err error) {
	z.pjs.logger.Warn(msg, zap.String("id", id), zap.Error(err))
}

// Interface guards
var (
	_ caddy.Provisioner           = (*PeerJSServer)(nil)
	_ caddy.CleanerUpper          = (*PeerJSServer)(nil)
	_ caddy.Validator             = (*PeerJSServer)(nil)
	_ caddyhttp.MiddlewareHandler = (*PeerJSServer)(nil)

	_ signaling.IFunctionalLogger = (*ZapLoggerWrapper)(nil)
)
