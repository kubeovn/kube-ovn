package ovs

import (
	v12 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"k8s.io/api/networking/v1"
)

type LegacyClientInterface interface {
	ovnSbCommand(cmdArgs ...string) (string, error)
	DeleteChassisByNode(node string) error
	DeleteChassisByName(chassisName string) error
	GetChassis(node string) (string, error)
	ChassisExist(chassisName string) (bool, error)
	InitChassisNodeTag(chassisName string, nodeName string) error
	GetAllChassis() ([]string, error)
	ovnIcNbCommand(cmdArgs ...string) (string, error)
	GetTsSubnet(ts string) (string, error)
	ovnIcSbCommand(cmdArgs ...string) (string, error)
	FindUUIDWithAttrInTable(attribute, value, table string) ([]string, error)
	DestroyTableWithUUID(uuid, table string) error
	GetAzUUID(az string) (string, error)
	GetGatewayUUIDsInOneAZ(uuid string) ([]string, error)
	GetRouteUUIDsInOneAZ(uuid string) ([]string, error)
	DestroyGateways(uuids []string)
	DestroyRoutes(uuids []string)
	DestroyChassis(uuid string) error
	ovnNbCommand(cmdArgs ...string) (string, error)
	GetVersion() (string, error)
	SetAzName(azName string) error
	SetLsDnatModDlDst(enabled bool) error
	SetUseCtInvMatch() error
	SetICAutoRoute(enable bool, blackList []string) error
	DeleteLogicalSwitchPort(port string) error
	DeleteLogicalRouterPort(port string) error
	CreateICLogicalRouterPort(az, mac, subnet string, chassises []string) error
	DeleteICLogicalRouterPort(az string) error
	SetPortAddress(port, mac, ip string) error
	SetPortExternalIds(port, key, value string) error
	SetPortSecurity(portSecurity bool, ls, port, mac, ipStr, vips string) error
	CreateVirtualPort(ls, ip string) error
	SetVirtualParents(ls, ip, parents string) error
	ListVirtualPort(ls string) ([]string, error)
	EnablePortLayer2forward(ls, port string) error
	CreatePort(ls, port, ip, mac, pod, namespace string, portSecurity bool, securityGroups string, vips string, liveMigration bool, enableDHCP bool, dhcpOptions *DHCPOptionsUUIDs) error
	SetPortTag(name string, vlanID int) error
	ListPodLogicalSwitchPorts(pod, namespace string) ([]string, error)
	SetLogicalSwitchConfig(ls, lr, protocol, subnet, gateway string, excludeIps []string, needRouter bool) error
	CreateLogicalSwitch(ls, lr, subnet, gateway string, needRouter bool) error
	AddLbToLogicalSwitch(tcpLb, tcpSessLb, udpLb, udpSessLb, ls string) error
	RemoveLbFromLogicalSwitch(tcpLb, tcpSessLb, udpLb, udpSessLb, ls string) error
	DeleteLoadBalancer(lbs ...string) error
	ListLoadBalancer() ([]string, error)
	CreateGatewaySwitch(name, network string, vlan int, ip, mac string, chassises []string) error
	DeleteGatewaySwitch(name string) error
	ListLogicalSwitch(needVendorFilter bool, args ...string) ([]string, error)
	ListLogicalEntity(entity string, args ...string) ([]string, error)
	CustomFindEntity(entity string, attris []string, args ...string) (result []map[string][]string, err error)
	GetEntityInfo(entity string, index string, attris []string) (result map[string]string, err error)
	LogicalSwitchExists(logicalSwitch string, needVendorFilter bool, args ...string) (bool, error)
	ListLogicalSwitchPort(needVendorFilter bool) ([]string, error)
	LogicalSwitchPortExists(port string) (bool, error)
	ListRemoteLogicalSwitchPortAddress() ([]string, error)
	ListLogicalRouter(needVendorFilter bool, args ...string) ([]string, error)
	DeleteLogicalSwitch(ls string) error
	CreateLogicalRouter(lr string) error
	DeleteLogicalRouter(lr string) error
	RemoveRouterPort(ls, lr string) error
	createRouterPort(ls, lr string) error
	CreatePeerRouterPort(localRouter, remoteRouter, localRouterPortIP string) error
	ListStaticRoute() ([]StaticRoute, error)
	AddStaticRoute(policy, cidr, nextHop, router string, routeType string) error
	AddPolicyRoute(router string, priority int32, match, action, nextHop string, externalIDs map[string]string) error
	DeletePolicyRoute(router string, priority int32, match string) error
	IsPolicyRouteExist(router string, priority int32, match string) (bool, error)
	DeletePolicyRouteByNexthop(router string, priority int32, nexthop string) error
	GetPolicyRouteList(router string) (routeList []*PolicyRoute, err error)
	GetStaticRouteList(router string) (routeList []*StaticRoute, err error)
	UpdateNatRule(policy, logicalIP, externalIP, router, logicalMac, port string) error
	DeleteNatRule(logicalIP, router string) error
	NatRuleExists(logicalIP string) (bool, error)
	DeleteMatchedStaticRoute(cidr, nexthop, router string) error
	DeleteStaticRoute(cidr, router string) error
	DeleteStaticRouteByNextHop(nextHop string) error
	FindLoadbalancer(lb string) (string, error)
	CreateLoadBalancer(lb, protocol, selectFields string) error
	CreateLoadBalancerRule(lb, vip, ips, protocol string) error
	addLoadBalancerToLogicalSwitch(lb, ls string) error
	removeLoadBalancerFromLogicalSwitch(lb, ls string) error
	DeleteLoadBalancerVip(vip, lb string) error
	GetLoadBalancerVips(lb string) (map[string]string, error)
	CleanLogicalSwitchAcl(ls string) error
	ResetLogicalSwitchAcl(ls string) error
	SetPrivateLogicalSwitch(ls, cidr string, allow []string) error
	GetLogicalSwitchPortAddress(port string) ([]string, error)
	GetLogicalSwitchPortDynamicAddress(port string) ([]string, error)
	GetPortAddr(port string) ([]string, error)
	CreateNpPortGroup(pgName, npNs, npName string) error
	DeletePortGroup(pgName string) error
	ListNpPortGroup() ([]portGroup, error)
	CreateAddressSet(name string) error
	CreateAddressSetWithAddresses(name string, addresses ...string) error
	AddAddressSetAddresses(name string, address string) error
	RemoveAddressSetAddresses(name string, address string) error
	DeleteAddressSet(name string) error
	ListNpAddressSet(npNamespace, npName, direction string) ([]string, error)
	ListAddressesByName(addressSetName string) ([]string, error)
	CreateNpAddressSet(asName, npNamespace, npName, direction string) error
	CreateIngressACL(pgName, asIngressName, asExceptName, svcAsName, protocol string, npp []v1.NetworkPolicyPort, logEnable bool) error
	CreateEgressACL(pgName, asEgressName, asExceptName, protocol string, npp []v1.NetworkPolicyPort, portSvcName string, logEnable bool) error
	DeleteACL(pgName, direction string) (err error)
	CreateGatewayACL(pgName, gateway, cidr string) error
	CreateACLForNodePg(pgName, nodeIpStr string) error
	DeleteAclForNodePg(pgName string) error
	ListPgPorts(pgName string) ([]string, error)
	ListLspForNodePortgroup() (map[string]string, map[string]string, error)
	ListPgPortsForNodePortgroup() (map[string][]string, error)
	SetPortsToPortGroup(portGroup string, portNames []string) error
	SetAddressesToAddressSet(addresses []string, as string) error
	GetLogicalSwitchExcludeIPS(logicalSwitch string) ([]string, error)
	SetLogicalSwitchExcludeIPS(logicalSwitch string, excludeIPS []string) error
	GetLogicalSwitchPortByLogicalSwitch(logicalSwitch string) ([]string, error)
	CreateLocalnetPort(ls, port, provider string, vlanID int) error
	CreateSgPortGroup(sgName string) error
	DeleteSgPortGroup(sgName string) error
	CreateSgAssociatedAddressSet(sgName string) error
	ListSgRuleAddressSet(sgName string, direction AclDirection) ([]string, error)
	createSgRuleACL(sgName string, direction AclDirection, rule *v12.SgRule, index int) error
	CreateSgDenyAllACL() error
	UpdateSgACL(sg *v12.SecurityGroup, direction AclDirection) error
	OvnGet(table, record, column, key string) (string, error)
	SetLspExternalIds(name string, externalIDs map[string]string) error
	AclExists(priority, direction string) (bool, error)
	SetLBCIDR(svccidr string) error
	PortGroupExists(pgName string) (bool, error)
	PolicyRouteExists(priority int32, match string) (bool, error)
	GetPolicyRouteParas(priority int32, match string) ([]string, map[string]string, error)
	SetPolicyRouteExternalIds(priority int32, match string, nameIpMaps map[string]string) error
	CheckPolicyRouteNexthopConsistent(router, match, nexthop string, priority int32) (bool, error)
	ListDHCPOptions(needVendorFilter bool, ls string, protocol string) ([]dhcpOptions, error)
	createDHCPOptions(ls, cidr, optionsStr string) (dhcpOptionsUuid string, err error)
	updateDHCPv4Options(ls, v4CIDR, v4Gateway, dhcpV4OptionsStr string) (dhcpV4OptionsUuid string, err error)
	updateDHCPv6Options(ls, v6CIDR, dhcpV6OptionsStr string) (dhcpV6OptionsUuid string, err error)
	UpdateDHCPOptions(ls, cidrBlock, gateway, dhcpV4OptionsStr, dhcpV6OptionsStr string, enableDHCP bool) (dhcpOptionsUUIDs *DHCPOptionsUUIDs, err error)
	DeleteDHCPOptionsByUUIDs(uuidList []string) (err error)
	DeleteDHCPOptions(ls string, protocol string) error
	UpdateRouterPortIPv6RA(ls, lr, cidrBlock, gateway, ipv6RAConfigsStr string, enableIPv6RA bool) error
	DeleteSubnetACL(ls string) error
	UpdateSubnetACL(ls string, acls []v12.Acl) error
	GetLspExternalIds(lsp string) map[string]string
	SetAclLog(pgName string, logEnable, isIngress bool) error
	SetExternalGatewayType(gatewayType string)
	GetExternalGatewayType() string
	SetOvnICNbAddress(address string)
	SetOvnICSbAddress(address string)
	GetOvnICNbAddress() string
	GetOvnICSbAddress() string
}

