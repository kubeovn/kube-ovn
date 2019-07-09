package main

import (
	"fmt"
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
		klog.Fatal(http.ListenAndServe(fmt.Sprintf("localhost:%d", config.PprofPort), nil))
	}()

	if err = controller.InitClusterRouter(config); err != nil {
		klog.Fatalf("init cluster router failed %v", err)
	}

	if err = controller.InitLoadBalancer(config); err != nil {
		klog.Fatalf("init load balancer failed %v", err)
	}

	if err = controller.InitNodeSwitch(config); err != nil {
		klog.Fatalf("init node switch failed %v", err)
	}

	if err = controller.InitDefaultLogicalSwitch(config); err != nil {
		klog.Fatalf("init default switch failed %v", err)
	}

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
	}
}
