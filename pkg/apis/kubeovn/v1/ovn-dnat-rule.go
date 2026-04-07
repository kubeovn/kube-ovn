package v1

import (
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type OvnDnatRuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []OvnDnatRule `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient:nonNamespaced
// +resourceName=ovn-dnat-rules
// +kubebuilder:resource:scope="Cluster",shortName="odnat",path="ovn-dnat-rules",singular="ovn-dnat-rule"
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Vpc",type="string",JSONPath=".status.vpc"
// +kubebuilder:printcolumn:name="Eip",type="string",JSONPath=".spec.ovnEip"
// +kubebuilder:printcolumn:name="Protocol",type="string",JSONPath=".status.protocol"
// +kubebuilder:printcolumn:name="V4Eip",type="string",JSONPath=".status.v4Eip"
// +kubebuilder:printcolumn:name="V6Eip",type="string",JSONPath=".status.v6Eip"
// +kubebuilder:printcolumn:name="V4Ip",type="string",JSONPath=".status.v4Ip"
// +kubebuilder:printcolumn:name="V6Ip",type="string",JSONPath=".status.v6Ip"
// +kubebuilder:printcolumn:name="InternalPort",type="string",JSONPath=".status.internalPort"
// +kubebuilder:printcolumn:name="ExternalPort",type="string",JSONPath=".status.externalPort"
// +kubebuilder:printcolumn:name="IpName",type="string",JSONPath=".spec.ipName"
// +kubebuilder:printcolumn:name="Ready",type="boolean",JSONPath=".status.ready"
type OvnDnatRule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   OvnDnatRuleSpec   `json:"spec"`
	Status OvnDnatRuleStatus `json:"status"`
}

type OvnDnatRuleSpec struct {
	// OVN EIP name for DNAT rule
	OvnEip string `json:"ovnEip"`
	// IP type (e.g., ipv4, ipv6, dual)
	IPType string `json:"ipType"` // vip, ip
	// IP resource name
	IPName string `json:"ipName"` // vip, ip crd name
	// Internal port number to forward traffic to
	InternalPort string `json:"internalPort"`
	// External port number
	ExternalPort string `json:"externalPort"`
	// Protocol type (TCP or UDP)
	Protocol string `json:"protocol,omitempty"`
	// VPC name. This field is immutable after creation.
	Vpc string `json:"vpc"`
	// IPv4 address for DNAT
	V4Ip string `json:"v4Ip"`
	// IPv6 address for DNAT
	V6Ip string `json:"v6Ip"`
}

type OvnDnatRuleStatus struct {
	// VPC name where the DNAT rule is configured
	// +optional
	// +patchStrategy=merge
	Vpc string `json:"vpc" patchStrategy:"merge"`
	// V4Eip is the IPv4 EIP address
	V4Eip string `json:"v4Eip" patchStrategy:"merge"`
	// V6Eip is the IPv6 EIP address
	V6Eip string `json:"v6Eip" patchStrategy:"merge"`
	// ExternalPort is the external port of the DNAT rule
	ExternalPort string `json:"externalPort"`
	// V4Ip is the IPv4 address of the DNAT rule
	V4Ip string `json:"v4Ip" patchStrategy:"merge"`
	// V6Ip is the IPv6 address of the DNAT rule
	V6Ip string `json:"v6Ip" patchStrategy:"merge"`
	// InternalPort is the internal port of the DNAT rule
	InternalPort string `json:"internalPort"`
	// Protocol of the DNAT rule
	Protocol string `json:"protocol,omitempty"`
	// IP resource name
	IPName string `json:"ipName"`
	// Indicates whether the DNAT rule is ready
	Ready bool `json:"ready" patchStrategy:"merge"`

	// Conditions represents the latest state of the object
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

func (s *OvnDnatRuleStatus) Bytes() ([]byte, error) {
	bytes, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	newStr := fmt.Sprintf(`{"status": %s}`, string(bytes))
	klog.V(5).Info("status body", newStr)
	return []byte(newStr), nil
}
