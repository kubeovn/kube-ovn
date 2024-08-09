package healthz

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

const (
	LivenessEndpoint  = "/healthz"
	ReadinessEndpoint = "/readyz"
)

func Run(ctx context.Context, port int32, extraHandlers map[string]http.Handler) error {
	addr := util.JoinHostPort("127.0.0.1", port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("error listening on %s: %w", addr, err)
	}

	mux := http.NewServeMux()
	handler := healthz.CheckHandler{Checker: healthz.Ping}
	mux.Handle(ReadinessEndpoint, http.StripPrefix(ReadinessEndpoint, handler))
	mux.Handle(LivenessEndpoint, http.StripPrefix(LivenessEndpoint, handler))
	for path, handler := range extraHandlers {
		mux.Handle(path, handler)
	}

	server := &manager.Server{
		Name: "health probe",
		Server: &http.Server{
			Handler:           mux,
			MaxHeaderBytes:    1 << 20,
			IdleTimeout:       90 * time.Second, // matches http.DefaultTransport keep-alive timeout
			ReadHeaderTimeout: 32 * time.Second,
		},
		Listener: listener,
	}
	if err := server.Start(ctx); err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to start health probe server: %w", err)
	}

	return nil
}
