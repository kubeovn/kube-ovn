package rpc

import (
	"errors"
	"fmt"
	"net/http"
	"net/rpc"

	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

type Server struct {
	*http.Server
}

func NewServer(addr string, rcvr any) (*Server, error) {
	if err := rpc.Register(rcvr); err != nil {
		return nil, fmt.Errorf("failed to register rpc receiver's methods: %w", err)
	}

	rpc.HandleHTTP()
	svr := &http.Server{Addr: addr}
	go func() {
		if err := svr.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			framework.Failf("failed to listen and serve rpc on %q: %v", addr, err)
		}
	}()

	return &Server{Server: svr}, nil
}
