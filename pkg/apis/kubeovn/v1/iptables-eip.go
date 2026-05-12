package v1

import (
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type IptablesEIPList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []IptablesEIP `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient:nonNamespaced
// +resourceName=iptables-eips
// +kubebuilder:resource:scope="Cluster",shortName="eip",path="iptables-eips",singular="iptables-eip"
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Namespace",type="string",JSONPath=".spec.namespace"
// +kubebuilder:printcolumn:name="ExternalSubnet",type="string",JSONPath=".spec.externalSubnet"
// +kubebuilder:printcolumn:name="IP",type="string",JSONPath=".status.ip"
// +kubebuilder:printcolumn:name="Mac",type="string",JSONPath=".spec.macAddress"
// +kubebuilder:printcolumn:name="Nat",type="string",JSONPath=".status.nat"
// +kubebuilder:printcolumn:name="NatGwDp",type="string",JSONPath=".spec.natGwDp"
// +kubebuilder:printcolumn:name="Ready",type="boolean",JSONPath=".status.ready"
type IptablesEIP struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   IptablesEIPSpec   `json:"spec"`
	Status IptablesEIPStatus `json:"status"`
}
type IptablesEIPSpec struct {
	// IPv4 address for the EIP
	V4ip string `json:"v4ip"`
	// IPv6 address for the EIP
	V6ip string `json:"v6ip"`
	// MAC address for the EIP
	MacAddress string `json:"macAddress"`
	// NAT gateway datapath where the EIP is assigned.
	NatGwDp string `json:"natGwDp"`
	// QoS policy name to apply to the EIP
	QoSPolicy string `json:"qosPolicy"`
	// External subnet name. This field is immutable after creation.
	ExternalSubnet string `json:"externalSubnet"`
	// Namespace where the NAT gateway StatefulSet/Pod for this EIP resides.
	// If empty, defaults to the kube-ovn controller's own namespace.
	// +kubebuilder:validation:Optional
	Namespace string `json:"namespace,omitempty"`
}

type IptablesEIPStatus struct {
	// Conditions represents the latest state of the object
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	// Indicates whether the EIP is ready
	Ready bool `json:"ready" patchStrategy:"merge"`
	// IPv4 address of the EIP
	IP string `json:"ip" patchStrategy:"merge"`
	// NAT type (snat or dnat)
	Nat string `json:"nat" patchStrategy:"merge"`
	// Redo operation status
	Redo string `json:"redo" patchStrategy:"merge"`
	// QoS policy name
	QoSPolicy string `json:"qosPolicy" patchStrategy:"merge"`
}

func (s *IptablesEIPStatus) Bytes() ([]byte, error) {
	bytes, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	newStr := fmt.Sprintf(`{"status": %s}`, string(bytes))
	klog.V(5).Info("status body", newStr)
	return []byte(newStr), nil
}
