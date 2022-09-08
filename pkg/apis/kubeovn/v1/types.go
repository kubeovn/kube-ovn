package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

const (
	ProtocolIPv4 = "IPv4"
	ProtocolIPv6 = "IPv6"
	ProtocolDual = "Dual"

	GWDistributedType = "distributed"
	GWCentralizedType = "centralized"
)

type SgRemoteType string

const (
	SgRemoteTypeAddress SgRemoteType = "address"
	SgRemoteTypeSg      SgRemoteType = "securityGroup"
)

type SgProtocol string

const (
	ProtocolALL  SgProtocol = "all"
	ProtocolICMP SgProtocol = "icmp"
	ProtocolTCP  SgProtocol = "tcp"
	ProtocolUDP  SgProtocol = "udp"
)

type SgPolicy string

var (
	PolicyAllow = SgPolicy(ovnnb.ACLActionAllow)
	PolicyDrop  = SgPolicy(ovnnb.ACLActionDrop)
)

// Constants for condition
const (
	// Ready => controller considers this resource Ready
	Ready = "Ready"
	// Validated => Spec passed validating
	Validated = "Validated"
	// Error => last recorded error
	Error = "Error"

	ReasonInit = "Init"
)

// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient:nonNamespaced

type IP struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec IPSpec `json:"spec"`
}

type IPSpec struct {
	PodName       string   `json:"podName"`
	Namespace     string   `json:"namespace"`
	Subnet        string   `json:"subnet"`
	AttachSubnets []string `json:"attachSubnets"`
	NodeName      string   `json:"nodeName"`
	IPAddress     string   `json:"ipAddress"`
	V4IPAddress   string   `json:"v4IpAddress"`
	V6IPAddress   string   `json:"v6IpAddress"`
	AttachIPs     []string `json:"attachIps"`
	MacAddress    string   `json:"macAddress"`
	AttachMacs    []string `json:"attachMacs"`
	ContainerID   string   `json:"containerID"`
	PodType       string   `json:"podType"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type IPList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []IP `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient:nonNamespaced

type Subnet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SubnetSpec   `json:"spec"`
	Status SubnetStatus `json:"status,omitempty"`
}

type SubnetSpec struct {
	Default    bool     `json:"default"`
	Vpc        string   `json:"vpc,omitempty"`
	Protocol   string   `json:"protocol,omitempty"`
	Namespaces []string `json:"namespaces,omitempty"`
	CIDRBlock  string   `json:"cidrBlock"`
	Gateway    string   `json:"gateway"`
	ExcludeIps []string `json:"excludeIps,omitempty"`
	Provider   string   `json:"provider,omitempty"`

	GatewayType string `json:"gatewayType,omitempty"`
	GatewayNode string `json:"gatewayNode,omitempty"`
	NatOutgoing bool   `json:"natOutgoing"`
	U2oRouting  bool   `json:"u2oRouting,omitempty"`

	ExternalEgressGateway string `json:"externalEgressGateway,omitempty"`
	PolicyRoutingPriority uint32 `json:"policyRoutingPriority,omitempty"`
	PolicyRoutingTableID  uint32 `json:"policyRoutingTableID,omitempty"`

	Private      bool     `json:"private"`
	AllowSubnets []string `json:"allowSubnets,omitempty"`

	Vlan   string `json:"vlan,omitempty"`
	HtbQos string `json:"htbqos,omitempty"`

	Vips []string `json:"vips,omitempty"`

	LogicalGateway         bool `json:"logicalGateway,omitempty"`
	DisableGatewayCheck    bool `json:"disableGatewayCheck,omitempty"`
	DisableInterConnection bool `json:"disableInterConnection,omitempty"`

	EnableDHCP    bool   `json:"enableDHCP,omitempty"`
	DHCPv4Options string `json:"dhcpV4Options,omitempty"`
	DHCPv6Options string `json:"dhcpV6Options,omitempty"`

	EnableIPv6RA  bool   `json:"enableIPv6RA,omitempty"`
	IPv6RAConfigs string `json:"ipv6RAConfigs,omitempty"`

	Acls []Acl `json:"acls,omitempty"`
}

type Acl struct {
	Direction string `json:"direction,omitempty"`
	Priority  int    `json:"priority,omitempty"`
	Match     string `json:"match,omitempty"`
	Action    string `json:"action,omitempty"`
}

// ConditionType encodes information on the condition
type ConditionType string

