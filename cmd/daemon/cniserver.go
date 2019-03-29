package main

import (
	"os"
	"time"

	"bitbucket.org/mathildetech/kube-ovn/pkg/daemon"
	"bitbucket.org/mathildetech/kube-ovn/pkg/ovs"
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

	stopCh := signals.SetupSignalHandler()
	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(config.KubeClient, time.Second*30)
	ctl, err := daemon.NewController(config, kubeInformerFactory)
	if err != nil {
		klog.Errorf("create controller failed %v", err)
		os.Exit(1)
	}
	kubeInformerFactory.Start(stopCh)
	go ctl.Run(stopCh)
	daemon.RunServer(config)
}

func gc() {
	for {
		ovs.CleanLostInterface()
		time.Sleep(60 * time.Second)
	}
}
