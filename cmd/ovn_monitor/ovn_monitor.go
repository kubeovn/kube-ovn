package main

import (
	"net/http"

	ovn "github.com/alauda/kube-ovn/pkg/ovnmonitor"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/klog"
)

func main() {
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
