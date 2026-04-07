package v1

import (
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

type RoutePolicy string

const (
	PolicySrc RoutePolicy = "policySrc"
	PolicyDst RoutePolicy = "policyDst"
)

type PolicyRouteAction string

var (
	PolicyRouteActionAllow   = PolicyRouteAction(ovnnb.LogicalRouterPolicyActionAllow)
	PolicyRouteActionDrop    = PolicyRouteAction(ovnnb.LogicalRouterPolicyActionDrop)
	PolicyRouteActionReroute = PolicyRouteAction(ovnnb.LogicalRouterPolicyActionReroute)
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type VpcList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Vpc `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient:nonNamespaced
// +kubebuilder:resource:scope="Cluster",shortName="vpc",path="vpcs"
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="EnableExternal",type="boolean",JSONPath=".status.enableExternal"
// +kubebuilder:printcolumn:name="EnableBfd",type="boolean",JSONPath=".status.enableBfd"
// +kubebuilder:printcolumn:name="Standby",type="boolean",JSONPath=".status.standby"
// +kubebuilder:printcolumn:name="Subnets",type="string",JSONPath=".status.subnets"
// +kubebuilder:printcolumn:name="ExtraExternalSubnets",type="string",JSONPath=".status.extraExternalSubnets"
// +kubebuilder:printcolumn:name="Namespaces",type="string",JSONPath=".spec.namespaces"
// +kubebuilder:printcolumn:name="DefaultSubnet",type="string",JSONPath=".status.defaultLogicalSwitch"
type Vpc struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   VpcSpec   `json:"spec"`
	Status VpcStatus `json:"status"`
}

type VpcSpec struct {
	// The default subnet name for the VPC
	DefaultSubnet string `json:"defaultSubnet,omitempty"`
	// List of namespaces that can use this VPC
	Namespaces []string `json:"namespaces,omitempty"`
	// Static routes for the VPC.
	StaticRoutes []*StaticRoute `json:"staticRoutes,omitempty"`
	// Policy routes for the VPC.
	PolicyRoutes []*PolicyRoute `json:"policyRoutes,omitempty"`
	// VPC peering configurations.
	VpcPeerings []*VpcPeering `json:"vpcPeerings,omitempty"`

	// Enable external network access for the VPC
	EnableExternal bool `json:"enableExternal,omitempty"`
	// EnableExternal only handles default external subnet

	// Extra external subnets for provider-network VLAN. Immutable after creation.
	ExtraExternalSubnets []string `json:"extraExternalSubnets,omitempty"`
	// ExtraExternalSubnets only handles provider-network vlan subnet

	// Enable BFD (Bidirectional Forwarding Detection) for the VPC
	EnableBfd bool `json:"enableBfd,omitempty"`

	// optional BFD LRP configuration
	// currently the LRP is used for vpc external gateway only
	BFDPort *BFDPort `json:"bfdPort"`
}

type BFDPort struct {
	// Enable BFD port
	// +kubebuilder:default=false
	Enabled bool `json:"enabled"`
	// ip address(es) of the BFD port
	IP string `json:"ip,omitempty"`

	// Optional node selector used to select the nodes where the BFD LRP will be hosted.
	// If not specified, at most 3 nodes will be selected.
	NodeSelector *metav1.LabelSelector `json:"nodeSelector,omitempty"`
}

func (p *BFDPort) IsEnabled() bool {
	return p != nil && p.Enabled
}

type VpcPeering struct {
	RemoteVpc      string `json:"remoteVpc,omitempty"`
	LocalConnectIP string `json:"localConnectIP,omitempty"`
}

type StaticRoute struct {
	Policy     RoutePolicy `json:"policy,omitempty"`
	CIDR       string      `json:"cidr"`
	NextHopIP  string      `json:"nextHopIP"`
	ECMPMode   string      `json:"ecmpMode"`
	BfdID      string      `json:"bfdId"`
	RouteTable string      `json:"routeTable"`
}

type PolicyRoute struct {
	// Priority of the policy route (0-32767)
	Priority int `json:"priority,omitempty"`
	// Match of the policy route
	Match string `json:"match,omitempty"`
	// Action of the policy route
	Action PolicyRouteAction `json:"action,omitempty"`
	// NextHopIP is an optional parameter. It needs to be provided only when 'action' is 'reroute'.
	// +optional
	NextHopIP string `json:"nextHopIP,omitempty"`
}

type BFDPortStatus struct {
	// BFD port name
	Name string `json:"name,omitempty"`
	// BFD port IP address
	IP string `json:"ip,omitempty"`
	// Nodes where BFD port is deployed
	Nodes []string `json:"nodes,omitempty"`
}

func (s BFDPortStatus) IsEmpty() bool {
	return s.Name == "" && s.IP == "" && len(s.Nodes) == 0
}

func (s *BFDPortStatus) Clear() {
	s.Name, s.IP, s.Nodes = "", "", nil
}

type VpcStatus struct {
	// Conditions represents the latest state of the object
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	Standby bool `json:"standby"`
	// Whether this is the default VPC.
	Default                 bool     `json:"default"`
	DefaultLogicalSwitch    string   `json:"defaultLogicalSwitch"`
	Router                  string   `json:"router"`
	TCPLoadBalancer         string   `json:"tcpLoadBalancer"`
	UDPLoadBalancer         string   `json:"udpLoadBalancer"`
	SctpLoadBalancer        string   `json:"sctpLoadBalancer"`
	TCPSessionLoadBalancer  string   `json:"tcpSessionLoadBalancer"`
	UDPSessionLoadBalancer  string   `json:"udpSessionLoadBalancer"`
	SctpSessionLoadBalancer string   `json:"sctpSessionLoadBalancer"`
	Subnets                 []string `json:"subnets"`
	// VPC peering configurations.
	VpcPeerings    []string `json:"vpcPeerings"`
	EnableExternal bool     `json:"enableExternal"`
	// Extra external subnets for provider-network VLAN. Immutable after creation.
	ExtraExternalSubnets []string `json:"extraExternalSubnets"`
	EnableBfd            bool     `json:"enableBfd"`

	BFDPort BFDPortStatus `json:"bfdPort"`
}

func (s *VpcStatus) Bytes() ([]byte, error) {
	bytes, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	newStr := fmt.Sprintf(`{"status": %s}`, string(bytes))
	klog.V(5).Info("status body", newStr)
	return []byte(newStr), nil
}
