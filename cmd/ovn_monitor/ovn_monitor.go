package ovn_monitor

import (
	"os"
	"strings"

	"k8s.io/klog/v2"
	"kernel.org/pub/linux/libs/security/libcap/cap"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"

	"github.com/kubeovn/kube-ovn/pkg/metrics"
	ovn "github.com/kubeovn/kube-ovn/pkg/ovnmonitor"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/versions"
)

const port = 10661

func CmdMain() {
	defer klog.Flush()

	klog.Infof(versions.String())

	currentCaps := cap.GetProc()
	klog.Infof("current capabilities: %s", currentCaps.String())

	config, err := ovn.ParseFlags()
	if err != nil {
		util.LogFatalAndExit(err, "failed to parse config")
	}

	addr := config.ListenAddress
	if os.Getenv("ENABLE_BIND_LOCAL_IP") == "true" {
		if ips := strings.Split(os.Getenv("POD_IPS"), ","); len(ips) == 1 {
			addr = util.JoinHostPort(ips[0], port)
		}
	}

	exporter := ovn.NewExporter(config)
	if err = exporter.StartConnection(); err != nil {
		klog.Errorf("%s failed to connect db socket properly: %s", ovn.GetExporterName(), err)
		go exporter.TryClientConnection()
	}
	exporter.StartOvnMetrics()

	ctx := signals.SetupSignalHandler()
	if err = metrics.Run(ctx, nil, addr, config.SecureServing); err != nil {
		util.LogFatalAndExit(err, "failed to run metrics server")
	}
	<-ctx.Done()
}
