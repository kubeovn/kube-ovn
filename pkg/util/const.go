package util

const (
	CniTypeName = "kube-ovn"

	ControllerName = "kube-ovn-controller"

	AllocatedAnnotation  = "ovn.kubernetes.io/allocated"
	RoutedAnnotation     = "ovn.kubernetes.io/routed"
	MacAddressAnnotation = "ovn.kubernetes.io/mac_address"
	IpAddressAnnotation  = "ovn.kubernetes.io/ip_address"
	CidrAnnotation       = "ovn.kubernetes.io/cidr"
	GatewayAnnotation    = "ovn.kubernetes.io/gateway"
	IpPoolAnnotation     = "ovn.kubernetes.io/ip_pool"
	BgpAnnotation        = "ovn.kubernetes.io/bgp"
	SnatAnnotation       = "ovn.kubernetes.io/snat"
	EipAnnotation        = "ovn.kubernetes.io/eip"

	VpcNatGatewayAnnotation     = "ovn.kubernetes.io/vpc_nat_gw"
	VpcNatGatewayInitAnnotation = "ovn.kubernetes.io/vpc_nat_gw_init"
	VpcEipsAnnotation           = "ovn.kubernetes.io/vpc_eips"
	VpcFloatingIpMd5Annotation  = "ovn.kubernetes.io/vpc_floating_ips"
	VpcDnatMd5Annotation        = "ovn.kubernetes.io/vpc_dnat_md5"
	VpcSnatMd5Annotation        = "ovn.kubernetes.io/vpc_snat_md5"
	VpcCIDRsAnnotation          = "ovn.kubernetes.io/vpc_cidrs"

	LogicalRouterAnnotation = "ovn.kubernetes.io/logical_router"
	VpcAnnotation           = "ovn.kubernetes.io/vpc"

	PortSecurityAnnotation = "ovn.kubernetes.io/port_security"
	NorthGatewayAnnotation = "ovn.kubernetes.io/north_gateway"

	AllocatedAnnotationSuffix       = ".kubernetes.io/allocated"
	AllocatedAnnotationTemplate     = "%s.kubernetes.io/allocated"
	MacAddressAnnotationTemplate    = "%s.kubernetes.io/mac_address"
	IpAddressAnnotationTemplate     = "%s.kubernetes.io/ip_address"
	CidrAnnotationTemplate          = "%s.kubernetes.io/cidr"
	GatewayAnnotationTemplate       = "%s.kubernetes.io/gateway"
	IpPoolAnnotationTemplate        = "%s.kubernetes.io/ip_pool"
	LogicalSwitchAnnotationTemplate = "%s.kubernetes.io/logical_switch"
	VlanIdAnnotationTemplate        = "%s.kubernetes.io/vlan_id"

	ExcludeIpsAnnotation = "ovn.kubernetes.io/exclude_ips"

	IngressRateAnnotation = "ovn.kubernetes.io/ingress_rate"
	EgressRateAnnotation  = "ovn.kubernetes.io/egress_rate"

	PortNameAnnotation      = "ovn.kubernetes.io/port_name"
	LogicalSwitchAnnotation = "ovn.kubernetes.io/logical_switch"

	SubnetNameLabel    = "ovn.kubernetes.io/subnet"
	ICGatewayLabel     = "ovn.kubernetes.io/ic-gw"
	ExGatewayLabel     = "ovn.kubernetes.io/external-gw"
	VpcNatGatewayLabel = "ovn.kubernetes.io/vpc-nat-gw"

	ProtocolTCP = "tcp"
	ProtocolUDP = "udp"

	VlanIdAnnotation      = "ovn.kubernetes.io/vlan_id"
	VlanRangeAnnotation   = "ovn.kubernetes.io/vlan_range"
	NetworkType           = "ovn.kubernetes.io/network_types"
	NetworkTypeVlan       = "vlan"
	ProviderInterfaceName = "ovn.kubernetes.io/provider_interface_name"
	HostInterfaceName     = "ovn.kubernetes.io/host_interface_name"

	NodeNic           = "ovn0"
	NodeAllowPriority = "3000"

	IngressExceptDropPriority = "2002"
	IngressAllowPriority      = "2001"
	IngressDefaultDrop        = "2000"

	EgressExceptDropPriority = "2002"
	EgressAllowPriority      = "2001"
	EgressDefaultDrop        = "2000"

	SubnetAllowPriority = "1001"
	DefaultDropPriority = "1000"

	GeneveHeaderLength = 100
	TcpIpHeaderLength  = 40

	OvnProvider                 = "ovn"
	AttachmentNetworkAnnotation = "k8s.v1.cni.cncf.io/networks"
	DefaultNetworkAnnotation    = "v1.multus-cni.io/default-network"

	SRIOVResourceName = "mellanox.com/cx5_sriov_switchdev"

	InterconnectionConfig = "ovn-ic-config"
	ExternalGatewayConfig = "ovn-external-gw-config"
	InterconnectionSwitch = "ts"
	ExternalGatewaySwitch = "ovn-external"
	VpcNatGatewayConfig   = "ovn-vpc-nat-gw-config"
	VpcExternalNet        = "ovn-vpc-external-network"

	DefaultVpc = "ovn-cluster"
)
