package controller

import (
	"flag"
	"github.com/oilbeater/libovsdb"
	"github.com/spf13/pflag"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"
)

type Configuration struct {
	BindAddress    string
	OvnNbSocket    string
	OvnNbHost      string
	OvnNbPort      int
	KubeConfigFile string
	KubeClient     kubernetes.Interface
	OvnClient      *libovsdb.OvsdbClient
}

// TODO: validate configuration
func ParseFlags() (*Configuration, error) {
	var (
		argOvnNbSocket    = pflag.String("ovn-nb-socket", "", "The ovn-nb socket file. (If not set use ovn-nb-address)")
		argOvnNbHost      = pflag.String("ovn-nb-host", "0.0.0.0", "The ovn-nb host address. (If not set use ovn-nb-socket)")
		argOvnNbPort      = pflag.Int("ovn-nb-port", 6641, "")
		argKubeConfigFile = pflag.String("kubeconfig", "", "Path to kubeconfig file with authorization and master location information. If not set use the inCluster token.")
	)

	flag.Set("alsologtostderr", "true")
	klogFlags := flag.NewFlagSet("klog", flag.ExitOnError)
	klog.InitFlags(klogFlags)

	// Sync the glog and klog flags.
	flag.CommandLine.VisitAll(func(f1 *flag.Flag) {
		f2 := klogFlags.Lookup(f1.Name)
		if f2 != nil {
			value := f1.Value.String()
			f2.Value.Set(value)
		}
	})

	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.Parse()
	config := &Configuration{
		OvnNbSocket:    *argOvnNbSocket,
		OvnNbHost:      *argOvnNbHost,
		OvnNbPort:      *argOvnNbPort,
		KubeConfigFile: *argKubeConfigFile,
	}
	err := config.initKubeClient()
	if err != nil {
		return nil, err
	}
	err = config.initOvnClient()
	if err != nil {
		return nil, err
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
	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		klog.Errorf("init kubernetes client failed %v", err)
		return err
	}

	config.KubeClient = kubeClient
	return nil
}

func (config *Configuration) initOvnClient() error {
	var ovs *libovsdb.OvsdbClient
	var err error
	if config.OvnNbSocket != "" {
		ovs, err = libovsdb.ConnectWithUnixSocket(config.OvnNbSocket)
		if err != nil {
			return err
		}
	} else {
		ovs, err = libovsdb.Connect(config.OvnNbHost, config.OvnNbPort)
		if err != nil {
			return err
		}
	}
	config.OvnClient = ovs
	return nil
}
