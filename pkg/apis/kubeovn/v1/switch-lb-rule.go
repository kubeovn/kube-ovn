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
	// HealthCheck probes backends over HTTP on a port that may differ from the
	// traffic port. A backend is used only when the probe returns HTTP 200;
	// otherwise it is treated as unhealthy and excluded from load balancing.
	// +optional
	HealthCheck *SwitchLBRuleHealthCheck `json:"healthCheck,omitempty"`
}

// SwitchLBRuleHealthCheck configures HTTP health checks for SwitchLBRule backends.
type SwitchLBRuleHealthCheck struct {
	// Port is the HTTP port probed on each backend. It may differ from the traffic port.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port"`
	// Path is the HTTP path to request. Defaults to "/".
	// Only an HTTP 200 OK response is treated as healthy.
	// +optional
	Path string `json:"path,omitempty"`
	// IntervalSeconds is the probe interval. Defaults to 5.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=5
	IntervalSeconds int32 `json:"intervalSeconds,omitempty"`
	// TimeoutSeconds is the probe timeout. Defaults to 2.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=2
	TimeoutSeconds int32 `json:"timeoutSeconds,omitempty"`
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
