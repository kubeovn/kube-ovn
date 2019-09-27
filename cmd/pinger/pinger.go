package main

import (
	"fmt"
	"github.com/alauda/kube-ovn/pkg/pinger"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/klog"
	"net/http"
)

func main() {
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
	pinger.StartPinger(config)
}
