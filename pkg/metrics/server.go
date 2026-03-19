package metrics

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"time"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func filterProvider(c *rest.Config, httpClient *http.Client) (server.Filter, error) {
	return func(log logr.Logger, handler http.Handler) (http.Handler, error) {
		filter, err := filters.WithAuthenticationAndAuthorization(c, httpClient)
		if err != nil {
			return nil, fmt.Errorf("failed to create filter: %w", err)
		}
		h, err := filter(log, handler)
		if err != nil {
			return nil, fmt.Errorf("failed to create handler: %w", err)
		}
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			switch req.URL.Path {
			case "/healthz", "/livez", "/readyz":
				handler.ServeHTTP(w, req)
			default:
				h.ServeHTTP(w, req)
			}
		}), nil
	}, nil
}

func TLSVersionFromString(version string) (uint16, error) {
	switch version {
	case "1.0", "TLS 1.0", "TLS10":
		return tls.VersionTLS10, nil
	case "1.1", "TLS 1.1", "TLS11":
		return tls.VersionTLS11, nil
	case "1.2", "TLS 1.2", "TLS12":
		return tls.VersionTLS12, nil
	case "1.3", "TLS 1.3", "TLS13":
		return tls.VersionTLS13, nil
	case "", "auto", "default":
		return 0, nil
	default:
		return 0, fmt.Errorf("unsupported TLS version: %s", version)
	}
}

func CipherSuiteFromName(name string) (uint16, error) {
	for _, c := range tls.CipherSuites() {
		if c.Name == name {
			return c.ID, nil
		}
	}
	for _, c := range tls.InsecureCipherSuites() {
		if c.Name == name {
			return 0, fmt.Errorf("insecure cipher suite: %s", name)
		}
	}
	return 0, fmt.Errorf("unsupported TLS cipher suite: %s", name)
}

func CipherSuitesFromNames(suites []string) ([]uint16, error) {
	if len(suites) == 0 {
		return nil, nil
	}

	cipherSuites := make([]uint16, 0, len(suites))
	for _, suite := range suites {
		cipherSuite, err := CipherSuiteFromName(suite)
		if err != nil {
			return nil, fmt.Errorf("invalid cipher suite %s: %w", suite, err)
		}
		cipherSuites = append(cipherSuites, cipherSuite)
	}
	return cipherSuites, nil
}

func Run(ctx context.Context, config *rest.Config, addr string, secureServing, withPprof bool, tlsMinVersion, tlsMaxVersion string, tlsCipherSuites []string) error {
	if config == nil {
		config = ctrl.GetConfigOrDie()
	}

	minVersion, err := TLSVersionFromString(tlsMinVersion)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to parse TLS minimum version: %w", err)
	}
	maxVersion, err := TLSVersionFromString(tlsMaxVersion)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to parse TLS maximum version: %w", err)
	}
	cipherSuites, err := CipherSuitesFromNames(tlsCipherSuites)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to parse TLS cipher suites: %w", err)
	}

	// Validate that if both minVersion and maxVersion are set, minVersion is not greater than maxVersion.
	if maxVersion != 0 && minVersion > maxVersion {
		err = fmt.Errorf("tls: MinVersion (%s) must be less than or equal to MaxVersion (%s)", tlsMinVersion, tlsMaxVersion)
		klog.Error(err)
		return err
	}

	// #nosec G402
	tlsConfig := &tls.Config{
		MinVersion:   minVersion,
		MaxVersion:   maxVersion,
		CipherSuites: cipherSuites,
	}
	getConfigForClient, err := tlsGetConfigForClient(tlsConfig)
	if err != nil {
		err = fmt.Errorf("failed to set GetConfigForClient for TLS config: %w", err)
		klog.Error(err)
		return err
	}

	client, err := rest.HTTPClientFor(config)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to create http client: %w", err)
	}

	options := server.Options{
		SecureServing: secureServing,
		BindAddress:   addr,
	}
	if secureServing {
		options.FilterProvider = filterProvider
		options.TLSOpts = []func(*tls.Config){
			func(c *tls.Config) {
				c.GetConfigForClient = getConfigForClient
			},
		}
	}
	options.ExtraHandlers = map[string]http.Handler{
		"/healthz": http.HandlerFunc(util.DefaultHealthCheckHandler),
		"/livez":   http.HandlerFunc(util.DefaultHealthCheckHandler),
		"/readyz":  http.HandlerFunc(util.DefaultHealthCheckHandler),
	}
	if withPprof {
		options.ExtraHandlers["/debug/pprof/"] = http.HandlerFunc(pprof.Index)
		options.ExtraHandlers["/debug/pprof/cmdline"] = http.HandlerFunc(pprof.Cmdline)
		options.ExtraHandlers["/debug/pprof/profile"] = http.HandlerFunc(pprof.Profile)
		options.ExtraHandlers["/debug/pprof/symbol"] = http.HandlerFunc(pprof.Symbol)
		options.ExtraHandlers["/debug/pprof/trace"] = http.HandlerFunc(pprof.Trace)
	}
	svr, err := server.NewServer(options, config, client)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to create metrics server: %w", err)
	}

	return svr.Start(ctx)
}

