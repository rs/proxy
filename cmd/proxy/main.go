package main

import (
	"net"
	"net/http"
	"strings"

	"golang.org/x/net/context"

	"github.com/rs/proxy"
	"github.com/rs/xaccess"
	"github.com/rs/xhandler"
	"github.com/rs/xlog"
)

func main() {
	p := proxy.New()

	p.Accept = func(ctx context.Context, r *http.Request) bool {
		xlog.Debugf("Accepting %s", r.URL.Host)
		return strings.HasPrefix(r.URL.Host, "www.apple.com")
	}

	dialer := net.Dialer{}
	p.Dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		xlog.Debugf("Dialing %s, %s", network, address)
		return dialer.Dial(network, address)
	}

	c := xhandler.Chain{}
	c.UseC(xlog.NewHandler(xlog.Config{}))
	c.UseC(xlog.MethodHandler("method"))
	c.UseC(xlog.URLHandler("url"))
	c.UseC(xlog.RemoteAddrHandler("ip"))
	c.UseC(xlog.UserAgentHandler("user_agent"))
	c.UseC(xaccess.NewHandler())

	xlog.Info("Listening on :8080")
	if err := http.ListenAndServe(":8080", c.Handler(p)); err != nil {
		xlog.Fatal(err)
	}
}
