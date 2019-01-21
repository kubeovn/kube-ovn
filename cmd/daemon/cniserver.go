package main

import (
	"bitbucket.org/mathildetech/kube-ovn/pkg/daemon"
	"k8s.io/klog"
	"os"
)

func main() {
	klog.SetOutput(os.Stdout)
	defer klog.Flush()

	config, err := daemon.ParseFlags()
	if err != nil {
		klog.Errorf("parse config failed %v", err)
		os.Exit(1)
	}
	daemon.RunServer(config)
}