// ServeWithListener starts a metrics server using a pre-created listener.
// This avoids the race condition where the listener creation and address
// transfer happen concurrently, causing bind failures.
func ServeWithListener(ctx context.Context, config *rest.Config, listener net.Listener, secureServing, withPprof bool, tlsMinVersion, tlsMaxVersion string, tlsCipherSuites []string) error {
	if config == nil {
		config = ctrl.GetConfigOrDie()
	}

	mux := http.NewServeMux()

	metricsHandler := promhttp.HandlerFor(metrics.Registry, promhttp.HandlerOpts{
		ErrorHandling: promhttp.HTTPErrorOnError,
	})

	// When secureServing is enabled, wrap handlers with auth filter.
	// Health check endpoints are exempt from auth, matching the
	// behavior of the filterProvider used in the controller-runtime path.
	var authFilter func(logr.Logger, http.Handler) (http.Handler, error)
	if secureServing {
		client, err := rest.HTTPClientFor(config)
		if err != nil {
			return fmt.Errorf("failed to create http client: %w", err)
		}
		authFilter, err = filters.WithAuthenticationAndAuthorization(config, client)
		if err != nil {
			return fmt.Errorf("failed to create metrics filter: %w", err)
		}
		log := klog.NewKlogr()
		metricsHandler, err = authFilter(log, metricsHandler)
		if err != nil {
			return fmt.Errorf("failed to apply metrics filter: %w", err)
		}
	}

	mux.Handle("/metrics", metricsHandler)
	mux.HandleFunc("/healthz", util.DefaultHealthCheckHandler)
	mux.HandleFunc("/livez", util.DefaultHealthCheckHandler)
	mux.HandleFunc("/readyz", util.DefaultHealthCheckHandler)
	if withPprof {
		pprofHandlers := map[string]http.Handler{
			"/debug/pprof/":        http.HandlerFunc(pprof.Index),
			"/debug/pprof/cmdline": http.HandlerFunc(pprof.Cmdline),
			"/debug/pprof/profile": http.HandlerFunc(pprof.Profile),
			"/debug/pprof/symbol":  http.HandlerFunc(pprof.Symbol),
			"/debug/pprof/trace":   http.HandlerFunc(pprof.Trace),
		}
		for path, h := range pprofHandlers {
			if authFilter != nil {
				log := klog.NewKlogr().WithValues("path", path)
				var err error
				h, err = authFilter(log, h)
				if err != nil {
					return fmt.Errorf("failed to apply auth filter to pprof handler %s: %w", path, err)
				}
			}
			mux.Handle(path, h)
		}
	}

	if secureServing {
		minVersion, err := TLSVersionFromString(tlsMinVersion)
		if err != nil {
			return fmt.Errorf("failed to parse TLS minimum version: %w", err)
		}
		maxVersion, err := TLSVersionFromString(tlsMaxVersion)
		if err != nil {
			return fmt.Errorf("failed to parse TLS maximum version: %w", err)
		}
		cipherSuites, err := CipherSuitesFromNames(tlsCipherSuites)
		if err != nil {
			return fmt.Errorf("failed to parse TLS cipher suites: %w", err)
		}
		if maxVersion != 0 && minVersion > maxVersion {
			return fmt.Errorf("tls: MinVersion (%s) must be less than or equal to MaxVersion (%s)", tlsMinVersion, tlsMaxVersion)
		}
		// #nosec G402
		tlsConfig := &tls.Config{
			MinVersion:   minVersion,
			MaxVersion:   maxVersion,
			CipherSuites: cipherSuites,
		}
		getConfigForClient, err := tlsGetConfigForClient(tlsConfig)
		if err != nil {
			return fmt.Errorf("failed to set GetConfigForClient for TLS config: %w", err)
		}
		tlsConfig.GetConfigForClient = getConfigForClient
		listener = tls.NewListener(listener, tlsConfig)
	}

	srv := &http.Server{
		Handler:           mux,
		MaxHeaderBytes:    defaultServerMaxHeaderBytes,
		IdleTimeout:       defaultServerIdleTimeout,
		ReadHeaderTimeout: defaultServerReadHeaderTimeout,
	}

	idleConnsClosed := make(chan struct{})
	go func() { // #nosec G118
		<-ctx.Done()
		klog.Info("Shutting down metrics server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 1*time.Minute) //nolint:contextcheck
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			klog.Errorf("error shutting down the metrics server: %v", err)
		}
		close(idleConnsClosed)
	}()

	klog.Infof("Serving metrics server on %s", listener.Addr())
	if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
		return err
	}

	<-idleConnsClosed
	return nil
}
