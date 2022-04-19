package daemon

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/vishvananda/netlink"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

const (
	defaultBindSocket  = `/run/openvswitch/kube-ovn-daemon.sock`
	defaultNetworkType = `geneve`
)

func (config *Configuration) initNicConfig(nicBridgeMappings map[string]string) error {
	var (
		iface   *net.Interface
		err     error
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

	isDPDKNode := node.GetLabels()[util.OvsDpTypeLabel] == "userspace"
	if isDPDKNode {
		config.Iface = config.DPDKTunnelIface
	}
	if config.Iface == "" {
		podIP, ok := os.LookupEnv(util.POD_IP)
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
		addrs, err := iface.Addrs()
		if err != nil {
			return fmt.Errorf("failed to get iface addr. %v", err)
		}
		if len(addrs) == 0 {
			return fmt.Errorf("iface %s has no ip address", tunnelNic)
		}
		encapIP = strings.Split(addrs[0].String(), "/")[0]
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
