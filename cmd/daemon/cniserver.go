package main

import (
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"time"

	"github.com/alauda/kube-ovn/pkg/daemon"
	"github.com/alauda/kube-ovn/pkg/ovs"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/klog"
	"k8s.io/sample-controller/pkg/signals"
)

func main() {
	defer klog.Flush()
	go gc()

	config, err := daemon.ParseFlags()
	if err != nil {
		klog.Errorf("parse config failed %v", err)
		os.Exit(1)
	}

	err = daemon.InitNodeGateway(config)
	if err != nil {
		klog.Errorf("init node gateway failed %v", err)
		os.Exit(1)
	}

	stopCh := signals.SetupSignalHandler()
	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(config.KubeClient, time.Second*30)
	ctl, err := daemon.NewController(config, kubeInformerFactory)
	if err != nil {
		klog.Errorf("create controller failed %v", err)
		os.Exit(1)
	}
	kubeInformerFactory.Start(stopCh)
	go ctl.Run(stopCh)
	go func() {
		klog.Fatal(http.ListenAndServe(fmt.Sprintf("localhost:%d", config.PprofPort), nil))
	}()
	daemon.RunServer(config)
}

func gc() {
	for {
		ovs.CleanLostInterface()
		time.Sleep(60 * time.Second)
	}
}
