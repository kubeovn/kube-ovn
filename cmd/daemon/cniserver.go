package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/pprof"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/klog/v2"
	"k8s.io/sample-controller/pkg/signals"

	kubeovninformer "github.com/kubeovn/kube-ovn/pkg/client/informers/externalversions"
	"github.com/kubeovn/kube-ovn/pkg/daemon"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/versions"
)

func CmdMain() {
	defer klog.Flush()

	daemon.InitMetrics()
	util.InitKlogMetrics()

	config := daemon.ParseFlags()
	klog.Infof(versions.String())

	if err := initForOS(); err != nil {
		klog.Fatal(err)
	}

	nicBridgeMappings, err := daemon.InitOVSBridges()
	if err != nil {
		klog.Fatalf("failed to initialize OVS bridges: %v", err)
	}

	if err = config.Init(nicBridgeMappings); err != nil {
		klog.Fatalf("failed to initialize config: %v", err)
	}

	if err := Retry(util.ChasRetryTime, util.ChasRetryIntev, initChassisAnno, config); err != nil {
		klog.Fatalf("failed to annotate chassis id, %v", err)
	}

	if err = daemon.InitMirror(config); err != nil {
		klog.Fatalf("failed to init mirror nic, %v", err)
	}

	if err = daemon.InitNodeGateway(config); err != nil {
		klog.Fatalf("init node gateway failed %v", err)
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
		klog.Fatalf("create controller failed %v", err)
	}
	podInformerFactory.Start(stopCh)
	nodeInformerFactory.Start(stopCh)
	kubeovnInformerFactory.Start(stopCh)
	go ctl.Run(stopCh)
	go daemon.RunServer(config, ctl)
	if err := mvCNIConf(config.CniConfDir, config.CniConfFile, config.CniConfName); err != nil {
		klog.Fatalf("failed to mv cni conf, %v", err)
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

	// conform to Gosec G114
	// https://github.com/securego/gosec#available-rules
	server := &http.Server{
		Addr:              fmt.Sprintf("0.0.0.0:%d", config.PprofPort),
		ReadHeaderTimeout: 3 * time.Second,
		Handler:           mux,
	}
	klog.Fatal(server.ListenAndServe())
}

func mvCNIConf(configDir, configFile, confName string) error {
	// #nosec
	src, err := os.Open(configFile)
	if err != nil {
		return err
	}
	// conform to Gosec G304 ,Potential file inclusion via variable
	// https://github.com/securego/gosec#available-rules
	cfName := filepath.Clean(confName)
	cniConfPath := filepath.Join(configDir, cfName)
	// conform to Gosec G302  Poor file permissions used with chmod
	dst, err := os.OpenFile(cniConfPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	if _, err = io.Copy(dst, src); err != nil {
		return err
	}
	return nil
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
