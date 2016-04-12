package main

import (
	"net/http"

	"github.com/rs/proxy"
	"github.com/rs/xhandler"
	"github.com/rs/xlog"
)

func main() {
	p := proxy.New()

	c := xhandler.Chain{}
	c.UseC(xlog.NewHandler(xlog.Config{}))
	c.UseC(xlog.MethodHandler("method"))
	c.UseC(xlog.URLHandler("url"))
	c.UseC(xlog.RemoteAddrHandler("ip"))
	c.UseC(xlog.UserAgentHandler("user_agent"))

	xlog.Info("Listening on :8080")
	if err := http.ListenAndServe(":8080", c.Handler(p)); err != nil {
		xlog.Fatal(err)
	}
}
