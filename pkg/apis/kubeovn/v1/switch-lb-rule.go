package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type SwitchLBRuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []SwitchLBRule `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient:nonNamespaced
// +resourceName=switch-lb-rules
// +kubebuilder:resource:scope="Cluster",shortName="slr",path="switch-lb-rules",singular="switch-lb-rule"
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="vip",type="string",JSONPath=".spec.vip"
// +kubebuilder:printcolumn:name="port(s)",type="string",JSONPath=".status.ports"
// +kubebuilder:printcolumn:name="service",type="string",JSONPath=".status.service"
// +kubebuilder:printcolumn:name="age",type="date",JSONPath=".metadata.creationTimestamp"
type SwitchLBRule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   SwitchLBRuleSpec   `json:"spec"`
	Status SwitchLBRuleStatus `json:"status"`
}

type SwitchLBRuleSpec struct {
	Vip             string             `json:"vip"`
	Namespace       string             `json:"namespace"`
	Selector        []string           `json:"selector"`
	Endpoints       []string           `json:"endpoints"`
	SessionAffinity string             `json:"sessionAffinity,omitempty"`
	Ports           []SwitchLBRulePort `json:"ports"`
}

type SwitchLBRulePort struct {
	// Port name
	Name string `json:"name"`
	// Service port number (1-65535)
	Port int32 `json:"port"`
	// Target port number (1-65535)
	TargetPort int32 `json:"targetPort,omitempty"`
	// Protocol (TCP or UDP)
	Protocol string `json:"protocol"`
}

type SwitchLBRuleStatus struct {
	// Conditions represents the latest state of the object
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	// Configured ports
	Ports string `json:"ports" patchStrategy:"merge"`
	// Associated service name
	Service string `json:"service" patchStrategy:"merge"`
}
