package request

import (
	"context"
	"net"
	"net/http"

	"github.com/parnurzeal/gorequest"
)

// NewCniServerClient return a new cniserver client
func NewCniServerClient(socketAddress string) CniServerClient {
	request := gorequest.New()
	request.Transport = &http.Transport{DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
		return net.Dial("unix", socketAddress)
	}}
	return CniServerClient{request}
}
