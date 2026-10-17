package main

import (
	"os"

	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/daemon"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func main() {
	nodeName := os.Getenv(util.EnvNodeName)
	if err := daemon.ServeNodeGatewayHelper(nodeName); err != nil {
		klog.ErrorS(err, "node gateway helper stopped")
		os.Exit(1)
	}
}
