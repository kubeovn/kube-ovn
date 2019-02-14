package controller

import "bitbucket.org/mathildetech/kube-ovn/pkg/ovs"

func InitDefaultLogicalSwitch(config *Configuration) error {
	client := ovs.NewClient(config.OvnNbHost, config.OvnNbPort)
	return client.CreateLogicalSwitch(config.DefaultLogicalSwitch, config.DefaultCIDR, config.DefaultGateway, config.DefaultExcludeIps)
}
