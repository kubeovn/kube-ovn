package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type VpcDnsList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []VpcDns `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient:nonNamespaced
// +resourceName=vpc-dnses
// +kubebuilder:resource:scope="Cluster",shortName="vpc-dns",path="vpc-dnses",singular="vpc-dns"
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Active",type="boolean",JSONPath=".status.active"
// +kubebuilder:printcolumn:name="Vpc",type="string",JSONPath=".spec.vpc"
// +kubebuilder:printcolumn:name="Subnet",type="string",JSONPath=".spec.subnet"
// +kubebuilder:printcolumn:name="Corefile",type="string",JSONPath=".spec.corefile"
type VpcDns struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   VpcDNSSpec   `json:"spec"`
	Status VpcDNSStatus `json:"status"`
}

type VpcDNSSpec struct {
	// Number of DNS server replicas (1-3)
	Replicas int32 `json:"replicas,omitempty"`
	// VPC name for the DNS service. This field is immutable after creation.
	Vpc string `json:"vpc"`
	// Subnet name for the DNS service. This field is immutable after creation.
	Subnet string `json:"subnet"`
	// CoreDNS corefile configuration
	// +kubebuilder:default=vpc-dns-corefile
	Corefile string `json:"corefile,omitempty"`
}

type VpcDNSStatus struct {
	// Conditions represent the latest state of the VPC DNS
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	// Whether the VPC DNS service is active
	Active bool `json:"active" patchStrategy:"merge"`
}
