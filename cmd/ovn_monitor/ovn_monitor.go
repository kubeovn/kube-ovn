package ovn_monitor

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
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
	mux := http.NewServeMux()
	if config.EnableMetrics {
		mux.Handle(config.MetricsPath, promhttp.Handler())
		klog.Infoln("Listening on", config.ListenAddress)
	}

	// conform to Gosec G114
	// https://github.com/securego/gosec#available-rules

	addr := config.ListenAddress
	if os.Getenv("ENABLE_BIND_LOCAL_IP") == "true" {
		podIpsEnv := os.Getenv("POD_IPS")
		podIps := strings.Split(podIpsEnv, ",")
		// when pod in dual mode, golang can't support bind v4 and v6 address in the same time,
		// so not support bind local ip when in dual mode
		if len(podIps) == 1 {
			addr = fmt.Sprintf("%s:10661", podIps[0])
			if util.CheckProtocol(podIps[0]) == kubeovnv1.ProtocolIPv6 {
				addr = fmt.Sprintf("[%s]:10661", podIps[0])
			}
		}
	}

	server := &http.Server{
		Addr:              addr,
		ReadHeaderTimeout: 3 * time.Second,
		Handler:           mux,
	}
	util.LogFatalAndExit(server.ListenAndServe(), "failed to listen and server on %s", config.ListenAddress)
}
