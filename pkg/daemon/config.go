package daemon

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	certmanagerclientset "github.com/cert-manager/cert-manager/pkg/client/clientset/versioned"
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
	InstallCNIConfig bool
	CniConfDir       string
	CniConfFile      string
	CniConfName      string

	// interface being used for tunnel
	tunnelIface               string
	Iface                     string
	HostTunnelSrc             bool
	DPDKTunnelIface           string
	MTU                       int
	MSS                       int
	EnableMirror              bool
	MirrorNic                 string
	BindSocket                string
	OvsSocket                 string
	KubeConfigFile            string
	KubeClient                kubernetes.Interface
	KubeOvnClient             clientset.Interface
	CertManagerClient         certmanagerclientset.Interface
	PodName                   string
	PodNamespace              string
	NodeName                  string
	NodeIPv4                  string
	NodeIPv6                  string
	ServiceClusterIPRange     string
	ClusterRouter             string
	NodeSwitch                string
	EncapChecksum             bool
	EnablePprof               bool
	MacLearningFallback       bool
	PprofPort                 int32
	SecureServing             bool
	NetworkType               string
	DefaultProviderName       string
	DefaultInterfaceName      string
	ExternalGatewayConfigNS   string
	ExternalGatewaySwitch     string // provider network underlay vlan subnet
	EnableMetrics             bool
	EnableOVNIPSec            bool
	CertManagerIPSecCert      bool
	CertManagerIssuerName     string
	IPSecCertDuration         int
	EnableArpDetectIPConflict bool
	KubeletDir                string
	EnableVerboseConnCheck    bool
	TCPConnCheckPort          int32
	UDPConnCheckPort          int32
	EnableTProxy              bool
	OVSVsctlConcurrency       int32
	SetVxlanTxOff             bool
	LogPerm                   string

	// Node-local EIP access configuration for VPC NAT Gateway
	EnableNodeLocalAccessVpcNatGwEIP bool

	// TLS configuration for secure serving
	TLSMinVersion   string
	TLSMaxVersion   string
	TLSCipherSuites []string

	// NodeNetworks stores the mapping of network name to encap IP from node annotation
	NodeNetworks      map[string]string
	nodeNetworksMutex sync.RWMutex
	DefaultEncapIP    string
}

