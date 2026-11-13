// Package controllerruntime contains the controller-runtime migration shell.
package controllerruntime

import (
	"context"
	"errors"
	"fmt"

	netattachv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	appsv1 "k8s.io/api/apps/v1"
	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	coreclient "sigs.k8s.io/controller-runtime/pkg/client"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	networkpolicyv1alpha1 "sigs.k8s.io/network-policy-api/apis/v1alpha1"
	networkpolicyv1alpha2 "sigs.k8s.io/network-policy-api/apis/v1alpha2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/controller"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// NewScheme builds the scheme used by the migration manager. Optional APIs can
// be registered here even when their CRDs are absent; scheme registration does
// not perform discovery or start a list/watch request.
func NewScheme() (*runtime.Scheme, error) {
	scheme := runtime.NewScheme()
	for name, addToScheme := range map[string]func(*runtime.Scheme) error{
		"core/v1":                           corev1.AddToScheme,
		"apps/v1":                           appsv1.AddToScheme,
		"certificates/v1":                   certificatesv1.AddToScheme,
		"discovery/v1":                      discoveryv1.AddToScheme,
		"networking/v1":                     networkingv1.AddToScheme,
		"k8s.cni.cncf.io/v1":                netattachv1.AddToScheme,
		"kube-ovn/v1":                       kubeovnv1.AddToScheme,
		"policy.networking.k8s.io/v1alpha1": networkpolicyv1alpha1.Install,
		"policy.networking.k8s.io/v1alpha2": networkpolicyv1alpha2.Install,
	} {
		if err := addToScheme(scheme); err != nil {
			return nil, fmt.Errorf("add %s to controller-runtime scheme: %w", name, err)
		}
	}
	return scheme, nil
}

// NewManager constructs the migration manager without registering business
// reconcilers. Leader election remains owned by the legacy entrypoint until
// all resources have moved to manager-owned reconcilers.
func NewManager(config *controller.Configuration) (ctrl.Manager, error) {
	if config == nil {
		return nil, errors.New("controller-runtime manager configuration is nil")
	}
	if config.KubeRestConfig == nil {
		return nil, errors.New("controller-runtime manager Kubernetes REST config is nil")
	}

	scheme, err := NewScheme()
	if err != nil {
		return nil, err
	}

	mgr, err := ctrl.NewManager(config.KubeRestConfig, ctrl.Options{
		Scheme: scheme,
		Cache: cache.Options{
			DefaultTransform:            trimCacheObject,
			ReaderFailOnMissingInformer: true,
			DefaultEnableWatchBookmarks: new(true),
		},
		Client: coreclient.Options{
			Scheme: scheme,
		},
		LeaderElection:         false,
		Metrics:                metricsserver.Options{BindAddress: "0"},
		HealthProbeBindAddress: "0",
		PprofBindAddress:       "0",
	})
	if err != nil {
		return nil, fmt.Errorf("create controller-runtime manager: %w", err)
	}
	return mgr, nil
}

func trimCacheObject(obj any) (any, error) {
	if _, ok := obj.(*corev1.Pod); ok {
		return util.TrimPodForController(obj)
	}
	return util.TrimManagedFields(obj)
}

// Run starts the migration manager under the legacy leader election.
func Run(ctx context.Context, config *controller.Configuration) error {
	return run(ctx, config, nil)
}

// RunWithIPPool starts the manager with the controller that owns the shared
// IPAM backend. In IPPool mode the controller may still be restoring durable
// state; reconcile requests wait on its recovery gate before allocation.
func RunWithIPPool(ctx context.Context, config *controller.Configuration, backend *controller.Controller) error {
	return run(ctx, config, backend)
}

func setupSubnetShadowReconciler(mgr ctrl.Manager) error {
	if mgr == nil {
		return errors.New("controller-runtime manager is nil")
	}
	return (&SubnetShadowReconciler{Reader: mgr.GetAPIReader()}).SetupWithManager(mgr)
}

func run(ctx context.Context, config *controller.Configuration, backend *controller.Controller) error {
	mgr, err := NewManager(config)
	if err != nil {
		return err
	}
	if config.ControllerRuntimeMode == controller.ControllerRuntimeModeIPPool && backend == nil {
		return errors.New("IPPool runtime mode requires an initialized controller backend")
	}
	var recoveryGate func(context.Context) error
	if config.ControllerRuntimeMode == controller.ControllerRuntimeModeIPPool {
		recoveryGate = backend.WaitForIPPoolRuntimeRecovery
	}
	if config.ControllerRuntimeMode == controller.ControllerRuntimeModeVlan ||
		config.ControllerRuntimeMode == controller.ControllerRuntimeModeIPPool {
		if err := setupVlanReconcilers(mgr, config, recoveryGate); err != nil {
			return err
		}
	}
	if config.ControllerRuntimeMode == controller.ControllerRuntimeModeIPPool {
		if err := backend.SetIPPoolWriter(&ipPoolWriter{ctx: ctx, client: mgr.GetClient(), reader: mgr.GetAPIReader()}); err != nil {
			return err
		}
		r := &IPPoolReconciler{Events: backend, Adapter: backend}
		if err := r.SetupWithManager(mgr); err != nil {
			return err
		}
	}
	if config.ControllerRuntimeMode == controller.ControllerRuntimeModeSubnetShadow {
		if err := setupSubnetShadowReconciler(mgr); err != nil {
			return err
		}
		if err := setupSubnetShadowCacheReadiness(mgr); err != nil {
			return err
		}
	}
	return mgr.Start(ctx)
}
