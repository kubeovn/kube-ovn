package controller

import (
	"fmt"
	"net/http"
	_ "net/http/pprof" // #nosec
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/klog"
	"k8s.io/sample-controller/pkg/signals"

	"github.com/kubeovn/kube-ovn/pkg/controller"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/versions"
)

func CmdMain() {
	defer klog.Flush()

	stopCh := signals.SetupSignalHandler()
	klog.Infof(versions.String())

	controller.InitClientGoMetrics()
	controller.InitWorkQueueMetrics()
	config, err := controller.ParseFlags()
	if err != nil {
		klog.Fatalf("parse config failed %v", err)
	}

	go loopOvnNbctlDaemon(config)
	go func() {
		http.Handle("/metrics", promhttp.Handler())
		klog.Fatal(http.ListenAndServe(fmt.Sprintf("0.0.0.0:%d", config.PprofPort), nil))
	}()

	ctl := controller.NewController(config)
	ctl.Run(stopCh)
}

func loopOvnNbctlDaemon(config *controller.Configuration) {
	for {
		daemonSocket := os.Getenv("OVN_NB_DAEMON")
		time.Sleep(5 * time.Second)

		if _, err := os.Stat(daemonSocket); os.IsNotExist(err) || daemonSocket == "" {
			if err := ovs.StartOvnNbctlDaemon(config.OvnNbAddr); err != nil {
				klog.Errorf("failed to start ovn-nbctl daemon %v", err)
			}
		}

		// ovn-nbctl daemon may hang and cannot process further request.
		// In case of that, we need to start a new daemon.
		if err := ovs.CheckAlive(); err != nil {
			klog.Warningf("ovn-nbctl daemon doesn't return, start a new daemon")
			if err := ovs.StartOvnNbctlDaemon(config.OvnNbAddr); err != nil {
				klog.Errorf("failed to start ovn-nbctl daemon %v", err)
			}
		}
	}
}
