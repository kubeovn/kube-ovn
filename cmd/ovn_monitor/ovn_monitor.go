package ovn_monitor

import (
	"net"
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/klog/v2"

	ovn "github.com/kubeovn/kube-ovn/pkg/ovnmonitor"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/versions"
)

func CmdMain() {
	defer klog.Flush()

	klog.Infof(versions.String())
	config, err := ovn.ParseFlags()
	if err != nil {
		util.LogFatalAndExit(err, "failed to parse config")
	}

	exporter := ovn.NewExporter(config)
	if err = exporter.StartConnection(); err != nil {
		klog.Errorf("%s failed to connect db socket properly: %s", ovn.GetExporterName(), err)
		go exporter.TryClientConnection()
	}
	exporter.StartOvnMetrics()

	if config.EnableMetrics {
		http.Handle(config.MetricsPath, promhttp.Handler())
		klog.Infoln("Listening on", config.ListenAddress)
	}

	// conform to Gosec G114
	// https://github.com/securego/gosec#available-rules

	addr := config.ListenAddress
	if os.Getenv("ENABLE_BIND_LOCAL_IP") == "true" && addr == ":10661" {
		addr = net.JoinHostPort(os.Getenv("POD_IP"), "10661")
	}

	server := &http.Server{
		Addr:              addr,
		ReadHeaderTimeout: 3 * time.Second,
	}
	util.LogFatalAndExit(server.ListenAndServe(), "failed to listen and server on %s", config.ListenAddress)
}
