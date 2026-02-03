package speaker

import (
	"os"
	"strconv"

	"k8s.io/klog/v2"
	"kernel.org/pub/linux/libs/security/libcap/cap"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"

	"github.com/kubeovn/kube-ovn/pkg/metrics"
	"github.com/kubeovn/kube-ovn/pkg/speaker"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/versions"
)

func CmdMain() {
	defer klog.Flush()
	klog.Info(versions.String())

	currentCaps := cap.GetProc()
	klog.Infof("current capabilities: %s", currentCaps.String())

	config, err := speaker.ParseFlags()
	if err != nil {
		util.LogFatalAndExit(err, "failed to parse config")
	}

	// Do not try to redirect the logs on the node if we're running in a NAT gateway
	if !config.NatGwMode {
		logFilePerm, err := strconv.ParseUint(config.LogPerm, 8, 32)
		if err != nil {
			util.LogFatalAndExit(err, "failed to parse log-perm")
		}
		util.InitLogFilePerm("kube-ovn-speaker", os.FileMode(logFilePerm))
	}

	ctrl.SetLogger(klog.NewKlogr())
	ctx := signals.SetupSignalHandler()
	go func() {
		if config.EnableMetrics {
			metrics.InitKlogMetrics()
			if err = metrics.Run(ctx, nil, util.JoinHostPort("0.0.0.0", config.PprofPort), false, false, "", "", nil); err != nil {
				util.LogFatalAndExit(err, "failed to run metrics server")
			}
		}
		<-ctx.Done()
	}()

	speaker.NewController(config).Run(ctx.Done())
}
