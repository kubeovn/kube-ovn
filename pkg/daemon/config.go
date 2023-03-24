package daemon

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	clientset "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// Configuration is the daemon conf
type Configuration struct {
	// interface being used for tunnel
	tunnelIface             string
	Iface                   string
	DPDKTunnelIface         string
	MTU                     int
	MSS                     int
	EnableMirror            bool
	MirrorNic               string
	BindSocket              string
	OvsSocket               string
	KubeConfigFile          string
	KubeClient              kubernetes.Interface
	KubeOvnClient           clientset.Interface
	NodeName                string
	ServiceClusterIPRange   string
	NodeSwitch              string
	NodeLocalDnsIP          string
	EncapChecksum           bool
	EnablePprof             bool
	MacLearningFallback     bool
	PprofPort               int
	NetworkType             string
	CniConfDir              string
	CniConfFile             string
	CniConfName             string
	DefaultProviderName     string
	DefaultInterfaceName    string
	ExternalGatewayConfigNS string
	ExternalGatewaySwitch   string // provider network underlay vlan subnet
	EnableMetrics           bool
}

// ParseFlags will parse cmd args then init kubeClient and configuration
// TODO: validate configuration
func ParseFlags() *Configuration {
	var (
		argNodeName              = pflag.String("node-name", "", "Name of the node on which the daemon is running on.")
		argIface                 = pflag.String("iface", "", "The iface used to inter-host pod communication, can be a nic name or a group of regex separated by comma (default the default route iface)")
		argDPDKTunnelIface       = pflag.String("dpdk-tunnel-iface", "br-phy", "Specifies the name of the dpdk tunnel iface.")
		argMTU                   = pflag.Int("mtu", 0, "The MTU used by pod iface in overlay networks (default iface MTU - 100)")
		argEnableMirror          = pflag.Bool("enable-mirror", false, "Enable traffic mirror (default false)")
		argMirrorNic             = pflag.String("mirror-iface", "mirror0", "The mirror nic name that will be created by kube-ovn")
		argBindSocket            = pflag.String("bind-socket", defaultBindSocket, "The socket daemon bind to.")
		argOvsSocket             = pflag.String("ovs-socket", "", "The socket to local ovs-server")
		argKubeConfigFile        = pflag.String("kubeconfig", "", "Path to kubeconfig file with authorization and master location information. If not set use the inCluster token.")
		argServiceClusterIPRange = pflag.String("service-cluster-ip-range", "10.96.0.0/12", "The kubernetes service cluster ip range")
		argNodeSwitch            = pflag.String("node-switch", "join", "The name of node gateway switch which help node to access pod network")
		argNodeLocalDnsIP        = pflag.String("node-local-dns-ip", "", "If use nodelocaldns the local dns server ip should be set here.")
		argEncapChecksum         = pflag.Bool("encap-checksum", true, "Enable checksum")
		argEnablePprof           = pflag.Bool("enable-pprof", false, "Enable pprof")
		argPprofPort             = pflag.Int("pprof-port", 10665, "The port to get profiling data")
		argMacLearningFallback   = pflag.Bool("mac-learning-fallback", false, "Fallback to the legacy MAC learning mode")

		argsNetworkType            = pflag.String("network-type", util.NetworkTypeGeneve, "Tunnel encapsulation protocol in overlay networks")
		argCniConfDir              = pflag.String("cni-conf-dir", "/etc/cni/net.d", "Path of the CNI config directory.")
		argCniConfFile             = pflag.String("cni-conf-file", "/kube-ovn/01-kube-ovn.conflist", "Path of the CNI config file.")
		argsCniConfName            = pflag.String("cni-conf-name", "01-kube-ovn.conflist", "Specify the name of kube ovn conflist name in dir /etc/cni/net.d/, default: 01-kube-ovn.conflist")
		argsDefaultProviderName    = pflag.String("default-provider-name", "provider", "The vlan or vxlan type default provider interface name")
		argsDefaultInterfaceName   = pflag.String("default-interface-name", "", "The default host interface name in the vlan/vxlan type")
		argExternalGatewayConfigNS = pflag.String("external-gateway-config-ns", "kube-system", "The namespace of configmap external-gateway-config, default: kube-system")
		argExternalGatewaySwitch   = pflag.String("external-gateway-switch", "external", "The name of the external gateway switch which is a ovs bridge to provide external network, default: external")
		argEnableMetrics           = pflag.Bool("enable-metrics", true, "Whether to support metrics query")
	)

	// mute info log for ipset lib
	logrus.SetLevel(logrus.WarnLevel)

	klogFlags := flag.NewFlagSet("klog", flag.ExitOnError)
	klog.InitFlags(klogFlags)

	// Sync the glog and klog flags.
	pflag.CommandLine.VisitAll(func(f1 *pflag.Flag) {
		f2 := klogFlags.Lookup(f1.Name)
		if f2 != nil {
			value := f1.Value.String()
			if err := f2.Value.Set(value); err != nil {
				util.LogFatalAndExit(err, "failed to set flag")
			}
		}
	})

	pflag.CommandLine.AddGoFlagSet(klogFlags)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.Parse()

	config := &Configuration{
		Iface:                   *argIface,
		DPDKTunnelIface:         *argDPDKTunnelIface,
		MTU:                     *argMTU,
		EnableMirror:            *argEnableMirror,
		MirrorNic:               *argMirrorNic,
		BindSocket:              *argBindSocket,
		OvsSocket:               *argOvsSocket,
		KubeConfigFile:          *argKubeConfigFile,
		EnablePprof:             *argEnablePprof,
		PprofPort:               *argPprofPort,
		MacLearningFallback:     *argMacLearningFallback,
		NodeName:                strings.ToLower(*argNodeName),
		ServiceClusterIPRange:   *argServiceClusterIPRange,
		NodeSwitch:              *argNodeSwitch,
		NodeLocalDnsIP:          *argNodeLocalDnsIP,
		EncapChecksum:           *argEncapChecksum,
		NetworkType:             *argsNetworkType,
		CniConfDir:              *argCniConfDir,
		CniConfFile:             *argCniConfFile,
		CniConfName:             *argsCniConfName,
		DefaultProviderName:     *argsDefaultProviderName,
		DefaultInterfaceName:    *argsDefaultInterfaceName,
		ExternalGatewayConfigNS: *argExternalGatewayConfigNS,
		ExternalGatewaySwitch:   *argExternalGatewaySwitch,
		EnableMetrics:           *argEnableMetrics,
	}
	return config
}

