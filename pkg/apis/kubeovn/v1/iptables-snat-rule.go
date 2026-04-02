package v1

import (
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type IptablesSnatRuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []IptablesSnatRule `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient:nonNamespaced
// +resourceName=iptables-snat-rules
// +kubebuilder:resource:scope="Cluster",shortName="snat",path="iptables-snat-rules"
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="EIP",type="string",JSONPath=".spec.eip"
// +kubebuilder:printcolumn:name="V4ip",type="string",JSONPath=".status.v4ip"
// +kubebuilder:printcolumn:name="V6ip",type="string",JSONPath=".status.v6ip"
// +kubebuilder:printcolumn:name="InternalCIDR",type="string",JSONPath=".spec.internalCIDR"
// +kubebuilder:printcolumn:name="NatGwDp",type="string",JSONPath=".status.natGwDp"
// +kubebuilder:printcolumn:name="Ready",type="boolean",JSONPath=".status.ready"
type IptablesSnatRule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   IptablesSnatRuleSpec   `json:"spec"`
	Status IptablesSnatRuleStatus `json:"status"`
}

type IptablesSnatRuleSpec struct {
	// EIP name for SNAT rule
	EIP string `json:"eip"`
	// Internal CIDR to be translated via SNAT
	InternalCIDR string `json:"internalCIDR"`
}

type IptablesSnatRuleStatus struct {
	// Conditions represents the latest state of the object
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	// Indicates whether the SNAT rule is ready
	Ready bool `json:"ready" patchStrategy:"merge"`
	// V4ip is the IPv4 address of the SNAT rule
	V4ip string `json:"v4ip" patchStrategy:"merge"`
	// V6ip is the IPv6 address of the SNAT rule
	V6ip string `json:"v6ip" patchStrategy:"merge"`
	// NatGwDp is the NAT gateway data path
	NatGwDp string `json:"natGwDp" patchStrategy:"merge"`
	// Redo operation status
	Redo string `json:"redo" patchStrategy:"merge"`
	// InternalCIDR is the internal CIDR of the SNAT rule
	InternalCIDR string `json:"internalCIDR" patchStrategy:"merge"`
}

func (s *IptablesSnatRuleStatus) Bytes() ([]byte, error) {
	bytes, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	newStr := fmt.Sprintf(`{"status": %s}`, string(bytes))
	klog.V(5).Info("status body", newStr)
	return []byte(newStr), nil
}
