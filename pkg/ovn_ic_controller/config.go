package ovn_ic_controller

import (
	"flag"
	"fmt"
	"os"

	"github.com/spf13/pflag"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	clientset "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// Configuration is the controller conf
type Configuration struct {
	KubeConfigFile string
	KubeClient     kubernetes.Interface
	KubeOvnClient  clientset.Interface

	PodNamespace           string
	OvnNbAddr              string
	OvnSbAddr              string
	OvnTimeout             int
	OvsDbConnectTimeout    int
	OvsDbConnectMaxRetry   int
	OvsDbInactivityTimeout int

	NodeSwitch     string
	ClusterRouter  string
	NodeSwitchCIDR string
	LogPerm        string
}

func ParseFlags() (*Configuration, error) {
	var (
		argKubeConfigFile = pflag.String("kubeconfig", "", "Path to kubeconfig file with authorization and master location information. If not set use the inCluster token.")

		argOvnNbAddr              = pflag.String("ovn-nb-addr", "", "ovn-nb address")
		argOvnSbAddr              = pflag.String("ovn-sb-addr", "", "ovn-sb address")
		argOvnTimeout             = pflag.Int("ovn-timeout", 60, "")
		argOvsDbConTimeout        = pflag.Int("ovsdb-con-timeout", 3, "")
		argOvsDbConnectMaxRetry   = pflag.Int("ovsdb-con-maxretry", 60, "")
		argOvsDbInactivityTimeout = pflag.Int("ovsdb-inactivity-timeout", 10, "")

		argClusterRouter  = pflag.String("cluster-router", util.DefaultVpc, "The router name for cluster router")
		argNodeSwitch     = pflag.String("node-switch", "join", "The name of node gateway switch which help node to access pod network")
		argNodeSwitchCIDR = pflag.String("node-switch-cidr", "100.64.0.0/16", "The cidr for node switch")
		argLogPerm        = pflag.String("log-perm", "640", "The permission for the log file")
	)

	klogFlags := flag.NewFlagSet("klog", flag.ContinueOnError)
	klog.InitFlags(klogFlags)

	// Sync the glog and klog flags.
	pflag.CommandLine.VisitAll(func(f1 *pflag.Flag) {
		f2 := klogFlags.Lookup(f1.Name)
		if f2 != nil {
			value := f1.Value.String()
			if err := f2.Value.Set(value); err != nil {
				klog.Errorf("failed to set flag %v", err)
			}
		}
	})

	// change the behavior of cmdline
	// not exit. not good
	pflag.CommandLine.Init(os.Args[0], pflag.ContinueOnError)
	pflag.CommandLine.AddGoFlagSet(klogFlags)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)

	if err := pflag.CommandLine.Parse(os.Args[1:]); err != nil {
		return nil, err
	}

	config := &Configuration{
		KubeConfigFile: *argKubeConfigFile,

		PodNamespace:           os.Getenv(util.EnvPodNamespace),
		OvnNbAddr:              *argOvnNbAddr,
		OvnSbAddr:              *argOvnSbAddr,
		OvnTimeout:             *argOvnTimeout,
		OvsDbConnectTimeout:    *argOvsDbConTimeout,
		OvsDbConnectMaxRetry:   *argOvsDbConnectMaxRetry,
		OvsDbInactivityTimeout: *argOvsDbInactivityTimeout,

		ClusterRouter:  *argClusterRouter,
		NodeSwitch:     *argNodeSwitch,
		NodeSwitchCIDR: *argNodeSwitchCIDR,
		LogPerm:        *argLogPerm,
	}

	if err := config.initKubeClient(); err != nil {
		return nil, fmt.Errorf("failed to init kube client, %w", err)
	}

	return config, nil
}

func (config *Configuration) initKubeClient() error {
	var cfg *rest.Config
	var err error
	if config.KubeConfigFile == "" {
		klog.Infof("no --kubeconfig, use in-cluster kubernetes config")
		cfg, err = rest.InClusterConfig()
		if err != nil {
			klog.Errorf("use in cluster config failed %v", err)
			return err
		}
	} else {
		cfg, err = clientcmd.BuildConfigFromFlags("", config.KubeConfigFile)
		if err != nil {
			klog.Errorf("use --kubeconfig %s failed %v", config.KubeConfigFile, err)
			return err
		}
	}
	cfg.QPS = 1000
	cfg.Burst = 2000

	kubeOvnClient, err := clientset.NewForConfig(cfg)
	if err != nil {
		klog.Errorf("init kubeovn client failed %v", err)
		return err
	}
	config.KubeOvnClient = kubeOvnClient

	cfg.ContentType = util.ContentTypeProtobuf
	cfg.AcceptContentTypes = util.AcceptContentTypes
	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		klog.Errorf("init kubernetes client failed %v", err)
		return err
	}
	config.KubeClient = kubeClient
	return nil
}
