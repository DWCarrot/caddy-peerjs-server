# caddy-peerjs-server
A simple (maybe uncompleted) implement of peerjs-server in caddy extensions


> [!NOTE]
> This is not an official repository of the [Caddy Web Server](https://github.com/caddyserver) organization.



## Syntax

```
peerjs_server [<matcher>] {
	[path <string>]
	[key <string>]
	[expire_timeout <duration>]
	[alive_timeout <duration>]
	[concurrent_limit <uint>]
	[queue_limit <uint>]
	[allow_discovery [<bool>]]
	[transmission_extend <string>...]
}
```

These params are defined like this, mostly the same as [peerjs/peerjs-server](https://github.com/peers/peerjs-server?tab=readme-ov-file#config--cli-options)

Scalar options accept exactly one argument, except `allow_discovery`, which can
also appear without an argument. `transmission_extend` requires at least one
message type. Unknown options, excess arguments, and nested option blocks are
rejected. Custom `id_manager` / JSON `client_id_manager` configuration is not
implemented and is rejected rather than silently ignored (JSON `null` is allowed).

Timeouts must be positive after defaults are applied; zero selects the default.
Paths must start with `/`. Each configuration instance owns its expiration worker;
cleanup cancels and waits for that worker before clearing its signaling state.

| Variable                                     | Type          | Default  | Description                                                  |
| -------------------------------------------- | ------------- | -------- | ------------------------------------------------------------ |
| Path<br/>`path`                              | string        | "/"      | The server responds for requests to the root URL + path. Example: Set to /myapp for [http://127.0.0.1:9000/myapp](vscode-file://vscode-app/d:/Storage/Softwares/Microsoft VS Code/resources/app/out/vs/code/electron-sandbox/workbench/workbench.html) |
| Key<br/>`key`                                | string        | "peerjs" | Connection key that clients must provide to call API methods |
| ExpireTimeout<br/>`expire_timeout`           | time.Duration | 5000ms   | Time after which a sent message will expire, triggering an EXPIRE message to sender |
| AliveTimeout<br/>`alive_timeout`             | time.Duration | 60000ms  | Timeout for broken connections. Server destroys client connection if no data received |
| ConcurrentLimit<br/>`concurrent_limit`       | uint          | 64       | Maximum number of concurrent client connections to WebSocket server |
| QueueLimit<br/>`queue_limit`                 | uint          | 16       | [Additional] Maximum number of messages in the queue for each client |
| AllowDiscovery<br/>`allow_discovery`         | bool          | false    | Allow GET /peers HTTP API method to get array of all connected client IDs; `allow_discovery` exist or with value `true` means true; otherwise false |
| TransmissionExtend<br/>`transmission_extend` | []string      | nil      | [Additional] MessageTypes allowed to be transmitted beyond standard ones |
|                                              |               |          |                                                              |



## Limitations

### Only support http/1.1

This extension relies on [github.com/gorilla/websocket](https://pkg.go.dev/github.com/gorilla/websocket) , which have no support for http/2 currently.
