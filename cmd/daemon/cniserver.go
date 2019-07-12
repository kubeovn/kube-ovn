package main

import (
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"time"

	kubeovninformer "github.com/alauda/kube-ovn/pkg/client/informers/externalversions"
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
		klog.Fatalf("parse config failed %v", err)
	}

	if config.EnableMirror {
		if err = daemon.InitMirror(config); err != nil {
			klog.Fatalf("failed to init mirror nic, %v", err)
		}
	}

	if err = daemon.InitNodeGateway(config); err != nil {
		klog.Fatalf("init node gateway failed %v", err)
	}

	stopCh := signals.SetupSignalHandler()
	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(config.KubeClient, time.Second*30)
	kubeovnInformerFactory := kubeovninformer.NewSharedInformerFactoryWithOptions(config.KubeOvnClient, time.Second*30)
	ctl, err := daemon.NewController(config, kubeInformerFactory, kubeovnInformerFactory)
	if err != nil {
		klog.Fatalf("create controller failed %v", err)
	}
	kubeInformerFactory.Start(stopCh)
	kubeovnInformerFactory.Start(stopCh)
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