// Condition describes the state of an object at a certain point.
// +k8s:deepcopy-gen=true
type SubnetCondition struct {
	// Type of condition.
	Type ConditionType `json:"type"`
	// Status of the condition, one of True, False, Unknown.
	Status corev1.ConditionStatus `json:"status"`
	// The reason for the condition's last transition.
	// +optional
	Reason string `json:"reason,omitempty"`
	// A human readable message indicating details about the transition.
	// +optional
	Message string `json:"message,omitempty"`
	// Last time the condition was probed
	// +optional
	LastUpdateTime metav1.Time `json:"lastUpdateTime,omitempty"`
	// Last time the condition transitioned from one status to another.
	// +optional
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
}

type SubnetStatus struct {
	// Conditions represents the latest state of the object
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []SubnetCondition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	AvailableIPs      float64 `json:"availableIPs"`
	UsingIPs          float64 `json:"usingIPs"`
	V4AvailableIPs    float64 `json:"v4availableIPs"`
	V4UsingIPs        float64 `json:"v4usingIPs"`
	V6AvailableIPs    float64 `json:"v6availableIPs"`
	V6UsingIPs        float64 `json:"v6usingIPs"`
	ActivateGateway   string  `json:"activateGateway"`
	DHCPv4OptionsUUID string  `json:"dhcpV4OptionsUUID"`
	DHCPv6OptionsUUID string  `json:"dhcpV6OptionsUUID"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type SubnetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Subnet `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient:nonNamespaced

type Vlan struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VlanSpec   `json:"spec"`
	Status VlanStatus `json:"status"`
}

type VlanSpec struct {
	// deprecated fields, use ID & Provider instead
	VlanId                int    `json:"vlanId,omitempty"`
	ProviderInterfaceName string `json:"providerInterfaceName,omitempty"`

	ID       int    `json:"id"`
	Provider string `json:"provider,omitempty"`
}

type VlanStatus struct {
	// +optional
	// +patchStrategy=merge
	Subnets []string `json:"subnets,omitempty"`

	// Conditions represents the latest state of the object
	// +optional
	Conditions []VlanCondition `json:"conditions,omitempty"`
}

// Condition describes the state of an object at a certain point.
// +k8s:deepcopy-gen=true
type VlanCondition struct {
	// Type of condition.
	Type ConditionType `json:"type"`
	// Status of the condition, one of True, False, Unknown.
	Status corev1.ConditionStatus `json:"status"`
	// The reason for the condition's last transition.
	// +optional
	Reason string `json:"reason,omitempty"`
	// A human readable message indicating details about the transition.
	// +optional
	Message string `json:"message,omitempty"`
	// Last time the condition was probed
	// +optional
	LastUpdateTime metav1.Time `json:"lastUpdateTime,omitempty"`
	// Last time the condition transitioned from one status to another.
	// +optional
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type VlanList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Vlan `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient:nonNamespaced
// +resourceName=provider-networks

type ProviderNetwork struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ProviderNetworkSpec   `json:"spec"`
	Status ProviderNetworkStatus `json:"status"`
}

type CustomInterface struct {
	Interface string   `json:"interface"`
	Nodes     []string `json:"nodes"`
}
type ProviderNetworkSpec struct {
	DefaultInterface string            `json:"defaultInterface,omitempty"`
	CustomInterfaces []CustomInterface `json:"customInterfaces,omitempty"`
	ExcludeNodes     []string          `json:"excludeNodes,omitempty"`
	ExchangeLinkName bool              `json:"exchangeLinkName,omitempty"`
}

type ProviderNetworkStatus struct {
	// +optional
	Ready bool `json:"ready"`

	// +optional
	ReadyNodes []string `json:"readyNodes,omitempty"`

	// +optional
	Vlans []string `json:"vlans,omitempty"`

	// Conditions represents the latest state of the object
	// +optional
	Conditions []ProviderNetworkCondition `json:"conditions,omitempty"`
}

// Condition describes the state of an object at a certain point.
// +k8s:deepcopy-gen=true
type ProviderNetworkCondition struct {
	// Node name
	Node string `json:"node"`
	// Type of condition.
	Type ConditionType `json:"type"`
	// Status of the condition, one of True, False, Unknown.
	Status corev1.ConditionStatus `json:"status"`
	// The reason for the condition's last transition.
	// +optional
	Reason string `json:"reason,omitempty"`
	// A human readable message indicating details about the transition.
	// +optional
	Message string `json:"message,omitempty"`
	// Last time the condition was probed
	// +optional
	LastUpdateTime metav1.Time `json:"lastUpdateTime,omitempty"`
	// Last time the condition transitioned from one status to another.
	// +optional
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type ProviderNetworkList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []ProviderNetwork `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient:nonNamespaced

type Vpc struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VpcSpec   `json:"spec"`
	Status VpcStatus `json:"status,omitempty"`
}

