package v1

import (
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type IptablesFIPRuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []IptablesFIPRule `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient:nonNamespaced
// +resourceName=iptables-fip-rules
// +kubebuilder:resource:scope="Cluster",shortName="fip",path="iptables-fip-rules"
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Eip",type="string",JSONPath=".spec.eip"
// +kubebuilder:printcolumn:name="V4ip",type="string",JSONPath=".status.v4ip"
// +kubebuilder:printcolumn:name="InternalIp",type="string",JSONPath=".spec.internalIp"
// +kubebuilder:printcolumn:name="V6ip",type="string",JSONPath=".status.v6ip"
// +kubebuilder:printcolumn:name="Ready",type="boolean",JSONPath=".status.ready"
// +kubebuilder:printcolumn:name="NatGwDp",type="string",JSONPath=".status.natGwDp"
type IptablesFIPRule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   IptablesFIPRuleSpec   `json:"spec"`
	Status IptablesFIPRuleStatus `json:"status"`
}
type IptablesFIPRuleSpec struct {
	// EIP name to use for floating IP
	EIP string `json:"eip"`
	// Internal IP address to map to the floating IP
	InternalIP string `json:"internalIp"`
}

type IptablesFIPRuleStatus struct {
	// Indicates whether the FIP rule is ready
	// +optional
	// +patchStrategy=merge
	Ready bool `json:"ready" patchStrategy:"merge"`
	// IPv4 address of the EIP
	V4ip string `json:"v4ip" patchStrategy:"merge"`
	// IPv6 address of the EIP
	V6ip string `json:"v6ip" patchStrategy:"merge"`
	// NAT gateway datapath where the FIP is configured
	NatGwDp string `json:"natGwDp" patchStrategy:"merge"`
	// Redo operation status
	Redo string `json:"redo" patchStrategy:"merge"`
	// Internal IP address mapped to the FIP
	InternalIP string `json:"internalIp"  patchStrategy:"merge"`

	// Conditions represents the latest state of the object
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

func (s *IptablesFIPRuleStatus) Bytes() ([]byte, error) {
	bytes, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	newStr := fmt.Sprintf(`{"status": %s}`, string(bytes))
	klog.V(5).Info("status body", newStr)
	return []byte(newStr), nil
}
