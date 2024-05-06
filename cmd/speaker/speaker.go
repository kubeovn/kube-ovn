package speaker

import (
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/klog/v2"
	"k8s.io/sample-controller/pkg/signals"

	"github.com/kubeovn/kube-ovn/pkg/speaker"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/versions"
)

func CmdMain() {
	defer klog.Flush()

	klog.Infof(versions.String())
	config, err := speaker.ParseFlags()
	if err != nil {
		util.LogFatalAndExit(err, "failed to parse config")
	}

	stopCh := signals.SetupSignalHandler().Done()
	ctl := speaker.NewController(config)

	go func() {
		http.Handle("/metrics", promhttp.Handler())

		// conform to Gosec G114
		// https://github.com/securego/gosec#available-rules
		server := &http.Server{
			Addr:              fmt.Sprintf("0.0.0.0:%d", config.PprofPort),
			ReadHeaderTimeout: 3 * time.Second,
		}
		util.LogFatalAndExit(server.ListenAndServe(), "failed to listen and serve on %s", server.Addr)
	}()

	ctl.Run(stopCh)
}
