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
	ChassisAnnotation    = "ovn.kubernetes.io/chassis"

	VpcNatGatewayAnnotation     = "ovn.kubernetes.io/vpc_nat_gw"
	VpcNatGatewayInitAnnotation = "ovn.kubernetes.io/vpc_nat_gw_init"
	VpcEipsAnnotation           = "ovn.kubernetes.io/vpc_eips"
	VpcFloatingIpMd5Annotation  = "ovn.kubernetes.io/vpc_floating_ips"
	VpcDnatMd5Annotation        = "ovn.kubernetes.io/vpc_dnat_md5"
	VpcSnatMd5Annotation        = "ovn.kubernetes.io/vpc_snat_md5"
	VpcCIDRsAnnotation          = "ovn.kubernetes.io/vpc_cidrs"
	VpcExternalLabel            = "ovn.kubernetes.io/vpc_external"

	LogicalRouterAnnotation = "ovn.kubernetes.io/logical_router"
	VpcAnnotation           = "ovn.kubernetes.io/vpc"

	PortSecurityAnnotationTemplate = "%s.kubernetes.io/port_security"
	PortSecurityAnnotation         = "ovn.kubernetes.io/port_security"
	NorthGatewayAnnotation         = "ovn.kubernetes.io/north_gateway"

	AllocatedAnnotationSuffix       = ".kubernetes.io/allocated"
	AllocatedAnnotationTemplate     = "%s.kubernetes.io/allocated"
	MacAddressAnnotationTemplate    = "%s.kubernetes.io/mac_address"
	IpAddressAnnotationTemplate     = "%s.kubernetes.io/ip_address"
	CidrAnnotationTemplate          = "%s.kubernetes.io/cidr"
	GatewayAnnotationTemplate       = "%s.kubernetes.io/gateway"
	IpPoolAnnotationTemplate        = "%s.kubernetes.io/ip_pool"
	LogicalSwitchAnnotationTemplate = "%s.kubernetes.io/logical_switch"
	LogicalRouterAnnotationTemplate = "%s.kubernetes.io/logical_router"
	VlanIdAnnotationTemplate        = "%s.kubernetes.io/vlan_id"
	NetworkTypeTemplate             = "%s.kubernetes.io/network_type"
	IngressRateAnnotationTemplate   = "%s.kubernetes.io/ingress_rate"
	EgressRateAnnotationTemplate    = "%s.kubernetes.io/egress_rate"
	SecurityGroupAnnotationTemplate = "%s.kubernetes.io/security_groups"

	ProviderNetworkTemplate          = "%s.kubernetes.io/provider_network"
	ProviderNetworkReadyTemplate     = "%s.provider-network.kubernetes.io/ready"
	ProviderNetworkExcludeTemplate   = "%s.provider-network.kubernetes.io/exclude"
	ProviderNetworkInterfaceTemplate = "%s.provider-network.kubernetes.io/interface"
	ProviderNetworkMtuTemplate       = "%s.provider-network.kubernetes.io/mtu"
	MirrorControlAnnotationTemplate  = "%s.kubernetes.io/mirror"
	VmTemplate                       = "%s.kubernetes.io/virtualmachine"

	ExcludeIpsAnnotation = "ovn.kubernetes.io/exclude_ips"

	IngressRateAnnotation = "ovn.kubernetes.io/ingress_rate"
	EgressRateAnnotation  = "ovn.kubernetes.io/egress_rate"

	PortNameAnnotation      = "ovn.kubernetes.io/port_name"
	LogicalSwitchAnnotation = "ovn.kubernetes.io/logical_switch"

	TunnelInterfaceAnnotation = "ovn.kubernetes.io/tunnel_interface"

	SubnetNameLabel    = "ovn.kubernetes.io/subnet"
	ICGatewayLabel     = "ovn.kubernetes.io/ic-gw"
	ExGatewayLabel     = "ovn.kubernetes.io/external-gw"
	VpcNatGatewayLabel = "ovn.kubernetes.io/vpc-nat-gw"

	ProtocolTCP = "tcp"
	ProtocolUDP = "udp"

	NetworkTypeVlan   = "vlan"
	NetworkTypeGeneve = "geneve"

	NodeNic           = "ovn0"
	NodeAllowPriority = "3000"

	SecurityGroupHighestPriority = "2300"
	SecurityGroupAllowPriority   = "2004"
	SecurityGroupDropPriority    = "2003"

	IngressAllowPriority = "2001"
	IngressDefaultDrop   = "2000"

	EgressAllowPriority = "2001"
	EgressDefaultDrop   = "2000"

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

	EcmpRouteType   = "ecmp"
	NormalRouteType = "normal"

	PodNicAnnotation = "ovn.kubernetes.io/pod_nic_type"
	VethType         = "veth-pair"
	OffloadType      = "offload-port"
	InternalType     = "internal-port"

	ChassisLoc     = "/etc/openvswitch/system-id.conf"
	HostnameEnv    = "KUBE_NODE_NAME"
	ChasRetryTime  = 5
	ChasRetryIntev = 1
	VmInstance     = "VirtualMachineInstance"

	VfioSysDir = "/sys/bus/pci/drivers/vfio-pci"
	NetSysDir  = "/sys/class/net"

	MirrorControlAnnotation = "ovn.kubernetes.io/mirror"
	MirrorDefaultName       = "m0"

	DenyAllSecurityGroup = "kubeovn_deny_all"
)
