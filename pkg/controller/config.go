package controller

import (
	"flag"
	"os"

	clientset "github.com/alauda/kube-ovn/pkg/client/clientset/versioned"
	"github.com/alauda/kube-ovn/pkg/util"
	"github.com/spf13/pflag"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"
)

// Configuration is the controller conf
type Configuration struct {
	BindAddress    string
	OvnNbSocket    string
	OvnNbHost      string
	OvnNbPort      int
	OvnSbHost      string
	OvnSbPort      int
	OvnTimeout     int
	KubeConfigFile string
	KubeClient     kubernetes.Interface
	KubeOvnClient  clientset.Interface

	DefaultLogicalSwitch string
	DefaultCIDR          string
	DefaultGateway       string
	DefaultExcludeIps    string

	ClusterRouter     string
	NodeSwitch        string
	NodeSwitchCIDR    string
	NodeSwitchGateway string

	ClusterTcpLoadBalancer string
	ClusterUdpLoadBalancer string

	PodName      string
	PodNamespace string

	WorkerNum int
	PprofPort int

	NetworkType          string
	DefaultProviderName  string
	DefaultHostInterface string
	DefaultVlanName      string
	DefaultVlanRange     string
	DefaultVlanID        int
}

// ParseFlags parses cmd args then init kubeclient and conf
// TODO: validate configuration
func ParseFlags() (*Configuration, error) {
	var (
		argOvnNbSocket    = pflag.String("ovn-nb-socket", "", "The ovn-nb socket file. (If not set use ovn-nb-address)")
		argOvnNbHost      = pflag.String("ovn-nb-host", "0.0.0.0", "The ovn-nb host address. (If not set use ovn-nb-socket)")
		argOvnNbPort      = pflag.Int("ovn-nb-port", 6641, "")
		argOvnSbHost      = pflag.String("ovn-sb-host", "0.0.0.0", "The ovn-sb host address.")
		argOvnSbPort      = pflag.Int("ovn-sb-port", 6642, "")
		argOvnTimeout     = pflag.Int("ovn-timeout", 30, "")
		argKubeConfigFile = pflag.String("kubeconfig", "", "Path to kubeconfig file with authorization and master location information. If not set use the inCluster token.")

		argDefaultLogicalSwitch = pflag.String("default-ls", "ovn-default", "The default logical switch name, default: ovn-default")
		argDefaultCIDR          = pflag.String("default-cidr", "10.16.0.0/16", "Default CIDR for namespace with no logical switch annotation, default: 10.16.0.0/16")
		argDefaultGateway       = pflag.String("default-gateway", "", "Default gateway for default-cidr, default the first ip in default-cidr")
		argDefaultExcludeIps    = pflag.String("default-exclude-ips", "", "Exclude ips in default switch, default equals to gateway address")

		argClusterRouter     = pflag.String("cluster-router", "ovn-cluster", "The router name for cluster router, default: ovn-cluster")
		argNodeSwitch        = pflag.String("node-switch", "join", "The name of node gateway switch which help node to access pod network, default: join")
		argNodeSwitchCIDR    = pflag.String("node-switch-cidr", "100.64.0.0/16", "The cidr for node switch, default: 100.64.0.0/16")
		argNodeSwitchGateway = pflag.String("node-switch-gateway", "", "The gateway for node switch, default the first ip in node-switch-cidr")

		argClusterTcpLoadBalancer = pflag.String("cluster-tcp-loadbalancer", "cluster-tcp-loadbalancer", "The name for cluster tcp loadbalancer")
		argClusterUdpLoadBalancer = pflag.String("cluster-udp-loadbalancer", "cluster-udp-loadbalancer", "The name for cluster udp loadbalancer")

		argWorkerNum = pflag.Int("worker-num", 3, "The parallelism of each worker, default: 3")
		argPprofPort = pflag.Int("pprof-port", 10660, "The port to get profiling data, default 10660")

		argsNetworkType          = pflag.String("network-type", "geneve", "The ovn network type, default: geneve")
		argsDefaultProviderName  = pflag.String("default-provider-name", "provider", "The vlan or xvlan type default provider interface name, default: provider")
		argsDefaultInterfaceName = pflag.String("default-interface-name", "", "The default host interface name in the vlan/xvlan type")
		argsDefaultVlanName      = pflag.String("default-vlan-name", "ovn-vlan", "The default vlan name, default: ovn-vlan")
		argsDefaultVlanID        = pflag.Int("default-vlan-id", 1, "The default vlan id, default: 1")
		argsDefaultVlanRange     = pflag.String("default-vlan-range", "1,4095", "The default vlan range, default: 1-4095")
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

	pflag.CommandLine.AddGoFlagSet(klogFlags)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.Parse()

	config := &Configuration{
		OvnNbSocket:            *argOvnNbSocket,
		OvnNbHost:              *argOvnNbHost,
		OvnNbPort:              *argOvnNbPort,
		OvnSbHost:              *argOvnSbHost,
		OvnSbPort:              *argOvnSbPort,
		OvnTimeout:             *argOvnTimeout,
		KubeConfigFile:         *argKubeConfigFile,
		DefaultLogicalSwitch:   *argDefaultLogicalSwitch,
		DefaultCIDR:            *argDefaultCIDR,
		DefaultGateway:         *argDefaultGateway,
		DefaultExcludeIps:      *argDefaultExcludeIps,
		ClusterRouter:          *argClusterRouter,
		NodeSwitch:             *argNodeSwitch,
		NodeSwitchCIDR:         *argNodeSwitchCIDR,
		NodeSwitchGateway:      *argNodeSwitchGateway,
		ClusterTcpLoadBalancer: *argClusterTcpLoadBalancer,
		ClusterUdpLoadBalancer: *argClusterUdpLoadBalancer,
		WorkerNum:              *argWorkerNum,
		PprofPort:              *argPprofPort,
		NetworkType:            *argsNetworkType,
		DefaultVlanID:          *argsDefaultVlanID,
		DefaultProviderName:    *argsDefaultProviderName,
		DefaultHostInterface:   *argsDefaultInterfaceName,
		DefaultVlanName:        *argsDefaultVlanName,
		DefaultVlanRange:       *argsDefaultVlanRange,
		PodName:                os.Getenv("POD_NAME"),
		PodNamespace:           os.Getenv("KUBE_NAMESPACE"),
	}

	if config.DefaultGateway == "" {
		gw, err := util.FirstSubnetIP(config.DefaultCIDR)
		if err != nil {
			return nil, err
		}
		config.DefaultGateway = gw
	}

	if config.DefaultExcludeIps == "" {
		config.DefaultExcludeIps = config.DefaultGateway
	}

	if config.NodeSwitchGateway == "" {
		gw, err := util.FirstSubnetIP(config.NodeSwitchCIDR)
		if err != nil {
			return nil, err
		}
		config.NodeSwitchGateway = gw
	}

	if err := config.initKubeClient(); err != nil {
		return nil, err
	}

	klog.Infof("config is  %+v", config)
	return config, nil
}

func (config *Configuration) initKubeClient() error {
	var cfg *rest.Config
	var err error
	if config.KubeConfigFile == "" {
		klog.Infof("no --kubeconfig, use in-cluster kubernetes config")
		cfg, err = rest.InClusterConfig()
	} else {
		cfg, err = clientcmd.BuildConfigFromFlags("", config.KubeConfigFile)
	}
	if err != nil {
		klog.Errorf("failed to build kubeconfig %v", err)
		return err
	}
	cfg.QPS = 1000
	cfg.Burst = 2000

	kubeOvnClient, err := clientset.NewForConfig(cfg)
	if err != nil {
		klog.Errorf("init kubeovn client failed %v", err)
		return err
	}
	config.KubeOvnClient = kubeOvnClient

	cfg.ContentType = "application/vnd.kubernetes.protobuf"
	cfg.AcceptContentTypes = "application/vnd.kubernetes.protobuf,application/json"
	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		klog.Errorf("init kubernetes client failed %v", err)
		return err
	}
	config.KubeClient = kubeClient
	return nil
}