func (config *Configuration) Init(nicBridgeMappings map[string]string) error {
	if config.NodeName == "" {
		klog.Info("node name not specified in command line parameters, fall back to the environment variable")
		if config.NodeName = strings.ToLower(os.Getenv(util.HostnameEnv)); config.NodeName == "" {
			klog.Info("node name not specified in environment variables, fall back to the hostname")
			hostname, err := os.Hostname()
			if err != nil {
				return fmt.Errorf("failed to get hostname: %v", err)
			}
			config.NodeName = strings.ToLower(hostname)
		}
	}

	if err := config.initKubeClient(); err != nil {
		return err
	}
	if err := config.initNicConfig(nicBridgeMappings); err != nil {
		return err
	}

	klog.Infof("daemon config: %v", config)
	return nil
}

func (config *Configuration) initNicConfig(nicBridgeMappings map[string]string) error {
	// Support to specify node network card separately
	node, err := config.KubeClient.CoreV1().Nodes().Get(context.Background(), config.NodeName, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("Failed to find node info, err: %v", err)
		return err
	}
	if nodeTunnelName := node.GetAnnotations()[util.TunnelInterfaceAnnotation]; nodeTunnelName != "" {
		config.Iface = nodeTunnelName
		klog.Infof("Find node tunnel interface name: %v", nodeTunnelName)
	}

	isDPDKNode := node.GetLabels()[util.OvsDpTypeLabel] == "userspace"
	if isDPDKNode {
		config.Iface = config.DPDKTunnelIface
	}

	var mtu int
	var encapIP string
	if config.Iface == "" {
		encapIP = config.getEncapIP(node)
		if config.Iface, mtu, err = getIfaceByIP(encapIP); err != nil {
			klog.Errorf("failed to get interface by IP %s: %v", encapIP, err)
			return err
		}
	} else {
		tunnelNic := config.Iface
		if brName := nicBridgeMappings[tunnelNic]; brName != "" {
			klog.Infof("nic %s has been bridged to %s, use %s as the tunnel interface instead", tunnelNic, brName, brName)
			tunnelNic = brName
		}

		iface, err := findInterface(tunnelNic)
		if err != nil {
			klog.Errorf("failed to find iface %s, %v", tunnelNic, err)
			return err
		}
		srcIPs, err := getSrcIPsByRoutes(iface)
		if err != nil {
			return fmt.Errorf("failed to get src IPs by routes on interface %s: %v", iface.Name, err)
		}
		addrs, err := iface.Addrs()
		if err != nil {
			return fmt.Errorf("failed to get iface addr. %v", err)
		}
		for _, addr := range addrs {
			ipStr := strings.Split(addr.String(), "/")[0]
			if ip := net.ParseIP(ipStr); ip == nil || ip.IsLinkLocalUnicast() {
				continue
			}
			if len(srcIPs) == 0 || util.ContainsString(srcIPs, ipStr) {
				encapIP = ipStr
				break
			}
		}
		if len(encapIP) == 0 {
			return fmt.Errorf("iface %s has no valid IP address", tunnelNic)
		}

		klog.Infof("use %s on %s as tunnel address", encapIP, iface.Name)
		mtu = iface.MTU
		config.tunnelIface = iface.Name
	}

	encapIsIPv6 := util.CheckProtocol(encapIP) == kubeovnv1.ProtocolIPv6
	if encapIsIPv6 && runtime.GOOS == "windows" {
		// OVS windows datapath does not IPv6 tunnel in version v2.17
		err = errors.New("IPv6 tunnel is not supported on Windows currently")
		klog.Error(err)
		return err
	}

	if config.MTU == 0 {
		switch config.NetworkType {
		case util.NetworkTypeGeneve, util.NetworkTypeVlan:
			config.MTU = mtu - util.GeneveHeaderLength
		case util.NetworkTypeVxlan:
			config.MTU = mtu - util.VxlanHeaderLength
		case util.NetworkTypeStt:
			config.MTU = mtu - util.SttHeaderLength
		default:
			return fmt.Errorf("invalid network type: %s", config.NetworkType)
		}
		if encapIsIPv6 {
			// IPv6 header size is 40
			config.MTU -= 20
		}
	}

	config.MSS = config.MTU - util.TcpIpHeaderLength
	if !config.EncapChecksum {
		if err := disableChecksum(); err != nil {
			klog.Errorf("failed to set checksum offload, %v", err)
		}
	}

	if err = config.initRuntimeConfig(node); err != nil {
		klog.Error(err)
		return err
	}

	return setEncapIP(encapIP)
}

