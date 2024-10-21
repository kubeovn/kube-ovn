package rpc

import (
	"fmt"
	"net"
	"net/http"
	"net/rpc"

	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

type Server struct {
	listener net.Listener
}

func (s *Server) Addr() string {
	return s.listener.Addr().String()
}

func (s *Server) Close() error {
	return s.listener.Close()
}

func NewServer(addr string, rcvr any) (*Server, error) {
	if err := rpc.Register(rcvr); err != nil {
		return nil, fmt.Errorf("failed to register rpc receiver's methods: %w", err)
	}

	rpc.HandleHTTP()
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on %q: %w", addr, err)
	}

	go func() {
		if err := http.Serve(listener, nil); err != nil {
			framework.Failf("failed to serve rpc: %v", err)
		}
	}()

	return &Server{listener: listener}, nil
}
