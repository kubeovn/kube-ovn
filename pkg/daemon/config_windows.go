package daemon

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/Microsoft/hcsshim"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const defaultBindSocket = util.WindowsListenPipe

func getSrcIPsByRoutes(iface *net.Interface) ([]string, error) {
	// to be implemented in the future
	return nil, nil
}

func getIfaceByIP(ip string) (string, int, error) {
	iface, err := util.GetInterfaceByIP(ip)
	if err != nil {
		klog.Error(err)
		return "", 0, err
	}

	return iface.InterfaceAlias, int(iface.NlMtu), err
}

func (config *Configuration) initRuntimeConfig(node *corev1.Node) error {
	hnsNetwork, err := hcsshim.GetHNSNetworkByName(util.HnsNetwork)
	if err != nil {
		if !hcsshim.IsNotExist(err) {
			klog.Errorf("failed to get HNS network %s: %v", util.HnsNetwork, err)
			return err
		}
	}

	if hnsNetwork != nil {
		if !strings.EqualFold(hnsNetwork.Type, "transparent") {
			err = fmt.Errorf(`type of HNS network %s is "%s", while "transparent" is required`, util.HnsNetwork, hnsNetwork.Type)
			klog.Error(err)
			return err
		}
		config.Iface = hnsNetwork.NetworkAdapterName
	} else {
		// dual stack is not supported on windows currently
		nodeIP, ipv6 := util.GetNodeInternalIP(*node)
		if nodeIP == "" {
			nodeIP = ipv6
		}
		if nodeIP == "" {
			return fmt.Errorf("failed to get IP of node %s", node.Name)
		}

		// create hns network
		if err = config.createHnsNetwork(util.HnsNetwork, config.Iface, util.CheckProtocol(nodeIP)); err != nil {
			return err
		}

		// ideas from antrea: https://github.com/antrea-io/antrea/pull/3067
		commands := [...]string{
			fmt.Sprintf(`$name = $(Get-VMNetworkAdapter -managementos -SwitchName %s).Name;`, util.HnsNetwork),
			`Rename-VMNetworkAdapter -ManagementOS -ComputerName "$(hostname)" -Name "$name" -NewName br-provider;`,
			`Rename-NetAdapter -name "vEthernet (br-provider)" -NewName br-provider;`,
		}
		if _, err = util.Powershell(strings.Join(commands[:], " ")); err != nil {
			return err
		}
	}

	if _, err = util.Powershell(fmt.Sprintf(`Enable-VMSwitchExtension "Open vSwitch Extension" "%s"`, util.HnsNetwork)); err != nil {
		return err
	}

	output, err := ovs.Exec(ovs.MayExist, "add-br", "br-int")
	if err != nil {
		return fmt.Errorf("failed to add OVS bridge %s, %v: %q", "br-int", err, output)
	}
	if output, err = ovs.Exec(ovs.MayExist, "add-br", "br-provider", "--", "set", "bridge", "br-provider", "external_ids:vendor="+util.CniTypeName); err != nil {
		return fmt.Errorf("failed to add OVS bridge %s, %v: %q", "br-provider", err, output)
	}
	if output, err = ovs.Exec(ovs.MayExist, "add-port", "br-provider", config.Iface, "--", "set", "port", config.Iface, "external_ids:vendor="+util.CniTypeName); err != nil {
		return fmt.Errorf("failed to add OVS port %s, %v: %q", config.Iface, err, output)
	}

	if err = util.EnableAdapter("br-int"); err != nil {
		return fmt.Errorf("failed to enable network adapter %s: %v", "br-int", err)
	}

	return nil
}

func (config *Configuration) createHnsNetwork(name, adapter, protocol string) error {
	subnets, err := config.KubeOvnClient.KubeovnV1().Subnets().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		klog.Errorf("failed to list subnets: %v", err)
		return err
	}

	hcsSubnets := make([]hcsshim.Subnet, 0, len(subnets.Items))
	for _, subnet := range subnets.Items {
		for _, cidr := range strings.Split(subnet.Spec.CIDRBlock, ",") {
			if util.CheckProtocol(cidr) == protocol {
				var gateway string
				for _, gw := range strings.Split(subnet.Spec.Gateway, ",") {
					if util.CheckProtocol(gw) == protocol {
						gateway = gw
						break
					}
				}
				if gateway == "" {
					if gateway, err = util.GetGwByCidr(cidr); err != nil {
						klog.Errorf("failed to get gateway of CIDR %s: %v", cidr, err)
						return err
					}
				}

				hcsSubnets = append(hcsSubnets, hcsshim.Subnet{
					AddressPrefix:  cidr,
					GatewayAddress: gateway,
				})
				break
			}
		}
	}

	network := &hcsshim.HNSNetwork{
		Name:               name,
		Type:               "transparent",
		NetworkAdapterName: adapter,
		Subnets:            hcsSubnets,
	}
	if _, err := network.Create(); err != nil {
		klog.Errorf("failed to create hns network: %v", err)
		return err
	}
	return nil
}
