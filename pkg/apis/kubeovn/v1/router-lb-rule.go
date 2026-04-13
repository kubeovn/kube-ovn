package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type RouterLBRuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []RouterLBRule `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient:nonNamespaced
// +resourceName=router-lb-rules
// +kubebuilder:resource:scope="Cluster",shortName="rlr",path="router-lb-rules",singular="router-lb-rule"
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="ovnEip",type="string",JSONPath=".spec.ovnEip"
// +kubebuilder:printcolumn:name="vpc",type="string",JSONPath=".spec.vpc"
// +kubebuilder:printcolumn:name="port(s)",type="string",JSONPath=".status.ports"
// +kubebuilder:printcolumn:name="service",type="string",JSONPath=".status.service"
// +kubebuilder:printcolumn:name="age",type="date",JSONPath=".metadata.creationTimestamp"
type RouterLBRule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   RouterLBRuleSpec   `json:"spec"`
	Status RouterLBRuleStatus `json:"status"`
}

type RouterLBRuleSpec struct {
	OvnEip          string             `json:"ovnEip"`
	Vpc             string             `json:"vpc"`
	Namespace       string             `json:"namespace"`
	Selector        []string           `json:"selector"`
	Endpoints       []string           `json:"endpoints"`
	SessionAffinity string             `json:"sessionAffinity,omitempty"`
	Ports           []RouterLBRulePort `json:"ports"`
}

type RouterLBRulePort struct {
	Name       string `json:"name"`
	Port       int32  `json:"port"`
	TargetPort int32  `json:"targetPort,omitempty"`
	Protocol   string `json:"protocol"`
}

type RouterLBRuleStatus struct {
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	Ports   string `json:"ports" patchStrategy:"merge"`
	Service string `json:"service" patchStrategy:"merge"`
}