// ParseFlags will parse cmd args then init kubeClient and configuration
// TODO: validate configuration
func ParseFlags() *Configuration {
	var (
		argInstallCNIConfig = pflag.Bool("install-cni-config", false, "Install CNI config")
		argCniConfDir       = pflag.String("cni-conf-dir", "/etc/cni/net.d", "Path of the CNI config directory.")
		argCniConfFile      = pflag.String("cni-conf-file", "/kube-ovn/01-kube-ovn.conflist", "Path of the CNI config file.")
		argsCniConfName     = pflag.String("cni-conf-name", "01-kube-ovn.conflist", "Specify the name of kube ovn conflist name in dir /etc/cni/net.d/, default: 01-kube-ovn.conflist")

		argIface                 = pflag.String("iface", "", "The iface used to inter-host pod communication, can be a nic name or a group of regex separated by comma (default the default route iface)")
		argHostTunnelSrc         = pflag.Bool("host-tunnel-src", false, "Enable /32 address selection for the tunnel source, excludes localhost addresses unless explicitly allowed.")
		argDPDKTunnelIface       = pflag.String("dpdk-tunnel-iface", "br-phy", "Specifies the name of the dpdk tunnel iface.")
		argMTU                   = pflag.Int("mtu", 0, "The MTU used by pod iface in overlay networks (default iface MTU - 100)")
		argEnableMirror          = pflag.Bool("enable-mirror", false, "Enable traffic mirror (default false)")
		argMirrorNic             = pflag.String("mirror-iface", "mirror0", "The mirror nic name that will be created by kube-ovn")
		argBindSocket            = pflag.String("bind-socket", defaultBindSocket, "The socket daemon bind to.")
		argOvsSocket             = pflag.String("ovs-socket", "", "The socket to local ovs-server")
		argKubeConfigFile        = pflag.String("kubeconfig", "", "Path to kubeconfig file with authorization and master location information. If not set use the inCluster token.")
		argServiceClusterIPRange = pflag.String("service-cluster-ip-range", "10.96.0.0/12", "The kubernetes service cluster ip range")
		argClusterRouter         = pflag.String("cluster-router", util.DefaultVpc, "The router name for cluster router")
		argNodeSwitch            = pflag.String("node-switch", "join", "The name of node gateway switch which help node to access pod network")
		argEncapChecksum         = pflag.Bool("encap-checksum", true, "Enable checksum")
		argEnablePprof           = pflag.Bool("enable-pprof", false, "Enable pprof")
		argPprofPort             = pflag.Int32("pprof-port", 10665, "The port to get profiling data")
		argSecureServing         = pflag.Bool("secure-serving", false, "Enable secure serving")
		argMacLearningFallback   = pflag.Bool("mac-learning-fallback", false, "Fallback to the legacy MAC learning mode")

		argsNetworkType              = pflag.String("network-type", util.NetworkTypeGeneve, "Tunnel encapsulation protocol in overlay networks")
		argsDefaultProviderName      = pflag.String("default-provider-name", "provider", "The vlan or vxlan type default provider interface name")
		argsDefaultInterfaceName     = pflag.String("default-interface-name", "", "The default host interface name in the vlan/vxlan type")
		argExternalGatewayConfigNS   = pflag.String("external-gateway-config-ns", "kube-system", "The namespace of configmap external-gateway-config, default: kube-system")
		argExternalGatewaySwitch     = pflag.String("external-gateway-switch", "external", "The name of the external gateway switch which is a ovs bridge to provide external network, default: external")
		argEnableMetrics             = pflag.Bool("enable-metrics", true, "Whether to support metrics query")
		argEnableArpDetectIPConflict = pflag.Bool("enable-arp-detect-ip-conflict", true, "Whether to support arp detect ip conflict in underlay network")
		argKubeletDir                = pflag.String("kubelet-dir", "/var/lib/kubelet", "Path of the kubelet dir, default: /var/lib/kubelet")
		argEnableVerboseConnCheck    = pflag.Bool("enable-verbose-conn-check", false, "enable TCP/UDP connectivity check listen port")
		argTCPConnectivityCheckPort  = pflag.Int32("tcp-conn-check-port", 8100, "TCP connectivity Check Port")
		argUDPConnectivityCheckPort  = pflag.Int32("udp-conn-check-port", 8101, "UDP connectivity Check Port")
		argEnableTProxy              = pflag.Bool("enable-tproxy", false, "enable tproxy for vpc pod liveness or readiness probe")
		argOVSVsctlConcurrency       = pflag.Int32("ovs-vsctl-concurrency", 100, "concurrency limit of ovs-vsctl")
		argEnableOVNIPSec            = pflag.Bool("enable-ovn-ipsec", false, "Whether to enable ovn ipsec")
		argCertManagerIPSecCert      = pflag.Bool("cert-manager-ipsec-cert", false, "Whether to use cert-manager for signing IPSec certificates")
		argCertManagerIssuerName     = pflag.String("cert-manager-issuer-name", "kube-ovn", "The cert-manager issuer name to request certificates from")
		argOVNIPSecCertDuration      = pflag.Int("ovn-ipsec-cert-duration", 2*365*24*60*60, "The duration requested for IPSec certificates (seconds)")
		argSetVxlanTxOff             = pflag.Bool("set-vxlan-tx-off", false, "Whether to set vxlan_sys_4789 tx off")
		argLogPerm                   = pflag.String("log-perm", "640", "The permission for the log file")

		argTLSMinVersion   = pflag.String("tls-min-version", "", "The minimum TLS version to use for secure serving. Supported values: TLS10, TLS11, TLS12, TLS13. If not set, the default is used based on the Go version.")
		argTLSMaxVersion   = pflag.String("tls-max-version", "", "The maximum TLS version to use for secure serving. Supported values: TLS10, TLS11, TLS12, TLS13. If not set, the default is used based on the Go version.")
		argTLSCipherSuites = pflag.StringSlice("tls-cipher-suites", nil, "Comma-separated list of TLS cipher suite names to use for secure serving (e.g., 'TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384'). Names must match Go's crypto/tls package. See Go documentation for available suites. If not set, defaults are used. Users are responsible for selecting secure cipher suites.")

		// Node-local EIP access for VPC NAT Gateway
		argEnableNodeLocalAccessVpcNatGwEIP = pflag.Bool("enable-node-local-access-vpc-nat-gw-eip", true, "Enable node local access to VPC NAT gateway iptables EIP addresses via macvlan sub-interface")
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
		InstallCNIConfig:          *argInstallCNIConfig,
		CniConfDir:                *argCniConfDir,
		CniConfFile:               *argCniConfFile,
		CniConfName:               *argsCniConfName,
		Iface:                     *argIface,
		HostTunnelSrc:             *argHostTunnelSrc,
		DPDKTunnelIface:           *argDPDKTunnelIface,
		MTU:                       *argMTU,
		EnableMirror:              *argEnableMirror,
		MirrorNic:                 *argMirrorNic,
		BindSocket:                *argBindSocket,
		OvsSocket:                 *argOvsSocket,
		KubeConfigFile:            *argKubeConfigFile,
		EnablePprof:               *argEnablePprof,
		SecureServing:             *argSecureServing,
		PprofPort:                 *argPprofPort,
		MacLearningFallback:       *argMacLearningFallback,
		NodeName:                  os.Getenv(util.EnvNodeName),
		PodNamespace:              os.Getenv(util.EnvPodNamespace),
		PodName:                   os.Getenv(util.EnvPodName),
		ServiceClusterIPRange:     *argServiceClusterIPRange,
		ClusterRouter:             *argClusterRouter,
		NodeSwitch:                *argNodeSwitch,
		EncapChecksum:             *argEncapChecksum,
		NetworkType:               *argsNetworkType,
		DefaultProviderName:       *argsDefaultProviderName,
		DefaultInterfaceName:      *argsDefaultInterfaceName,
		ExternalGatewayConfigNS:   *argExternalGatewayConfigNS,
		ExternalGatewaySwitch:     *argExternalGatewaySwitch,
		EnableMetrics:             *argEnableMetrics,
		EnableOVNIPSec:            *argEnableOVNIPSec,
		EnableArpDetectIPConflict: *argEnableArpDetectIPConflict,
		KubeletDir:                *argKubeletDir,
		EnableVerboseConnCheck:    *argEnableVerboseConnCheck,
		TCPConnCheckPort:          *argTCPConnectivityCheckPort,
		UDPConnCheckPort:          *argUDPConnectivityCheckPort,
		EnableTProxy:              *argEnableTProxy,
		OVSVsctlConcurrency:       *argOVSVsctlConcurrency,
		SetVxlanTxOff:             *argSetVxlanTxOff,
		LogPerm:                   *argLogPerm,
		TLSMinVersion:             *argTLSMinVersion,
		TLSMaxVersion:             *argTLSMaxVersion,
		TLSCipherSuites:           *argTLSCipherSuites,
		CertManagerIPSecCert:      *argCertManagerIPSecCert,
		CertManagerIssuerName:     *argCertManagerIssuerName,
		IPSecCertDuration:         *argOVNIPSecCertDuration,

		// Node-local access to eip in VPC NAT Gateway pod
		EnableNodeLocalAccessVpcNatGwEIP: *argEnableNodeLocalAccessVpcNatGwEIP,
	}

	return config
}

