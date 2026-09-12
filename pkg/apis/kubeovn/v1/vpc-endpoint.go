package v1

import (
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type VpcEndpointServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []VpcEndpointService `json:"items"`
}

// VpcEndpointService publishes a Kubernetes Service from a provider VPC onto a
// unique IPv4 transit address so consumer VPCs with overlapping CIDRs can reach
// it. NAT and load-balancing run in privileged dual-NIC stitcher pods via
// iptables (IPv4 only); Multus is required for the transit NIC.
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient:nonNamespaced
// +resourceName=vpc-endpoint-services
// +kubebuilder:resource:scope="Cluster",shortName="ves",path="vpc-endpoint-services",singular="vpc-endpoint-service"
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Vpc",type="string",JSONPath=".spec.vpc"
// +kubebuilder:printcolumn:name="Service",type="string",JSONPath=".spec.service"
// +kubebuilder:printcolumn:name="Namespace",type="string",JSONPath=".spec.namespace"
// +kubebuilder:printcolumn:name="TransitVIP",type="string",JSONPath=".status.transitVIP"
// +kubebuilder:printcolumn:name="Ready",type="boolean",JSONPath=".status.ready"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type VpcEndpointService struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   VpcEndpointServiceSpec   `json:"spec"`
	Status VpcEndpointServiceStatus `json:"status"`
}

type VpcEndpointServiceSpec struct {
	// Provider VPC that owns the backend Service.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="vpc is immutable"
	Vpc string `json:"vpc"`
	// Namespace of the provider Kubernetes Service.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="namespace is immutable"
	Namespace string `json:"namespace"`
	// Name of the provider Kubernetes Service.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="service is immutable"
	Service string `json:"service"`
	// Consumer VPCs allowed to attach. Empty means any VPC may consume the service.
	AllowedVpcs []string `json:"allowedVpcs,omitempty"`
}

type VpcEndpointServiceStatus struct {
	// Unique IPv4 address allocated from the transit subnet and owned by the
	// provider stitcher Multus interface. Consumer stitchers DNAT LocalVIP to
	// this address; provider stitchers DNAT it to Service backends via iptables.
	TransitVIP string `json:"transitVIP,omitempty"`
	// Deprecated: unused by the stitcher datapath; retained for API compatibility.
	Mac string `json:"mac,omitempty"`
	// Human-readable summary of published service ports.
	Ports string `json:"ports,omitempty"`
	// Indicates whether the endpoint service is ready to be consumed.
	Ready bool `json:"ready"`
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

func (s *VpcEndpointServiceStatus) Bytes() ([]byte, error) {
	bytes, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	newStr := fmt.Sprintf(`{"status": %s}`, string(bytes))
	klog.V(5).Info("status body", newStr)
	return []byte(newStr), nil
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type VpcEndpointList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []VpcEndpoint `json:"items"`
}

// VpcEndpoint allocates a local IPv4 VIP in a consumer subnet and DNATs it to a
// VpcEndpointService transit VIP via a dual-NIC stitcher pod, with SNAT on the
// transit path so overlapping tenant CIDRs stay isolated. IPv6 is not supported.
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient:nonNamespaced
// +resourceName=vpc-endpoints
// +kubebuilder:resource:scope="Cluster",shortName="vep",path="vpc-endpoints",singular="vpc-endpoint"
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Vpc",type="string",JSONPath=".spec.vpc"
// +kubebuilder:printcolumn:name="Subnet",type="string",JSONPath=".spec.subnet"
// +kubebuilder:printcolumn:name="EndpointService",type="string",JSONPath=".spec.endpointService"
// +kubebuilder:printcolumn:name="LocalVIP",type="string",JSONPath=".status.localVIP"
// +kubebuilder:printcolumn:name="TransitVIP",type="string",JSONPath=".status.transitVIP"
// +kubebuilder:printcolumn:name="Ready",type="boolean",JSONPath=".status.ready"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type VpcEndpoint struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   VpcEndpointSpec   `json:"spec"`
	Status VpcEndpointStatus `json:"status"`
}

type VpcEndpointSpec struct {
	// Consumer VPC.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="vpc is immutable"
	Vpc string `json:"vpc"`
	// Consumer subnet used to allocate the local VIP.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="subnet is immutable"
	Subnet string `json:"subnet"`
	// Name of the cluster-scoped VpcEndpointService to consume.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="endpointService is immutable"
	EndpointService string `json:"endpointService"`
	// Optional static IPv4 address for the local VIP. Allocated from the subnet when empty.
	// IPv6 addresses are rejected; the stitcher datapath is IPv4-only.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="ip is immutable"
	IP string `json:"ip,omitempty"`
}

type VpcEndpointStatus struct {
	// IPv4 address in the consumer subnet that applications dial.
	LocalVIP string `json:"localVIP,omitempty"`
	// Provider transit VIP this endpoint maps to.
	TransitVIP string `json:"transitVIP,omitempty"`
	// IPv4 address of the consumer stitcher transit-leg interface.
	// Provider backends do not see this address; they see the provider stitcher
	// VPC-leg IP after MASQUERADE on the provider stitcher.
	SnatIP string `json:"snatIP,omitempty"`
	// Indicates whether the endpoint is ready.
	Ready bool `json:"ready"`
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

func (s *VpcEndpointStatus) Bytes() ([]byte, error) {
	bytes, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	newStr := fmt.Sprintf(`{"status": %s}`, string(bytes))
	klog.V(5).Info("status body", newStr)
	return []byte(newStr), nil
}
