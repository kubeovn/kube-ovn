package speaker

import (
	"k8s.io/klog/v2"
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
