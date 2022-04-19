package daemon

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

const (
	defaultBindSocket  = util.WindowsListenPipe
	defaultNetworkType = `vxlan`
)

func (config *Configuration) initNicConfig(nicBridgeMappings map[string]string) error {
	node, err := config.KubeClient.CoreV1().Nodes().Get(context.Background(), config.NodeName, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("Failed to get node %s: %v", config.NodeName, err)
		return err
	}

	nodeIP, _ := util.GetNodeInternalIP(*node)
	if nodeIP == "" {
		klog.Errorf("failed to get internal IPv4 for node %s", config.NodeName)
		return err
	}
	iface, mtu, err := getIfaceByIP(nodeIP)
	if err != nil {
		klog.Errorf("failed to get interface by IP %s: %v", nodeIP, err)
		return err
	}

	config.Iface = iface
	if config.MTU == 0 {
		config.MTU = mtu - util.GeneveHeaderLength
	}

	config.MSS = config.MTU - util.TcpIpHeaderLength
	if !config.EncapChecksum {
		if err := disableChecksum(); err != nil {
			klog.Errorf("failed to set checksum offload, %v", err)
		}
	}

	return setEncapIP(nodeIP)
}

func getIfaceByIP(ip string) (string, int, error) {
	iface, err := util.GetInterfaceByIP(ip)
	if err != nil {
		klog.Error(err)
		return "", 0, err
	}

	return iface.InterfaceAlias, int(iface.NlMtu), err
}
