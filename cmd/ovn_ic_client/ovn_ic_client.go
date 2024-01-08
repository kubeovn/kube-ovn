package ovn_ic_client

import (
	"k8s.io/klog/v2"
	"k8s.io/sample-controller/pkg/signals"

	"github.com/kubeovn/kube-ovn/pkg/ovn_ic_client"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/versions"
)

func CmdMain() {
	defer klog.Flush()

	klog.Infof(versions.String())
	config, err := ovn_ic_client.ParseFlags()
	if err != nil {
		util.LogFatalAndExit(err, "failed to parse config")
	}

	stopCh := signals.SetupSignalHandler()
	ctl := ovn_ic_client.NewController(config)
	ctl.Run(stopCh)
}
