package speaker

import (
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/klog/v2"
	"k8s.io/sample-controller/pkg/signals"

	"github.com/kubeovn/kube-ovn/pkg/speaker"
	"github.com/kubeovn/kube-ovn/versions"
)

func CmdMain() {
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
