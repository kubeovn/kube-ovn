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
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
	"github.com/vishvananda/netlink"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	clientset "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// Configuration is the daemon conf
type Configuration struct {
	// interface being used for tunnel
	tunnelIface             string
	Iface                   string
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
	NodeLocalDnsIP          string
	EncapChecksum           bool
	EnablePprof             bool
	MacLearningFallback     bool
	PprofPort               int
	NetworkType             string
	DefaultProviderName     string
	DefaultInterfaceName    string
	ExternalGatewayConfigNS string
	EnableMetrics           bool
	EnableAddIPTosFlow      bool
}

// ParseFlags will parse cmd args then init kubeClient and configuration
// TODO: validate configuration
func ParseFlags(nicBridgeMappings map[string]string) (*Configuration, error) {
	var (
		argIface                 = pflag.String("iface", "", "The iface used to inter-host pod communication, can be a nic name or a group of regex separated by comma (default the default route iface)")
		argMTU                   = pflag.Int("mtu", 0, "The MTU used by pod iface in overlay networks (default iface MTU - 100)")
		argEnableMirror          = pflag.Bool("enable-mirror", false, "Enable traffic mirror (default false)")
		argMirrorNic             = pflag.String("mirror-iface", "mirror0", "The mirror nic name that will be created by kube-ovn")
		argBindSocket            = pflag.String("bind-socket", "/run/openvswitch/kube-ovn-daemon.sock", "The socket daemon bind to.")
		argOvsSocket             = pflag.String("ovs-socket", "", "The socket to local ovs-server")
		argKubeConfigFile        = pflag.String("kubeconfig", "", "Path to kubeconfig file with authorization and master location information. If not set use the inCluster token.")
		argServiceClusterIPRange = pflag.String("service-cluster-ip-range", "10.96.0.0/12", "The kubernetes service cluster ip range")
		argNodeLocalDnsIP        = pflag.String("node-local-dns-ip", "", "If use nodelocaldns the local dns server ip should be set here.")
		argEncapChecksum         = pflag.Bool("encap-checksum", true, "Enable checksum")
		argEnablePprof           = pflag.Bool("enable-pprof", false, "Enable pprof")
		argPprofPort             = pflag.Int("pprof-port", 10665, "The port to get profiling data")
		argMacLearningFallback   = pflag.Bool("mac-learning-fallback", false, "Fallback to the legacy MAC learning mode")

		argsNetworkType            = pflag.String("network-type", "geneve", "The ovn network type")
		argsDefaultProviderName    = pflag.String("default-provider-name", "provider", "The vlan or vxlan type default provider interface name")
		argsDefaultInterfaceName   = pflag.String("default-interface-name", "", "The default host interface name in the vlan/vxlan type")
		argExternalGatewayConfigNS = pflag.String("external-gateway-config-ns", "kube-system", "The namespace of configmap external-gateway-config, default: kube-system")
		argEnableMetrics           = pflag.Bool("enable-metrics", true, "Whether to support metrics query")
		argEnableAddIPTosFlow      = pflag.Bool("enable-ip-tos", false, "If enable add ovs flows to map ip tos to vlan priority")
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

	nodeName := os.Getenv(util.HostnameEnv)
	if nodeName == "" {
		klog.Errorf("env KUBE_NODE_NAME not exists")
		return nil, fmt.Errorf("env KUBE_NODE_NAME not exists")
	}
	config := &Configuration{
		Iface:                   *argIface,
		MTU:                     *argMTU,
		EnableMirror:            *argEnableMirror,
		MirrorNic:               *argMirrorNic,
		BindSocket:              *argBindSocket,
		OvsSocket:               *argOvsSocket,
		KubeConfigFile:          *argKubeConfigFile,
		EnablePprof:             *argEnablePprof,
		PprofPort:               *argPprofPort,
		MacLearningFallback:     *argMacLearningFallback,
		NodeName:                nodeName,
		ServiceClusterIPRange:   *argServiceClusterIPRange,
		NodeLocalDnsIP:          *argNodeLocalDnsIP,
		EncapChecksum:           *argEncapChecksum,
		NetworkType:             *argsNetworkType,
		DefaultProviderName:     *argsDefaultProviderName,
		DefaultInterfaceName:    *argsDefaultInterfaceName,
		ExternalGatewayConfigNS: *argExternalGatewayConfigNS,
		EnableMetrics:           *argEnableMetrics,
		EnableAddIPTosFlow:      *argEnableAddIPTosFlow,
	}

	if err := config.initKubeClient(); err != nil {
		return nil, err
	}

	if err := config.initNicConfig(nicBridgeMappings); err != nil {
		return nil, err
	}

	klog.Infof("daemon config: %v", config)
	return config, nil
}

func getSrcIPsByRoutes(iface *net.Interface) ([]string, error) {
	link, err := netlink.LinkByName(iface.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to get link %s: %v", iface.Name, err)
	}
	routes, err := netlink.RouteList(link, netlink.FAMILY_ALL)
	if err != nil {
		return nil, fmt.Errorf("failed to get routes on link %s: %v", iface.Name, err)
	}

	srcIPs := make([]string, 0, 2)
	for _, r := range routes {
		if r.Src != nil && r.Scope == netlink.SCOPE_LINK {
			srcIPs = append(srcIPs, r.Src.String())
		}
	}
	return srcIPs, nil
}

func (config *Configuration) initNicConfig(nicBridgeMappings map[string]string) error {
	var (
		iface   *net.Interface
		err     error
		encapIP string
	)

	//Support to specify node network card separately
	node, err := config.KubeClient.CoreV1().Nodes().Get(context.Background(), config.NodeName, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("Failed to find node info, err: %v", err)
		return err
	}
	if nodeTunnelName := node.GetAnnotations()[util.TunnelInterfaceAnnotation]; nodeTunnelName != "" {
		config.Iface = nodeTunnelName
		klog.Infof("Find node tunnel interface name: %v", nodeTunnelName)
	}

	if config.Iface == "" {
		podIP, ok := os.LookupEnv("POD_IP")
		if !ok || podIP == "" {
			return errors.New("failed to lookup env POD_IP")
		}
		iface, err = getIfaceOwnPodIP(podIP)
		if err != nil {
			klog.Errorf("failed to find POD_IP iface %v", err)
			return err
		}
		encapIP = podIP
	} else {
		tunnelNic := config.Iface
		if brName := nicBridgeMappings[tunnelNic]; brName != "" {
			klog.Infof("nic %s has been bridged to %s, use %s as the tunnel interface instead", tunnelNic, brName, brName)
			tunnelNic = brName
		}

		iface, err = findInterface(tunnelNic)
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
		config.tunnelIface = iface.Name
	}

	if config.MTU == 0 {
		config.MTU = iface.MTU - util.GeneveHeaderLength
	}

	config.MSS = config.MTU - util.TcpIpHeaderLength
	if !config.EncapChecksum {
		if err := disableChecksum(); err != nil {
			klog.Errorf("failed to set checksum offload, %v", err)
		}
	}

	return setEncapIP(encapIP)
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

func getIfaceOwnPodIP(podIP string) (*net.Interface, error) {
	links, err := netlink.LinkList()
	if err != nil {
		return nil, err
	}

	for _, link := range links {
		addrs, err := netlink.AddrList(link, netlink.FAMILY_ALL)
		if err != nil {
			return nil, fmt.Errorf("failed to get a list of IP addresses %v", err)
		}
		for _, addr := range addrs {
			if addr.IPNet.Contains(net.ParseIP(podIP)) && addr.IP.String() == podIP {
				return &net.Interface{
					Index:        link.Attrs().Index,
					MTU:          link.Attrs().MTU,
					Name:         link.Attrs().Name,
					HardwareAddr: link.Attrs().HardwareAddr,
					Flags:        link.Attrs().Flags,
				}, nil
			}
		}
	}

	return nil, errors.New("unable to find podIP interface")
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
