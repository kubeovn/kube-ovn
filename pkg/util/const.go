package util

const (
	MacAddressAnnotation = "ovn.kubernetes.io/mac_address"
	IpAddressAnnotation  = "ovn.kubernetes.io/ip_address"
	CidrAnnotation       = "ovn.kubernetes.io/cidr"
	GatewayAnnotation    = "ovn.kubernetes.io/gateway"
	IpPoolAnnotation     = "ovn.kubernetes.io/ip_pool"

	IngressRateAnnotation = "ovn.kubernetes.io/ingress_rate"
	EgressRateAnnotation  = "ovn.kubernetes.io/egress_rate"

	PortNameAnnotation = "ovn.kubernetes.io/port_name"

	LogicalSwitchAnnotation = "ovn.kubernetes.io/logical_switch"
	ExcludeIpsAnnotation    = "ovn.kubernetes.io/exclude_ips"

	ProtocolTCP = "tcp"
	ProtocolUDP = "udp"

	NodeNic = "ovn0"

	PrivateSwitchAnnotation = "ovn.kubernetes.io/private"
	AllowAccessAnnotation   = "ovn.kubernetes.io/allow"

	DefaultDropPriority = "1000"
	NodeAllowPriority   = "1001"
	SubnetAllowPriority = "1002"

	GWTypeAnnotation  = "ovn.kubernetes.io/gateway_type"
	GWDistributedMode = "distributed"
	GWCentralizedMode = "centralized"
	GWNode            = "ovn.kubernetes.io/gateway_node"
)
