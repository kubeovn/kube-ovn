package ovn_ic_controller

import (
	"k8s.io/klog/v2"
	"k8s.io/sample-controller/pkg/signals"

	"github.com/kubeovn/kube-ovn/pkg/ovn_ic_controller"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/versions"
)

func CmdMain() {
	defer klog.Flush()

	klog.Infof(versions.String())
	config, err := ovn_ic_controller.ParseFlags()
	if err != nil {
		util.LogFatalAndExit(err, "failed to parse config")
	}

	stopCh := signals.SetupSignalHandler().Done()
	ctl := ovn_ic_controller.NewController(config)
	ctl.Run(stopCh)
}
