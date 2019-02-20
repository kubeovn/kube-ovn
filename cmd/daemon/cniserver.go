package main

import (
	"bitbucket.org/mathildetech/kube-ovn/pkg/daemon"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/klog"
	"k8s.io/sample-controller/pkg/signals"
	"os"
	"time"
)

func main() {
	klog.SetOutput(os.Stdout)
	defer klog.Flush()

	config, err := daemon.ParseFlags()
	if err != nil {
		klog.Errorf("parse config failed %v", err)
		os.Exit(1)
	}

	stopCh := signals.SetupSignalHandler()
	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(config.KubeClient, time.Second*30)
	ctl := daemon.NewController(config, kubeInformerFactory)
	kubeInformerFactory.Start(stopCh)
	go ctl.Run(stopCh)
	daemon.RunServer(config)
}