type VpcSpec struct {
	Namespaces   []string       `json:"namespaces,omitempty"`
	StaticRoutes []*StaticRoute `json:"staticRoutes,omitempty"`
	PolicyRoutes []*PolicyRoute `json:"policyRoutes,omitempty"`
	VpcPeerings  []*VpcPeering  `json:"vpcPeerings,omitempty"`
}

type VpcPeering struct {
	RemoteVpc      string `json:"remoteVpc,omitempty"`
	LocalConnectIP string `json:"localConnectIP,omitempty"`
}

type RoutePolicy string

const (
	PolicySrc RoutePolicy = "policySrc"
	PolicyDst RoutePolicy = "policyDst"
)

type StaticRoute struct {
	Policy    RoutePolicy `json:"policy,omitempty"`
	CIDR      string      `json:"cidr"`
	NextHopIP string      `json:"nextHopIP"`
}

type PolicyRouteAction string

var (
	PolicyRouteActionAllow   = PolicyRouteAction(ovnnb.LogicalRouterPolicyActionAllow)
	PolicyRouteActionDrop    = PolicyRouteAction(ovnnb.LogicalRouterPolicyActionDrop)
	PolicyRouteActionReroute = PolicyRouteAction(ovnnb.LogicalRouterPolicyActionReroute)
)

type PolicyRoute struct {
	Priority int32             `json:"priority,omitempty"`
	Match    string            `json:"match,omitempty"`
	Action   PolicyRouteAction `json:"action,omitempty"`
	// NextHopIP is an optional parameter. It needs to be provided only when 'action' is 'reroute'.
	// +optional
	NextHopIP string `json:"nextHopIP,omitempty"`
}

type VpcStatus struct {
	// Conditions represents the latest state of the object
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []VpcCondition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	Standby                bool     `json:"standby"`
	Default                bool     `json:"default"`
	DefaultLogicalSwitch   string   `json:"defaultLogicalSwitch"`
	Router                 string   `json:"router"`
	TcpLoadBalancer        string   `json:"tcpLoadBalancer"`
	UdpLoadBalancer        string   `json:"udpLoadBalancer"`
	TcpSessionLoadBalancer string   `json:"tcpSessionLoadBalancer"`
	UdpSessionLoadBalancer string   `json:"udpSessionLoadBalancer"`
	Subnets                []string `json:"subnets"`
	VpcPeerings            []string `json:"vpcPeerings"`
}

// Condition describes the state of an object at a certain point.
// +k8s:deepcopy-gen=true
type VpcCondition struct {
	// Type of condition.
	Type ConditionType `json:"type"`
	// Status of the condition, one of True, False, Unknown.
	Status corev1.ConditionStatus `json:"status"`
	// The reason for the condition's last transition.
	// +optional
	Reason string `json:"reason,omitempty"`
	// A human readable message indicating details about the transition.
	// +optional
	Message string `json:"message,omitempty"`
	// Last time the condition was probed
	// +optional
	LastUpdateTime metav1.Time `json:"lastUpdateTime,omitempty"`
	// Last time the condition transitioned from one status to another.
	// +optional
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type VpcList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Vpc `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient:nonNamespaced
// +resourceName=vpc-nat-gateways

type VpcNatGateway struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec VpcNatSpec `json:"spec"`
}

type VpcNatSpec struct {
	Vpc      string   `json:"vpc"`
	Subnet   string   `json:"subnet"`
	LanIp    string   `json:"lanIp"`
	Selector []string `json:"selector"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient:nonNamespaced
// +resourceName=iptables-eips

type IptablesEIP struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   IptablesEipSpec   `json:"spec"`
	Status IptablesEipStatus `json:"status,omitempty"`
}
type IptablesEipSpec struct {
	V4ip       string `json:"v4ip"`
	V6ip       string `json:"v6ip"`
	MacAddress string `json:"macAddress"`
	NatGwDp    string `json:"natGwDp"`
}

