package ovn_ic_controller

import (
	"os"
	"strconv"

	"k8s.io/klog/v2"
	"kernel.org/pub/linux/libs/security/libcap/cap"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"

	"github.com/kubeovn/kube-ovn/pkg/ovn_ic_controller"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/versions"
)

func CmdMain() {
	defer klog.Flush()

	klog.Info(versions.String())

	currentCaps := cap.GetProc()
	klog.Infof("current capabilities: %s", currentCaps.String())

	config, err := ovn_ic_controller.ParseFlags()
	if err != nil {
		util.LogFatalAndExit(err, "failed to parse config")
	}

	logFilePerm, err := strconv.ParseUint(config.LogPerm, 8, 32)
	if err != nil {
		util.LogFatalAndExit(err, "failed to parse log-perm")
	}
	util.InitLogFilePerm("kube-ovn-ic-controller", os.FileMode(logFilePerm))

	stopCh := signals.SetupSignalHandler().Done()
	ctl := ovn_ic_controller.NewController(config)
	ctl.Run(stopCh)
}
