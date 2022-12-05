package pinger

import (
	"fmt"
	"net/http"
	_ "net/http/pprof" // #nosec
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/pinger"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/versions"
)

func CmdMain() {
	defer klog.Flush()

	klog.Infof(versions.String())
	pinger.InitPingerMetrics()
	util.InitKlogMetrics()
	config, err := pinger.ParseFlags()
	if err != nil {
		util.LogFatalAndExit(err, "failed to parse config")
	}
	if config.Mode == "server" {
		http.Handle("/metrics", promhttp.Handler())
		go func() {
			// conform to Gosec G114
			// https://github.com/securego/gosec#available-rules
			server := &http.Server{
				Addr:              fmt.Sprintf("0.0.0.0:%d", config.Port),
				ReadHeaderTimeout: 3 * time.Second,
			}
			klog.Fatal(server.ListenAndServe())
		}()
	}
	e := pinger.NewExporter(config)
	pinger.StartPinger(config, e)
}
