package ovn_central_controller

import (
	"github.com/kubeovn/kube-ovn/pkg/ovn_central_controller"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func CmdMain() {
	if err := ovn_central_controller.Run(); err != nil {
		util.LogFatalAndExit(err, "ovn-central-controller failed")
	}
}
