package v1

import (
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

type SgRemoteType string

const (
	SgRemoteTypeAddress SgRemoteType = "address"
	SgRemoteTypeSg      SgRemoteType = "securityGroup"
)

type SgProtocol string

const (
	SgProtocolALL  SgProtocol = "all"
	SgProtocolICMP SgProtocol = "icmp"
	SgProtocolTCP  SgProtocol = "tcp"
	SgProtocolUDP  SgProtocol = "udp"
)

type SgPolicy string

var (
	SgPolicyAllow SgPolicy = "allow"
	SgPolicyDrop  SgPolicy = "drop"
	// Pass ACL processing to next tier
	SgPolicyPass SgPolicy = "pass"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type SecurityGroupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []SecurityGroup `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient:nonNamespaced
// +resourceName=security-groups
// +kubebuilder:resource:scope="Cluster",shortName="sg",path="security-groups",singular="security-group"
// +kubebuilder:subresource:status
type SecurityGroup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   SecurityGroupSpec   `json:"spec"`
	Status SecurityGroupStatus `json:"status"`
}

type SecurityGroupSpec struct {
	// Ingress traffic rules for the security group
	IngressRules []SecurityGroupRule `json:"ingressRules,omitempty"`
	// Egress traffic rules for the security group
	EgressRules []SecurityGroupRule `json:"egressRules,omitempty"`
	// Allow traffic between pods in the same security group
	AllowSameGroupTraffic bool `json:"allowSameGroupTraffic,omitempty"`
	// ACL tier to which the rules are added
	Tier int `json:"tier,omitempty"`
}

type SecurityGroupRule struct {
	// IP version (IPv4 or IPv6)
	IPVersion string `json:"ipVersion"`
	// Protocol (tcp, udp, icmp, or all)
	Protocol SgProtocol `json:"protocol,omitempty"`
	// Rule priority (1-16384)
	Priority int `json:"priority,omitempty"`
	// Type of remote (address, cidr, or securityGroup)
	RemoteType SgRemoteType `json:"remoteType"`
	// Remote address or CIDR
	RemoteAddress string `json:"remoteAddress,omitempty"`
	// Remote security group name
	RemoteSecurityGroup string `json:"remoteSecurityGroup,omitempty"`
	// Start of port range (1-65535)
	PortRangeMin int `json:"portRangeMin,omitempty"`
	// End of port range (1-65535)
	PortRangeMax int `json:"portRangeMax,omitempty"`
	// Local address or CIDR
	LocalAddress string `json:"localAddress,omitempty"`
	// Start of source port range (1-65535)
	SourcePortRangeMin int `json:"sourcePortRangeMin,omitempty"`
	// End of source port range (1-65535)
	SourcePortRangeMax int `json:"sourcePortRangeMax,omitempty"`
	// Policy action (allow, pass or deny)
	Policy SgPolicy `json:"policy"`
}

type SecurityGroupStatus struct {
	// OVN port group name
	PortGroup string `json:"portGroup"`
	// Current allow same group traffic setting
	AllowSameGroupTraffic bool `json:"allowSameGroupTraffic"`
	// MD5 hash of ingress rules
	IngressMd5 string `json:"ingressMd5"`
	// MD5 hash of egress rules
	EgressMd5 string `json:"egressMd5"`
	// Last ingress sync success status
	IngressLastSyncSuccess bool `json:"ingressLastSyncSuccess"`
	// Last egress sync success status
	EgressLastSyncSuccess bool `json:"egressLastSyncSuccess"`
}

func (s *SecurityGroupStatus) Bytes() ([]byte, error) {
	bytes, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	newStr := fmt.Sprintf(`{"status": %s}`, string(bytes))
	klog.V(5).Info("status body", newStr)
	return []byte(newStr), nil
}
