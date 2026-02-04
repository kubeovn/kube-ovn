package metrics

import (
	"context"
	"net"
	"net/http"
	"net/http/pprof"
	"time"

	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

const (
	defaultServerIdleTimeout       = 90 * time.Second
	defaultServerReadHeaderTimeout = 32 * time.Second
	defaultServerMaxHeaderBytes    = 1 << 20
)

func NewPprofServer(host string, port int) (*manager.Server, error) {
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.ParseIP(host), Port: port})
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	return &manager.Server{
		Name: "pprof",
		Server: &http.Server{
			Handler:           mux,
			MaxHeaderBytes:    defaultServerMaxHeaderBytes,
			IdleTimeout:       defaultServerIdleTimeout,
			ReadHeaderTimeout: defaultServerReadHeaderTimeout,
		},
		Listener: listener,
	}, nil
}

func NewHealthOnlyServer(addr string, port int) (*manager.Server, error) {
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.ParseIP(addr), Port: port})
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", util.DefaultHealthCheckHandler)
	mux.HandleFunc("/livez", util.DefaultHealthCheckHandler)
	mux.HandleFunc("/readyz", util.DefaultHealthCheckHandler)
	return &manager.Server{
		Name: "health-check",
		Server: &http.Server{
			Handler:           mux,
			MaxHeaderBytes:    defaultServerMaxHeaderBytes,
			IdleTimeout:       defaultServerIdleTimeout,
			ReadHeaderTimeout: defaultServerReadHeaderTimeout,
		},
		Listener: listener,
	}, nil
}

func StartPprofServerIfNeeded(ctx context.Context, enablePprof, servePprofInMetrics bool, host string, port int) {
	if !enablePprof || servePprofInMetrics {
		return
	}
	svr, err := NewPprofServer(host, port)
	if err != nil {
		util.LogFatalAndExit(err, "failed to run pprof server")
	}
	go func() {
		if err := svr.Start(ctx); err != nil {
			util.LogFatalAndExit(err, "failed to run pprof server")
		}
	}()
}

func StartMetricsOrHealthServer(ctx context.Context, enableMetrics bool, addrs []string, port int, config *rest.Config, secureServing, withPprof bool, tlsMinVersion, tlsMaxVersion string, tlsCipherSuites []string) {
	if enableMetrics {
		InitKlogMetrics()
		InitClientGoMetrics()
		for _, addr := range addrs {
			if port < 0 || port > 65535 {
				util.LogFatalAndExit(nil, "invalid port number: %d", port)
				return
			}
			listenAddr := util.JoinHostPort(addr, int32(port)) // #nosec G115
			go func() {
				if err := Run(ctx, config, listenAddr, secureServing, withPprof, tlsMinVersion, tlsMaxVersion, tlsCipherSuites); err != nil {
					util.LogFatalAndExit(err, "failed to run metrics server")
				}
			}()
		}
		return
	}
	klog.Info("metrics server is disabled")
	svr, err := NewHealthOnlyServer(addrs[0], port)
	if err != nil {
		util.LogFatalAndExit(err, "failed to run health check server")
	}
	go func() {
		if err := svr.Start(ctx); err != nil {
			util.LogFatalAndExit(err, "failed to run health check server")
		}
	}()
}
