package ovn_monitor

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/klog"

	ovn "github.com/kubeovn/kube-ovn/pkg/ovnmonitor"
	"github.com/kubeovn/kube-ovn/versions"
)

func CmdMain() {
	defer klog.Flush()

	klog.Infof(versions.String())
	config, err := ovn.ParseFlags()
	if err != nil {
		klog.Fatalf("parse config failed %v", err)
	}

	exporter := ovn.NewExporter(config)
	if err = exporter.StartConnection(); err != nil {
		klog.Errorf("%s failed to connect db socket properly: %s", ovn.GetExporterName(), err)
		go exporter.TryClientConnection()
	}
	exporter.StartOvnMetrics()

	http.Handle(config.MetricsPath, promhttp.Handler())
	klog.Infoln("Listening on", config.ListenAddress)
	klog.Fatal(http.ListenAndServe(config.ListenAddress, nil))
}
