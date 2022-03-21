package daemon

import (
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"strings"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	clientset "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// Configuration is the daemon conf
type Configuration struct {
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
	NodeLocalDnsIP          string
	EncapChecksum           bool
	PprofPort               int
	NetworkType             string
	CniConfName             string
	DefaultProviderName     string
	DefaultInterfaceName    string
	ExternalGatewayConfigNS string
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
			klog.Infof("use %s as tunnel interface", iface.Name)
			return &iface, nil
		}
	}
	klog.Errorf("network interface %s not exist", ifaceStr)
	return nil, fmt.Errorf("network interface %s not exist", ifaceStr)
}

func getIfaceByIP(ip string) (*net.Interface, []net.Addr, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, nil, err
	}

	for _, iface := range interfaces {
		addresses, err := iface.Addrs()
		if err != nil {
			return nil, nil, err
		}

		for _, addr := range addresses {
			if addr.String() == ip {
				return &iface, addresses, nil
			}
		}
	}

	return nil, nil, fmt.Errorf("failed to find interface by IP %s", ip)
}