// Condition describes the state of an object at a certain point.
// +k8s:deepcopy-gen=true
type IptablesEIPCondition struct {
	// Type of condition.
	Type ConditionType `json:"type"`
	// Status of the condition, one of True, False, Unknown.
	Status corev1.ConditionStatus `json:"status"`
	// The reason for the condition's last transition.
	// +optional
	Reason string `json:"reason,omitempty"`
	// A human readable message indicating details about the transition.
	// +optional
	Message string `json:"message,omitempty"`
	// Last time the condition was probed
	// +optional
	LastUpdateTime metav1.Time `json:"lastUpdateTime,omitempty"`
	// Last time the condition transitioned from one status to another.
	// +optional
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
}

type IptablesEipStatus struct {
	// +optional
	// +patchStrategy=merge
	Ready bool   `json:"ready" patchStrategy:"merge"`
	IP    string `json:"ip" patchStrategy:"merge"`
	Redo  string `json:"redo" patchStrategy:"merge"`
	Nat   string `json:"nat" patchStrategy:"merge"`

	// Conditions represents the latest state of the object
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []IptablesEIPCondition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type IptablesEIPList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []IptablesEIP `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient:nonNamespaced
// +resourceName=iptables-fip-rules

type IptablesFIPRule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   IptablesFIPRuleSpec   `json:"spec"`
	Status IptablesFIPRuleStatus `json:"status,omitempty"`
}
type IptablesFIPRuleSpec struct {
	EIP        string `json:"eip"`
	InternalIp string `json:"internalIp"`
}

// Condition describes the state of an object at a certain point.
// +k8s:deepcopy-gen=true
type IptablesFIPRuleCondition struct {
	// Type of condition.
	Type ConditionType `json:"type"`
	// Status of the condition, one of True, False, Unknown.
	Status corev1.ConditionStatus `json:"status"`
	// The reason for the condition's last transition.
	// +optional
	Reason string `json:"reason,omitempty"`
	// A human readable message indicating details about the transition.
	// +optional
	Message string `json:"message,omitempty"`
	// Last time the condition was probed
	// +optional
	LastUpdateTime metav1.Time `json:"lastUpdateTime,omitempty"`
	// Last time the condition transitioned from one status to another.
	// +optional
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
}

type IptablesFIPRuleStatus struct {
	// +optional
	// +patchStrategy=merge
	Ready   bool   `json:"ready" patchStrategy:"merge"`
	V4ip    string `json:"v4ip" patchStrategy:"merge"`
	V6ip    string `json:"v6ip" patchStrategy:"merge"`
	NatGwDp string `json:"natGwDp" patchStrategy:"merge"`
	Redo    string `json:"redo" patchStrategy:"merge"`

	// Conditions represents the latest state of the object
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []IptablesFIPRuleCondition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type IptablesFIPRuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []IptablesFIPRule `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient:nonNamespaced
// +resourceName=iptables-snat-rules
type IptablesSnatRule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   IptablesSnatRuleSpec   `json:"spec"`
	Status IptablesSnatRuleStatus `json:"status,omitempty"`
}

type IptablesSnatRuleSpec struct {
	EIP          string `json:"eip"`
	InternalCIDR string `json:"internalCIDR"`
}

// Condition describes the state of an object at a certain point.
// +k8s:deepcopy-gen=true
type IptablesSnatRuleCondition struct {
	// Type of condition.
	Type ConditionType `json:"type"`
	// Status of the condition, one of True, False, Unknown.
	Status corev1.ConditionStatus `json:"status"`
	// The reason for the condition's last transition.
	// +optional
	Reason string `json:"reason,omitempty"`
	// A human readable message indicating details about the transition.
	// +optional
	Message string `json:"message,omitempty"`
	// Last time the condition was probed
	// +optional
	LastUpdateTime metav1.Time `json:"lastUpdateTime,omitempty"`
	// Last time the condition transitioned from one status to another.
	// +optional
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
}

