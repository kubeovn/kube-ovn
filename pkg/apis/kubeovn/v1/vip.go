package v1

import (
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type VipList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Vip `json:"items"`
}

// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient:nonNamespaced
// +kubebuilder:resource:scope="Cluster",shortName="vip",path="vips"
// +kubebuilder:printcolumn:name="Namespace",type="string",JSONPath=".spec.namespace"
// +kubebuilder:printcolumn:name="V4IP",type="string",JSONPath=".status.v4ip"
// +kubebuilder:printcolumn:name="V6IP",type="string",JSONPath=".status.v6ip"
// +kubebuilder:printcolumn:name="Mac",type="string",JSONPath=".status.mac"
// +kubebuilder:printcolumn:name="Subnet",type="string",JSONPath=".spec.subnet"
// +kubebuilder:printcolumn:name="Type",type="string",JSONPath=".status.type"
type Vip struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   VipSpec   `json:"spec"`
	Status VipStatus `json:"status"`
}

type VipSpec struct {
	// Namespace where the VIP is created. This field is immutable after creation.
	Namespace string `json:"namespace"`
	// Subnet name for the VIP. This field is immutable after creation.
	Subnet string `json:"subnet"`
	// Type of VIP. This field is immutable after creation.
	Type string `json:"type"`
	// usage type: switch lb vip, allowed address pair vip by default
	V4ip string `json:"v4ip"`
	// Specific IPv6 address to use (optional, will be allocated if not specified)
	V6ip string `json:"v6ip"`
	// MAC address for the VIP
	MacAddress string `json:"macAddress"`
	// Pod names to be selected by this VIP
	Selector []string `json:"selector"`
	// Additional subnets to attach
	AttachSubnets []string `json:"attachSubnets"`
}

type VipStatus struct {
	// Conditions represents the latest state of the object
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	// Type of VIP (e.g., Layer2, HealthCheck)
	Type string `json:"type"`
	// Allocated IPv4 address
	V4ip string `json:"v4ip" patchStrategy:"merge"`
	// Allocated IPv6 address
	V6ip string `json:"v6ip" patchStrategy:"merge"`
	// MAC address associated with the VIP
	Mac string `json:"mac" patchStrategy:"merge"`
	// Pod names selected by this VIP
	Selector []string `json:"selector,omitempty"`
}

func (s *VipStatus) Bytes() ([]byte, error) {
	bytes, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	newStr := fmt.Sprintf(`{"status": %s}`, string(bytes))
	klog.V(5).Info("status body", newStr)
	return []byte(newStr), nil
}
