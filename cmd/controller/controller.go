package main

import (
	"bitbucket.org/mathildetech/kube-ovn/pkg/controller"
	"k8s.io/klog"
	"os"
)

func main() {
	klog.SetOutput(os.Stdout)
	defer klog.Flush()

	config, err := controller.ParseFlags()
	if err != nil {
		klog.Errorf("parse config failed %v", err)
		os.Exit(1)
	}
	controller.RunServer(config)
}
