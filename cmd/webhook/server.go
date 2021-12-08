package webhook

import (
	"flag"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/klogr"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"

	ovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	ovnwebhook "github.com/kubeovn/kube-ovn/pkg/webhook"
	"github.com/kubeovn/kube-ovn/versions"
)

const (
	hookServerCertDir = "/tmp/k8s-webhook-server/serving-certs"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	if err := corev1.AddToScheme(scheme); err != nil {
		klog.Fatalf("failed to add scheme, %v", err)
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
		klog.Fatalf("failed to add scheme, %v", err)
	}
	if err := ovnv1.AddToScheme(scheme); err != nil {
		klog.Fatalf("failed to add scheme, %v", err)
	}
}

func CmdMain() {
	var port int
	klog.Infof(versions.String())

	flag.IntVar(&port, "port", 8443, "The port webhook listen on.")
	klog.InitFlags(nil)
	flag.Parse()

	// set logger for controller-runtime framework
	ctrl.SetLogger(klogr.New())

	// Create a webhook server.
	hookServer := &ctrlwebhook.Server{
		Port:    port,
		CertDir: hookServerCertDir,
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		// disable metrics to avoid port conflict
		MetricsBindAddress: "0",
	})
	if err != nil {
		panic(err)
	}

	validatingHook, err := ovnwebhook.NewValidatingHook(mgr.GetCache())
	if err != nil {
		panic(err)
	}

	klog.Infof("register path /validate-ip")
	// Register the webhooks in the server.
	hookServer.Register("/validate-ip", &ctrlwebhook.Admission{Handler: validatingHook})

	if err := mgr.Add(hookServer); err != nil {
		panic(err)
	}

	// Start the server by starting a previously-set-up manager
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		panic(err)
	}
}
