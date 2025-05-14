package speaker

import (
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
	util.InitLogFile("kube-ovn-speaker")
	klog.Info(versions.String())

	currentCaps := cap.GetProc()
	klog.Infof("current capabilities: %s", currentCaps.String())

	config, err := speaker.ParseFlags()
	if err != nil {
		util.LogFatalAndExit(err, "failed to parse config")
	}

	ctrl.SetLogger(klog.NewKlogr())
	ctx := signals.SetupSignalHandler()
	go func() {
		if config.EnableMetrics {
			metrics.InitKlogMetrics()
			if err = metrics.Run(ctx, nil, util.JoinHostPort("0.0.0.0", config.PprofPort), false, false); err != nil {
				util.LogFatalAndExit(err, "failed to run metrics server")
			}
		}
		<-ctx.Done()
	}()

	speaker.NewController(config).Run(ctx.Done())
}
