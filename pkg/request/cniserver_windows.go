package request

import (
	"context"
	"net"
	"net/http"

	"github.com/Microsoft/go-winio"
	"github.com/parnurzeal/gorequest"
)

// NewCniServerClient return a new cniserver client
func NewCniServerClient(pipePath string) CniServerClient {
	request := gorequest.New()
	request.Transport = &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return winio.DialPipeContext(ctx, pipePath)
	}}
	return CniServerClient{request}
}