func (config *Configuration) getEncapIP(node *corev1.Node) string {
	if podIP := os.Getenv(util.POD_IP); podIP != "" {
		return podIP
	}

	klog.Info("environment variable POD_IP not found, fall back to node address")
	ipv4, ipv6 := util.GetNodeInternalIP(*node)
	if ipv4 != "" {
		return ipv4
	}
	return ipv6
}

func findInterface(ifaceStr string) (*net.Interface, error) {
	iface, err := net.InterfaceByName(ifaceStr)
	if err == nil && iface != nil {
		return iface, nil
	}
	ifaceRegex, err := regexp.Compile("(" + strings.Join(strings.Split(ifaceStr, ","), ")|(") + ")")
	if err != nil {
		klog.Errorf("Invalid interface regex %s", ifaceStr)
		return nil, err
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		klog.Errorf("failed to list interfaces, %v", err)
		return nil, err
	}
	for _, iface := range ifaces {
		if ifaceRegex.MatchString(iface.Name) {
			return &iface, nil
		}
	}
	klog.Errorf("network interface %s not exist", ifaceStr)
	return nil, fmt.Errorf("network interface %s not exist", ifaceStr)
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

	// try to connect to apiserver's tcp port
	if err = util.DialApiServer(cfg.Host); err != nil {
		klog.Errorf("failed to dial apiserver: %v", err)
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

	cfg.ContentType = util.ContentType
	cfg.AcceptContentTypes = util.AcceptContentTypes
	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		klog.Errorf("init kubernetes client failed %v", err)
		return err
	}
	config.KubeClient = kubeClient
	return nil
}

func setEncapIP(ip string) error {
	raw, err := exec.Command(
		"ovs-vsctl", "set", "open", ".", fmt.Sprintf("external-ids:ovn-encap-ip=%s", ip)).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to set ovn-encap-ip, %s", string(raw))
	}
	return nil
}

func disableChecksum() error {
	raw, err := exec.Command(
		"ovs-vsctl", "set", "open", ".", "external-ids:ovn-encap-csum=false").CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to set ovn-encap-csum, %s", string(raw))
	}
	return nil
}
