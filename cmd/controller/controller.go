package main

import (
	"fmt"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"net/http"
	_ "net/http/pprof"
	"os"
	"time"

	"github.com/alauda/kube-ovn/pkg/controller"
	"github.com/alauda/kube-ovn/pkg/ovs"

	"k8s.io/klog"
	"k8s.io/sample-controller/pkg/signals"
)

func main() {
	defer klog.Flush()

	stopCh := signals.SetupSignalHandler()

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
			ovs.StartOvnNbctlDaemon(config.OvnNbHost, config.OvnNbPort)
		}

		// ovn-nbctl daemon may hang and cannot precess further request.
		// In case of that, we need to start a new daemon.
		if  err := ovs.CheckAlive(); err != nil {
			klog.Warningf("ovn-nbctl daemon doesn't return, start a new daemon")
			ovs.StartOvnNbctlDaemon(config.OvnNbHost, config.OvnNbPort)
		}
	}
}
