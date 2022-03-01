package controller

import (
	"flag"
	"fmt"
	"os"

	attacnetclientset "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/client/clientset/versioned"
	"github.com/spf13/pflag"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	clientset "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"kubevirt.io/client-go/kubecli"
)

// Configuration is the controller conf
type Configuration struct {
	BindAddress     string
	OvnNbAddr       string
	OvnSbAddr       string
	OvnTimeout      int
	KubeConfigFile  string
	KubeRestConfig  *rest.Config
	KubeClient      kubernetes.Interface
	KubeOvnClient   clientset.Interface
	AttachNetClient attacnetclientset.Interface
	KubevirtClient  kubecli.KubevirtClient

	DefaultLogicalSwitch string
	DefaultCIDR          string
	DefaultGateway       string
	DefaultExcludeIps    string

	ClusterRouter     string
	NodeSwitch        string
	NodeSwitchCIDR    string
	NodeSwitchGateway string

	ClusterTcpLoadBalancer        string
	ClusterUdpLoadBalancer        string
	ClusterTcpSessionLoadBalancer string
	ClusterUdpSessionLoadBalancer string

	PodName      string
	PodNamespace string
	PodNicType   string

	WorkerNum int
	PprofPort int

	NetworkType          string
	DefaultProviderName  string
	DefaultHostInterface string
	DefaultVlanName      string
	DefaultVlanID        int

	EnableLb          bool
	EnableNP          bool
	EnableExternalVpc bool
	EnableEcmp        bool
	EnableKeepVmIP    bool
}

