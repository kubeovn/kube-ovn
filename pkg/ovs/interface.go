package ovs

import (
	netv1 "k8s.io/api/networking/v1"

	"github.com/ovn-org/libovsdb/ovsdb"

	v1alpha1 "sigs.k8s.io/network-policy-api/apis/v1alpha1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnsb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

type NBGlobal interface {
	UpdateNbGlobal(nbGlobal *ovnnb.NBGlobal, fields ...interface{}) error
	SetAzName(azName string) error
	SetUseCtInvMatch() error
	SetICAutoRoute(enable bool, blackList []string) error
	SetLsDnatModDlDst(enabled bool) error
	SetLsCtSkipDstLportIPs(enabled bool) error
	SetOVNIPSec(enabled bool) error
	SetNodeLocalDNSIP(nodeLocalDNSIP string) error
	GetNbGlobal() (*ovnnb.NBGlobal, error)
}

type LogicalRouter interface {
	CreateLogicalRouter(lrName string) error
	UpdateLogicalRouter(lr *ovnnb.LogicalRouter, fields ...interface{}) error
	DeleteLogicalRouter(lrName string) error
	LogicalRouterUpdateLoadBalancers(lrName string, op ovsdb.Mutator, lbNames ...string) error
	GetLogicalRouter(lrName string, ignoreNotFound bool) (*ovnnb.LogicalRouter, error)
	ListLogicalRouter(needVendorFilter bool, filter func(lr *ovnnb.LogicalRouter) bool) ([]ovnnb.LogicalRouter, error)
	LogicalRouterExists(name string) (bool, error)
}

type LogicalRouterPort interface {
	CreatePeerRouterPort(localRouter, remoteRouter, localRouterPortIP string) error
	CreateLogicalRouterPort(lrName, lrpName, mac string, networks []string) error
	UpdateLogicalRouterPortRA(lrpName, ipv6RAConfigsStr string, enableIPv6RA bool) error
	UpdateLogicalRouterPortNetworks(lrpName string, networks []string) error
	UpdateLogicalRouterPortOptions(lrpName string, options map[string]string) error
	SetLogicalRouterPortHAChassisGroup(lrpName, haChassisGroupName string) error
	DeleteLogicalRouterPort(lrpName string) error
	DeleteLogicalRouterPorts(externalIDs map[string]string, filter func(lrp *ovnnb.LogicalRouterPort) bool) error
	GetLogicalRouterPort(lrpName string, ignoreNotFound bool) (*ovnnb.LogicalRouterPort, error)
	GetLogicalRouterPortByUUID(uuid string) (*ovnnb.LogicalRouterPort, error)
	ListLogicalRouterPorts(externalIDs map[string]string, filter func(lrp *ovnnb.LogicalRouterPort) bool) ([]ovnnb.LogicalRouterPort, error)
	ListGatewayChassisByLogicalRouterPort(lrpName string, ignoreNotFound bool) ([]ovnnb.GatewayChassis, error)
	LogicalRouterPortExists(lrpName string) (bool, error)
}

type HAChassisGroup interface {
	CreateHAChassisGroup(name string, chassises []string, externalIDs map[string]string) error
	GetHAChassisGroup(name string, ignoreNotFound bool) (*ovnnb.HAChassisGroup, error)
	DeleteHAChassisGroup(name string) error
}

type GatewayChassis interface {
	UpdateGatewayChassis(gwChassis *ovnnb.GatewayChassis, fields ...interface{}) error
}

type BFD interface {
	CreateBFD(lrpName, dstIP string, minRx, minTx, detectMult int, externalIDs map[string]string) (*ovnnb.BFD, error)
	DeleteBFD(uuid string) error
	DeleteBFDByDstIP(lrpName, dstIP string) error
	ListBFDs(lrpName, dstIP string) ([]ovnnb.BFD, error)
	ListDownBFDs(dstIP string) ([]ovnnb.BFD, error)
	ListUpBFDs(dstIP string) ([]ovnnb.BFD, error)
	FindBFD(externalIDs map[string]string) ([]ovnnb.BFD, error)
	UpdateBFD(bfd *ovnnb.BFD, fields ...interface{}) error
	MonitorBFD()
}

type LogicalSwitch interface {
	CreateLogicalSwitch(lsName, lrName, cidrBlock, gateway, gatewayMAC string, needRouter, randomAllocateGW bool) error
	CreateBareLogicalSwitch(lsName string) error
	LogicalSwitchUpdateLoadBalancers(lsName string, op ovsdb.Mutator, lbNames ...string) error
	LogicalSwitchUpdateOtherConfig(lsName string, op ovsdb.Mutator, otherConfig map[string]string) error
	DeleteLogicalSwitch(lsName string) error
	ListLogicalSwitch(needVendorFilter bool, filter func(ls *ovnnb.LogicalSwitch) bool) ([]ovnnb.LogicalSwitch, error)
	LogicalSwitchExists(lsName string) (bool, error)
}

type LogicalSwitchPort interface {
	CreateLogicalSwitchPort(lsName, lspName, ip, mac, podName, namespace string, portSecurity bool, securityGroups, vips string, enableDHCP bool, dhcpOptions *DHCPOptionsUUIDs, vpc string) error
	CreateBareLogicalSwitchPort(lsName, lspName, ip, mac string) error
	CreateLocalnetLogicalSwitchPort(lsName, lspName, provider, cidrBlock string, vlanID int) error
	CreateVirtualLogicalSwitchPorts(lsName string, ips ...string) error
	// create virtual type logical switch port for allowed-address-pair
	CreateVirtualLogicalSwitchPort(lspName, lsName, ip string) error
	// update virtual type logical switch port virtual-parents for allowed-address-pair
	SetVirtualLogicalSwitchPortVirtualParents(lsName, parents string) error
	SetLogicalSwitchPortSecurity(portSecurity bool, lspName, mac, ips, vips string) error
	SetLogicalSwitchPortVirtualParents(lsName, parents string, ips ...string) error
	SetLogicalSwitchPortArpProxy(lspName string, enableArpProxy bool) error
	SetLogicalSwitchPortExternalIDs(lspName string, externalIDs map[string]string) error
	SetLogicalSwitchPortVlanTag(lspName string, vlanID int) error
	SetLogicalSwitchPortsSecurityGroup(sgName, op string) error
	EnablePortLayer2forward(lspName string) error
	DeleteLogicalSwitchPort(lspName string) error
	DeleteLogicalSwitchPorts(externalIDs map[string]string, filter func(lsp *ovnnb.LogicalSwitchPort) bool) error
	ListLogicalSwitchPorts(needVendorFilter bool, externalIDs map[string]string, filter func(lsp *ovnnb.LogicalSwitchPort) bool) ([]ovnnb.LogicalSwitchPort, error)
	ListNormalLogicalSwitchPorts(needVendorFilter bool, externalIDs map[string]string) ([]ovnnb.LogicalSwitchPort, error)
	ListLogicalSwitchPortsWithLegacyExternalIDs() ([]ovnnb.LogicalSwitchPort, error)
	GetLogicalSwitchPort(lspName string, ignoreNotFound bool) (*ovnnb.LogicalSwitchPort, error)
	LogicalSwitchPortExists(name string) (bool, error)
	SetLogicalSwitchPortActivationStrategy(lspName, chassis string) error
	// vm live migrate
	SetLogicalSwitchPortMigrateOptions(lspName, srcNodeName, targetNodeName string) error
	ResetLogicalSwitchPortMigrateOptions(lspName, srcNodeName, targetNodeName string, migratedFail bool) error
	CleanLogicalSwitchPortMigrateOptions(lspName string) error
}

type LoadBalancer interface {
	CreateLoadBalancer(lbName, protocol, selectFields string) error
	LoadBalancerAddVip(lbName, vip string, backends ...string) error
	LoadBalancerDeleteVip(lbName, vip string, ignoreHealthCheck bool) error
	LoadBalancerAddIPPortMapping(lbName, vip string, ipPortMappings map[string]string) error
	LoadBalancerUpdateIPPortMapping(lbName, vip string, ipPortMappings map[string]string) error
	LoadBalancerDeleteIPPortMapping(lbName, vip string) error
	LoadBalancerAddHealthCheck(lbName, vip string, ignoreHealthCheck bool, ipPortMapping, externals map[string]string) error
	LoadBalancerDeleteHealthCheck(lbName, uuid string) error
	SetLoadBalancerAffinityTimeout(lbName string, timeout int) error
	DeleteLoadBalancers(filter func(lb *ovnnb.LoadBalancer) bool) error
	GetLoadBalancer(lbName string, ignoreNotFound bool) (*ovnnb.LoadBalancer, error)
	ListLoadBalancers(filter func(lb *ovnnb.LoadBalancer) bool) ([]ovnnb.LoadBalancer, error)
	LoadBalancerExists(lbName string) (bool, error)
}

type LoadBalancerHealthCheck interface {
	AddLoadBalancerHealthCheck(lbName, vip string, externals map[string]string) error
	CreateLoadBalancerHealthCheck(lbName, vip string, lbhc *ovnnb.LoadBalancerHealthCheck) error
	DeleteLoadBalancerHealthCheck(lbName, vip string) error
	DeleteLoadBalancerHealthChecks(filter func(lbhc *ovnnb.LoadBalancerHealthCheck) bool) error
	GetLoadBalancerHealthCheck(lbName, vip string, ignoreNotFound bool) (*ovnnb.LoadBalancer, *ovnnb.LoadBalancerHealthCheck, error)
	ListLoadBalancerHealthChecks(filter func(lbhc *ovnnb.LoadBalancerHealthCheck) bool) ([]ovnnb.LoadBalancerHealthCheck, error)
	LoadBalancerHealthCheckExists(lbName, vip string) (bool, error)
}

type PortGroup interface {
	CreatePortGroup(pgName string, externalIDs map[string]string) error
	PortGroupAddPorts(pgName string, lspNames ...string) error
	PortGroupRemovePorts(pgName string, lspNames ...string) error
	PortGroupSetPorts(pgName string, ports []string) error
	DeletePortGroup(pgName ...string) error
	ListPortGroups(externalIDs map[string]string) ([]ovnnb.PortGroup, error)
	GetPortGroup(pgName string, ignoreNotFound bool) (*ovnnb.PortGroup, error)
	PortGroupExists(pgName string) (bool, error)
}

type ACL interface {
	UpdateIngressACLOps(pgName, asIngressName, asExceptName, protocol, aclName string, npp []netv1.NetworkPolicyPort, logEnable bool, logACLActions []ovnnb.ACLAction, namedPortMap map[string]*util.NamedPortInfo) ([]ovsdb.Operation, error)
	UpdateEgressACLOps(pgName, asEgressName, asExceptName, protocol, aclName string, npp []netv1.NetworkPolicyPort, logEnable bool, logACLActions []ovnnb.ACLAction, namedPortMap map[string]*util.NamedPortInfo) ([]ovsdb.Operation, error)
	CreateGatewayACL(lsName, pgName, gateway, u2oInterconnectionIP string) error
	CreateNodeACL(pgName, nodeIPStr, joinIPStr string) error
	CreateSgDenyAllACL(sgName string) error
	CreateSgBaseACL(sgName, direction string) error
	UpdateSgACL(sg *kubeovnv1.SecurityGroup, direction string) error
	UpdateLogicalSwitchACL(lsName, cidrBlock string, subnetAcls []kubeovnv1.ACL, allowEWTraffic bool) error
	SetACLLog(pgName string, logEnable, isIngress bool) error
	SetLogicalSwitchPrivate(lsName, cidrBlock, nodeSwitchCIDR string, allowSubnets []string) error
	SGLostACL(sg *kubeovnv1.SecurityGroup) (bool, error)
	DeleteAcls(parentName, parentType, direction string, externalIDs map[string]string) error
	DeleteAclsOps(parentName, parentType, direction string, externalIDs map[string]string) ([]ovsdb.Operation, error)
	UpdateAnpRuleACLOps(pgName, asName, protocol, aclName string, priority int, aclAction ovnnb.ACLAction, logACLActions []ovnnb.ACLAction, rulePorts []v1alpha1.AdminNetworkPolicyPort, isIngress, isBanp bool) ([]ovsdb.Operation, error)
}

type AddressSet interface {
	CreateAddressSet(asName string, externalIDs map[string]string) error
	AddressSetUpdateAddress(asName string, addresses ...string) error
	DeleteAddressSet(asName ...string) error
	DeleteAddressSets(externalIDs map[string]string) error
	ListAddressSets(externalIDs map[string]string) ([]ovnnb.AddressSet, error)
}

type LogicalRouterStaticRoute interface {
	AddLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix string, bfdID *string, nexthops ...string) error
	UpdateLogicalRouterStaticRoute(route *ovnnb.LogicalRouterStaticRoute, fields ...interface{}) error
	ClearLogicalRouterStaticRoute(lrName string) error
	DeleteLogicalRouterStaticRoute(lrName string, routeTable, policy *string, ipPrefix, nextHop string) error
	DeleteLogicalRouterStaticRouteByUUID(lrName, uuid string) error
	DeleteLogicalRouterStaticRouteByExternalIDs(lrName string, externalIDs map[string]string) error
	ListLogicalRouterStaticRoutesByOption(lrName, routeTable, key, value string) ([]*ovnnb.LogicalRouterStaticRoute, error)
	ListLogicalRouterStaticRoutes(lrName string, routeTable, policy *string, ipPrefix string, externalIDs map[string]string) ([]*ovnnb.LogicalRouterStaticRoute, error)
	LogicalRouterStaticRouteExists(lrName, routeTable, policy, ipPrefix, nexthop string) (bool, error)
}

