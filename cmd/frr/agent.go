package frr

import (
	"os"
	"strconv"

	"github.com/spf13/pflag"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"

	frragent "github.com/kubeovn/kube-ovn/pkg/frr"
	"github.com/kubeovn/kube-ovn/pkg/metrics"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/versions"
)

func AgentMain() {
	defer klog.Flush()
	klog.Info(versions.String())

	config, err := frragent.ParseFlags()
	if err != nil {
		util.LogFatalAndExit(err, "failed to parse config")
	}

	perm, err := strconv.ParseUint(config.LogPerm, 8, 32)
	if err != nil {
		util.LogFatalAndExit(err, "failed to parse log-perm")
	}
	util.InitLogFilePerm("kube-ovn-frr", os.FileMode(perm))

	ctrl.SetLogger(klog.NewKlogr())
	ctx := signals.SetupSignalHandler()
	go func() {
		if config.EnableMetrics {
			metrics.InitKlogMetrics()
			if err := metrics.Run(ctx, nil, util.JoinHostPort("0.0.0.0", config.PprofPort), false, false, "", "", nil); err != nil {
				util.LogFatalAndExit(err, "failed to run metrics server")
			}
		}
		<-ctx.Done()
	}()

	controller, err := frragent.NewController(config)
	if err != nil {
		util.LogFatalAndExit(err, "failed to create controller")
	}
	if err = controller.Run(ctx); err != nil {
		util.LogFatalAndExit(err, "failed to run controller")
	}
}

func InitMain() {
	defer klog.Flush()
	klog.Info(versions.String())

	fs := pflag.NewFlagSet("init", pflag.ExitOnError)
	frrDir := fs.String("frr-dir", "/etc/frr", "Directory shared with the FRR container")
	if err := fs.Parse(os.Args[2:]); err != nil {
		util.LogFatalAndExit(err, "failed to parse flags")
	}
	if err := frragent.InitFrrDir(*frrDir, os.Getenv(util.EnvNodeName)); err != nil {
		util.LogFatalAndExit(err, "failed to initialize frr directory")
	}
	klog.Infof("initialized frr directory %s", *frrDir)
}
