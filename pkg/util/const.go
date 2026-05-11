package util

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubevirtv1 "kubevirt.io/api/core/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

const (
	CniTypeName = "kube-ovn"

	DepreciatedFinalizerName   = "kube-ovn-controller"
	KubeOVNControllerFinalizer = "kubeovn.io/kube-ovn-controller"

	AllocatedAnnotation          = "ovn.kubernetes.io/allocated"
	RoutedAnnotation             = "ovn.kubernetes.io/routed"
	RoutesAnnotation             = "ovn.kubernetes.io/routes"
	MacAddressAnnotation         = "ovn.kubernetes.io/mac_address"
	IPAddressAnnotation          = "ovn.kubernetes.io/ip_address"
	CidrAnnotation               = "ovn.kubernetes.io/cidr"
	GatewayAnnotation            = "ovn.kubernetes.io/gateway"
	IPPoolAnnotation             = "ovn.kubernetes.io/ip_pool"
	BgpAnnotation                = "ovn.kubernetes.io/bgp"
	SnatAnnotation               = "ovn.kubernetes.io/snat"
	EipAnnotation                = "ovn.kubernetes.io/eip"
	FipFinalizer                 = "ovn.kubernetes.io/fip"
	VipAnnotation                = "ovn.kubernetes.io/vip"
	AAPsAnnotation               = "ovn.kubernetes.io/aaps"
	ChassisAnnotation            = "ovn.kubernetes.io/chassis"
	VMAnnotation                 = "ovn.kubernetes.io/virtualmachine"
	ActivationStrategyAnnotation = "ovn.kubernetes.io/activation_strategy"

	VpcNatGatewayAnnotation                 = "ovn.kubernetes.io/vpc_nat_gw"
	VpcNatGatewayInitAnnotation             = "ovn.kubernetes.io/vpc_nat_gw_init"
	VpcNatGatewayContainerRestartAnnotation = "ovn.kubernetes.io/vpc_nat_gw_container_restarted"
	VpcNatGatewayActivatedAnnotation        = "ovn.kubernetes.io/vpc_nat_gw_activated"
	VpcEipsAnnotation                       = "ovn.kubernetes.io/vpc_eips"
	VpcFloatingIPMd5Annotation              = "ovn.kubernetes.io/vpc_floating_ips"
	VpcDnatMd5Annotation                    = "ovn.kubernetes.io/vpc_dnat_md5"
	VpcSnatMd5Annotation                    = "ovn.kubernetes.io/vpc_snat_md5"
	VpcCIDRsAnnotation                      = "ovn.kubernetes.io/vpc_cidrs"
	VpcCIDRsAnnotationTemplate              = "%s.kubernetes.io/vpc_cidrs"
	VpcLbAnnotation                         = "ovn.kubernetes.io/vpc_lb"
	VpcExternalLabel                        = "ovn.kubernetes.io/vpc_external"
	VpcEipAnnotation                        = "ovn.kubernetes.io/vpc_eip"
	VpcDnatEPortLabel                       = "ovn.kubernetes.io/vpc_dnat_eport"
	VpcNatAnnotation                        = "ovn.kubernetes.io/vpc_nat"
	OvnEipTypeLabel                         = "ovn.kubernetes.io/ovn_eip_type"
	EipV4IpLabel                            = "ovn.kubernetes.io/eip_v4_ip"
	EipV6IpLabel                            = "ovn.kubernetes.io/eip_v6_ip"

	SwitchLBRuleVipsAnnotation = "ovn.kubernetes.io/switch_lb_vip"
	SwitchLBRuleVip            = "switch_lb_vip"
	KubeHostVMVip              = "kube_host_vm_vip"
	// BgpLbVip marks a VIP reserved as a BGP-announced external IP for a LoadBalancer Service.
	// Unlike SwitchLBRuleVip and KubeHostVMVip, no OVN logical switch port is created;
	// the IP is held in IPAM only, and the controller writes it into the Service
	// status.loadBalancer.ingress for the BGP speaker to announce.
	// spec.externalIPs is intentionally left empty to avoid duplicate output in kubectl.
	BgpLbVip           = "bgp_lb_vip"
	SwitchLBRuleSubnet = "switch_lb_subnet"

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
	DefaultRouteAnnotationTemplate  = "%s.kubernetes.io/default_route"
	VfRepresentorNameTemplate       = "%s.kubernetes.io/vf_representor"
	VfNameTemplate                  = "%s.kubernetes.io/vf"
	ActivationStrategyTemplate      = "%s.kubernetes.io/activation_strategy"

	ProviderNetworkTemplate           = "%s.kubernetes.io/provider_network"
	ProviderNetworkErrMessageTemplate = "%s.provider-network.kubernetes.io/err_mesg"
	ProviderNetworkReadyTemplate      = "%s.provider-network.kubernetes.io/ready"
	ProviderNetworkExcludeTemplate    = "%s.provider-network.kubernetes.io/exclude"
	ProviderNetworkInterfaceTemplate  = "%s.provider-network.kubernetes.io/interface"
	ProviderNetworkMtuTemplate        = "%s.provider-network.kubernetes.io/mtu"
	ProviderNetworkVlanIntTemplate    = "%s.provider-network.kubernetes.io/vlan_interfaces"
	MirrorControlAnnotationTemplate   = "%s.kubernetes.io/mirror"
	PodNicAnnotationTemplate          = "%s.kubernetes.io/pod_nic_type"
	VMAnnotationTemplate              = "%s.kubernetes.io/virtualmachine"

	ExcludeIpsAnnotation = "ovn.kubernetes.io/exclude_ips"

	IngressRateAnnotation = "ovn.kubernetes.io/ingress_rate"
	EgressRateAnnotation  = "ovn.kubernetes.io/egress_rate"

	PortNameAnnotation      = "ovn.kubernetes.io/port_name"
	LogicalSwitchAnnotation = "ovn.kubernetes.io/logical_switch"

	// TunnelKeyAnnotation stores the OVN logical switch tunnel_key from Datapath_Binding
	TunnelKeyAnnotation         = "ovn.kubernetes.io/tunnel_key"
	TunnelKeyAnnotationTemplate = "%s.kubernetes.io/tunnel_key"

	TunnelInterfaceAnnotation = "ovn.kubernetes.io/tunnel_interface"
	NodeNetworksAnnotation    = "ovn.kubernetes.io/node_networks"

	OvsDpTypeLabel = "ovn.kubernetes.io/ovs_dp_type"

	VpcNameLabel                       = "ovn.kubernetes.io/vpc"
	SubnetNameLabel                    = "ovn.kubernetes.io/subnet"
	ICGatewayLabel                     = "ovn.kubernetes.io/ic-gw"
	ExGatewayLabel                     = "ovn.kubernetes.io/external-gw"
	NodeExtGwLabel                     = "ovn.kubernetes.io/node-ext-gw"
	VpcNatGatewayLabel                 = "ovn.kubernetes.io/vpc-nat-gw"
	IPReservedLabel                    = "ovn.kubernetes.io/ip_reserved"
	VpcNatGatewayNameLabel             = "ovn.kubernetes.io/vpc-nat-gw-name"
	VpcLbLabel                         = "ovn.kubernetes.io/vpc_lb"
	VpcDNSNameLabel                    = "ovn.kubernetes.io/vpc-dns"
	QoSLabel                           = "ovn.kubernetes.io/qos"
	NodeNameLabel                      = "ovn.kubernetes.io/node-name"
	NetworkPolicyLogAnnotation         = "ovn.kubernetes.io/enable_log"
	NetworkPolicyEnforcementAnnotation = "ovn.kubernetes.io/network_policy_enforcement"
	ACLActionsLogAnnotation            = "ovn.kubernetes.io/log_acl_actions"
	ACLLogMeterAnnotation              = "ovn.kubernetes.io/acl_log_meter_rate"

	// NadMacvlanMasterAnnotation value indicates the macvlan master interface
	NadMacvlanMasterAnnotation = "ovn.kubernetes.io/nad-macvlan-master"
	// NadMacvlanTypeLabel is set to "true" when subnet uses macvlan NAD, used for efficient label selector filtering
	NadMacvlanTypeLabel = "ovn.kubernetes.io/nad-macvlan-type"

	VpcEgressGatewayLabel  = "ovn.kubernetes.io/vpc-egress-gateway"
	GenerateHashAnnotation = "ovn.kubernetes.io/generate-hash"

	ServiceExternalIPFromSubnetAnnotation = "ovn.kubernetes.io/service_external_ip_from_subnet"
	ServiceHealthCheck                    = "ovn.kubernetes.io/service_health_check"

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
	NodeLspPrefix     = "node-"
	NodeAllowPriority = "3000"

	VxlanNic  = "vxlan_sys_4789"
	GeneveNic = "genev_sys_6081"

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

	// ClusterNetworkPolicy
	CnpMaxRules       = 25
	CnpMaxPriority    = 399
	CnpACLMaxPriority = 30000
	CnpMaxDomains     = 25
	CnpMaxNetworks    = 25

	AnpMaxRules        = 100
	AnpMaxPriority     = 99
	AnpACLMaxPriority  = 30000
	BanpACLMaxPriority = 1800
	AnpACLTier         = 1
	NetpolACLTier      = 2
	BanpACLTier        = 3

	DefaultMTU         = 1500
	GeneveHeaderLength = 100
	VxlanHeaderLength  = 50
	SttHeaderLength    = 72
	TCPIPHeaderLength  = 40

	OvnProvider                         = "ovn"
	DefaultNetworkAnnotation            = "v1.multus-cni.io/default-network"
	AttachNetworkResourceNameAnnotation = "k8s.v1.cni.cncf.io/resourceName"

	SRIOVResourceName = "mellanox.com/cx5_sriov_switchdev"

	SriovNicType = "sriov"

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

	GatewayRouterPolicyPriority      = 29000
	EgressGatewayDropPolicyPriority  = 29090
	EgressGatewayPolicyPriority      = 29100
	EgressGatewayLocalPolicyPriority = 29150
	NorthGatewayRoutePolicyPriority  = 29250
	U2OSubnetPolicyPriority          = 29400
	OvnICPolicyPriority              = 29500
	NodeRouterPolicyPriority         = 30000
	NodeLocalDNSPolicyPriority       = 30100
	SubnetRouterPolicyPriority       = 31000

	OffloadType = "offload-port"
	DpdkType    = "dpdk-port"
	VethType    = "veth-pair"

	MirrosRetryMaxTimes = 5
	MirrosRetryInterval = 1

	ChassisRetryMaxTimes           = 5
	ChassisCniDaemonRetryInterval  = 1
	ChassisControllerRetryInterval = 3

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

	McastQuerierName = "mcast-querier.%s"

	DefaultServiceSessionStickinessTimeout = 10800

	OvnSubnetGatewayIptables = "ovn-subnet-gateway"

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

	KubevirtNamespace = "kubevirt"

	DefaultOVNIPSecCA       = "ovn-ipsec-ca"
	DefaultOVSCACertPath    = "/var/lib/openvswitch/pki/switchca/cacert.pem"
	DefaultOVSCACertKeyPath = "/var/lib/openvswitch/pki/switchca/private/cakey.pem"

	SignerName = "kubeovn.io/signer"

	UnderlaySvcLocalOpenFlowPriority = 10000
	U2OKeepSrcMacPriority            = 10001

	UnderlaySvcLocalOpenFlowCookieV4 = 0x1000
	UnderlaySvcLocalOpenFlowCookieV6 = 0x1001

	MasqueradeExternalLBAccessMac = "00:00:00:01:00:01"
	MasqueradeCheckIP             = "0.0.0.0"
)

