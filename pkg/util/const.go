package util

const (
	CniTypeName = "kube-ovn"

	DepreciatedFinalizerName   = "kube-ovn-controller"
	KubeOVNControllerFinalizer = "kubeovn.io/kube-ovn-controller"

	AllocatedAnnotation  = "ovn.kubernetes.io/allocated"
	RoutedAnnotation     = "ovn.kubernetes.io/routed"
	RoutesAnnotation     = "ovn.kubernetes.io/routes"
	MacAddressAnnotation = "ovn.kubernetes.io/mac_address"
	IPAddressAnnotation  = "ovn.kubernetes.io/ip_address"
	CidrAnnotation       = "ovn.kubernetes.io/cidr"
	GatewayAnnotation    = "ovn.kubernetes.io/gateway"
	IPPoolAnnotation     = "ovn.kubernetes.io/ip_pool"
	BgpAnnotation        = "ovn.kubernetes.io/bgp"
	SnatAnnotation       = "ovn.kubernetes.io/snat"
	EipAnnotation        = "ovn.kubernetes.io/eip"
	FipFinalizer         = "ovn.kubernetes.io/fip"
	VipAnnotation        = "ovn.kubernetes.io/vip"
	AAPsAnnotation       = "ovn.kubernetes.io/aaps"
	ChassisAnnotation    = "ovn.kubernetes.io/chassis"

	ExternalIPAnnotation         = "ovn.kubernetes.io/external_ip"
	ExternalMacAnnotation        = "ovn.kubernetes.io/external_mac"
	ExternalCidrAnnotation       = "ovn.kubernetes.io/external_cidr"
	ExternalSwitchAnnotation     = "ovn.kubernetes.io/external_switch"
	ExternalGatewayAnnotation    = "ovn.kubernetes.io/external_gateway"
	ExternalGwPortNameAnnotation = "ovn.kubernetes.io/external_gw_port_name"

	VpcNatGatewayAnnotation     = "ovn.kubernetes.io/vpc_nat_gw"
	VpcNatGatewayInitAnnotation = "ovn.kubernetes.io/vpc_nat_gw_init"
	VpcEipsAnnotation           = "ovn.kubernetes.io/vpc_eips"
	VpcFloatingIPMd5Annotation  = "ovn.kubernetes.io/vpc_floating_ips"
	VpcDnatMd5Annotation        = "ovn.kubernetes.io/vpc_dnat_md5"
	VpcSnatMd5Annotation        = "ovn.kubernetes.io/vpc_snat_md5"
	VpcCIDRsAnnotation          = "ovn.kubernetes.io/vpc_cidrs"
	VpcLbAnnotation             = "ovn.kubernetes.io/vpc_lb"
	VpcExternalLabel            = "ovn.kubernetes.io/vpc_external"
	VpcEipAnnotation            = "ovn.kubernetes.io/vpc_eip"
	VpcDnatEPortLabel           = "ovn.kubernetes.io/vpc_dnat_eport"
	VpcNatAnnotation            = "ovn.kubernetes.io/vpc_nat"
	OvnEipTypeLabel             = "ovn.kubernetes.io/ovn_eip_type"
	EipV4IpLabel                = "ovn.kubernetes.io/eip_v4_ip"

	SwitchLBRuleVipsAnnotation = "ovn.kubernetes.io/switch_lb_vip"
	SwitchLBRuleVip            = "switch_lb_vip"
	SwitchLBRuleSubnet         = "switch_lb_subnet"

	LogicalRouterAnnotation = "ovn.kubernetes.io/logical_router"
	VpcAnnotation           = "ovn.kubernetes.io/vpc"

	Layer2ForwardAnnotationTemplate = "%s.kubernetes.io/layer2_forward"
	PortSecurityAnnotationTemplate  = "%s.kubernetes.io/port_security"
	PortVipAnnotationTemplate       = "%s.kubernetes.io/port_vips"
	PortSecurityAnnotation          = "ovn.kubernetes.io/port_security"
	NorthGatewayAnnotation          = "ovn.kubernetes.io/north_gateway"

	AllocatedAnnotationSuffix       = ".kubernetes.io/allocated"
	AllocatedAnnotationTemplate     = "%s.kubernetes.io/allocated"
	RoutedAnnotationTemplate        = "%s.kubernetes.io/routed"
	RoutesAnnotationTemplate        = "%s.kubernetes.io/routes"
	MacAddressAnnotationTemplate    = "%s.kubernetes.io/mac_address"
	IPAddressAnnotationTemplate     = "%s.kubernetes.io/ip_address"
	CidrAnnotationTemplate          = "%s.kubernetes.io/cidr"
	GatewayAnnotationTemplate       = "%s.kubernetes.io/gateway"
	IPPoolAnnotationTemplate        = "%s.kubernetes.io/ip_pool"
	LogicalSwitchAnnotationTemplate = "%s.kubernetes.io/logical_switch"
	LogicalRouterAnnotationTemplate = "%s.kubernetes.io/logical_router"
	VlanIDAnnotationTemplate        = "%s.kubernetes.io/vlan_id"
	IngressRateAnnotationTemplate   = "%s.kubernetes.io/ingress_rate"
	EgressRateAnnotationTemplate    = "%s.kubernetes.io/egress_rate"
	SecurityGroupAnnotationTemplate = "%s.kubernetes.io/security_groups"
	LiveMigrationAnnotationTemplate = "%s.kubernetes.io/allow_live_migration"
	DefaultRouteAnnotationTemplate  = "%s.kubernetes.io/default_route"
	VfRepresentorNameTemplate       = "%s.kubernetes.io/vf_representor"
	VfNameTemplate                  = "%s.kubernetes.io/vf"

	ProviderNetworkTemplate           = "%s.kubernetes.io/provider_network"
	ProviderNetworkErrMessageTemplate = "%s.provider-network.kubernetes.io/err_mesg"
	ProviderNetworkReadyTemplate      = "%s.provider-network.kubernetes.io/ready"
	ProviderNetworkExcludeTemplate    = "%s.provider-network.kubernetes.io/exclude"
	ProviderNetworkInterfaceTemplate  = "%s.provider-network.kubernetes.io/interface"
	ProviderNetworkMtuTemplate        = "%s.provider-network.kubernetes.io/mtu"
	MirrorControlAnnotationTemplate   = "%s.kubernetes.io/mirror"
	PodNicAnnotationTemplate          = "%s.kubernetes.io/pod_nic_type"
	VMTemplate                        = "%s.kubernetes.io/virtualmachine"

	ExcludeIpsAnnotation = "ovn.kubernetes.io/exclude_ips"

	IngressRateAnnotation = "ovn.kubernetes.io/ingress_rate"
	EgressRateAnnotation  = "ovn.kubernetes.io/egress_rate"

	PortNameAnnotation      = "ovn.kubernetes.io/port_name"
	LogicalSwitchAnnotation = "ovn.kubernetes.io/logical_switch"

	TunnelInterfaceAnnotation = "ovn.kubernetes.io/tunnel_interface"

	OvsDpTypeLabel = "ovn.kubernetes.io/ovs_dp_type"

	VpcNameLabel               = "ovn.kubernetes.io/vpc"
	SubnetNameLabel            = "ovn.kubernetes.io/subnet"
	ICGatewayLabel             = "ovn.kubernetes.io/ic-gw"
	ExGatewayLabel             = "ovn.kubernetes.io/external-gw"
	NodeExtGwLabel             = "ovn.kubernetes.io/node-ext-gw"
	VpcNatGatewayLabel         = "ovn.kubernetes.io/vpc-nat-gw"
	IPReservedLabel            = "ovn.kubernetes.io/ip_reserved"
	VpcNatGatewayNameLabel     = "ovn.kubernetes.io/vpc-nat-gw-name"
	VpcLbLabel                 = "ovn.kubernetes.io/vpc_lb"
	VpcDNSNameLabel            = "ovn.kubernetes.io/vpc-dns"
	QoSLabel                   = "ovn.kubernetes.io/qos"
	NodeNameLabel              = "ovn.kubernetes.io/node-name"
	NetworkPolicyLogAnnotation = "ovn.kubernetes.io/enable_log"

	VpcLastName     = "ovn.kubernetes.io/last_vpc_name"
	VpcLastPolicies = "ovn.kubernetes.io/last_policies"

	ProtocolTCP  = "tcp"
	ProtocolUDP  = "udp"
	ProtocolSCTP = "sctp"

	NetworkTypeVlan   = "vlan"
	NetworkTypeGeneve = "geneve"
	NetworkTypeVxlan  = "vxlan"
	NetworkTypeStt    = "stt"

	LoNic         = "lo"
	NodeGwNic     = "ovnext0"
	NodeGwNs      = "ovnext"
	NodeGwNsPath  = "/var/run/netns/ovnext"
	BindMountPath = "/run/netns"

	NodeNic           = "ovn0"
	NodeAllowPriority = "3000"

	SecurityGroupHighestPriority = "2300"
	SecurityGroupBasePriority    = "2005"
	SecurityGroupAllowPriority   = "2004"
	SecurityGroupDropPriority    = "2003"

	IngressAllowPriority = "2001"
	IngressDefaultDrop   = "2000"

	EgressAllowPriority = "2001"
	EgressDefaultDrop   = "2000"

	AllowEWTrafficPriority = "1900"

	SubnetAllowPriority = "1001"
	DefaultDropPriority = "1000"

	GwChassisMaxPriority = 100

	DefaultMTU = 1500

	GeneveHeaderLength = 100
	VxlanHeaderLength  = 50
	SttHeaderLength    = 72
	TCPIPHeaderLength  = 40

	OvnProvider                 = "ovn"
	AttachmentNetworkAnnotation = "k8s.v1.cni.cncf.io/networks"
	DefaultNetworkAnnotation    = "v1.multus-cni.io/default-network"

	SRIOVResourceName = "mellanox.com/cx5_sriov_switchdev"

	InterconnectionConfig  = "ovn-ic-config"
	ExternalGatewayConfig  = "ovn-external-gw-config"
	InterconnectionSwitch  = "ts"
	ExternalGatewaySwitch  = "ovn-external"
	VpcNatGatewayConfig    = "ovn-vpc-nat-gw-config"
	VpcLbNetworkAttachment = "ovn-vpc-lb"
	VpcDNSConfig           = "vpc-dns-config"
	VpcDNSDepTemplate      = "vpc-dns-dep"
	VpcNatConfig           = "ovn-vpc-nat-config"

	DefaultSecurityGroupName = "default-securitygroup"

	DefaultVpc    = "ovn-cluster"
	DefaultSubnet = "ovn-default"

	NormalRouteType    = "normal"
	EcmpRouteType      = "ecmp"
	StaticRouteBfdEcmp = "ecmp_symmetric_reply"

	Vip = "vip"

	OvnEipTypeLRP = "lrp"
	OvnEipTypeLSP = "lsp"
	OvnEipTypeNAT = "nat"

	FipUsingEip  = "fip"
	SnatUsingEip = "snat"
	DnatUsingEip = "dnat"

	OvnFip      = "ovn"
	IptablesFip = "iptables"

	U2OSubnetPolicyPriority     = 29400
	GatewayRouterPolicyPriority = 29000
	OvnICPolicyPriority         = 29500
	NodeRouterPolicyPriority    = 30000
	NodeLocalDNSPolicyPriority  = 30100
	SubnetRouterPolicyPriority  = 31000

	OffloadType  = "offload-port"
	InternalType = "internal-port"
	DpdkType     = "dpdk-port"

	HostnameEnv = "KUBE_NODE_NAME"

	ChassisRetryMaxTimes           = 5
	ChassisCniDaemonRetryInterval  = 1
	ChassisControllerRetryInterval = 3

	VM         = "VirtualMachine"
	VMInstance = "VirtualMachineInstance"

	StatefulSet = "StatefulSet"

	MirrorControlAnnotation = "ovn.kubernetes.io/mirror"
	MirrorDefaultName       = "m0"

	DenyAllSecurityGroup = "kubeovn_deny_all"

	NetemQosLatencyAnnotation = "ovn.kubernetes.io/latency"
	NetemQosLimitAnnotation   = "ovn.kubernetes.io/limit"
	NetemQosLossAnnotation    = "ovn.kubernetes.io/loss"
	NetemQosJitterAnnotation  = "ovn.kubernetes.io/jitter"

	NetemQosLatencyAnnotationTemplate = "%s.kubernetes.io/latency"
	NetemQosLimitAnnotationTemplate   = "%s.kubernetes.io/limit"
	NetemQosLossAnnotationTemplate    = "%s.kubernetes.io/loss"
	NetemQosJitterAnnotationTemplate  = "%s.kubernetes.io/jitter"

	PodIP              = "POD_IP"
	ContentType        = "application/vnd.kubernetes.protobuf"
	AcceptContentTypes = "application/vnd.kubernetes.protobuf,application/json"

	AttachmentProvider = "ovn.kubernetes.io/attachmentprovider"
	LbSvcPodImg        = "ovn.kubernetes.io/lb_svc_img"

	OvnICKey       = "origin"
	OvnICConnected = "connected"
	OvnICStatic    = "static"
	OvnICNone      = ""

	MatchV4Src = "ip4.src"
	MatchV4Dst = "ip4.dst"
	MatchV6Src = "ip6.src"
	MatchV6Dst = "ip6.dst"

	U2OInterconnName = "u2o-interconnection.%s.%s"
	U2OExcludeIPAg   = "%s.u2o_exclude_ip.%s"

	DefaultServiceSessionStickinessTimeout = 10800

	OvnSubnetGatewayIptables = "ovn-subnet-gateway"

	QoSDirectionIngress = "ingress"
	QoSDirectionEgress  = "egress"

	MainRouteTable = ""

	NatPolicyRuleActionNat     = "nat"
	NatPolicyRuleActionForward = "forward"
	NatPolicyRuleIDLength      = 12

	NAT                        = "nat"
	Mangle                     = "mangle"
	Prerouting                 = "PREROUTING"
	Postrouting                = "POSTROUTING"
	Output                     = "OUTPUT"
	OvnPrerouting              = "OVN-PREROUTING"
	OvnPostrouting             = "OVN-POSTROUTING"
	OvnOutput                  = "OVN-OUTPUT"
	OvnMasquerade              = "OVN-MASQUERADE"
	OvnNatOutGoingPolicy       = "OVN-NAT-POLICY"
	OvnNatOutGoingPolicySubnet = "OVN-NAT-PSUBNET-"

	TProxyListenPort = 8102
	TProxyRouteTable = 10001

	TProxyOutputMark     = 0x90003
	TProxyOutputMask     = 0x90003
	TProxyPreroutingMark = 0x90004
	TProxyPreroutingMask = 0x90004

	HealthCheckNamedVipTemplate = "%s:%s" // ip name, health check vip

	ConsumptionKubevirt       = "kubevirt"
	VhostUserSocketVolumeName = "vhostuser-sockets"
)
