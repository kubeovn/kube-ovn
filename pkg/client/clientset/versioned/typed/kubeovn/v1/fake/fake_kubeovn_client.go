/*
Copyright The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Code generated by client-gen. DO NOT EDIT.

package fake

import (
	v1 "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/typed/kubeovn/v1"
	rest "k8s.io/client-go/rest"
	testing "k8s.io/client-go/testing"
)

type FakeKubeovnV1 struct {
	*testing.Fake
}

func (c *FakeKubeovnV1) HtbQoses() v1.HtbQosInterface {
	return &FakeHtbQoses{c}
}

func (c *FakeKubeovnV1) IPs() v1.IPInterface {
	return &FakeIPs{c}
}

func (c *FakeKubeovnV1) IptablesDnatRules() v1.IptablesDnatRuleInterface {
	return &FakeIptablesDnatRules{c}
}

func (c *FakeKubeovnV1) IptablesEIPs() v1.IptablesEIPInterface {
	return &FakeIptablesEIPs{c}
}

func (c *FakeKubeovnV1) IptablesFIPRules() v1.IptablesFIPRuleInterface {
	return &FakeIptablesFIPRules{c}
}

func (c *FakeKubeovnV1) IptablesSnatRules() v1.IptablesSnatRuleInterface {
	return &FakeIptablesSnatRules{c}
}

func (c *FakeKubeovnV1) ProviderNetworks() v1.ProviderNetworkInterface {
	return &FakeProviderNetworks{c}
}

func (c *FakeKubeovnV1) SecurityGroups() v1.SecurityGroupInterface {
	return &FakeSecurityGroups{c}
}

func (c *FakeKubeovnV1) Subnets() v1.SubnetInterface {
	return &FakeSubnets{c}
}

func (c *FakeKubeovnV1) SwitchLBRules() v1.SwitchLBRuleInterface {
	return &FakeSwitchLBRules{c}
}

func (c *FakeKubeovnV1) Vips() v1.VipInterface {
	return &FakeVips{c}
}

func (c *FakeKubeovnV1) Vlans() v1.VlanInterface {
	return &FakeVlans{c}
}

func (c *FakeKubeovnV1) Vpcs() v1.VpcInterface {
	return &FakeVpcs{c}
}

func (c *FakeKubeovnV1) VpcNatGateways() v1.VpcNatGatewayInterface {
	return &FakeVpcNatGateways{c}
}

// RESTClient returns a RESTClient that is used to communicate
// with API server by this client implementation.
func (c *FakeKubeovnV1) RESTClient() rest.Interface {
	var ret *rest.RESTClient
	return ret
}
