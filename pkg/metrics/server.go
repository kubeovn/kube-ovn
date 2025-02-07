package metrics

import (
	"context"
	"fmt"
	"net/http"
	"net/http/pprof"

	"github.com/go-logr/logr"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
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

func Run(ctx context.Context, config *rest.Config, addr string, secureServing, withPprof bool) error {
	if config == nil {
		config = ctrl.GetConfigOrDie()
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
