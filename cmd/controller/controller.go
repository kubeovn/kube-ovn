package main

import (
	"bitbucket.org/mathildetech/kube-ovn/pkg/controller"
	"time"

	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/klog"
	"k8s.io/sample-controller/pkg/signals"
	"os"
)

func main() {
	klog.SetOutput(os.Stdout)
	defer klog.Flush()

	stopCh := signals.SetupSignalHandler()

	config, err := controller.ParseFlags()
	if err != nil {
		klog.Errorf("parse config failed %v", err)
		os.Exit(1)
	}

	err = controller.InitDefaultLogicalSwitch(config)
	if err != nil {
		klog.Errorf("init default switch failed %v", err)
		os.Exit(1)
	}

	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(config.KubeClient, time.Second*30)
	ctl := controller.NewController(config, kubeInformerFactory)
	kubeInformerFactory.Start(stopCh)
	ctl.Run(stopCh)
}
