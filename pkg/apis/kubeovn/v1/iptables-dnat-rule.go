package v1

import (
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type IptablesDnatRuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []IptablesDnatRule `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient:nonNamespaced
// +resourceName=iptables-dnat-rules
// +kubebuilder:resource:scope="Cluster",shortName="dnat",path="iptables-dnat-rules",singular="iptables-dnat-rule"
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Eip",type="string",JSONPath=".spec.eip"
// +kubebuilder:printcolumn:name="Protocol",type="string",JSONPath=".spec.protocol"
// +kubebuilder:printcolumn:name="V4ip",type="string",JSONPath=".status.v4ip"
// +kubebuilder:printcolumn:name="V6ip",type="string",JSONPath=".status.v6ip"
// +kubebuilder:printcolumn:name="InternalIp",type="string",JSONPath=".spec.internalIp"
// +kubebuilder:printcolumn:name="ExternalPort",type="string",JSONPath=".spec.externalPort"
// +kubebuilder:printcolumn:name="InternalPort",type="string",JSONPath=".spec.internalPort"
// +kubebuilder:printcolumn:name="Type",type="string",JSONPath=".spec.type"
// +kubebuilder:printcolumn:name="NatGwDp",type="string",JSONPath=".status.natGwDp"
// +kubebuilder:printcolumn:name="Ready",type="boolean",JSONPath=".status.ready"
type IptablesDnatRule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   IptablesDnatRuleSpec   `json:"spec"`
	Status IptablesDnatRuleStatus `json:"status"`
}

const (
	// DnatRuleTypeExclusive means the EIP:Port is exclusively used by this DNAT rule.
	// Only one internal IP:Port can be mapped to a given EIP:Port identity (eip + externalPort + protocol).
	// This is the original (default) behavior: uses a single iptables DNAT rule for 1:1 port forwarding.
	// Creating another DNAT rule with the same identity is rejected.
	DnatRuleTypeExclusive = "exclusive"

	// DnatRuleTypeShare means multiple DNAT rules can share the same EIP:Port.
	// Different internal IP:Port backends are allowed to coexist under the same identity,
	// enabling load-balancing across backends. Implemented via nftables numgen random
	// map-based DNAT: new connections are distributed randomly across backends and then
	// pinned by conntrack (connection-level balancing, no client-IP affinity).
	// IPv4 only: the nft rules use `ip daddr`/`ip saddr`, so share requires an EIP with an
	// IPv4 address; the webhook rejects share on an IPv6-only EIP and the controller requeues
	// while the EIP is still being allocated (v4 not yet assigned).
	DnatRuleTypeShare = "share"
)

type IptablesDnatRuleSpec struct {
	// EIP name for DNAT rule
	EIP string `json:"eip"`
	// External port number
	ExternalPort string `json:"externalPort"`
	// Protocol type (TCP or UDP)
	Protocol string `json:"protocol,omitempty"`
	// Internal IP address to forward traffic to
	InternalIP string `json:"internalIp"`
	// Internal port number to forward traffic to
	InternalPort string `json:"internalPort"`
	// Type of the DNAT rule, controls whether the EIP:Port identity can be shared
	// by multiple internal backends:
	// - "exclusive" (default): The EIP:Port is exclusively owned by this single DNAT rule.
	//   Uses iptables DNAT for 1:1 port forwarding. This was the only mode before the
	//   nft LB feature was introduced; any duplicate identity is rejected.
	// - "share": Multiple DNAT rules may share the same EIP:Port identity, each
	//   contributing a different internal IP:Port as a backend. Traffic is distributed
	//   across backends using nftables numgen random map-based DNAT: each new connection
	//   picks a backend at random and is then pinned by conntrack (connection-level
	//   balancing, no client-IP affinity).
	// +kubebuilder:validation:Enum=exclusive;share
	// +kubebuilder:default=exclusive
	// +optional
	Type string `json:"type,omitempty"`
}

type IptablesDnatRuleStatus struct {
	// Conditions represents the latest state of the object
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	// Indicates whether the DNAT rule is ready
	Ready bool `json:"ready" patchStrategy:"merge"`
	// V4ip is the IPv4 address of the DNAT rule
	V4ip string `json:"v4ip" patchStrategy:"merge"`
	// V6ip is the IPv6 address of the DNAT rule
	V6ip string `json:"v6ip" patchStrategy:"merge"`
	// NatGwDp is the NAT gateway data path
	NatGwDp string `json:"natGwDp" patchStrategy:"merge"`
	// Redo operation status
	Redo string `json:"redo" patchStrategy:"merge"`
	// Protocol type of the DNAT rule
	Protocol string `json:"protocol"  patchStrategy:"merge"`
	// Internal IP address configured in the DNAT rule
	InternalIP string `json:"internalIp"  patchStrategy:"merge"`
	// Internal port configured in the DNAT rule
	InternalPort string `json:"internalPort"  patchStrategy:"merge"`
	// External port configured in the DNAT rule
	ExternalPort string `json:"externalPort"  patchStrategy:"merge"`
}

func (s *IptablesDnatRuleStatus) Bytes() ([]byte, error) {
	bytes, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	newStr := fmt.Sprintf(`{"status": %s}`, string(bytes))
	klog.V(5).Info("status body", newStr)
	return []byte(newStr), nil
}
