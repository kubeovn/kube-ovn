package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

const (
	PolicyAllow SgPolicy = "allow"
	PolicyDrop  SgPolicy = "drop"
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
	Protocol   string   `json:"protocol"`
	Namespaces []string `json:"namespaces,omitempty"`
	CIDRBlock  string   `json:"cidrBlock"`
	Gateway    string   `json:"gateway"`
	ExcludeIps []string `json:"excludeIps,omitempty"`
	Provider   string   `json:"provider,omitempty"`

	GatewayType string `json:"gatewayType"`
	GatewayNode string `json:"gatewayNode"`
	NatOutgoing bool   `json:"natOutgoing"`

	ExternalEgressGateway string `json:"externalEgressGateway,omitempty"`
	PolicyRoutingPriority uint32 `json:"policyRoutingPriority,omitempty"`
	PolicyRoutingTableID  uint32 `json:"policyRoutingTableID,omitempty"`

	Private      bool     `json:"private"`
	AllowSubnets []string `json:"allowSubnets,omitempty"`

	Vlan   string `json:"vlan,omitempty"`
	HtbQos string `json:"htbqos,omitempty"`

	Vips []string `json:"vips,omitempty"`

	LogicalGateway         bool `json:"logicalGateway"`
	DisableGatewayCheck    bool `json:"disableGatewayCheck"`
	DisableInterConnection bool `json:"disableInterConnection"`

	EnableDHCP    bool   `json:"enableDHCP"`
	DHCPv4Options string `json:"dhcpV4Options"`
	DHCPv6Options string `json:"dhcpV6Options"`

	EnableIPv6RA  bool   `json:"enableIPv6RA"`
	IPv6RAConfigs string `json:"ipv6RAConfigs"`

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
	Subnets []string `json:"subnets,omitempty" patchStrategy:"merge"`

	// Conditions represents the latest state of the object
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []VlanCondition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
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
}

type ProviderNetworkStatus struct {
	// +optional
	// +patchStrategy=merge
	Ready bool `json:"ready" patchStrategy:"merge"`

	// +optional
	// +patchStrategy=merge
	ReadyNodes []string `json:"readyNodes,omitempty" patchStrategy:"merge"`

	// +optional
	// +patchStrategy=merge
	Vlans []string `json:"vlans,omitempty" patchStrategy:"merge"`

	// Conditions represents the latest state of the object
	// +optional
	// +patchMergeKey=node
	// +patchStrategy=merge
	Conditions []ProviderNetworkCondition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"node"`
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

const (
	PolicyRouteActionAllow   PolicyRouteAction = "allow"
	PolicyRouteActionDrop    PolicyRouteAction = "drop"
	PolicyRouteActionReroute PolicyRouteAction = "reroute"
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
	Vpc             string            `json:"vpc"`
	Subnet          string            `json:"subnet"`
	LanIp           string            `json:"lanIp"`
	Selector        []string          `json:"selector"`
	Eips            []*Eip            `json:"eips,omitempty"`
	FloatingIpRules []*FloutingIpRule `json:"floatingIpRules,omitempty"`
	DnatRules       []*DnatRule       `json:"dnatRules,omitempty"`
	SnatRules       []*SnatRule       `json:"snatRules,omitempty"`
}

type Eip struct {
	EipCIDR string `json:"eipCIDR"`
	Gateway string `json:"gateway"`
}

type FloutingIpRule struct {
	Eip        string `json:"eip"`
	InternalIp string `json:"internalIp"`
}

type SnatRule struct {
	Eip          string `json:"eip"`
	InternalCIDR string `json:"internalCIDR"`
}

type DnatRule struct {
	Eip          string `json:"eip"`
	ExternalPort string `json:"externalPort"`
	Protocol     string `json:"protocol,omitempty"`
	InternalIp   string `json:"internalIp"`
	InternalPort string `json:"internalPort"`
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