type FakeOvnLegacyClient struct{}

func (f FakeOvnLegacyClient) SetExternalGatewayType(gatewayType string) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) GetExternalGatewayType() string {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) SetOvnICNbAddress(address string) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) SetOvnICSbAddress(address string) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) GetOvnICNbAddress() string {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) GetOvnICSbAddress() string {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) ovnSbCommand(cmdArgs ...string) (string, error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) DeleteChassisByNode(node string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) DeleteChassisByName(chassisName string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) GetChassis(node string) (string, error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) ChassisExist(chassisName string) (bool, error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) InitChassisNodeTag(chassisName string, nodeName string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) GetAllChassis() ([]string, error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) ovnIcNbCommand(cmdArgs ...string) (string, error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) GetTsSubnet(ts string) (string, error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) ovnIcSbCommand(cmdArgs ...string) (string, error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) FindUUIDWithAttrInTable(attribute, value, table string) ([]string, error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) DestroyTableWithUUID(uuid, table string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) GetAzUUID(az string) (string, error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) GetGatewayUUIDsInOneAZ(uuid string) ([]string, error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) GetRouteUUIDsInOneAZ(uuid string) ([]string, error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) DestroyGateways(uuids []string) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) DestroyRoutes(uuids []string) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) DestroyChassis(uuid string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) ovnNbCommand(cmdArgs ...string) (string, error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) GetVersion() (string, error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) SetAzName(azName string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) SetLsDnatModDlDst(enabled bool) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) SetUseCtInvMatch() error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) SetICAutoRoute(enable bool, blackList []string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) DeleteLogicalSwitchPort(port string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) DeleteLogicalRouterPort(port string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) CreateICLogicalRouterPort(az, mac, subnet string, chassises []string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) DeleteICLogicalRouterPort(az string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) SetPortAddress(port, mac, ip string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) SetPortExternalIds(port, key, value string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) SetPortSecurity(portSecurity bool, ls, port, mac, ipStr, vips string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) CreateVirtualPort(ls, ip string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) SetVirtualParents(ls, ip, parents string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) ListVirtualPort(ls string) ([]string, error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) EnablePortLayer2forward(ls, port string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) CreatePort(ls, port, ip, mac, pod, namespace string, portSecurity bool, securityGroups string, vips string, liveMigration bool, enableDHCP bool, dhcpOptions *DHCPOptionsUUIDs) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) SetPortTag(name string, vlanID int) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) ListPodLogicalSwitchPorts(pod, namespace string) ([]string, error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) SetLogicalSwitchConfig(ls, lr, protocol, subnet, gateway string, excludeIps []string, needRouter bool) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) CreateLogicalSwitch(ls, lr, subnet, gateway string, needRouter bool) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) AddLbToLogicalSwitch(tcpLb, tcpSessLb, udpLb, udpSessLb, ls string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) RemoveLbFromLogicalSwitch(tcpLb, tcpSessLb, udpLb, udpSessLb, ls string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) DeleteLoadBalancer(lbs ...string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) ListLoadBalancer() ([]string, error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) CreateGatewaySwitch(name, network string, vlan int, ip, mac string, chassises []string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) DeleteGatewaySwitch(name string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) ListLogicalSwitch(needVendorFilter bool, args ...string) ([]string, error) {
	return []string{"ovn-default", "join"}, nil
}