// ParseFlags parses cmd args then init kubeclient and conf
// TODO: validate configuration
func ParseFlags() (*Configuration, error) {
	var (
		argOvnNbAddr      = pflag.String("ovn-nb-addr", "", "ovn-nb address")
		argOvnSbAddr      = pflag.String("ovn-sb-addr", "", "ovn-sb address")
		argOvnTimeout     = pflag.Int("ovn-timeout", 60, "")
		argKubeConfigFile = pflag.String("kubeconfig", "", "Path to kubeconfig file with authorization and master location information. If not set use the inCluster token.")

		argDefaultLogicalSwitch = pflag.String("default-ls", "ovn-default", "The default logical switch name, default: ovn-default")
		argDefaultCIDR          = pflag.String("default-cidr", "10.16.0.0/16", "Default CIDR for namespace with no logical switch annotation, default: 10.16.0.0/16")
		argDefaultGateway       = pflag.String("default-gateway", "", "Default gateway for default-cidr, default the first ip in default-cidr")
		argDefaultExcludeIps    = pflag.String("default-exclude-ips", "", "Exclude ips in default switch, default equals to gateway address")

		argClusterRouter     = pflag.String("cluster-router", "ovn-cluster", "The router name for cluster router, default: ovn-cluster")
		argNodeSwitch        = pflag.String("node-switch", "join", "The name of node gateway switch which help node to access pod network, default: join")
		argNodeSwitchCIDR    = pflag.String("node-switch-cidr", "100.64.0.0/16", "The cidr for node switch, default: 100.64.0.0/16")
		argNodeSwitchGateway = pflag.String("node-switch-gateway", "", "The gateway for node switch, default the first ip in node-switch-cidr")

		argClusterTcpLoadBalancer        = pflag.String("cluster-tcp-loadbalancer", "cluster-tcp-loadbalancer", "The name for cluster tcp loadbalancer")
		argClusterUdpLoadBalancer        = pflag.String("cluster-udp-loadbalancer", "cluster-udp-loadbalancer", "The name for cluster udp loadbalancer")
		argClusterTcpSessionLoadBalancer = pflag.String("cluster-tcp-session-loadbalancer", "cluster-tcp-session-loadbalancer", "The name for cluster tcp session loadbalancer")
		argClusterUdpSessionLoadBalancer = pflag.String("cluster-udp-session-loadbalancer", "cluster-udp-session-loadbalancer", "The name for cluster udp session loadbalancer")

		argWorkerNum = pflag.Int("worker-num", 3, "The parallelism of each worker, default: 3")
		argPprofPort = pflag.Int("pprof-port", 10660, "The port to get profiling data, default 10660")

		argNetworkType          = pflag.String("network-type", util.NetworkTypeGeneve, "The ovn network type, default: geneve")
		argDefaultProviderName  = pflag.String("default-provider-name", "provider", "The vlan or vxlan type default provider interface name, default: provider")
		argDefaultInterfaceName = pflag.String("default-interface-name", "", "The default host interface name in the vlan/vxlan type")
		argDefaultVlanName      = pflag.String("default-vlan-name", "ovn-vlan", "The default vlan name, default: ovn-vlan")
		argDefaultVlanID        = pflag.Int("default-vlan-id", 1, "The default vlan id, default: 1")
		argPodNicType           = pflag.String("pod-nic-type", "veth-pair", "The default pod network nic implementation type, default: veth-pair")
		argEnableLb             = pflag.Bool("enable-lb", true, "Enable load balancer, default: true")
		argEnableNP             = pflag.Bool("enable-np", true, "Enable network policy support, default: true")
		argEnableExternalVpc    = pflag.Bool("enable-external-vpc", true, "Enable external vpc support, default: true")
		argEnableEcmp           = pflag.Bool("enable-ecmp", false, "Enable ecmp route for centralized subnet")
		argKeepVmIP             = pflag.Bool("keep-vm-ip", false, "Whether to keep ip for kubevirt pod when pod is rebuild")
	)

	klogFlags := flag.NewFlagSet("klog", flag.ExitOnError)
	klog.InitFlags(klogFlags)

	// Sync the glog and klog flags.
	pflag.CommandLine.VisitAll(func(f1 *pflag.Flag) {
		f2 := klogFlags.Lookup(f1.Name)
		if f2 != nil {
			value := f1.Value.String()
			if err := f2.Value.Set(value); err != nil {
				klog.Fatalf("failed to set pflag, %v", err)
			}
		}
	})

	pflag.CommandLine.AddGoFlagSet(klogFlags)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.Parse()

	config := &Configuration{
		OvnNbAddr:                     *argOvnNbAddr,
		OvnSbAddr:                     *argOvnSbAddr,
		OvnTimeout:                    *argOvnTimeout,
		KubeConfigFile:                *argKubeConfigFile,
		DefaultLogicalSwitch:          *argDefaultLogicalSwitch,
		DefaultCIDR:                   *argDefaultCIDR,
		DefaultGateway:                *argDefaultGateway,
		DefaultExcludeIps:             *argDefaultExcludeIps,
		ClusterRouter:                 *argClusterRouter,
		NodeSwitch:                    *argNodeSwitch,
		NodeSwitchCIDR:                *argNodeSwitchCIDR,
		NodeSwitchGateway:             *argNodeSwitchGateway,
		ClusterTcpLoadBalancer:        *argClusterTcpLoadBalancer,
		ClusterUdpLoadBalancer:        *argClusterUdpLoadBalancer,
		ClusterTcpSessionLoadBalancer: *argClusterTcpSessionLoadBalancer,
		ClusterUdpSessionLoadBalancer: *argClusterUdpSessionLoadBalancer,
		WorkerNum:                     *argWorkerNum,
		PprofPort:                     *argPprofPort,
		NetworkType:                   *argNetworkType,
		DefaultVlanID:                 *argDefaultVlanID,
		DefaultProviderName:           *argDefaultProviderName,
		DefaultHostInterface:          *argDefaultInterfaceName,
		DefaultVlanName:               *argDefaultVlanName,
		PodName:                       os.Getenv("POD_NAME"),
		PodNamespace:                  os.Getenv("KUBE_NAMESPACE"),
		PodNicType:                    *argPodNicType,
		EnableLb:                      *argEnableLb,
		EnableNP:                      *argEnableNP,
		EnableExternalVpc:             *argEnableExternalVpc,
		EnableEcmp:                    *argEnableEcmp,
		EnableKeepVmIP:                *argKeepVmIP,
	}

	if config.NetworkType == util.NetworkTypeVlan && config.DefaultHostInterface == "" {
		return nil, fmt.Errorf("no host nic for vlan")
	}

	if config.DefaultGateway == "" {
		gw, err := util.GetGwByCidr(config.DefaultCIDR)
		if err != nil {
			return nil, err
		}
		config.DefaultGateway = gw
	}

	if config.DefaultExcludeIps == "" {
		config.DefaultExcludeIps = config.DefaultGateway
	}

	if config.NodeSwitchGateway == "" {
		gw, err := util.GetGwByCidr(config.NodeSwitchCIDR)
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

	config.KubeRestConfig = cfg

	AttachNetClient, err := attacnetclientset.NewForConfig(cfg)
	if err != nil {
		klog.Errorf("init attach network client failed %v", err)
		return err
	}
	config.AttachNetClient = AttachNetClient

	// get the kubevirt client, using which kubevirt resources can be managed.
	virtClient, err := kubecli.GetKubevirtClientFromRESTConfig(cfg)
	if err != nil {
		klog.Errorf("init kubevirt client failed %v", err)
		return err
	}
	config.KubevirtClient = virtClient

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
