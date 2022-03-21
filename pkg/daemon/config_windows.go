package daemon

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/Microsoft/hcsshim"
	"github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

const defaultMTU = 1500

// ParseFlags will parse cmd args then init kubeClient and configuration
// TODO: validate configuration
func ParseFlags(nicBridgeMappings map[string]string) (*Configuration, error) {
	var (
		argNodeName              = pflag.String("node-name", "", "Node name")
		argIface                 = pflag.String("iface", "", "The iface used to inter-host pod communication, can be a nic name or a group of regex separated by comma (default the default route iface)")
		argDPDKTunnelIface       = pflag.String("dpdk-tunnel-iface", "br-phy", "Specifies the name of the dpdk tunnel iface.")
		argMTU                   = pflag.Int("mtu", 0, "The MTU used by pod iface in overlay networks (default iface MTU - 100)")
		argEnableMirror          = pflag.Bool("enable-mirror", false, "Enable traffic mirror (default false)")
		argMirrorNic             = pflag.String("mirror-iface", "mirror0", "The mirror nic name that will be created by kube-ovn")
		argBindSocket            = pflag.String("bind-socket", util.WindowsListenPipe, "The socket daemon bind to.")
		argOvsSocket             = pflag.String("ovs-socket", "", "The socket to local ovs-server")
		argKubeConfigFile        = pflag.String("kubeconfig", "", "Path to kubeconfig file with authorization and master location information. If not set use the inCluster token.")
		argServiceClusterIPRange = pflag.String("service-cluster-ip-range", "10.96.0.0/12", "The kubernetes service cluster ip range")
		argNodeLocalDnsIP        = pflag.String("node-local-dns-ip", "", "If use nodelocaldns the local dns server ip should be set here.")
		argEncapChecksum         = pflag.Bool("encap-checksum", true, "Enable checksum")
		argPprofPort             = pflag.Int("pprof-port", 10665, "The port to get profiling data")

		argsNetworkType            = pflag.String("network-type", "geneve", "The ovn network type")
		argsCniConfName            = pflag.String("cni-conf-name", "01-kube-ovn.conflist", "Specify the name of kube ovn conflist name in dir /etc/cni/net.d/, default: 01-kube-ovn.conflist")
		argsDefaultProviderName    = pflag.String("default-provider-name", "provider", "The vlan or vxlan type default provider interface name")
		argsDefaultInterfaceName   = pflag.String("default-interface-name", "", "The default host interface name in the vlan/vxlan type")
		argExternalGatewayConfigNS = pflag.String("external-gateway-config-ns", "kube-system", "The namespace of configmap external-gateway-config, default: kube-system")
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
				klog.Fatalf("failed to set flag, %v", err)
			}
		}
	})

	pflag.CommandLine.AddGoFlagSet(klogFlags)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.Parse()

	nodeName := *argNodeName
	if nodeName == "" {
		klog.Info("node name not specified, fall back to hostname")
		var err error
		if nodeName, err = os.Hostname(); err != nil {
			return nil, fmt.Errorf("failed to get hostname: %v", err)
		}
	}
	config := &Configuration{
		Iface:                   *argIface,
		DPDKTunnelIface:         *argDPDKTunnelIface,
		MTU:                     *argMTU,
		EnableMirror:            *argEnableMirror,
		MirrorNic:               *argMirrorNic,
		BindSocket:              *argBindSocket,
		OvsSocket:               *argOvsSocket,
		KubeConfigFile:          *argKubeConfigFile,
		PprofPort:               *argPprofPort,
		NodeName:                strings.ToLower(nodeName),
		ServiceClusterIPRange:   *argServiceClusterIPRange,
		NodeLocalDnsIP:          *argNodeLocalDnsIP,
		EncapChecksum:           *argEncapChecksum,
		NetworkType:             *argsNetworkType,
		CniConfName:             *argsCniConfName,
		DefaultProviderName:     *argsDefaultProviderName,
		DefaultInterfaceName:    *argsDefaultInterfaceName,
		ExternalGatewayConfigNS: *argExternalGatewayConfigNS,
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

func (config *Configuration) initNicConfig(_ map[string]string) error {
	if err := config.initHnsNetwork(); err != nil {
		return err
	}

	if config.MTU == 0 {
		config.MTU = defaultMTU - util.GeneveHeaderLength
	}

	if !config.EncapChecksum {
		if err := disableChecksum(); err != nil {
			klog.Errorf("failed to set checksum offload, %v", err)
		}
	}

	return nil
}

func (config *Configuration) initHnsNetwork() error {
	netName := "kube-ovn"
	hnsNetwork, err := hcsshim.GetHNSNetworkByName(netName)
	if err != nil {
		klog.Errorf("failed to get HNS network %s: %v", netName)
		return err
	}

	if hnsNetwork != nil {
		// TODO: check more network settings
		if !strings.EqualFold(hnsNetwork.Type, "Transparent") {
			err = fmt.Errorf(`type of HNS network %s is "%s", while "Transparent" is required`, netName, hnsNetwork.Type)
			klog.Error(err)
			return err
		}
		return nil
	}

	var (
		iface   *net.Interface
		addrs   []net.Addr
		encapIP string
	)

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

	if config.Iface == "" {
		// support IPv4 only now
		nodeIP, _ := util.GetNodeInternalIP(*node)
		if nodeIP == "" {
			klog.Errorf("failed to get internal IPv4 for node %s", config.NodeName)
			return err
		}
		if iface, addrs, err = getIfaceByIP(nodeIP); err != nil {
			klog.Errorf("failed to get interface by IP %s: %v", nodeIP, err)
			return err
		}
		encapIP = nodeIP
	} else {
		if iface, err = findInterface(config.Iface); err != nil {
			klog.Errorf("failed to find iface %s, %v", config.Iface, err)
			return err
		}
		if addrs, err = iface.Addrs(); err != nil {
			return fmt.Errorf("failed to get addresses of interface %s: %v", iface.Name, err)
		}
		if len(addrs) == 0 {
			return fmt.Errorf("iface %s has no ip address", config.Iface)
		}
		encapIP = strings.Split(addrs[0].String(), "/")[0]
	}

	if err = createHnsNetwork(netName, "10.99.0.0/16", "10.99.0.1", encapIP, iface, addrs); err != nil {
		return err
	}

	return nil
}

func createHnsNetwork(name string, subnet, gateway string, encapIP string, iface *net.Interface, addrs []net.Addr) error {
	network := &hcsshim.HNSNetwork{
		Name:               name,
		Type:               "Transparent",
		NetworkAdapterName: iface.Name,
		Subnets: []hcsshim.Subnet{
			{
				AddressPrefix:  subnet,
				GatewayAddress: gateway,
			},
		},
		ManagementIP: encapIP,
		SourceMac:    iface.HardwareAddr.String(),
	}
	if _, err := network.Create(); err != nil {
		klog.Errorf("failed to create hns network: %v", err)
		return err
	}
	return nil
}
