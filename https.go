package proxy

import (
	"io"
	"net"
	"net/http"
	"sync"

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

	p.setupSocket(targetConn)
	p.setupSocket(clientConn)

	clientConn.Write(connectionEstablishedHeader)

	var buf1 []byte
	var buf2 []byte
	var buf3 []byte
	// Get buffers from pool if any
	if p.bufferPool != nil {
		buf1 = p.bufferPool.Get()
		buf2 = p.bufferPool.Get()
		buf3 = p.bufferPool.Get()
		defer func() {
			p.bufferPool.Put(buf1)
			p.bufferPool.Put(buf2)
			p.bufferPool.Put(buf3)
		}()
	}
	done := make(chan bool, 2)
	go copy(targetConn, clientConn, buf1, done)
	go pcopy(clientConn, targetConn, buf2, buf3, done)
	// As soon a one way returns an error or the context is cancelled
	// exit the current context do activate the defers and thus close
	// the connections
	select {
	case <-ctx.Done():
	case <-done:
	}
}

func (p *Handler) setupSocket(conn net.Conn) {
	if p.SocketBufferSize == 0 {
		return
	}
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		tcpConn.SetReadBuffer(p.SocketBufferSize)
		tcpConn.SetWriteBuffer(p.SocketBufferSize)
	}
}

func copy(dst io.Writer, src io.Reader, buf []byte, done chan bool) {
	io.CopyBuffer(dst, src, buf)
	done <- true
}

type bufSize struct {
	n int
	b []byte
}

func pcopy(dst io.Writer, src io.Reader, buf1, buf2 []byte, done chan bool) {
	if buf1 == nil {
		buf1 = make([]byte, 32*1024)
	}
	if buf2 == nil {
		buf1 = make([]byte, 32*1024)
	}
	rBufs := make(chan bufSize, 2)
	rBufs <- bufSize{b: buf1}
	rBufs <- bufSize{b: buf2}
	wBufs := make(chan bufSize, 2)
	wg := sync.WaitGroup{}
	wg.Add(2)
	go func() {
		for {
			buf, more := <-rBufs
			if !more {
				break
			}
			nr, err := src.Read(buf.b)
			buf.n = nr
			if err != nil {
				break
			}
			if nr > 0 {
				wBufs <- buf
			} else {
				rBufs <- buf
			}
		}
		close(wBufs)
		wg.Done()
	}()
	go func() {
		for {
			buf, more := <-wBufs
			if !more {
				break
			}
			n, err := dst.Write(buf.b[0:buf.n])
			if err != nil || n != buf.n {
				break
			}
			rBufs <- buf
		}
		close(rBufs)
		wg.Done()
	}()
	wg.Wait()
	done <- true
}
