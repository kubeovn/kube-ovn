package v1

import (
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type OvnSnatRuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []OvnSnatRule `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient:nonNamespaced
// +resourceName=ovn-snat-rules
// +kubebuilder:resource:scope="Cluster",shortName="osnat",path="ovn-snat-rules"
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Vpc",type="string",JSONPath=".status.vpc"
// +kubebuilder:printcolumn:name="V4Eip",type="string",JSONPath=".status.v4Eip"
// +kubebuilder:printcolumn:name="V6Eip",type="string",JSONPath=".status.v6Eip"
// +kubebuilder:printcolumn:name="V4IpCidr",type="string",JSONPath=".status.v4IpCidr"
// +kubebuilder:printcolumn:name="V6IpCidr",type="string",JSONPath=".status.v6IpCidr"
// +kubebuilder:printcolumn:name="Ready",type="boolean",JSONPath=".status.ready"
type OvnSnatRule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   OvnSnatRuleSpec   `json:"spec"`
	Status OvnSnatRuleStatus `json:"status"`
}

type OvnSnatRuleSpec struct {
	// OVN EIP name for SNAT rule
	OvnEip string `json:"ovnEip"`
	// VPC subnet name for SNAT
	VpcSubnet string `json:"vpcSubnet"`
	// IP resource name
	IPName string `json:"ipName"`
	// VPC name. This field is immutable after creation.
	Vpc string `json:"vpc"`
	// IPv4 CIDR for SNAT
	V4IpCidr string `json:"v4IpCidr"` // subnet cidr or pod ip address
	// IPv6 CIDR for SNAT
	V6IpCidr string `json:"v6IpCidr"` // subnet cidr or pod ip address
}

type OvnSnatRuleStatus struct {
	// Conditions represents the latest state of the object
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	// Indicates whether the SNAT rule is ready
	Ready bool `json:"ready" patchStrategy:"merge"`
	// VPC name where the SNAT rule is configured
	Vpc string `json:"vpc" patchStrategy:"merge"`
	// V4Eip is the IPv4 EIP address
	V4Eip string `json:"v4Eip" patchStrategy:"merge"`
	// V6Eip is the IPv6 EIP address
	V6Eip string `json:"v6Eip" patchStrategy:"merge"`
	// V4IpCidr is the IPv4 CIDR of the SNAT rule
	V4IpCidr string `json:"v4IpCidr" patchStrategy:"merge"`
	// V6IpCidr is the IPv6 CIDR of the SNAT rule
	V6IpCidr string `json:"v6IpCidr" patchStrategy:"merge"`
}

func (s *OvnSnatRuleStatus) Bytes() ([]byte, error) {
	bytes, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	newStr := fmt.Sprintf(`{"status": %s}`, string(bytes))
	klog.V(5).Info("status body", newStr)
	return []byte(newStr), nil
}
