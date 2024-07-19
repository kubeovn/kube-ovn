package ovn_monitor

import (
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/klog/v2"

	ovn "github.com/kubeovn/kube-ovn/pkg/ovnmonitor"
	"github.com/kubeovn/kube-ovn/pkg/server"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/versions"
)

const svcName = "kube-ovn-monitor"

const port = 10661

func CmdMain() {
	defer klog.Flush()

	klog.Infof(versions.String())
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
	mux := http.NewServeMux()
	if config.EnableMetrics {
		mux.Handle(config.MetricsPath, promhttp.Handler())
		klog.Infoln("Listening on", addr)
	}

	if !config.SecureServing {
		server := &http.Server{
			Addr:              addr,
			ReadHeaderTimeout: 3 * time.Second,
			Handler:           mux,
		}
		util.LogFatalAndExit(server.ListenAndServe(), "failed to listen and server on %s", addr)
	} else {
		ch, err := server.SecureServing(addr, svcName, mux)
		if err != nil {
			util.LogFatalAndExit(err, "failed to serve on %s", addr)
		}
		<-ch
	}
}
