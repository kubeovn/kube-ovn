package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// ExternalEndpointConditionReady is True when the logical switch port of the
	// endpoint exists and matches the spec.
	ExternalEndpointConditionReady = "Ready"
	// ExternalEndpointConditionBound is True when an OVN chassis has claimed the
	// logical switch port, i.e. the workload NIC is plugged in and up.
	ExternalEndpointConditionBound = "Bound"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ExternalEndpointList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []ExternalEndpoint `json:"items"`
}

// ExternalEndpoint is a network interface of a workload that runs outside of
// Kubernetes, for example a libvirt VM on a hypervisor that runs ovn-controller
// against the Kube-OVN OVN southbound database. Kube-OVN owns the address and
// the logical switch port of the endpoint; the hypervisor owns the VM and its
// OVS interface, which it binds to the port by setting
// external_ids:iface-id=<status.interfaceID>.
//
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +resourceName=external-endpoints
// +kubebuilder:resource:scope="Namespaced",shortName={"eep"},path="external-endpoints",singular="external-endpoint"
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Subnet",type="string",JSONPath=".spec.subnet"
// +kubebuilder:printcolumn:name="V4IP",type="string",JSONPath=".status.v4ip"
// +kubebuilder:printcolumn:name="V6IP",type="string",JSONPath=".status.v6ip"
// +kubebuilder:printcolumn:name="Mac",type="string",JSONPath=".status.macAddress"
// +kubebuilder:printcolumn:name="Chassis",type="string",JSONPath=".spec.chassis"
// +kubebuilder:printcolumn:name="Up",type="boolean",JSONPath=".status.up"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type ExternalEndpoint struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec ExternalEndpointSpec `json:"spec"`
	// +optional
	Status ExternalEndpointStatus `json:"status"`
}

// +kubebuilder:validation:XValidation:rule="self.subnet == oldSelf.subnet",message="subnet is immutable"
// +kubebuilder:validation:XValidation:rule="has(self.macAddress) == has(oldSelf.macAddress) && (!has(self.macAddress) || self.macAddress == oldSelf.macAddress)",message="macAddress is immutable"
// +kubebuilder:validation:XValidation:rule="has(self.ipAddress) == has(oldSelf.ipAddress) && (!has(self.ipAddress) || self.ipAddress == oldSelf.ipAddress)",message="ipAddress is immutable"
// +kubebuilder:validation:XValidation:rule="has(self.interfaceID) == has(oldSelf.interfaceID) && (!has(self.interfaceID) || self.interfaceID == oldSelf.interfaceID)",message="interfaceID is immutable"
type ExternalEndpointSpec struct {
	// Subnet is the Kube-OVN subnet the endpoint joins.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Subnet string `json:"subnet"`

	// MacAddress of the endpoint NIC. A random address is allocated when empty.
	// +optional
	// +kubebuilder:validation:Pattern=`^([0-9a-fA-F]{2}:){5}[0-9a-fA-F]{2}$`
	MacAddress string `json:"macAddress,omitempty"`

	// IPAddress requests static addresses, comma separated for dual stack.
	// A random address is allocated from the subnet when empty.
	// +optional
	IPAddress string `json:"ipAddress,omitempty"`

	// InterfaceID is the name of the logical switch port and the value the
	// hypervisor sets as external_ids:iface-id on the OVS interface. It must
	// be a UUID so that libvirt can set it natively through
	// <virtualport type='openvswitch'><parameters interfaceid='...'/>.
	// Defaults to metadata.uid.
	// +optional
	// +kubebuilder:validation:Pattern=`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`
	InterfaceID string `json:"interfaceID,omitempty"`

	// Chassis is the OVN chassis name or hostname of the hypervisor that is
	// allowed to bind the port (logical switch port options:requested-chassis).
	// It is required: the endpoint names the one host that owns its binding, the
	// chassis garbage collector keeps that host's chassis, and no other chassis
	// connected to the southbound database can claim the port. Changing it moves
	// the binding to another hypervisor.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Chassis string `json:"chassis"`

	// PortSecurity restricts the traffic sent by the endpoint to its own MAC
	// and IP addresses. Defaults to true.
	// +optional
	PortSecurity *bool `json:"portSecurity,omitempty"`

	// SecurityGroups is the list of Kube-OVN security groups applied to the port.
	// +optional
	// +listType=set
	SecurityGroups []string `json:"securityGroups,omitempty"`
}

type ExternalEndpointStatus struct {
	// InterfaceID is the iface-id the hypervisor must set on the OVS interface.
	// +optional
	InterfaceID string `json:"interfaceID,omitempty"`
	// +optional
	V4IP string `json:"v4ip,omitempty"`
	// +optional
	V6IP string `json:"v6ip,omitempty"`
	// +optional
	MacAddress string `json:"macAddress,omitempty"`
	// Up reports whether an OVN chassis has claimed the port.
	// +optional
	Up bool `json:"up"`

	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// EnablePortSecurity reports whether port security is enabled, which is the default.
func (s *ExternalEndpointSpec) EnablePortSecurity() bool {
	return s.PortSecurity == nil || *s.PortSecurity
}
