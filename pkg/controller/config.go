package controller

import (
	"flag"
	"fmt"
	"os"
	"time"

	attachnetclientset "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/client/clientset/versioned"
	"github.com/spf13/pflag"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	"kubevirt.io/client-go/kubecli"

	clientset "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// Configuration is the controller conf
type Configuration struct {
	BindAddress          string
	OvnNbAddr            string
	OvnSbAddr            string
	OvnTimeout           int
	CustCrdRetryMaxDelay int
	CustCrdRetryMinDelay int
	KubeConfigFile       string
	KubeRestConfig       *rest.Config

	KubeClient      kubernetes.Interface
	KubeOvnClient   clientset.Interface
	AttachNetClient attachnetclientset.Interface
	KubevirtClient  kubecli.KubevirtClient

	// with no timeout
	KubeFactoryClient    kubernetes.Interface
	KubeOvnFactoryClient clientset.Interface

	DefaultLogicalSwitch      string
	DefaultCIDR               string
	DefaultGateway            string
	DefaultExcludeIps         string
	DefaultGatewayCheck       bool
	DefaultLogicalGateway     bool
	DefaultU2OInterconnection bool

	ClusterRouter     string
	NodeSwitch        string
	NodeSwitchCIDR    string
	NodeSwitchGateway string

	ServiceClusterIPRange string

	ClusterTcpLoadBalancer         string
	ClusterUdpLoadBalancer         string
	ClusterSctpLoadBalancer        string
	ClusterTcpSessionLoadBalancer  string
	ClusterUdpSessionLoadBalancer  string
	ClusterSctpSessionLoadBalancer string

	PodName      string
	PodNamespace string
	PodNicType   string

	PodDefaultFipType string

	WorkerNum       int
	PprofPort       int
	EnablePprof     bool
	NodePgProbeTime int

	NetworkType             string
	DefaultProviderName     string
	DefaultHostInterface    string
	DefaultExchangeLinkName bool
	DefaultVlanName         string
	DefaultVlanID           int
	LsDnatModDlDst          bool

	EnableLb          bool
	EnableNP          bool
	EnableEipSnat     bool
	EnableExternalVpc bool
	EnableEcmp        bool
	EnableKeepVmIP    bool
	EnableLbSvc       bool
	EnableMetrics     bool

	ExternalGatewaySwitch   string
	ExternalGatewayConfigNS string
	ExternalGatewayNet      string
	ExternalGatewayVlanID   int

	GCInterval      int
	InspectInterval int

	BfdMinTx      int
	BfdMinRx      int
	BfdDetectMult int
}

// ParseFlags parses cmd args then init kubeclient and conf
// TODO: validate configuration
func ParseFlags() (*Configuration, error) {
	var (
		argOvnNbAddr            = pflag.String("ovn-nb-addr", "", "ovn-nb address")
		argOvnSbAddr            = pflag.String("ovn-sb-addr", "", "ovn-sb address")
		argOvnTimeout           = pflag.Int("ovn-timeout", 60, "")
		argCustCrdRetryMinDelay = pflag.Int("cust-crd-retry-min-delay", 1, "The min delay seconds between custom crd two retries")
		argCustCrdRetryMaxDelay = pflag.Int("cust-crd-retry-max-delay", 20, "The max delay seconds between custom crd two retries")
		argKubeConfigFile       = pflag.String("kubeconfig", "", "Path to kubeconfig file with authorization and master location information. If not set use the inCluster token.")

		argDefaultLogicalSwitch  = pflag.String("default-ls", util.DefaultSubnet, "The default logical switch name")
		argDefaultCIDR           = pflag.String("default-cidr", "10.16.0.0/16", "Default CIDR for namespace with no logical switch annotation")
		argDefaultGateway        = pflag.String("default-gateway", "", "Default gateway for default-cidr (default the first ip in default-cidr)")
		argDefaultGatewayCheck   = pflag.Bool("default-gateway-check", true, "Check switch for the default subnet's gateway")
		argDefaultLogicalGateway = pflag.Bool("default-logical-gateway", false, "Create a logical gateway for the default subnet instead of using underlay gateway. Take effect only when the default subnet is in underlay mode. (default false)")
		argDefaultExcludeIps     = pflag.String("default-exclude-ips", "", "Exclude ips in default switch (default gateway address)")

		argDefaultU2OInterconnection = pflag.Bool("default-u2o-interconnection", false, "usage for underlay to overlay interconnection")

		argClusterRouter     = pflag.String("cluster-router", util.DefaultVpc, "The router name for cluster router")
		argNodeSwitch        = pflag.String("node-switch", "join", "The name of node gateway switch which help node to access pod network")
		argNodeSwitchCIDR    = pflag.String("node-switch-cidr", "100.64.0.0/16", "The cidr for node switch")
		argNodeSwitchGateway = pflag.String("node-switch-gateway", "", "The gateway for node switch (default the first ip in node-switch-cidr)")

		argServiceClusterIPRange = pflag.String("service-cluster-ip-range", "10.96.0.0/12", "The kubernetes service cluster ip range")

		argClusterTcpLoadBalancer         = pflag.String("cluster-tcp-loadbalancer", "cluster-tcp-loadbalancer", "The name for cluster tcp loadbalancer")
		argClusterUdpLoadBalancer         = pflag.String("cluster-udp-loadbalancer", "cluster-udp-loadbalancer", "The name for cluster udp loadbalancer")
		argClusterSctpLoadBalancer        = pflag.String("cluster-sctp-loadbalancer", "cluster-sctp-loadbalancer", "The name for cluster sctp loadbalancer")
		argClusterTcpSessionLoadBalancer  = pflag.String("cluster-tcp-session-loadbalancer", "cluster-tcp-session-loadbalancer", "The name for cluster tcp session loadbalancer")
		argClusterUdpSessionLoadBalancer  = pflag.String("cluster-udp-session-loadbalancer", "cluster-udp-session-loadbalancer", "The name for cluster udp session loadbalancer")
		argClusterSctpSessionLoadBalancer = pflag.String("cluster-sctp-session-loadbalancer", "cluster-sctp-session-loadbalancer", "The name for cluster sctp session loadbalancer")

		argWorkerNum       = pflag.Int("worker-num", 3, "The parallelism of each worker")
		argEnablePprof     = pflag.Bool("enable-pprof", false, "Enable pprof")
		argPprofPort       = pflag.Int("pprof-port", 10660, "The port to get profiling data")
		argNodePgProbeTime = pflag.Int("nodepg-probe-time", 1, "The probe interval for node port-group, the unit is minute")

		argNetworkType             = pflag.String("network-type", util.NetworkTypeGeneve, "The ovn network type")
		argDefaultProviderName     = pflag.String("default-provider-name", "provider", "The vlan or vxlan type default provider interface name")
		argDefaultInterfaceName    = pflag.String("default-interface-name", "", "The default host interface name in the vlan/vxlan type")
		argDefaultExchangeLinkName = pflag.Bool("default-exchange-link-name", false, "exchange link names of OVS bridge and the provider nic in the default provider-network")
		argDefaultVlanName         = pflag.String("default-vlan-name", "ovn-vlan", "The default vlan name")
		argDefaultVlanID           = pflag.Int("default-vlan-id", 1, "The default vlan id")
		argLsDnatModDlDst          = pflag.Bool("ls-dnat-mod-dl-dst", true, "Set ethernet destination address for DNAT on logical switch")
		argPodNicType              = pflag.String("pod-nic-type", "veth-pair", "The default pod network nic implementation type")
		argPodDefaultFipType       = pflag.String("pod-default-fip-type", "", "The type of fip bind to pod automatically: iptables")
		argEnableLb                = pflag.Bool("enable-lb", true, "Enable load balancer")
		argEnableNP                = pflag.Bool("enable-np", true, "Enable network policy support")
		argEnableEipSnat           = pflag.Bool("enable-eip-snat", true, "Enable EIP and SNAT")
		argEnableExternalVpc       = pflag.Bool("enable-external-vpc", true, "Enable external vpc support")
		argEnableEcmp              = pflag.Bool("enable-ecmp", false, "Enable ecmp route for centralized subnet")
		argKeepVmIP                = pflag.Bool("keep-vm-ip", true, "Whether to keep ip for kubevirt pod when pod is rebuild")
		argEnableLbSvc             = pflag.Bool("enable-lb-svc", false, "Whether to support loadbalancer service")
		argEnableMetrics           = pflag.Bool("enable-metrics", true, "Whether to support metrics query")

		argExternalGatewayConfigNS = pflag.String("external-gateway-config-ns", "kube-system", "The namespace of configmap external-gateway-config, default: kube-system")
		argExternalGatewaySwitch   = pflag.String("external-gateway-switch", "external", "The name of the external gateway switch which is a ovs bridge to provide external network, default: external")
		argExternalGatewayNet      = pflag.String("external-gateway-net", "external", "The name of the external network which mappings with an ovs bridge, default: external")
		argExternalGatewayVlanID   = pflag.Int("external-gateway-vlanid", 0, "The vlanId of port ln-ovn-external, default: 0")

		argGCInterval      = pflag.Int("gc-interval", 360, "The interval between GC processes, default 360 seconds")
		argInspectInterval = pflag.Int("inspect-interval", 20, "The interval between inspect processes, default 20 seconds")

		argBfdMinTx      = pflag.Int("bfd-min-tx", 100, "This is the minimum interval, in milliseconds, ovn would like to use when transmitting BFD Control packets")
		argBfdMinRx      = pflag.Int("bfd-min-rx", 100, "This is the minimum interval, in milliseconds, between received BFD Control packets")
		argBfdDetectMult = pflag.Int("detect-mult", 3, "The negotiated transmit interval, multiplied by this value, provides the Detection Time for the receiving system in Asynchronous mode.")
	)

	klogFlags := flag.NewFlagSet("klog", flag.ExitOnError)
	klog.InitFlags(klogFlags)

	// Sync the glog and klog flags.
	pflag.CommandLine.VisitAll(func(f1 *pflag.Flag) {
		f2 := klogFlags.Lookup(f1.Name)
		if f2 != nil {
			value := f1.Value.String()
			if err := f2.Value.Set(value); err != nil {
				util.LogFatalAndExit(err, "failed to set pflag")
			}
		}
	})

	pflag.CommandLine.AddGoFlagSet(klogFlags)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.Parse()

	config := &Configuration{
		OvnNbAddr:                      *argOvnNbAddr,
		OvnSbAddr:                      *argOvnSbAddr,
		OvnTimeout:                     *argOvnTimeout,
		CustCrdRetryMinDelay:           *argCustCrdRetryMinDelay,
		CustCrdRetryMaxDelay:           *argCustCrdRetryMaxDelay,
		KubeConfigFile:                 *argKubeConfigFile,
		DefaultLogicalSwitch:           *argDefaultLogicalSwitch,
		DefaultCIDR:                    *argDefaultCIDR,
		DefaultGateway:                 *argDefaultGateway,
		DefaultGatewayCheck:            *argDefaultGatewayCheck,
		DefaultLogicalGateway:          *argDefaultLogicalGateway,
		DefaultU2OInterconnection:      *argDefaultU2OInterconnection,
		DefaultExcludeIps:              *argDefaultExcludeIps,
		ClusterRouter:                  *argClusterRouter,
		NodeSwitch:                     *argNodeSwitch,
		NodeSwitchCIDR:                 *argNodeSwitchCIDR,
		NodeSwitchGateway:              *argNodeSwitchGateway,
		ServiceClusterIPRange:          *argServiceClusterIPRange,
		ClusterTcpLoadBalancer:         *argClusterTcpLoadBalancer,
		ClusterUdpLoadBalancer:         *argClusterUdpLoadBalancer,
		ClusterSctpLoadBalancer:        *argClusterSctpLoadBalancer,
		ClusterTcpSessionLoadBalancer:  *argClusterTcpSessionLoadBalancer,
		ClusterUdpSessionLoadBalancer:  *argClusterUdpSessionLoadBalancer,
		ClusterSctpSessionLoadBalancer: *argClusterSctpSessionLoadBalancer,
		WorkerNum:                      *argWorkerNum,
		EnablePprof:                    *argEnablePprof,
		PprofPort:                      *argPprofPort,
		NetworkType:                    *argNetworkType,
		DefaultVlanID:                  *argDefaultVlanID,
		LsDnatModDlDst:                 *argLsDnatModDlDst,
		DefaultProviderName:            *argDefaultProviderName,
		DefaultHostInterface:           *argDefaultInterfaceName,
		DefaultExchangeLinkName:        *argDefaultExchangeLinkName,
		DefaultVlanName:                *argDefaultVlanName,
		PodName:                        os.Getenv("POD_NAME"),
		PodNamespace:                   os.Getenv("KUBE_NAMESPACE"),
		PodNicType:                     *argPodNicType,
		PodDefaultFipType:              *argPodDefaultFipType,
		EnableLb:                       *argEnableLb,
		EnableNP:                       *argEnableNP,
		EnableEipSnat:                  *argEnableEipSnat,
		EnableExternalVpc:              *argEnableExternalVpc,
		ExternalGatewayConfigNS:        *argExternalGatewayConfigNS,
		ExternalGatewaySwitch:          *argExternalGatewaySwitch,
		ExternalGatewayNet:             *argExternalGatewayNet,
		ExternalGatewayVlanID:          *argExternalGatewayVlanID,
		EnableEcmp:                     *argEnableEcmp,
		EnableKeepVmIP:                 *argKeepVmIP,
		NodePgProbeTime:                *argNodePgProbeTime,
		GCInterval:                     *argGCInterval,
		InspectInterval:                *argInspectInterval,
		EnableLbSvc:                    *argEnableLbSvc,
		EnableMetrics:                  *argEnableMetrics,
		BfdMinTx:                       *argBfdMinTx,
		BfdMinRx:                       *argBfdMinRx,
		BfdDetectMult:                  *argBfdDetectMult,
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

	if err := config.initKubeFactoryClient(); err != nil {
		return nil, err
	}

	if err := util.CheckSystemCIDR([]string{config.NodeSwitchCIDR, config.DefaultCIDR, config.ServiceClusterIPRange}); err != nil {
		return nil, fmt.Errorf("check system cidr failed, %v", err)
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

	// try to connect to apiserver's tcp port
	if err = util.DialApiServer(cfg.Host); err != nil {
		klog.Errorf("failed to dial apiserver: %v", err)
		return err
	}

	cfg.QPS = 1000
	cfg.Burst = 2000
	// use cmd arg to modify timeout later
	cfg.Timeout = 30 * time.Second

	AttachNetClient, err := attachnetclientset.NewForConfig(cfg)
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

func (config *Configuration) initKubeFactoryClient() error {
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

	kubeOvnClient, err := clientset.NewForConfig(cfg)
	if err != nil {
		klog.Errorf("init kubeovn client failed %v", err)
		return err
	}
	config.KubeOvnFactoryClient = kubeOvnClient

	cfg.ContentType = "application/vnd.kubernetes.protobuf"
	cfg.AcceptContentTypes = "application/vnd.kubernetes.protobuf,application/json"
	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		klog.Errorf("init kubernetes client failed %v", err)
		return err
	}
	config.KubeFactoryClient = kubeClient
	return nil
}