type LogicalRouterPolicy interface {
	AddLogicalRouterPolicy(lrName string, priority int, match, action string, nextHops, bfdSessions []string, externalIDs map[string]string) error
	DeleteLogicalRouterPolicy(lrName string, priority int, match string) error
	DeleteLogicalRouterPolicies(lrName string, priority int, externalIDs map[string]string) error
	DeleteLogicalRouterPolicyByUUID(lrName, uuid string) error
	DeleteLogicalRouterPolicyByNexthop(lrName string, priority int, nexthop string) error
	ClearLogicalRouterPolicy(lrName string) error
	ListLogicalRouterPolicies(lrName string, priority int, externalIDs map[string]string, ignoreExtIDEmptyValue bool) ([]*ovnnb.LogicalRouterPolicy, error)
	GetLogicalRouterPolicy(lrName string, priority int, match string, ignoreNotFound bool) ([]*ovnnb.LogicalRouterPolicy, error)
	GetLogicalRouterPoliciesByExtID(lrName, key, value string) ([]*ovnnb.LogicalRouterPolicy, error)
	UpdateLogicalRouterPolicy(policy *ovnnb.LogicalRouterPolicy, fields ...interface{}) error
}

type NAT interface {
	GetNATByUUID(uuid string) (*ovnnb.NAT, error)
	AddNat(lrName, natType, externalIP, logicalIP, logicalMac, port string, options map[string]string) error
	UpdateSnat(lrName, externalIP, logicalIP string) error
	UpdateDnatAndSnat(lrName, externalIP, logicalIP, lspName, externalMac, gatewayType string) error
	DeleteNats(lrName, natType, logicalIP string) error
	DeleteNat(lrName, natType, externalIP, logicalIP string) error
	NatExists(lrName, natType, externalIP, logicalIP string) (bool, error)
	ListNats(lrName, natType, logicalIP string, externalIDs map[string]string) ([]*ovnnb.NAT, error)
}

