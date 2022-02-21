package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	_ "net/http/pprof" // #nosec
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

	klog.Infof(versions.String())
	daemon.InitMetrics()
	util.InitKlogMetrics()

	nicBridgeMappings, err := daemon.InitOVSBridges()
	if err != nil {
		klog.Fatalf("failed to initialize OVS bridges: %v", err)
	}

	config, err := daemon.ParseFlags(nicBridgeMappings)
	if err != nil {
		klog.Fatalf("parse config failed %v", err)
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
	if err := mvCNIConf(); err != nil {
		klog.Fatalf("failed to mv cni conf, %v", err)
	}
	http.Handle("/metrics", promhttp.Handler())
	klog.Fatal(http.ListenAndServe(fmt.Sprintf("0.0.0.0:%d", config.PprofPort), nil))
}

func mvCNIConf() error {
	data, err := ioutil.ReadFile("/kube-ovn/01-kube-ovn.conflist")
	if err != nil {
		return err
	}
	return ioutil.WriteFile("/etc/cni/net.d/01-kube-ovn.conflist", data, 0444)
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
	chassisID, err := ioutil.ReadFile(util.ChassisLoc)
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
