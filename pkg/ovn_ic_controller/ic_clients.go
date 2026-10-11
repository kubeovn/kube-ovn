package ovn_ic_controller

import (
	"fmt"

	"github.com/kubeovn/kube-ovn/pkg/ovs"
)

func (c *Controller) ensureICNbClient(host, port string) error {
	if host == "" || port == "" {
		return fmt.Errorf("IC NB endpoint is incomplete: host=%q port=%q", host, port)
	}
	address := genHostAddress(host, port)
	if c.icNbClient != nil && c.icNbAddress == address {
		return nil
	}
	client, err := ovs.NewOvnICNbClient(
		address,
		c.config.OvnTimeout,
		c.config.OvsDbConnectTimeout,
		c.config.OvsDbInactivityTimeout,
		c.config.OvsDbConnectMaxRetry,
	)
	if err != nil {
		return fmt.Errorf("create IC NB client for %s: %w", address, err)
	}
	if c.icNbClient != nil {
		c.icNbClient.Close()
	}
	c.icNbClient = client
	c.ICNbTables = client
	c.icNbAddress = address
	return nil
}

func (c *Controller) ensureICSbClient(host, port string) error {
	if host == "" || port == "" {
		return fmt.Errorf("IC SB endpoint is incomplete: host=%q port=%q", host, port)
	}
	address := genHostAddress(host, port)
	if c.icSbClient != nil && c.icSbAddress == address {
		return nil
	}
	client, err := ovs.NewOvnICSbClient(
		address,
		c.config.OvnTimeout,
		c.config.OvsDbConnectTimeout,
		c.config.OvsDbInactivityTimeout,
		c.config.OvsDbConnectMaxRetry,
	)
	if err != nil {
		return fmt.Errorf("create IC SB client for %s: %w", address, err)
	}
	if c.icSbClient != nil {
		c.icSbClient.Close()
	}
	c.icSbClient = client
	c.ICSbTables = client
	c.icSbAddress = address
	return nil
}

func (c *Controller) closeICClients() {
	if c.icNbClient != nil {
		c.icNbClient.Close()
	}
	if c.icSbClient != nil {
		c.icSbClient.Close()
	}
	c.icNbClient = nil
	c.icSbClient = nil
	c.ICNbTables = nil
	c.ICSbTables = nil
	c.icNbAddress = ""
	c.icSbAddress = ""
}
