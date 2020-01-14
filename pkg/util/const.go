package util

const (
	ControllerName       = "kube-ovn-controller"
	AllocatedAnnotation  = "ovn.kubernetes.io/allocated"
	MacAddressAnnotation = "ovn.kubernetes.io/mac_address"
	IpAddressAnnotation  = "ovn.kubernetes.io/ip_address"
	CidrAnnotation       = "ovn.kubernetes.io/cidr"
	GatewayAnnotation    = "ovn.kubernetes.io/gateway"
	IpPoolAnnotation     = "ovn.kubernetes.io/ip_pool"
	ExcludeIpsAnnotation = "ovn.kubernetes.io/exclude_ips"

	IngressRateAnnotation = "ovn.kubernetes.io/ingress_rate"
	EgressRateAnnotation  = "ovn.kubernetes.io/egress_rate"

	PortNameAnnotation = "ovn.kubernetes.io/port_name"

	LogicalSwitchAnnotation = "ovn.kubernetes.io/logical_switch"

	SubnetNameLabel = "ovn.kubernetes.io/subnet"

	ProtocolTCP = "tcp"
	ProtocolUDP = "udp"

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
)
