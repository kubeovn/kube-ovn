package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type IPList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []IP `json:"items"`
}

// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient:nonNamespaced
// +kubebuilder:resource:scope="Cluster",shortName="ip",path="ips"
// +kubebuilder:printcolumn:name="V4IP",type="string",JSONPath=".spec.v4IpAddress"
// +kubebuilder:printcolumn:name="V6IP",type="string",JSONPath=".spec.v6IpAddress"
// +kubebuilder:printcolumn:name="Mac",type="string",JSONPath=".spec.macAddress"
// +kubebuilder:printcolumn:name="Node",type="string",JSONPath=".spec.nodeName"
// +kubebuilder:printcolumn:name="Subnet",type="string",JSONPath=".spec.subnet"
type IP struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec IPSpec `json:"spec"`
}

type IPSpec struct {
	// Pod name that this IP belongs to
	PodName string `json:"podName"`
	// Namespace of the pod
	Namespace string `json:"namespace"`
	// Primary subnet name for the IP. This field is immutable after creation.
	Subnet string `json:"subnet"`
	// Additional attached subnets
	AttachSubnets []string `json:"attachSubnets"`
	// Node name where the pod resides
	NodeName string `json:"nodeName"`
	// IP address (deprecated, use v4IpAddress or v6IpAddress)
	IPAddress string `json:"ipAddress"`
	// IPv4 address
	V4IPAddress string `json:"v4IpAddress"`
	// IPv6 address
	V6IPAddress string `json:"v6IpAddress"`
	// Additional IP addresses from attached subnets
	AttachIPs []string `json:"attachIps"`
	// MAC address for the primary IP
	MacAddress string `json:"macAddress"`
	// MAC addresses for attached IPs
	AttachMacs []string `json:"attachMacs"`
	// Container ID
	ContainerID string `json:"containerID"`
	// Pod type (e.g., pod, vm)
	PodType string `json:"podType"`
}
