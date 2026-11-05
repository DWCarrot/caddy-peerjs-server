package utils

import (
	"bufio"
	"net"
	"net/http"
)

// HijackResponseWriter lets Gorilla WebSocket v1.5.3 hijack connections through
// middleware that exposes Unwrap instead of implementing http.Hijacker.
type HijackResponseWriter struct {
	http.ResponseWriter
}

func (w HijackResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return http.NewResponseController(w.ResponseWriter).Hijack()
}

func (w HijackResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

var _ http.Hijacker = HijackResponseWriter{}
