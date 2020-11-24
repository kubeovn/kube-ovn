package main

import (
	"fmt"
	"io/ioutil"
	"net/http"
	_ "net/http/pprof" // #nosec

	"github.com/alauda/kube-ovn/pkg/util"
	"github.com/alauda/kube-ovn/versions"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovninformer "github.com/alauda/kube-ovn/pkg/client/informers/externalversions"
	"github.com/alauda/kube-ovn/pkg/daemon"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/klog"
	"k8s.io/sample-controller/pkg/signals"
)

func main() {
	defer klog.Flush()

	klog.Infof(versions.String())
	config, err := daemon.ParseFlags()
	if err != nil {
		klog.Fatalf("parse config failed %v", err)
	}

	if err = daemon.InitMirror(config); err != nil {
		klog.Fatalf("failed to init mirror nic, %v", err)
	}

	if err = daemon.InitNodeGateway(config); err != nil {
		klog.Fatalf("init node gateway failed %v", err)
	}

	if config.NetworkType == util.NetworkTypeVlan {
		if err = daemon.InitVlan(config); err != nil {
			klog.Fatalf("init vlan config failed %v", err)
		}
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
