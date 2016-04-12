package proxy

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rs/xlog"

	"golang.org/x/net/context"
)

// Handler is a http.Handler handling CONNECT requests in order to act as
// a proxy.
type Handler struct {
	// Accept takes a request and return if the connection is accepted
	// or not. The target address is the r.URL.Host. The Accept implementation
	// may modify this field to rewrite the target.
	Accept func(ctx context.Context, r *http.Request) bool
	// Dial specifies the dial function for creating unencrypted TCP connections.
	// If Dial is nil, net.Dial is used.
	Dial func(ctx context.Context, network, addr string) (net.Conn, error)
	// Size of the buffer for copping bytes from/to client from/to target.
	BufferSize int
	bufferPool *sync.Pool
}

const tcp = "tcp"

var connectionEstablishedHeader = []byte("HTTP/1.0 200 Connection Established\r\n\r\n")

// New returns a new Proxy handler
func New() *Handler {
	return &Handler{
		BufferSize: 256,
		bufferPool: &sync.Pool{},
	}
}

func (p *Handler) getBuffer() []byte {
	buf, ok := p.bufferPool.Get().([]byte)
	if !ok {
		buf = make([]byte, p.BufferSize)
	}
	return buf
}

func (p *Handler) dial(ctx context.Context, address string) (net.Conn, error) {
	if p.Dial != nil {
		return p.Dial(ctx, tcp, address)
	}
	return net.Dial(tcp, address)
}

// ServeHTTP implements http.Handler
func (p *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Upgrade a non context request
	p.ServeHTTPC(context.TODO(), w, r)
}

// ServeHTTPC implements xhandler.HandlerC
func (p *Handler) ServeHTTPC(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		panic("ResponseWriter doesn't support hijacking")
	}

	log := xlog.FromContext(ctx)
	statusCode := http.StatusOK
	defer func(t time.Time) {
		status := "ok"
		if statusCode >= 400 {
			status = "error"
		}
		log.Debugf("%s %s", r.Method, r.URL.String(), xlog.F{
			"status":      status,
			"status_code": statusCode,
			"duration":    time.Since(t).Seconds(),
		})
	}(time.Now())

	if r.Method != "CONNECT" {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		statusCode = http.StatusMethodNotAllowed
		return
	}

	if r.URL.Host == "" {
		http.Error(w, "Non Absolute URL", http.StatusBadRequest)
		statusCode = http.StatusBadRequest
		return
	}

	if p.Accept != nil && !p.Accept(ctx, r) {
		http.Error(w, "CONNECT Not Allowed", http.StatusForbidden)
		statusCode = http.StatusForbidden
		return
	}

	targetAddr := r.URL.Host
	if strings.IndexByte(targetAddr, ':') == -1 {
		targetAddr += ":80"
	}

	targetConn, err := p.dial(ctx, targetAddr)
	if err != nil {
		http.Error(w, "CONNECT Not Allowed", http.StatusBadGateway)
		statusCode = http.StatusBadGateway
		return
	}
	defer targetConn.Close()

	clientConn, _, err := hj.Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		statusCode = http.StatusInternalServerError
		return
	}
	defer clientConn.Close()

	clientConn.Write(connectionEstablishedHeader)

	ctx, cancel := context.WithCancel(ctx)
	err1 := make(chan error, 1)
	err2 := make(chan error, 1)
	buf1 := p.getBuffer()
	buf2 := p.getBuffer()
	timeout := 5 * time.Second
	go proxy(ctx, targetConn, clientConn, buf1, timeout, err1)
	go proxy(ctx, clientConn, targetConn, buf2, timeout, err2)
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

func proxy(ctx context.Context, from net.Conn, to net.Conn, buf []byte, timeout time.Duration, errs chan error) {
	for {
		select {
		case <-ctx.Done():
			// If context is canceled, exit
			errs <- nil
			return
		default:
			// Extend reader's deadline
			from.SetReadDeadline(time.Now().Add(timeout))
			// Read data from the source connection.
			read, err := from.Read(buf)
			// If read error occurs, check if it's a fatal error (not a timeout)
			// and stop the proxiying
			if err != nil {
				if isNetTimeout(err) {
					// On deadline exceeded, keep going so we check out
					// on the stop channel.
					continue
				}
				// On error, stop there and notify the caller
				errs <- err
				return
			}

			// Extend reader's deadline
			to.SetWriteDeadline(time.Now().Add(timeout))
			// Write data to the destination.
			_, err = to.Write(buf[:read])
			// If write error occurs, check if it's a fatal error (not a timeout)
			// and stop the proxiying
			if err != nil {
				if isNetTimeout(err) {
					// On deadline exceeded, keep going so we check out
					// on the stop channel.
					continue
				}
				errs <- err
				return
			}
		}
	}
}

func isNetTimeout(err error) bool {
	if err, ok := err.(net.Error); ok {
		return err.Timeout()
	}
	return false
}
