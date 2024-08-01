package main

import (
	_ "net/http/pprof" // #nosec

	"k8s.io/klog/v2"
	"kernel.org/pub/linux/libs/security/libcap/cap"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"

	"github.com/kubeovn/kube-ovn/pkg/metrics"
	"github.com/kubeovn/kube-ovn/pkg/pinger"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/versions"
)

func main() {
	defer klog.Flush()

	klog.Infof(versions.String())

	currentCaps := cap.GetProc()
	klog.Infof("current capabilities: %s", currentCaps.String())

	config, err := pinger.ParseFlags()
	if err != nil {
		util.LogFatalAndExit(err, "failed to parse config")
	}

	ctx := signals.SetupSignalHandler()
	if config.Mode == "server" {
		if config.EnableMetrics {
			go func() {
				pinger.InitPingerMetrics()
				metrics.InitKlogMetrics()
				if err := metrics.Run(ctx, nil, util.JoinHostPort("0.0.0.0", config.Port), false); err != nil {
					util.LogFatalAndExit(err, "failed to run metrics server")
				}
				<-ctx.Done()
			}()
		}

		if config.EnableVerboseConnCheck {
			addr := util.JoinHostPort("0.0.0.0", config.UDPConnCheckPort)
			if err = util.UDPConnectivityListen(addr); err != nil {
				util.LogFatalAndExit(err, "failed to start UDP listen on addr %s", addr)
			}

			addr = util.JoinHostPort("0.0.0.0", config.TCPConnCheckPort)
			if err = util.TCPConnectivityListen(addr); err != nil {
				util.LogFatalAndExit(err, "failed to start TCP listen on addr %s", addr)
			}
		}
	}
	pinger.StartPinger(config, ctx.Done())
}
