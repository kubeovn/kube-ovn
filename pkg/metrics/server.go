package metrics

import (
	"context"
	"fmt"

	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

func Run(ctx context.Context, config *rest.Config, addr string, secureServing bool) error {
	if config == nil {
		config = ctrl.GetConfigOrDie()
	}
	client, err := rest.HTTPClientFor(config)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to create http client: %v", err)
	}

	options := server.Options{
		SecureServing: secureServing,
		BindAddress:   addr,
	}
	if secureServing {
		options.FilterProvider = filters.WithAuthenticationAndAuthorization
	}
	svr, err := server.NewServer(options, config, client)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to create metrics server: %v", err)
	}

	return svr.Start(ctx)
}