type DHCPOptions interface {
	UpdateDHCPOptions(subnet *kubeovnv1.Subnet, mtu int) (*DHCPOptionsUUIDs, error)
	DeleteDHCPOptions(lsName, protocol string) error
	DeleteDHCPOptionsByUUIDs(uuidList ...string) error
	ListDHCPOptions(needVendorFilter bool, externalIDs map[string]string) ([]ovnnb.DHCPOptions, error)
}

type NbClient interface {
	ACL
	AddressSet
	BFD
	DHCPOptions
	GatewayChassis
	HAChassisGroup
	LoadBalancer
	LoadBalancerHealthCheck
	LogicalRouterPolicy
	LogicalRouterPort
	LogicalRouterStaticRoute
	LogicalRouter
	LogicalSwitchPort
	LogicalSwitch
	NAT
	NBGlobal
	PortGroup
	CreateGatewayLogicalSwitch(lsName, lrName, provider, ip, mac string, vlanID int, chassises ...string) error
	CreateLogicalPatchPort(lsName, lrName, lspName, lrpName, ip, mac string, chassises ...string) error
	RemoveLogicalPatchPort(lspName, lrpName string) error
	DeleteLogicalGatewaySwitch(lsName, lrName string) error
	DeleteSecurityGroup(sgName string) error
	Common
}

type SbClient interface {
	Chassis
	Common
}

type Common interface {
	Transact(method string, operations []ovsdb.Operation) error
	GetEntityInfo(entity interface{}) error
}

type Chassis interface {
	DeleteChassis(chassisName string) error
	DeleteChassisByHost(node string) error
	GetChassisByHost(nodeName string) (*ovnsb.Chassis, error)
	GetChassis(chassisName string, ignoreNotFound bool) (*ovnsb.Chassis, error)
	GetKubeOvnChassisses() (*[]ovnsb.Chassis, error)
	UpdateChassisTag(chassisName, nodeName string) error
	UpdateChassis(chassis *ovnsb.Chassis, fields ...interface{}) error
	ListChassis() (*[]ovnsb.Chassis, error)
}