const (
	EnvKubernetesServiceHost = "KUBERNETES_SERVICE_HOST"
	EnvKubernetesServicePort = "KUBERNETES_SERVICE_PORT"

	EnvPodName      = "POD_NAME"
	EnvPodNamespace = "POD_NAMESPACE"
	EnvPodIP        = "POD_IP"
	EnvPodIPs       = "POD_IPS"
	EnvNodeName     = "NODE_NAME"
	EnvHostIP       = "HOST_IP"
	EnvHostIPs      = "HOST_IPS"

	EnvSSLEnabled  = "ENABLE_SSL"
	EnvGatewayName = "GATEWAY_NAME"
)

const (
	DatabaseICNB = "OVN_IC_Northbound"
	DatabaseICSB = "OVN_IC_Southbound"
)

const (
	NBDatabasePort = int32(6641)
	SBDatabasePort = int32(6642)
	NBRaftPort     = int32(6643)
	SBRaftPort     = int32(6644)
)

const (
	ICNBDatabasePort = int32(6645)
	ICSBDatabasePort = int32(6646)
	ICNBRaftPort     = int32(6647)
	ICSBRaftPort     = int32(6648)
)

const (
	SslCACert   = "/var/run/tls/cacert"
	SslCertPath = "/var/run/tls/cert"
	SslKeyPath  = "/var/run/tls/key"
)

