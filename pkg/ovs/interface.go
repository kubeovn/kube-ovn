package ovs

import (
	netv1 "k8s.io/api/networking/v1"

	"github.com/ovn-org/libovsdb/ovsdb"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

type NbGlobal interface {
	UpdateNbGlobal(nbGlobal *ovnnb.NBGlobal, fields ...interface{}) error
	SetAzName(azName string) error
	SetUseCtInvMatch() error
	SetICAutoRoute(enable bool, blackList []string) error
	SetLBCIDR(serviceCIDR string) error
	SetLsDnatModDlDst(enabled bool) error
	GetNbGlobal() (*ovnnb.NBGlobal, error)
}

type LogicalRouter interface {
	CreateLogicalRouter(lrName string) error
	DeleteLogicalRouter(lrName string) error
	LogicalRouterUpdateLoadBalancers(lrName string, op ovsdb.Mutator, lbNames ...string) error
	GetLogicalRouter(lrName string, ignoreNotFound bool) (*ovnnb.LogicalRouter, error)
	ListLogicalRouter(needVendorFilter bool, filter func(lr *ovnnb.LogicalRouter) bool) ([]ovnnb.LogicalRouter, error)
	LogicalRouterExists(name string) (bool, error)
}

type LogicalRouterPort interface {
	CreatePeerRouterPort(localRouter, remoteRouter, localRouterPortIP string) error
	CreateLogicalRouterPort(lrName string, lrpName, mac string, networks []string) error
	UpdateLogicalRouterPortRA(lrpName, ipv6RAConfigsStr string, enableIPv6RA bool) error
	DeleteLogicalRouterPort(lrpName string) error
	DeleteLogicalRouterPorts(externalIDs map[string]string, filter func(lrp *ovnnb.LogicalRouterPort) bool) error
	GetLogicalRouterPort(lrpName string, ignoreNotFound bool) (*ovnnb.LogicalRouterPort, error)
	ListLogicalRouterPorts(externalIDs map[string]string, filter func(lrp *ovnnb.LogicalRouterPort) bool) ([]ovnnb.LogicalRouterPort, error)
	LogicalRouterPortExists(lrpName string) (bool, error)
}

type LogicalSwitch interface {
	CreateLogicalSwitch(lsName, lrName, cidrBlock, gateway string, needRouter, randomAllocateGW bool) error
	CreateBareLogicalSwitch(lsName string) error
	LogicalSwitchUpdateLoadBalancers(lsName string, op ovsdb.Mutator, lbNames ...string) error
	DeleteLogicalSwitch(lsName string) error
	ListLogicalSwitch(needVendorFilter bool, filter func(lr *ovnnb.LogicalSwitch) bool) ([]ovnnb.LogicalSwitch, error)
	LogicalSwitchExists(lsName string) (bool, error)
}

type LogicalSwitchPort interface {
	CreateLogicalSwitchPort(lsName, lspName, ip, mac, podName, namespace string, portSecurity bool, securityGroups string, vips string, enableDHCP bool, dhcpOptions *DHCPOptionsUUIDs, vpc string) error
	CreateBareLogicalSwitchPort(lsName, lspName, ip, mac string) error
	CreateLocalnetLogicalSwitchPort(lsName, lspName, provider string, vlanID int) error
	CreateVirtualLogicalSwitchPorts(lsName string, ips ...string) error
	SetLogicalSwitchPortSecurity(portSecurity bool, lspName, mac, ips, vips string) error
	SetLogicalSwitchPortVirtualParents(lsName, parents string, ips ...string) error
	SetLogicalSwitchPortExternalIds(lspName string, externalIds map[string]string) error
	SetLogicalSwitchPortVlanTag(lspName string, vlanID int) error
	SetLogicalSwitchPortsSecurityGroup(sgName string, op string) error
	UpdateLogicalSwitchAcl(lsName string, subnetAcls []kubeovnv1.Acl) error
	EnablePortLayer2forward(lspName string) error
	DeleteLogicalSwitchPort(lspName string) error
	ListLogicalSwitchPorts(needVendorFilter bool, externalIDs map[string]string, filter func(lsp *ovnnb.LogicalSwitchPort) bool) ([]ovnnb.LogicalSwitchPort, error)
	ListNormalLogicalSwitchPorts(needVendorFilter bool, externalIDs map[string]string) ([]ovnnb.LogicalSwitchPort, error)
	ListLogicalSwitchPortsWithLegacyExternalIDs() ([]ovnnb.LogicalSwitchPort, error)
	GetLogicalSwitchPort(lspName string, ignoreNotFound bool) (*ovnnb.LogicalSwitchPort, error)
	LogicalSwitchPortExists(name string) (bool, error)
}

type LoadBalancer interface {
	CreateLoadBalancer(lbName, protocol, selectFields string) error
	LoadBalancerAddVip(lbName, vip string, backends ...string) error
	LoadBalancerDeleteVip(lbName, vip string) error
	SetLoadBalancerAffinityTimeout(lbName string, timeout int) error
	DeleteLoadBalancers(filter func(lb *ovnnb.LoadBalancer) bool) error
	GetLoadBalancer(lbName string, ignoreNotFound bool) (*ovnnb.LoadBalancer, error)
	ListLoadBalancers(filter func(lb *ovnnb.LoadBalancer) bool) ([]ovnnb.LoadBalancer, error)
	LoadBalancerExists(lbName string) (bool, error)
}

type PortGroup interface {
	CreatePortGroup(pgName string, externalIDs map[string]string) error
	PortGroupAddPorts(pgName string, lspNames ...string) error
	PortGroupRemovePorts(pgName string, lspNames ...string) error
	PortGroupSetPorts(pgName string, ports []string) error
	DeletePortGroup(pgName string) error
	ListPortGroups(externalIDs map[string]string) ([]ovnnb.PortGroup, error)
	GetPortGroup(pgName string, ignoreNotFound bool) (*ovnnb.PortGroup, error)
	PortGroupExists(pgName string) (bool, error)
}

type ACL interface {
	CreateIngressAcl(pgName, asIngressName, asExceptName, protocol string, npp []netv1.NetworkPolicyPort) error
	CreateEgressAcl(pgName, asEgressName, asExceptName, protocol string, npp []netv1.NetworkPolicyPort) error
	CreateGatewayAcl(pgName, gateway string) error
	CreateNodeAcl(pgName, nodeIp string) error
	CreateSgDenyAllAcl(sgName string) error
	UpdateSgAcl(sg *kubeovnv1.SecurityGroup, direction string) error
	SetAclLog(pgName string, logEnable, isIngress bool) error
	SetLogicalSwitchPrivate(lsName, cidrBlock string, allowSubnets []string) error
	DeleteAcls(parentName, parentType string, direction string) error
}

type AddressSet interface {
	CreateAddressSet(asName string, externalIDs map[string]string) error
	AddressSetUpdateAddress(asName string, addresses ...string) error
	DeleteAddressSet(asName string) error
	DeleteAddressSets(externalIDs map[string]string) error
	ListAddressSets(externalIDs map[string]string) ([]ovnnb.AddressSet, error)
}

type LogicalRouterStaticRoute interface {
	AddLogicalRouterStaticRoute(lrName, policy, cidrBlock, nextHops, routeType string) error
	ClearLogicalRouterStaticRoute(lrName string) error
	DeleteLogicalRouterStaticRoute(lrName, policy, prefix, nextHop, routeType string) error
	GetLogicalRouterRouteByOpts(key, value string) ([]ovnnb.LogicalRouterStaticRoute, error)
	ListLogicalRouterStaticRoutes(externalIDs map[string]string) ([]ovnnb.LogicalRouterStaticRoute, error)
	LogicalRouterStaticRouteExists(lrName, policy, prefix, nextHop, routeType string) (bool, error)
}

type LogicalRouterPolicy interface {
	AddLogicalRouterPolicy(lrName string, priority int, match, action string, nextHops []string, externalIDs map[string]string) error
	DeleteLogicalRouterPolicy(lrName string, priority int, match string) error
	DeleteLogicalRouterPolicies(lrName string, priority int, externalIDs map[string]string) error
	DeleteLogicalRouterPolicyByUUID(lrName string, uuid string) error
	ClearLogicalRouterPolicy(lrName string) error
	ListLogicalRouterPolicies(priority int, externalIDs map[string]string) ([]ovnnb.LogicalRouterPolicy, error)
	GetLogicalRouterPolicy(lrName string, priority int, match string, ignoreNotFound bool) (*ovnnb.LogicalRouterPolicy, error)
}

type NAT interface {
	UpdateSnat(lrName, externalIP, logicalIP string) error
	UpdateDnatAndSnat(lrName, externalIP, logicalIP, lspName, externalMac, gatewayType string) error
	DeleteNats(lrName, natType, logicalIP string) error
	DeleteNat(lrName, natType, externalIP, logicalIP string) error
	NatExists(lrName, natType, externalIP, logicalIP string) (bool, error)
	ListNats(natType, logicalIP string, externalIDs map[string]string) ([]ovnnb.NAT, error)
}

type DHCPOptions interface {
	UpdateDHCPOptions(subnet *kubeovnv1.Subnet) (*DHCPOptionsUUIDs, error)
	DeleteDHCPOptions(lsName string, protocol string) error
	DeleteDHCPOptionsByUUIDs(uuidList ...string) error
	ListDHCPOptions(needVendorFilter bool, externalIDs map[string]string) ([]ovnnb.DHCPOptions, error)
}

type OvnClient interface {
	NbGlobal
	LogicalRouter
	LogicalRouterPort
	LogicalSwitch
	LogicalSwitchPort
	LoadBalancer
	PortGroup
	ACL
	AddressSet
	LogicalRouterStaticRoute
	LogicalRouterPolicy
	NAT
	DHCPOptions
	CreateGatewayLogicalSwitch(lsName, lrName, provider, ip, mac string, vlanID int, chassises ...string) error
	CreateLogicalPatchPort(lsName, lrName, lspName, lrpName, ip, mac string, chassises ...string) error
	RemoveLogicalPatchPort(lspName, lrpName string) error
	DeleteLogicalGatewaySwitch(lsName, lrName string) error
	DeleteSecurityGroup(sgName string) error
	GetEntityInfo(entity interface{}) error
}
