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
// unique transit address so consumer VPCs with overlapping CIDRs can reach it.
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
	// Unique IP allocated from the transit subnet and used as the provider OVN LB VIP.
	TransitVIP string `json:"transitVIP,omitempty"`
	// MAC used by the transit logical switch port that answers ARP for TransitVIP.
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

// VpcEndpoint allocates a local VIP in a consumer subnet and DNATs it to a
// VpcEndpointService transit VIP, with SNAT so overlapping tenant CIDRs stay isolated.
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
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="ip is immutable"
	IP string `json:"ip,omitempty"`
}

type VpcEndpointStatus struct {
	// IP in the consumer subnet that applications dial.
	LocalVIP string `json:"localVIP,omitempty"`
	// Provider transit VIP this endpoint maps to.
	TransitVIP string `json:"transitVIP,omitempty"`
	// Shared SNAT IP of the consumer VPC on the transit subnet.
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
