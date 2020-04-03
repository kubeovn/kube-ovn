package main

import (
	"flag"
	_ "net/http/pprof"
	"os"
	"time"

	ovnv1 "github.com/alauda/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/alauda/kube-ovn/pkg/ovs"
	ovnwebhook "github.com/alauda/kube-ovn/pkg/webhook"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog"
	"k8s.io/klog/klogr"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"
)

const (
	hookServerCertDir = "/tmp/k8s-webhook-server/serving-certs"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	corev1.AddToScheme(scheme)
	appsv1.AddToScheme(scheme)
	ovnv1.AddToScheme(scheme)
}

func main() {
	var (
		port         int
		ovnNbHost    string
		ovnNbPort    int
		ovnNbTimeout int
		defaultLS    string
	)
	flag.IntVar(&port, "port", 8443, "The port webhook listen on.")
	flag.IntVar(&ovnNbPort, "ovn-nb-port", 6641, "OVN nb port")
	flag.IntVar(&ovnNbTimeout, "ovn-nb-timeout", 30, "OVN nb timeout")
	flag.StringVar(&ovnNbHost, "ovn-nb-host", "0.0.0.0", "OVN nb host")
	flag.StringVar(&defaultLS, "default-ls", "ovn-default", "The default logical switch name, default: ovn-default")

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

	opt := &ovnwebhook.WebhookOptions{
		OvnNbHost:    ovnNbHost,
		OvnNbPort:    ovnNbPort,
		OvnNbTimeout: ovnNbTimeout,
		DefaultLS:    defaultLS,
	}
	validatingHook, err := ovnwebhook.NewValidatingHook(mgr.GetCache(), opt)
	if err != nil {
		panic(err)
	}
	// Register the webhooks in the server.
	hookServer.Register("/validate-ip", &ctrlwebhook.Admission{Handler: validatingHook})

	if err := mgr.Add(hookServer); err != nil {
		panic(err)
	}

	go loopOvnNbctlDaemon(ovnNbHost, ovnNbPort)

	// Start the server by starting a previously-set-up manager
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		panic(err)
	}
}

func loopOvnNbctlDaemon(ovnNbHost string, ovnNbPort int) {
	for {
		daemonSocket := os.Getenv("OVN_NB_DAEMON")
		time.Sleep(5 * time.Second)

		if _, err := os.Stat(daemonSocket); os.IsNotExist(err) || daemonSocket == "" {
			ovs.StartOvnNbctlDaemon(ovnNbHost, ovnNbPort)
		}

		if err := ovs.CheckAlive(); err != nil {
			klog.Warningf("ovn-nbctl daemon doesn't return, start a new daemon")
			ovs.StartOvnNbctlDaemon(ovnNbHost, ovnNbPort)
		}
	}
}
