# Repository Guidelines

## Repository Scope

This is the independent Go repository `github.com/DWCarrot/caddy-peerjs-server`, also used as a Git submodule of homelab-gateway. Run the commands below from this repository's root. Keep changes and commits in this repository; updating a consuming repository's submodule pointer is a separate change. Standalone development does not require the parent gateway or its other plugins.

## Project Structure

- `peerjs.go` registers the Caddy handler and implements HTTP routing, WebSocket upgrades, and module lifecycle behavior.
- `caddyfile.go` parses the `peerjs_server` directive.
- `pkg/instance.go` coordinates signaling sessions, message delivery, and expiration.
- `pkg/clients/`, `pkg/msgstorage/`, `pkg/protocol/`, and `pkg/idprovider/` implement client management, queued messages, protocol types, and client IDs.
- `pkg/utils/` contains iterator and HTTP response-writer helpers.
- `examples/Caddyfile` and `examples/web/index.html` provide a browser demo. `README.md` documents configuration; `PeerJS-Server-Protocol&Workflow.md` describes signaling behavior.

## Build and Development

Use Go 1.23.5 or newer. `go.mod` currently pins Caddy v2.9.1 and Coder WebSocket v1.8.15.

- `go mod download`: download module dependencies.
- `go build ./...`: compile the Go packages.
- `go test ./...`: run all tests.
- `go vet ./...`: run static checks.
- With xcaddy installed, `xcaddy build v2.9.1 --with github.com/DWCarrot/caddy-peerjs-server=.` builds a standalone Caddy binary containing this checkout.
- Set `CADDY_WWW_ROOT` to the absolute path of `examples/web`, then run `./caddy adapt --config ./examples/Caddyfile --adapter caddyfile --pretty --validate`.
- `./caddy run --config ./examples/Caddyfile --adapter caddyfile` starts the example. Review its ports and local TLS settings before use.

On Windows, use `./caddy.exe`. Building the Go packages alone does not produce a Caddy executable. WebSocket upgrades require HTTP/1.1 on the handler connection; the example proxies to a local HTTP/1.1 listener.

## Coding and Testing

Use `gofmt` and standard Go naming, retaining existing package names. Keep protocol types in `pkg/protocol/` and signaling logic separate from Caddy integration. Preserve module IDs, JSON fields, and directive syntax unless intentionally changing the public interface; update configuration and protocol documentation with behavior changes.

Place regression tests beside implementation files as `*_test.go`. Existing tests cover message storage and response-writer hijacking. Cover delivery, expiration, queue limits, disconnect cleanup, and invalid messages when changing those behaviors. Prefer fixed timestamps for expiration tests over sleeps. For upgrade changes, test wrapped response writers and unsupported hijacking as well as successful connections.

Run build, tests, and vet for Go changes. Run `go test -race ./...` when changing client management, shared message storage, goroutines, or shutdown, using a supported race-detector toolchain. Review lock ownership, channel closure, and goroutine lifetime together. Use the browser demo for integration checks affecting connection establishment or signaling.

## Security and Configuration

Keep `.env`, real tokens, private keys, and generated binaries out of commits. Do not log client tokens or sensitive signaling payloads. Preserve limits and timeout enforcement. Peer discovery is disabled by default; the example explicitly enables it. Keep permissive demo settings distinct from production deployment guidance.

## Commits and Pull Requests

Use concise, descriptive commit subjects. PRs should explain protocol or configuration changes, list validation results, and identify compatibility implications. Review this repository's `git status` and diff before committing, preserving unrelated edits. Commit module changes here before separately updating any parent submodule pointer.
