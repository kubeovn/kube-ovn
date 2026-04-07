package v1

import (
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type OvnFipList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []OvnFip `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient:nonNamespaced
// +resourceName=ovn-fips
// +kubebuilder:resource:scope="Cluster",shortName="ofip",path="ovn-fips",singular="ovn-fip"
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Vpc",type="string",JSONPath=".status.vpc"
// +kubebuilder:printcolumn:name="Type",type="string",JSONPath=".spec.type"
// +kubebuilder:printcolumn:name="V4Eip",type="string",JSONPath=".status.v4Eip"
// +kubebuilder:printcolumn:name="V6Eip",type="string",JSONPath=".status.v6Eip"
// +kubebuilder:printcolumn:name="V4Ip",type="string",JSONPath=".status.v4Ip"
// +kubebuilder:printcolumn:name="V6Ip",type="string",JSONPath=".status.v6Ip"
// +kubebuilder:printcolumn:name="Ready",type="boolean",JSONPath=".status.ready"
// +kubebuilder:printcolumn:name="IpType",type="string",JSONPath=".spec.ipType"
// +kubebuilder:printcolumn:name="IpName",type="string",JSONPath=".spec.ipName"
type OvnFip struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   OvnFipSpec   `json:"spec"`
	Status OvnFipStatus `json:"status"`
}
type OvnFipSpec struct {
	// OVN EIP name to use for floating IP
	OvnEip string `json:"ovnEip"`
	// IP type (e.g., ipv4, ipv6, dual)
	IPType string `json:"ipType"` // vip, ip
	// IP resource name
	IPName string `json:"ipName"` // vip, ip crd name
	// VPC name. This field is immutable after creation.
	Vpc string `json:"vpc"`
	// IPv4 address for the floating IP
	V4Ip string `json:"v4Ip"`
	// IPv6 address for the floating IP
	V6Ip string `json:"v6Ip"`
	// FIP type
	Type string `json:"type"` // distributed, centralized
}

type OvnFipStatus struct {
	// Conditions represents the latest state of the object
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	// Indicates whether the FIP rule is ready
	Ready bool `json:"ready" patchStrategy:"merge"`
	// VPC name where the FIP is configured
	Vpc string `json:"vpc" patchStrategy:"merge"`
	// V4Eip is the IPv4 EIP address
	V4Eip string `json:"v4Eip" patchStrategy:"merge"`
	// V6Eip is the IPv6 EIP address
	V6Eip string `json:"v6Eip" patchStrategy:"merge"`
	// V4Ip is the IPv4 address of the FIP
	V4Ip string `json:"v4Ip" patchStrategy:"merge"`
	// V6Ip is the IPv6 address of the FIP
	V6Ip string `json:"v6Ip" patchStrategy:"merge"`
	// MacAddress of the FIP
	MacAddress string `json:"macAddress" patchStrategy:"merge"`
}

func (s *OvnFipStatus) Bytes() ([]byte, error) {
	bytes, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	newStr := fmt.Sprintf(`{"status": %s}`, string(bytes))
	klog.V(5).Info("status body", newStr)
	return []byte(newStr), nil
}