const (
	ContentTypeJSON     = "application/json"
	ContentTypeProtobuf = runtime.ContentTypeProtobuf
	AcceptContentTypes  = runtime.ContentTypeProtobuf + "," + "application/json"
)

// BGP LB VIP annotations
//
// These annotations control how LoadBalancer VIPs (bgp_lb_vip type) are announced
// via BGP. The upstream router/switch only sees /32 prefixes and their BGP next-hops
// (node underlay IPs). It has zero visibility into pods, kube-proxy rules, or
// container ports — it simply forwards packets to the node that announced the route.
//
// Note: BgpAnnotation (ovn.kubernetes.io/bgp) is the shared policy key used across
// Pods, Subnets, and LoadBalancer VIPs. It is defined in the general annotation block
// above. The constants below are specific to the LoadBalancer VIP (bgp_lb_vip) path.
//
// Announcement modes (see collectSvcBgpPrefixes in pkg/speaker/subnet.go for full
// implementation rationale):
//
//	bgp=cluster / "true" (default, recommended for production):
//	  All speaker nodes announce the VIP simultaneously (ECMP). The upstream
//	  router distributes traffic across all nodes; kube-proxy/IPVS on each node
//	  routes to the correct VM regardless of entry point. VMs are migratable —
//	  the VIP is never tied to a node, so live migration is transparent.
//	  Requires the upstream switch/router to support ECMP.
//
//	bgp=local (ECMP with live-migration caveat):
//	  Currently behaves identically to bgp=cluster. The EIP is a floating IP
//	  programmed on kube-ipvs0 on EVERY node, so "local endpoint" filtering has
//	  no effect — all nodes still announce (ECMP). On VM live migration the BGP
//	  path does NOT automatically follow the VM to the destination node.
//	  TODO: implement EndpointSlice-aware local announcement.
//
//	bgp-speaker-node=<nodeName> (single-node, testing only):
//	  Only the named node announces the /32 route; all other speakers skip it.
//	  Use for testing or upstream switches that do NOT support ECMP.
//	  NOT suitable for production VM workloads: VM migration does not update
//	  the pinned node, adding a cross-node SNAT hop. Failover is fully manual.
//
// # MetalLB Compatibility
//
// Services previously managed by MetalLB carry the annotation:
//
//	metallb.universe.tf/allow-shared-ip: <vip-cr-name>
//
// The annotation value is the name of the VIP CR — not necessarily an IP address.
// It is common practice to name the VIP CR after the IP itself (e.g. "111.62.241.102"),
// but that is purely a business/operational naming convention; kube-ovn only checks
// whether the key is present, not the value format.
//
// This single annotation simultaneously replaces TWO kube-ovn annotations:
//
//  1. Replaces ovn.kubernetes.io/bgp-vip
//     The annotation VALUE is the VIP CR name. The controller resolves it to an IP
//     via virtualIpsLister.Get(vipName) → vip.Status.V4ip, exactly as it does for
//     ovn.kubernetes.io/bgp-vip, then writes the IP into status.loadBalancer.ingress.
//
//  2. Replaces ovn.kubernetes.io/bgp: "true"
//     Implies bgp=true (Default Mode / ECMP) announcement policy.
//     No explicit bgp annotation is required on the Service.
//     (Note: bgp=cluster is equivalent to bgp=true but is used for Pod/Subnet
//     BGP paths; the LB VIP path uses "true" as written by the controller.)
//
// This allows a zero-annotation-change migration from MetalLB to kube-ovn BGP
// speaker: the existing Service YAML needs no modification.
//
// Priority rule (if-else, mutually exclusive):
//
//	if metallb.universe.tf/allow-shared-ip is present
//	  → treat as BGP LB VIP (role 1) with bgp=true policy (role 2); ignore bgp-vip
//	else if ovn.kubernetes.io/bgp-vip is present
//	  → treat as BGP LB VIP; honour bgp annotation for policy selection
//	else
//	  → service is not a BGP LB VIP; skip
const (
	// BgpVipAnnotation is set on a LoadBalancer Service to specify the VIP name
	// (type=bgp_lb_vip) whose allocated IP will be written into
	// status.loadBalancer.ingress for BGP speaker announcement.
	BgpVipAnnotation = "ovn.kubernetes.io/bgp-vip"

	// BgpSpeakerNodeAnnotation pins a LoadBalancer VIP to a single BGP speaker
	// node. Only that node announces the /32 route; all others skip it.
	// Use for testing or non-ECMP upstreams only. Failover is manual.
	BgpSpeakerNodeAnnotation = "ovn.kubernetes.io/bgp-speaker-node"

	// BgpSpeakLbVipLabel gates whether the local node's BGP speaker is allowed
	// to announce LoadBalancer Service ingress IPs via BGP.
	// It intentionally uses the Label suffix to match the file-wide naming
	// convention for labels (for example, NodeNameLabel and VpcNatGatewayLabel),
	// unlike BgpAnnotation and BgpSpeakerNodeAnnotation which are annotations.
	// This is a node label, separate from BgpAnnotation which controls whether
	// the bgp-speaker Pod is scheduled onto the node.
	BgpSpeakLbVipLabel = "ovn.kubernetes.io/bgp-speak-lb-vip"

	// MetalLBAllowSharedIPAnnotation is MetalLB's shared-IP gate annotation.
	// Its value is the name of the VIP CR; naming it after the IP (e.g. "111.62.241.102")
	// is a common operational convention but is not required — kube-ovn only checks
	// whether the key is present.
	// kube-ovn treats its presence as simultaneously replacing two kube-ovn annotations:
	//   1. BgpVipAnnotation (ovn.kubernetes.io/bgp-vip): marks the service as a BGP LB VIP.
	//   2. BgpAnnotation=true (ovn.kubernetes.io/bgp: "true"): implies Default Mode (ECMP).
	//      (bgp=cluster is the equivalent for Pod/Subnet paths; LB VIP path uses "true".)
	// Takes priority over BgpVipAnnotation when both are present.
	MetalLBAllowSharedIPAnnotation = "metallb.universe.tf/allow-shared-ip"
)

// Readonly kinds of Kubernetes objects
var (
	KindNode    = ObjectKind[*corev1.Node]()
	KindPod     = ObjectKind[*corev1.Pod]()
	KindService = ObjectKind[*corev1.Service]()

	KindDeployment  = ObjectKind[*appsv1.Deployment]()
	KindDaemonSet   = ObjectKind[*appsv1.DaemonSet]()
	KindStatefulSet = ObjectKind[*appsv1.StatefulSet]()

	KindSubnet           = ObjectKind[*kubeovnv1.Subnet]()
	KindVpcEgressGateway = ObjectKind[*kubeovnv1.VpcEgressGateway]()

	KindVirtualMachine                  = ObjectKind[*kubevirtv1.VirtualMachine]()
	KindVirtualMachineInstance          = ObjectKind[*kubevirtv1.VirtualMachineInstance]()
	KindVirtualMachineInstanceMigration = ObjectKind[*kubevirtv1.VirtualMachineInstanceMigration]()
)