type IptablesSnatRuleStatus struct {
	// +optional
	// +patchStrategy=merge
	Ready   bool   `json:"ready" patchStrategy:"merge"`
	V4ip    string `json:"v4ip" patchStrategy:"merge"`
	V6ip    string `json:"v6ip" patchStrategy:"merge"`
	NatGwDp string `json:"natGwDp" patchStrategy:"merge"`
	Redo    string `json:"redo" patchStrategy:"merge"`

	// Conditions represents the latest state of the object
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []IptablesSnatRuleCondition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type IptablesSnatRuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []IptablesSnatRule `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient:nonNamespaced
// +resourceName=iptables-dnat-rules

type IptablesDnatRule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   IptablesDnatRuleSpec   `json:"spec"`
	Status IptablesDnatRuleStatus `json:"status,omitempty"`
}
type IptablesDnatRuleSpec struct {
	EIP          string `json:"eip"`
	ExternalPort string `json:"externalPort"`
	Protocol     string `json:"protocol,omitempty"`
	InternalIp   string `json:"internalIp"`
	InternalPort string `json:"internalPort"`
}

// Condition describes the state of an object at a certain point.
// +k8s:deepcopy-gen=true
type IptablesDnatRuleCondition struct {
	// Type of condition.
	Type ConditionType `json:"type"`
	// Status of the condition, one of True, False, Unknown.
	Status corev1.ConditionStatus `json:"status"`
	// The reason for the condition's last transition.
	// +optional
	Reason string `json:"reason,omitempty"`
	// A human readable message indicating details about the transition.
	// +optional
	Message string `json:"message,omitempty"`
	// Last time the condition was probed
	// +optional
	LastUpdateTime metav1.Time `json:"lastUpdateTime,omitempty"`
	// Last time the condition transitioned from one status to another.
	// +optional
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
}

type IptablesDnatRuleStatus struct {
	// +optional
	// +patchStrategy=merge
	Ready   bool   `json:"ready" patchStrategy:"merge"`
	V4ip    string `json:"v4ip" patchStrategy:"merge"`
	V6ip    string `json:"v6ip" patchStrategy:"merge"`
	NatGwDp string `json:"natGwDp" patchStrategy:"merge"`
	Redo    string `json:"redo" patchStrategy:"merge"`

	// Conditions represents the latest state of the object
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []IptablesDnatRuleCondition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type IptablesDnatRuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []IptablesDnatRule `json:"items"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type VpcNatGatewayList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []VpcNatGateway `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient:nonNamespaced
// +resourceName=security-groups

type SecurityGroup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SecurityGroupSpec   `json:"spec"`
	Status SecurityGroupStatus `json:"status"`
}

type SecurityGroupSpec struct {
	IngressRules          []*SgRule `json:"ingressRules,omitempty"`
	EgressRules           []*SgRule `json:"egressRules,omitempty"`
	AllowSameGroupTraffic bool      `json:"allowSameGroupTraffic,omitempty"`
}

type SecurityGroupStatus struct {
	PortGroup              string `json:"portGroup"`
	AllowSameGroupTraffic  bool   `json:"allowSameGroupTraffic"`
	IngressMd5             string `json:"ingressMd5"`
	EgressMd5              string `json:"egressMd5"`
	IngressLastSyncSuccess bool   `json:"ingressLastSyncSuccess"`
	EgressLastSyncSuccess  bool   `json:"egressLastSyncSuccess"`
}

type SgRule struct {
	IPVersion           string       `json:"ipVersion"`
	Protocol            SgProtocol   `json:"protocol,omitempty"`
	Priority            int          `json:"priority,omitempty"`
	RemoteType          SgRemoteType `json:"remoteType"`
	RemoteAddress       string       `json:"remoteAddress,omitempty"`
	RemoteSecurityGroup string       `json:"remoteSecurityGroup,omitempty"`
	PortRangeMin        int          `json:"portRangeMin,omitempty"`
	PortRangeMax        int          `json:"portRangeMax,omitempty"`
	Policy              SgPolicy     `json:"policy"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type SecurityGroupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []SecurityGroup `json:"items"`
}

// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient:nonNamespaced

type HtbQos struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec HtbQosSpec `json:"spec"`
}

type HtbQosSpec struct {
	Priority string `json:"priority"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type HtbQosList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []HtbQos `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient:nonNamespaced

type Vip struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VipSpec   `json:"spec"`
	Status VipStatus `json:"status,omitempty"`
}

type VipSpec struct {
	Namespace     string   `json:"namespace"`
	Subnet        string   `json:"subnet"`
	V4ip          string   `json:"v4ip"`
	V6ip          string   `json:"v6ip"`
	MacAddress    string   `json:"macAddress"`
	ParentV4ip    string   `json:"parentV4ip"`
	ParentV6ip    string   `json:"parentV6ip"`
	ParentMac     string   `json:"parentMac"`
	AttachSubnets []string `json:"attachSubnets"`
}

