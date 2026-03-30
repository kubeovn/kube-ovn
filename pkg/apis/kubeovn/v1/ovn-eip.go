package v1

import (
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type OvnEipList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []OvnEip `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient:nonNamespaced
// +resourceName=ovn-eips
// +kubebuilder:resource:scope="Cluster",shortName="oeip",path="ovn-eips"
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="V4IP",type="string",JSONPath=".status.v4Ip"
// +kubebuilder:printcolumn:name="V6IP",type="string",JSONPath=".status.v6Ip"
// +kubebuilder:printcolumn:name="Mac",type="string",JSONPath=".status.macAddress"
// +kubebuilder:printcolumn:name="Type",type="string",JSONPath=".status.type"
// +kubebuilder:printcolumn:name="Nat",type="string",JSONPath=".status.nat"
// +kubebuilder:printcolumn:name="Ready",type="boolean",JSONPath=".status.ready"
// +kubebuilder:printcolumn:name="ExternalSubnet",type="string",JSONPath=".spec.externalSubnet"
type OvnEip struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   OvnEipSpec   `json:"spec"`
	Status OvnEipStatus `json:"status"`
}
type OvnEipSpec struct {
	// External subnet name. This field is immutable after creation.
	ExternalSubnet string `json:"externalSubnet"`
	// IPv4 address for the EIP
	V4Ip string `json:"v4Ip"`
	// IPv6 address for the EIP
	V6Ip string `json:"v6Ip"`
	// MAC address for the EIP
	MacAddress string `json:"macAddress"`
	// Type of the OVN EIP (e.g., normal, distributed)
	Type string `json:"type"`
	// usage type: lrp, lsp, nat
	// nat: used by nat: dnat, snat, fip
	// lrp: lrp created by vpc enable external, and also could be used by nat
	// lsp: in the case of bfd session between lrp and lsp, the lsp is on the node as ecmp nexthop
}

type OvnEipStatus struct {
	// Conditions represents the latest state of the object
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	// Type of the OVN EIP
	Type string `json:"type" patchStrategy:"merge"`
	// NAT configuration status
	Nat string `json:"nat" patchStrategy:"merge"`
	// Indicates whether the EIP is ready
	Ready bool `json:"ready" patchStrategy:"merge"`
	// IPv4 address assigned to the EIP
	V4Ip string `json:"v4Ip" patchStrategy:"merge"`
	// IPv6 address assigned to the EIP
	V6Ip string `json:"v6Ip" patchStrategy:"merge"`
	// MAC address assigned to the EIP
	MacAddress string `json:"macAddress" patchStrategy:"merge"`
}

func (s *OvnEipStatus) Bytes() ([]byte, error) {
	bytes, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	newStr := fmt.Sprintf(`{"status": %s}`, string(bytes))
	klog.V(5).Info("status body", newStr)
	return []byte(newStr), nil
}
