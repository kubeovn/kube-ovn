package pinger

import (
	"fmt"
	"github.com/alauda/kube-ovn/versions"
	"net/http"
	_ "net/http/pprof" // #nosec

	"github.com/alauda/kube-ovn/pkg/pinger"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/klog"
)

func CmdMain() {
	defer klog.Flush()

	klog.Infof(versions.String())
	pinger.InitPingerMetrics()
	config, err := pinger.ParseFlags()
	if err != nil {
		klog.Fatalf("parse config failed %v", err)
	}
	if config.Mode == "server" {
		http.Handle("/metrics", promhttp.Handler())
		go func() {
			klog.Fatal(http.ListenAndServe(fmt.Sprintf("0.0.0.0:%d", config.Port), nil))
		}()
	}
	e := pinger.NewExporter(config)
	pinger.StartPinger(config, e)
}
