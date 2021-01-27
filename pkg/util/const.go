package util

const (
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

	LogicalRouterAnnotation = "ovn.kubernetes.io/logical_router"
	VpcAnnotation           = "ovn.kubernetes.io/vpc"

	PortSecurityAnnotation = "ovn.kubernetes.io/port_security"
	NorthGatewayAnnotation = "ovn.kubernetes.io/north_gateway"

	AllocatedAnnotationTemplate     = "%s.kubernetes.io/allocated"
	MacAddressAnnotationTemplate    = "%s.kubernetes.io/mac_address"
	IpAddressAnnotationTemplate     = "%s.kubernetes.io/ip_address"
	CidrAnnotationTemplate          = "%s.kubernetes.io/cidr"
	GatewayAnnotationTemplate       = "%s.kubernetes.io/gateway"
	IpPoolAnnotationTemplate        = "%s.kubernetes.io/ip_pool"
	LogicalSwitchAnnotationTemplate = "%s.kubernetes.io/logical_switch"

	ExcludeIpsAnnotation = "ovn.kubernetes.io/exclude_ips"

	IngressRateAnnotation = "ovn.kubernetes.io/ingress_rate"
	EgressRateAnnotation  = "ovn.kubernetes.io/egress_rate"

	PortNameAnnotation      = "ovn.kubernetes.io/port_name"
	LogicalSwitchAnnotation = "ovn.kubernetes.io/logical_switch"

	SubnetNameLabel = "ovn.kubernetes.io/subnet"
	ICGatewayLabel  = "ovn.kubernetes.io/ic-gw"
	ExGatewayLabel  = "ovn.kubernetes.io/external-gw"

	ProtocolTCP = "tcp"
	ProtocolUDP = "udp"

	VlanIdAnnotation      = "ovn.kubernetes.io/vlan_id"
	VlanRangeAnnotation   = "ovn.kubernetes.io/vlan_range"
	NetworkType           = "ovn.kubernetes.io/network_types"
	NetworkTypeVlan       = "vlan"
	NetworkTypeGeneve     = "geneve"
	NetworkTypeHybrid     = "hybrid"
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

	DefaultVpc = "ovn-cluster"
)