func (config *Configuration) Init(nicBridgeMappings map[string]string) error {
	if config.NodeName == "" {
		klog.Info("node name not specified in command line parameters, fall back to the environment variable")
		if config.NodeName = strings.ToLower(os.Getenv(util.EnvNodeName)); config.NodeName == "" {
			klog.Info("node name not specified in environment variables, fall back to the hostname")
			hostname, err := os.Hostname()
			if err != nil {
				return fmt.Errorf("failed to get hostname: %w", err)
			}
			config.NodeName = strings.ToLower(hostname)
		}
	}

	if err := config.initKubeClient(); err != nil {
		klog.Error(err)
		return err
	}
	if err := config.initNicConfig(nicBridgeMappings); err != nil {
		klog.Error(err)
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
	config.NodeIPv4, config.NodeIPv6 = util.GetNodeInternalIP(*node)
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
			return fmt.Errorf("failed to get src IPs by routes on interface %s: %w", iface.Name, err)
		}
		addrs, err := iface.Addrs()
		if err != nil {
			return fmt.Errorf("failed to get iface addr. %w", err)
		}
		for _, addr := range addrs {
			_, ipCidr, err := net.ParseCIDR(addr.String())
			if err != nil {
				klog.Errorf("Failed to parse CIDR address %s: %v, skipping", addr.String(), err)
				continue
			}
			// exclude the vip as encap ip unless host-tunnel-src is true
			if ones, bits := ipCidr.Mask.Size(); ones == bits && !config.HostTunnelSrc {
				klog.Infof("Skip address %s", ipCidr.String())
				continue
			}

			// exclude link-local and loopback addresses
			ipStr := strings.Split(addr.String(), "/")[0]
			if ip := net.ParseIP(ipStr); ip == nil || ip.IsLinkLocalUnicast() || ip.IsLoopback() {
				continue
			}
			if len(srcIPs) == 0 || slices.Contains(srcIPs, ipStr) {
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

	config.MSS = config.MTU - util.TCPIPHeaderLength

	if err := setChecksum(config.EncapChecksum); err != nil {
		klog.Errorf("failed to set checksum offload, %v", err)
	}

	if err = config.initRuntimeConfig(node); err != nil {
		klog.Error(err)
		return err
	}

	config.DefaultEncapIP = encapIP
	networks, err := parseNodeNetworks(node)
	if err != nil {
		klog.Errorf("failed to parse node networks, using empty networks: %v", err)
	}
	config.NodeNetworks = networks
	return config.setEncapIPs()
}

func (config *Configuration) getEncapIP(node *corev1.Node) string {
	if podIP := os.Getenv(util.EnvPodIP); podIP != "" {
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
	if err = util.DialAPIServer(cfg.Host, 3*time.Second, 10); err != nil {
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

	cfg.ContentType = util.ContentTypeProtobuf
	cfg.AcceptContentTypes = util.AcceptContentTypes
	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		klog.Errorf("init kubernetes client failed %v", err)
		return err
	}
	config.KubeClient = kubeClient

	if config.CertManagerIPSecCert {
		cfg.ContentType = util.ContentTypeJSON
		cmClient, err := certmanagerclientset.NewForConfig(cfg)
		if err != nil {
			klog.Errorf("init certmanager client failed %v", err)
			return err
		}
		config.CertManagerClient = cmClient
	}

	return nil
}

func parseNodeNetworks(node *corev1.Node) (map[string]string, error) {
	networks := make(map[string]string)
	if node == nil || node.Annotations == nil {
		return networks, nil
	}

	annotation := node.Annotations[util.NodeNetworksAnnotation]
	if annotation == "" {
		return networks, nil
	}

	if err := json.Unmarshal([]byte(annotation), &networks); err != nil {
		return nil, fmt.Errorf("failed to parse node networks annotation %q: %w", annotation, err)
	}

	for networkName, ip := range networks {
		if net.ParseIP(ip) == nil {
			return nil, fmt.Errorf("invalid encap IP %q for network %q", ip, networkName)
		}
	}

	return networks, nil
}

func (config *Configuration) setEncapIPs() error {
	config.nodeNetworksMutex.RLock()
	networks := config.NodeNetworks
	defaultIP := config.DefaultEncapIP
	config.nodeNetworksMutex.RUnlock()

	ips := []string{defaultIP}
	for _, ip := range networks {
		if ip != defaultIP && !slices.Contains(ips, ip) {
			ips = append(ips, ip)
		}
	}

	encapIPStr := strings.Join(ips, ",")
	// #nosec G204
	raw, err := exec.Command(
		"ovs-vsctl", "set", "open", ".", "external-ids:ovn-encap-ip="+encapIPStr).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to set ovn-encap-ip, %s", string(raw))
	}

	// #nosec G204
	raw, err = exec.Command(
		"ovs-vsctl", "set", "open", ".", "external-ids:ovn-encap-ip-default="+defaultIP).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to set ovn-encap-ip-default, %s", string(raw))
	}

	klog.Infof("set ovn-encap-ip=%s, ovn-encap-ip-default=%s", encapIPStr, defaultIP)
	return nil
}

func (config *Configuration) UpdateNodeNetworks(node *corev1.Node) error {
	newNetworks, err := parseNodeNetworks(node)
	if err != nil {
		return err
	}

	config.nodeNetworksMutex.Lock()
	config.NodeNetworks = newNetworks
	config.nodeNetworksMutex.Unlock()

	return config.setEncapIPs()
}

func (config *Configuration) GetEncapIPByNetwork(networkName string) (string, error) {
	if networkName == "" {
		return config.DefaultEncapIP, nil
	}

	config.nodeNetworksMutex.RLock()
	defer config.nodeNetworksMutex.RUnlock()

	if ip, ok := config.NodeNetworks[networkName]; ok {
		return ip, nil
	}

	return "", fmt.Errorf("network %s not found in node networks", networkName)
}

func setChecksum(encapChecksum bool) error {
	// #nosec G204
	raw, err := exec.Command("ovs-vsctl", "set", "open", ".", fmt.Sprintf("external-ids:ovn-encap-csum=%v", encapChecksum)).CombinedOutput()
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to set ovn-encap-csum to %v: %s", encapChecksum, string(raw))
	}
	return nil
}
