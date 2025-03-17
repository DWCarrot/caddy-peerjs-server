package caddy_peerjs_server

import (
	"strconv"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
)

func init() {
	httpcaddyfile.RegisterHandlerDirective("peerjs_server", parseCaddyfile)
}

// UnmarshalCaddyfile implements caddyfile.Unmarshaler.
//
//	peerjs_server [<matcher>] {
//		[key <string>] // Default: "peerjs"
//		[expire_timeout <duration>] // Default: 5000
//		[alive_timeout <duration>] // Default: 60000
//		[concurrent_limit <uint>] // Default: 64
//		[queue_limit <uint>] // Default: 16
//		[allow_discovery [<bool>]] // Default: false
//		[transmission_extend <string>...]
//		[id_manager <subdirective>]
//	}
func (pjs *PeerJSServer) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	for d.Next() {
		if d.NextArg() {
			return d.ArgErr()
		}
		for d.NextBlock(0) {
			switch d.Val() {
			case "path":
				if !d.NextArg() {
					return d.ArgErr()
				}
				pjs.Path = d.Val()
			case "key":
				if !d.NextArg() {
					return d.ArgErr()
				}
				pjs.Key = d.Val()
			case "expire_timeout":
				if !d.NextArg() {
					return d.ArgErr()
				}
				expireTimeout, err := caddy.ParseDuration(d.Val())
				if err != nil {
					return err
				}
				pjs.ExpireTimeout = expireTimeout
			case "alive_timeout":
				if !d.NextArg() {
					return d.ArgErr()
				}
				aliveTimeout, err := caddy.ParseDuration(d.Val())
				if err != nil {
					return err
				}
				pjs.AliveTimeout = aliveTimeout
			case "concurrent_limit":
				if !d.NextArg() {
					return d.ArgErr()
				}
				concurrentLimit, err := strconv.ParseUint(d.Val(), 10, 16)
				if err != nil {
					return err
				}
				pjs.ConcurrentLimit = uint(concurrentLimit)
			case "queue_limit":
				if !d.NextArg() {
					return d.ArgErr()
				}
				queueLimit, err := strconv.ParseUint(d.Val(), 10, 16)
				if err != nil {
					return err
				}
				pjs.QueueLimit = uint(queueLimit)
			case "allow_discovery":
				if !d.NextArg() {
					pjs.AllowDiscovery = true
				} else {
					allowDiscovery, err := strconv.ParseBool(d.Val())
					if err != nil {
						return err
					}
					pjs.AllowDiscovery = allowDiscovery
				}
			case "transmission_extend":
				data := make([]string, 0)
				for d.NextArg() {
					data = append(data, d.Val())
				}
				pjs.TransmissionExtend = data
			case "id_manager":
				// TODO: Implement this
				continue
			default:
				return d.Errf("unrecognized subdirective: %s", d.Val())
			}
		}
	}
	return nil
}

// parseCaddyfile sets up the handler from Caddyfile tokens. Syntax:
func parseCaddyfile(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
	m := new(PeerJSServer)
	err := m.UnmarshalCaddyfile(h.Dispenser)
	if err != nil {
		return nil, err
	}
	return m, nil
}

// Interface guards
var (
	_ caddyfile.Unmarshaler = (*PeerJSServer)(nil)
)
