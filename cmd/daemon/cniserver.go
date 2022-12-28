package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/pprof"
	"os"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/klog/v2"
	"k8s.io/sample-controller/pkg/signals"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovninformer "github.com/kubeovn/kube-ovn/pkg/client/informers/externalversions"
	"github.com/kubeovn/kube-ovn/pkg/daemon"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/versions"
)

func CmdMain() {
	defer klog.Flush()

	klog.Infof(versions.String())
	daemon.InitMetrics()
	util.InitKlogMetrics()

	nicBridgeMappings, err := daemon.InitOVSBridges()
	if err != nil {
		util.LogFatalAndExit(err, "failed to initialize OVS bridges")
	}

	config, err := daemon.ParseFlags(nicBridgeMappings)
	if err != nil {
		util.LogFatalAndExit(err, "failed to parse config")
	}

	if err := Retry(util.ChasRetryTime, util.ChasRetryIntev, initChassisAnno, config); err != nil {
		util.LogFatalAndExit(err, "failed to initialize ovn chassis annotation")
	}

	if err = daemon.InitMirror(config); err != nil {
		util.LogFatalAndExit(err, "failed to initialize ovs mirror")
	}

	if err = daemon.InitNodeGateway(config); err != nil {
		util.LogFatalAndExit(err, "failed to initialize node gateway")
	}

	stopCh := signals.SetupSignalHandler()
	podInformerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(config.KubeClient, 0,
		kubeinformers.WithTweakListOptions(func(listOption *v1.ListOptions) {
			listOption.FieldSelector = fmt.Sprintf("spec.nodeName=%s", config.NodeName)
			listOption.AllowWatchBookmarks = true
		}))
	nodeInformerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(config.KubeClient, 0,
		kubeinformers.WithTweakListOptions(func(listOption *v1.ListOptions) {
			listOption.AllowWatchBookmarks = true
		}))
	kubeovnInformerFactory := kubeovninformer.NewSharedInformerFactoryWithOptions(config.KubeOvnClient, 0,
		kubeovninformer.WithTweakListOptions(func(listOption *v1.ListOptions) {
			listOption.AllowWatchBookmarks = true
		}))
	ctl, err := daemon.NewController(config, podInformerFactory, nodeInformerFactory, kubeovnInformerFactory)
	if err != nil {
		util.LogFatalAndExit(err, "failed to create controller")
	}
	podInformerFactory.Start(stopCh)
	nodeInformerFactory.Start(stopCh)
	kubeovnInformerFactory.Start(stopCh)
	go ctl.Run(stopCh)
	go daemon.RunServer(config, ctl)
	if err := mvCNIConf(); err != nil {
		util.LogFatalAndExit(err, "failed to mv cni config")
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	if config.EnablePprof {
		mux.HandleFunc("/debug/pprof/", pprof.Index)
		mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	}

	addr := "0.0.0.0"
	if os.Getenv("ENABLE_BIND_LOCAL_IP") == "true" {
		podIpsEnv := os.Getenv("POD_IPS")
		podIps := strings.Split(podIpsEnv, ",")
		// when pod in dual mode, golang can't support bind v4 and v6 address in the same time,
		// so not support bind local ip when in dual mode
		if len(podIps) == 1 {
			addr = podIps[0]
			if util.CheckProtocol(podIps[0]) == kubeovnv1.ProtocolIPv6 {
				addr = fmt.Sprintf("[%s]", podIps[0])
			}
		}
	}
	// conform to Gosec G114
	// https://github.com/securego/gosec#available-rules
	server := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", addr, config.PprofPort),
		ReadHeaderTimeout: 3 * time.Second,
		Handler:           mux,
	}
	klog.Fatal(server.ListenAndServe())
}

func mvCNIConf() error {
	data, err := os.ReadFile("/kube-ovn/01-kube-ovn.conflist")
	if err != nil {
		return err
	}
	return os.WriteFile("/etc/cni/net.d/01-kube-ovn.conflist", data, 0444)
}

func Retry(attempts int, sleep int, f func(configuration *daemon.Configuration) error, ctrl *daemon.Configuration) (err error) {
	for i := 0; ; i++ {
		err = f(ctrl)
		if err == nil {
			return
		}
		if i >= (attempts - 1) {
			break
		}
		time.Sleep(time.Duration(sleep) * time.Second)
	}
	return err
}

func initChassisAnno(cfg *daemon.Configuration) error {
	chassisID, err := os.ReadFile(util.ChassisLoc)
	if err != nil {
		klog.Errorf("read chassis file failed, %v", err)
		return err
	}

	hostname := cfg.NodeName
	node, err := cfg.KubeClient.CoreV1().Nodes().Get(context.Background(), hostname, v1.GetOptions{})
	if err != nil {
		klog.Errorf("failed to get node %s %v", hostname, err)
		return err
	}

	chassistr := string(chassisID)
	node.Annotations[util.ChassisAnnotation] = strings.TrimSpace(chassistr)
	patchPayloadTemplate :=
		`[{
        "op": "%s",
        "path": "/metadata/annotations",
        "value": %s
    }]`
	op := "add"
	raw, _ := json.Marshal(node.Annotations)
	patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)
	_, err = cfg.KubeClient.CoreV1().Nodes().Patch(context.Background(), hostname, types.JSONPatchType, []byte(patchPayload), v1.PatchOptions{}, "")
	if err != nil {
		klog.Errorf("patch node %s failed %v", hostname, err)
		return err
	}
	klog.Infof("finish adding chassis annotation")
	return nil
}
