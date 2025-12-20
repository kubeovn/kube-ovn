package webhook

import (
	"flag"
	"os"

	"github.com/spf13/pflag"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	ctrlwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"

	ovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	ovnwebhook "github.com/kubeovn/kube-ovn/pkg/webhook"
	"github.com/kubeovn/kube-ovn/versions"
)

const hookServerCertDir = "/tmp/k8s-webhook-server/serving-certs"

var scheme = runtime.NewScheme()

func init() {
	if err := corev1.AddToScheme(scheme); err != nil {
		util.LogFatalAndExit(err, "failed to add core v1 scheme")
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
		util.LogFatalAndExit(err, "failed to add apps v1 scheme")
	}
	if err := ovnv1.AddToScheme(scheme); err != nil {
		util.LogFatalAndExit(err, "failed to add ovn v1 scheme")
	}
}

func CmdMain() {
	klog.Info(versions.String())

	port := pflag.Int("port", 8443, "The port webhook listen on.")
	healthProbePort := pflag.Int32("health-probe-port", 8080, "The port health probes listen on.")

	klogFlags := flag.NewFlagSet("klog", flag.ExitOnError)
	klog.InitFlags(klogFlags)

	// Sync the glog and klog flags.
	pflag.CommandLine.VisitAll(func(f1 *pflag.Flag) {
		f2 := klogFlags.Lookup(f1.Name)
		if f2 != nil {
			value := f1.Value.String()
			if err := f2.Value.Set(value); err != nil {
				util.LogFatalAndExit(err, "failed to set flag")
			}
		}
	})

	pflag.CommandLine.AddGoFlagSet(klogFlags)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.Parse()

	// set logger for controller-runtime framework
	ctrl.SetLogger(klog.NewKlogr())

	// Create a webhook server.
	hookServer := ctrlwebhook.NewServer(ctrlwebhook.Options{
		Port:    *port,
		CertDir: hookServerCertDir,
	})

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		// disable metrics to avoid port conflict
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
		HealthProbeBindAddress: util.JoinHostPort(os.Getenv(util.EnvPodIP), *healthProbePort),
	})
	if err != nil {
		panic(err)
	}

	validatingHook, err := ovnwebhook.NewValidatingHook(mgr.GetClient(), mgr.GetScheme(), mgr.GetCache())
	if err != nil {
		panic(err)
	}

	klog.Infof("register path /validating")
	// Register the webhooks in the server.
	hookServer.Register("/validating", &ctrlwebhook.Admission{Handler: validatingHook})

	if err := mgr.Add(hookServer); err != nil {
		panic(err)
	}

	if err = mgr.AddHealthzCheck("liveness probe", healthz.Ping); err != nil {
		panic(err)
	}
	if err = mgr.AddReadyzCheck("readiness probe", healthz.Ping); err != nil {
		panic(err)
	}

	// Start the server by starting a previously-set-up manager
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		panic(err)
	}
}
