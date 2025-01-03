package ovn_monitor

import (
	"net"
	"net/http"
	"time"

	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"

	"github.com/kubeovn/kube-ovn/pkg/metrics"
	ovn "github.com/kubeovn/kube-ovn/pkg/ovnmonitor"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/versions"
)

func CmdMain() {
	defer klog.Flush()

	klog.Info(versions.String())

	config, err := ovn.ParseFlags()
	if err != nil {
		util.LogFatalAndExit(err, "failed to parse config")
	}

	ctrl.SetLogger(klog.NewKlogr())
	ctx := signals.SetupSignalHandler()

	metricsAddr := util.GetDefaultListenAddr()
	if config.EnableMetrics {
		exporter := ovn.NewExporter(config)
		if err = exporter.StartConnection(); err != nil {
			klog.Errorf("%s failed to connect db socket properly: %s", ovn.GetExporterName(), err)
			go exporter.TryClientConnection()
		}
		exporter.StartOvnMetrics()
		addr := util.JoinHostPort(metricsAddr, config.MetricsPort)
		if err = metrics.Run(ctx, nil, addr, config.SecureServing, false); err != nil {
			util.LogFatalAndExit(err, "failed to run metrics server")
		}
	} else {
		klog.Info("metrics server is disabled")
		listerner, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.ParseIP(util.GetDefaultListenAddr()), Port: int(config.MetricsPort)})
		if err != nil {
			util.LogFatalAndExit(err, "failed to listen on %s", util.JoinHostPort(metricsAddr, config.MetricsPort))
		}
		svr := manager.Server{
			Name: "health-check",
			Server: &http.Server{
				Handler:           http.NewServeMux(),
				MaxHeaderBytes:    1 << 20,
				IdleTimeout:       90 * time.Second,
				ReadHeaderTimeout: 32 * time.Second,
			},
			Listener: listerner,
		}
		go func() {
			if err = svr.Start(ctx); err != nil {
				util.LogFatalAndExit(err, "failed to run health check server")
			}
		}()
	}

	<-ctx.Done()
}
