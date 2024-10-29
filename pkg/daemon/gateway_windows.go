package daemon

import kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"

func (c *Controller) setIPSet() error {
	return nil
}

func (c *Controller) setPolicyRouting() error {
	return nil
}

func (c *Controller) setIptables() error {
	return nil
}

func (c *Controller) gcIPSet() error {
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