// Condition describes the state of an object at a certain point.
// +k8s:deepcopy-gen=true
type VipCondition struct {
	// Type of condition.
	Type ConditionType `json:"type"`
	// Status of the condition, one of True, False, Unknown.
	Status corev1.ConditionStatus `json:"status"`
	// The reason for the condition's last transition.
	// +optional
	Reason string `json:"reason,omitempty"`
	// A human readable message indicating details about the transition.
	// +optional
	Message string `json:"message,omitempty"`
	// Last time the condition was probed
	// +optional
	LastUpdateTime metav1.Time `json:"lastUpdateTime,omitempty"`
	// Last time the condition transitioned from one status to another.
	// +optional
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
}

type VipStatus struct {
	// Conditions represents the latest state of the object
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []VipCondition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	Ready bool   `json:"ready" patchStrategy:"merge"`
	V4ip  string `json:"v4ip" patchStrategy:"merge"`
	V6ip  string `json:"v6ip" patchStrategy:"merge"`
	Mac   string `json:"mac" patchStrategy:"merge"`
	Pv4ip string `json:"pv4ip" patchStrategy:"merge"`
	Pv6ip string `json:"pv6ip" patchStrategy:"merge"`
	Pmac  string `json:"pmac" patchStrategy:"merge"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type VipList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Vip `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient:nonNamespaced
// +resourceName=vpc-dnses

type VpcDns struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VpcDnsSpec   `json:"spec"`
	Status VpcDnsStatus `json:"status,omitempty"`
}

type VpcDnsSpec struct {
	Vpc    string `json:"vpc"`
	Subnet string `json:"subnet"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type VpcDnsList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []VpcDns `json:"items"`
}

type VpcDnsStatus struct {
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []VpcDnsCondition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	Active bool `json:"active" patchStrategy:"merge"`
}

// Condition describes the state of an object at a certain point.
// +k8s:deepcopy-gen=true
type VpcDnsCondition struct {
	// Type of condition.
	Type ConditionType `json:"type"`
	// Status of the condition, one of True, False, Unknown.
	Status corev1.ConditionStatus `json:"status"`
	// The reason for the condition's last transition.
	// +optional
	Reason string `json:"reason,omitempty"`
	// A human readable message indicating details about the transition.
	// +optional
	Message string `json:"message,omitempty"`
	// Last time the condition was probed
	// +optional
	LastUpdateTime metav1.Time `json:"lastUpdateTime,omitempty"`
	// Last time the condition transitioned from one status to another.
	// +optional
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
}

type SlrPort struct {
	Name       string `json:"name"`
	Port       int32  `json:"port"`
	TargetPort int32  `json:"targetPort,omitempty"`
	Protocol   string `json:"protocol"`
}

type SwitchLBRuleSpec struct {
	Vip             string    `json:"vip"`
	Namespace       string    `json:"namespace"`
	Selector        []string  `json:"selector"`
	SessionAffinity string    `json:"sessionAffinity,omitempty"`
	Ports           []SlrPort `json:"ports"`
}

type SwitchLBRuleStatus struct {
	// Conditions represents the latest state of the object
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []SwitchLBRuleCondition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	Ports   string `json:"ports" patchStrategy:"merge"`
	Service string `json:"service" patchStrategy:"merge"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient:nonNamespaced
// +resourceName=switch-lb-rules
type SwitchLBRule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SwitchLBRuleSpec   `json:"spec"`
	Status SwitchLBRuleStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type SwitchLBRuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []SwitchLBRule `json:"items"`
}

// Condition describes the state of an object at a certain point.
// +k8s:deepcopy-gen=true
type SwitchLBRuleCondition struct {
	// Type of condition.
	Type ConditionType `json:"type"`
	// Status of the condition, one of True, False, Unknown.
	Status corev1.ConditionStatus `json:"status"`
	// The reason for the condition's last transition.
	// +optional
	Reason string `json:"reason,omitempty"`
	// A human readable message indicating details about the transition.
	// +optional
	Message string `json:"message,omitempty"`
	// Last time the condition was probed
	// +optional
	LastUpdateTime metav1.Time `json:"lastUpdateTime,omitempty"`
	// Last time the condition transitioned from one status to another.
	// +optional
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
}
