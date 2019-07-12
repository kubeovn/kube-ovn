package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ProtocolIPv4 = "IPv4"
	ProtocolIPv6 = "IPv6"
	PrtotcolDual = "Dual"

	GWDistributedType = "distributed"
	GWCentralizedType = "centralized"
)

// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient:nonNamespaced

type IP struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec IPSpec `json:"spec"`
}

type IPSpec struct {
	PodName     string `json:"podName"`
	Namespace   string `json:"namespace"`
	NodeName    string `json:"nodeName"`
	IPAddress   string `json:"ipAddress"`
	MacAddress  string `json:"macAddress"`
	ContainerID string `json:"containerID"`
}

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

type Subnet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec SubnetSpec `json:"spec"`
}

type SubnetSpec struct {
	Default    bool     `json:"default"`
	Protocol   string   `json:"protocol"`
	Namespaces []string `json:"namespaces,omitempty"`
	CIDRBlock  string   `json:"cidrBlock"`
	Gateway    string   `json:"gateway"`
	ExcludeIps []string `json:"excludeIps,omitempty"`

	GatewayType string `json:"gatewayType"`
	GatewayNode string `json:"gatewayNode"`
	NatOutgoing bool   `json:"natOutgoing"`

	Private      bool     `json:"private"`
	AllowSubnets []string `json:"allowSubnets,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type SubnetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Subnet `json:"items"`
}
