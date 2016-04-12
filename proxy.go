package proxy

import (
	"net"
	"net/http"
	"net/http/httputil"
	"strings"

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
	Dial         func(network, address string) (net.Conn, error)
	bufferPool   httputil.BufferPool
	reverseProxy *httputil.ReverseProxy
}

const tcp = "tcp"

// New returns a new Proxy handler
func New() *Handler {
	p := &Handler{}
	p.reverseProxy = &httputil.ReverseProxy{
		Transport: &http.Transport{
			Dial: func(network, address string) (net.Conn, error) {
				// TOFIX: circular reference
				return p.dial(address)
			},
		},
		Director: func(*http.Request) {
			// Do nothing, the request is self-sufficient
		},
	}
	return p
}

// SetBufferPool set the buffer pool to be used with io.CopyBuffer when copying data
// between client/backend sockets.
func (p *Handler) SetBufferPool(bpool httputil.BufferPool) {
	p.bufferPool = bpool
	p.reverseProxy.BufferPool = bpool
}

func (p *Handler) dial(address string) (net.Conn, error) {
	if p.Dial != nil {
		return p.Dial(tcp, address)
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
	if r.URL.Host == "" {
		http.Error(w, "Non Absolute URL", http.StatusBadRequest)
		return
	}

	if r.Method != "CONNECT" && r.URL.Scheme != "http" && r.URL.Scheme != "https" {
		http.Error(w, "Unsupported URL Scheme", http.StatusBadRequest)
		return
	}

	if strings.IndexByte(r.URL.Host, ':') == -1 {
		r.URL.Host += ":80"
	}

	if p.Accept != nil && !p.Accept(ctx, r) {
		http.Error(w, "CONNECT Not Allowed", http.StatusForbidden)
		return
	}

	if r.Method == "CONNECT" {
		p.handleHTTPS(ctx, w, r)
	} else {
		p.reverseProxy.ServeHTTP(w, r)
	}
}
