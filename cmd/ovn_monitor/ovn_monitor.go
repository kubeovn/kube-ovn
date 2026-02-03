package ovn_monitor

import (
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"k8s.io/klog/v2"
	"kernel.org/pub/linux/libs/security/libcap/cap"
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

	currentCaps := cap.GetProc()
	klog.Infof("current capabilities: %s", currentCaps.String())

	config, err := ovn.ParseFlags()
	if err != nil {
		util.LogFatalAndExit(err, "failed to parse config")
	}

	logFilePerm, err := strconv.ParseUint(config.LogPerm, 8, 32)
	if err != nil {
		util.LogFatalAndExit(err, "failed to parse log-perm")
	}
	util.InitLogFilePerm("kube-ovn-monitor", os.FileMode(logFilePerm))

	ctrl.SetLogger(klog.NewKlogr())
	ctx := signals.SetupSignalHandler()

	metricsAddrs := util.GetDefaultListenAddr()
	if config.EnableMetrics {
		exporter := ovn.NewExporter(config)
		if err = exporter.StartConnection(); err != nil {
			klog.Errorf("%s failed to connect db socket properly: %s", ovn.GetExporterName(), err)
			go exporter.TryClientConnection()
		}
		exporter.StartOvnMetrics()
		for _, metricsAddr := range metricsAddrs {
			addr := util.JoinHostPort(metricsAddr, config.MetricsPort)
			go func() {
				if err := metrics.Run(ctx, nil, addr, config.SecureServing, false, config.TLSMinVersion, config.TLSMaxVersion, config.TLSCipherSuites); err != nil {
					util.LogFatalAndExit(err, "failed to run metrics server")
				}
			}()
		}
	} else {
		klog.Info("metrics server is disabled")
		listerner, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.ParseIP(metricsAddrs[0]), Port: int(config.MetricsPort)})
		if err != nil {
			util.LogFatalAndExit(err, "failed to listen on %s", util.JoinHostPort(metricsAddrs[0], config.MetricsPort))
		}
		mux := http.NewServeMux()
		mux.HandleFunc("/healthz", util.DefaultHealthCheckHandler)
		mux.HandleFunc("/livez", util.DefaultHealthCheckHandler)
		mux.HandleFunc("/readyz", util.DefaultHealthCheckHandler)
		svr := manager.Server{
			Name: "health-check",
			Server: &http.Server{
				Handler:           mux,
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
