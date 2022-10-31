package daemon

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) setIPSet() error {
	return nil
}

func (c *Controller) setPolicyRouting() error {
	return nil
}

func (c *Controller) setIptables() error {
	return nil
}

func (c *Controller) addEgressConfig(subnet *kubeovnv1.Subnet, ip string) error {
	// nothing to do on Windows
	return nil
}

func (c *Controller) removeEgressConfig(subnet, ip string) error {
	// nothing to do on Windows
	return nil
}

func (c *Controller) setExGateway() error {
	node, err := c.nodesLister.Get(c.config.NodeName)
	if err != nil {
		klog.Errorf("failed to get node, %v", err)
		return err
	}
	enable := node.Labels[util.ExGatewayLabel]
	if enable == "true" {
		cm, err := c.config.KubeClient.CoreV1().ConfigMaps(c.config.ExternalGatewayConfigNS).Get(context.Background(), util.ExternalGatewayConfig, metav1.GetOptions{})
		if err != nil {
			klog.Errorf("failed to get ovn-external-gw-config, %v", err)
			return err
		}
		// enable external-gw-config without 'external-gw-nic' configured
		// to reuse existing physical network from arg 'external-gateway-net'
		// TODO
		if _, err := ovs.Exec(
			ovs.MayExist, "add-br", "br-external", "--",
			ovs.MayExist, "add-port", "br-external", cm.Data["external-gw-nic"],
		); err != nil {
			return fmt.Errorf("failed to enable external gateway, %v", err)
		}

		output, err := ovs.Exec(ovs.IfExists, "get", "open", ".", "external-ids:ovn-bridge-mappings")
		if err != nil {
			return fmt.Errorf("failed to get external-ids, %v", err)
		}
		bridgeMappings := "external:br-external"
		if output != "" && !util.IsStringIn(bridgeMappings, strings.Split(output, ",")) {
			bridgeMappings = fmt.Sprintf("%s,%s", output, bridgeMappings)
		}

		output, err = ovs.Exec("set", "open", ".", fmt.Sprintf("external-ids:ovn-bridge-mappings=%s", bridgeMappings))
		if err != nil {
			return fmt.Errorf("failed to set bridge-mappings, %v: %q", err, output)
		}
	} else {
		if _, err := ovs.Exec(
			ovs.IfExists, "del-br", "br-external"); err != nil {
			return fmt.Errorf("failed to disable external gateway, %v", err)
		}
	}
	return nil
}

// Generally, the MTU of the interface is set to 1400. But in special cases, a special pod (docker indocker) will introduce the docker0 interface to the pod. The MTU of docker0 is 1500.
// The network application in pod will calculate the TCP MSS according to the MTU of docker0, and then initiate communication with others. After the other party sends a response, the kernel protocol stack of Linux host will send ICMP unreachable message to the other party, indicating that IP fragmentation is needed, which is not supported by the other party, resulting in communication failure.
func (c *Controller) appendMssRule() {
}
