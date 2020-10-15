package main

import (
	"fmt"
	"github.com/alauda/kube-ovn/pkg/speaker"
	"github.com/alauda/kube-ovn/versions"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/klog"
	"k8s.io/sample-controller/pkg/signals"
	"net/http"
)

func main() {
	defer klog.Flush()

	klog.Infof(versions.String())
	config, err := speaker.ParseFlags()
	if err != nil {
		klog.Fatalf("failed to parse config %v", err)
	}

	stopCh := signals.SetupSignalHandler()
	ctl := speaker.NewController(config)

	go func() {
		http.Handle("/metrics", promhttp.Handler())
		klog.Fatal(http.ListenAndServe(fmt.Sprintf("0.0.0.0:%d", config.PprofPort), nil))
	}()

	ctl.Run(stopCh)
}
