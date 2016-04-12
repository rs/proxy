package proxy

import (
	"io"
	"net/http"

	"github.com/rs/xlog"

	"golang.org/x/net/context"
)

var connectionEstablishedHeader = []byte("HTTP/1.0 200 Connection Established\r\n\r\n")

func (p *Handler) handleHTTPS(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		panic("ResponseWriter doesn't support hijacking")
	}

	targetConn, err := p.dial(r.URL.Host)
	if err != nil {
		http.Error(w, "CONNECT Not Allowed", http.StatusBadGateway)
		return
	}
	defer targetConn.Close()

	clientConn, _, err := hj.Hijack()
	if err != nil {
		xlog.FromContext(ctx).Error("cannot hijack connection: ", err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer clientConn.Close()

	clientConn.Write(connectionEstablishedHeader)

	var buf1 []byte
	var buf2 []byte
	// Get buffers from pool if any
	if p.bufferPool != nil {
		buf1 = p.bufferPool.Get()
		buf2 = p.bufferPool.Get()
	}
	done := make(chan bool, 2)
	go copy(targetConn, clientConn, buf1, done)
	go copy(clientConn, targetConn, buf2, done)
	// As soon a one way returns an error or the context is cancelled
	// exit the current context do activate the defers and thus close
	// the connections
	select {
	case <-ctx.Done():
	case <-done:
	}
	// Put buffers back to their pool if any
	if p.bufferPool != nil {
		p.bufferPool.Put(buf1)
		p.bufferPool.Put(buf2)
	}
}

func copy(dst io.Writer, src io.Reader, buf []byte, done chan bool) {
	io.CopyBuffer(dst, src, buf)
	done <- true
}