func (f FakeOvnLegacyClient) ListLogicalEntity(entity string, args ...string) ([]string, error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) CustomFindEntity(entity string, attris []string, args ...string) (result []map[string][]string, err error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) GetEntityInfo(entity string, index string, attris []string) (result map[string]string, err error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) LogicalSwitchExists(logicalSwitch string, needVendorFilter bool, args ...string) (bool, error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) ListLogicalSwitchPort(needVendorFilter bool) ([]string, error) {
	//TODO implement me
	return []string{}, nil
}

func (f FakeOvnLegacyClient) LogicalSwitchPortExists(port string) (bool, error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) ListRemoteLogicalSwitchPortAddress() ([]string, error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) ListLogicalRouter(needVendorFilter bool, args ...string) ([]string, error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) DeleteLogicalSwitch(ls string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) CreateLogicalRouter(lr string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) DeleteLogicalRouter(lr string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) RemoveRouterPort(ls, lr string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) createRouterPort(ls, lr string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) CreatePeerRouterPort(localRouter, remoteRouter, localRouterPortIP string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) ListStaticRoute() ([]StaticRoute, error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) AddStaticRoute(policy, cidr, nextHop, router string, routeType string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) AddPolicyRoute(router string, priority int32, match, action, nextHop string, externalIDs map[string]string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) DeletePolicyRoute(router string, priority int32, match string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) IsPolicyRouteExist(router string, priority int32, match string) (bool, error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) DeletePolicyRouteByNexthop(router string, priority int32, nexthop string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) GetPolicyRouteList(router string) (routeList []*PolicyRoute, err error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) GetStaticRouteList(router string) (routeList []*StaticRoute, err error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) UpdateNatRule(policy, logicalIP, externalIP, router, logicalMac, port string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) DeleteNatRule(logicalIP, router string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) NatRuleExists(logicalIP string) (bool, error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) DeleteMatchedStaticRoute(cidr, nexthop, router string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) DeleteStaticRoute(cidr, router string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) DeleteStaticRouteByNextHop(nextHop string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) FindLoadbalancer(lb string) (string, error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) CreateLoadBalancer(lb, protocol, selectFields string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) CreateLoadBalancerRule(lb, vip, ips, protocol string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) addLoadBalancerToLogicalSwitch(lb, ls string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) removeLoadBalancerFromLogicalSwitch(lb, ls string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) DeleteLoadBalancerVip(vip, lb string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) GetLoadBalancerVips(lb string) (map[string]string, error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) CleanLogicalSwitchAcl(ls string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) ResetLogicalSwitchAcl(ls string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) SetPrivateLogicalSwitch(ls, cidr string, allow []string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) GetLogicalSwitchPortAddress(port string) ([]string, error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) GetLogicalSwitchPortDynamicAddress(port string) ([]string, error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) GetPortAddr(port string) ([]string, error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) CreateNpPortGroup(pgName, npNs, npName string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) DeletePortGroup(pgName string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) ListNpPortGroup() ([]portGroup, error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) CreateAddressSet(name string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) CreateAddressSetWithAddresses(name string, addresses ...string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) AddAddressSetAddresses(name string, address string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) RemoveAddressSetAddresses(name string, address string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) DeleteAddressSet(name string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) ListNpAddressSet(npNamespace, npName, direction string) ([]string, error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) ListAddressesByName(addressSetName string) ([]string, error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) CreateNpAddressSet(asName, npNamespace, npName, direction string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) CreateIngressACL(pgName, asIngressName, asExceptName, svcAsName, protocol string, npp []v1.NetworkPolicyPort, logEnable bool) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) CreateEgressACL(pgName, asEgressName, asExceptName, protocol string, npp []v1.NetworkPolicyPort, portSvcName string, logEnable bool) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) DeleteACL(pgName, direction string) (err error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) CreateGatewayACL(pgName, gateway, cidr string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) CreateACLForNodePg(pgName, nodeIpStr string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) DeleteAclForNodePg(pgName string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) ListPgPorts(pgName string) ([]string, error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) ListLspForNodePortgroup() (map[string]string, map[string]string, error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) ListPgPortsForNodePortgroup() (map[string][]string, error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) SetPortsToPortGroup(portGroup string, portNames []string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) SetAddressesToAddressSet(addresses []string, as string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) GetLogicalSwitchExcludeIPS(logicalSwitch string) ([]string, error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) SetLogicalSwitchExcludeIPS(logicalSwitch string, excludeIPS []string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) GetLogicalSwitchPortByLogicalSwitch(logicalSwitch string) ([]string, error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) CreateLocalnetPort(ls, port, provider string, vlanID int) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) CreateSgPortGroup(sgName string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) DeleteSgPortGroup(sgName string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) CreateSgAssociatedAddressSet(sgName string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) ListSgRuleAddressSet(sgName string, direction AclDirection) ([]string, error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) createSgRuleACL(sgName string, direction AclDirection, rule *v12.SgRule, index int) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) CreateSgDenyAllACL() error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) UpdateSgACL(sg *v12.SecurityGroup, direction AclDirection) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) OvnGet(table, record, column, key string) (string, error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) SetLspExternalIds(name string, externalIDs map[string]string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) AclExists(priority, direction string) (bool, error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) SetLBCIDR(svccidr string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) PortGroupExists(pgName string) (bool, error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) PolicyRouteExists(priority int32, match string) (bool, error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) GetPolicyRouteParas(priority int32, match string) ([]string, map[string]string, error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) SetPolicyRouteExternalIds(priority int32, match string, nameIpMaps map[string]string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) CheckPolicyRouteNexthopConsistent(router, match, nexthop string, priority int32) (bool, error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) ListDHCPOptions(needVendorFilter bool, ls string, protocol string) ([]dhcpOptions, error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) createDHCPOptions(ls, cidr, optionsStr string) (dhcpOptionsUuid string, err error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) updateDHCPv4Options(ls, v4CIDR, v4Gateway, dhcpV4OptionsStr string) (dhcpV4OptionsUuid string, err error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) updateDHCPv6Options(ls, v6CIDR, dhcpV6OptionsStr string) (dhcpV6OptionsUuid string, err error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) UpdateDHCPOptions(ls, cidrBlock, gateway, dhcpV4OptionsStr, dhcpV6OptionsStr string, enableDHCP bool) (dhcpOptionsUUIDs *DHCPOptionsUUIDs, err error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) DeleteDHCPOptionsByUUIDs(uuidList []string) (err error) {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) DeleteDHCPOptions(ls string, protocol string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) UpdateRouterPortIPv6RA(ls, lr, cidrBlock, gateway, ipv6RAConfigsStr string, enableIPv6RA bool) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) DeleteSubnetACL(ls string) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) UpdateSubnetACL(ls string, acls []v12.Acl) error {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) GetLspExternalIds(lsp string) map[string]string {
	//TODO implement me
	panic("implement me")
}

func (f FakeOvnLegacyClient) SetAclLog(pgName string, logEnable, isIngress bool) error {
	//TODO implement me
	panic("implement me")
}
