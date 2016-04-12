package proxy

import (
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

	targetConn, err := p.dial(ctx, r.URL.Host)
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

	ctx, cancel := context.WithCancel(ctx)
	err1 := make(chan error, 1)
	err2 := make(chan error, 1)
	buf1 := p.getBuffer()
	buf2 := p.getBuffer()
	go netCopy(ctx, targetConn, clientConn, buf1, err1)
	go netCopy(ctx, clientConn, targetConn, buf2, err2)
	select {
	case <-err1:
		// stop the other go routine and wait for its termination
		cancel()
		<-err2
	case <-err2:
		// stop the other go routine and wait for its termination
		cancel()
		<-err1
	}
	// Put buffers back to their pool
	p.bufferPool.Put(buf1)
	p.bufferPool.Put(buf2)
}
