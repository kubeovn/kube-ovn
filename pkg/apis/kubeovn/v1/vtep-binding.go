package v1

import (
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type VtepBindingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []VtepBinding `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient:nonNamespaced
// +resourceName=vtep-bindings
// +kubebuilder:resource:scope="Cluster",shortName="vtepb",path="vtep-bindings",singular="vtep-binding"
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Subnet",type="string",JSONPath=".spec.subnet"
// +kubebuilder:printcolumn:name="PhysicalSwitch",type="string",JSONPath=".spec.physicalSwitch"
// +kubebuilder:printcolumn:name="VtepLogicalSwitch",type="string",JSONPath=".status.vtepLogicalSwitch"
// +kubebuilder:printcolumn:name="PhysicalPort",type="string",JSONPath=".spec.physicalPort"
// +kubebuilder:printcolumn:name="VLAN",type="integer",JSONPath=".spec.vlanID"
// +kubebuilder:printcolumn:name="Chassis",type="string",JSONPath=".status.chassis"
// +kubebuilder:printcolumn:name="Ready",type="boolean",JSONPath=".status.ready"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// VtepBinding attaches a Kube-OVN Subnet (OVN Logical Switch) to a Hardware VTEP
// physical switch via an OVN Logical Switch Port of type "vtep".
type VtepBinding struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   VtepBindingSpec   `json:"spec"`
	Status VtepBindingStatus `json:"status"`
}

// VtepLogicalSwitchName returns the VTEP-side logical switch name used in
// options:vtep-logical-switch. When Spec.VtepLogicalSwitch is empty it defaults
// to the referenced Subnet name.
func (b *VtepBinding) VtepLogicalSwitchName() string {
	if b.Spec.VtepLogicalSwitch != "" {
		return b.Spec.VtepLogicalSwitch
	}
	return b.Spec.Subnet
}

// VtepBindingConflict returns an error when other occupies the same
// physicalSwitch+vtepLogicalSwitch or physicalSwitch+physicalPort+vlanID as binding.
// Terminating bindings still reserve the tuple until finalizer cleanup removes them
// from the API, so a replacement CR cannot claim shared VTEP state early.
func VtepBindingConflict(binding, other *VtepBinding) error {
	if binding == nil || other == nil || other.Name == binding.Name {
		return nil
	}
	vtepLogicalSwitch := binding.VtepLogicalSwitchName()
	if other.Spec.PhysicalSwitch == binding.Spec.PhysicalSwitch &&
		other.VtepLogicalSwitchName() == vtepLogicalSwitch {
		return fmt.Errorf("vtep binding %s conflicts with %s: physicalSwitch %q and vtepLogicalSwitch %q already in use",
			binding.Name, other.Name, binding.Spec.PhysicalSwitch, vtepLogicalSwitch)
	}
	if other.Spec.PhysicalSwitch == binding.Spec.PhysicalSwitch &&
		other.Spec.PhysicalPort == binding.Spec.PhysicalPort &&
		other.Spec.VlanID == binding.Spec.VlanID {
		return fmt.Errorf("vtep binding %s conflicts with %s: physicalSwitch %q physicalPort %q vlanID %d already in use",
			binding.Name, other.Name, binding.Spec.PhysicalSwitch, binding.Spec.PhysicalPort, binding.Spec.VlanID)
	}
	return nil
}

type VtepBindingSpec struct {
	// Subnet is the Kube-OVN Subnet (OVN Logical Switch) to extend. Immutable.
	// The Subnet must already exist; the validating webhook rejects the CR otherwise.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="spec.subnet is immutable"
	Subnet string `json:"subnet"`

	// PhysicalSwitch is the Hardware VTEP Physical_Switch.name.
	// Maps to options:vtep-physical-switch. Immutable.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="spec.physicalSwitch is immutable"
	PhysicalSwitch string `json:"physicalSwitch"`

	// VtepLogicalSwitch is the Hardware VTEP Logical_Switch.name.
	// Maps to options:vtep-logical-switch. Defaults to subnet when empty. Immutable.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="spec.vtepLogicalSwitch is immutable"
	VtepLogicalSwitch string `json:"vtepLogicalSwitch,omitempty"`

	// PhysicalPort is the Hardware VTEP Physical_Port.name. When --vtep-db-addr
	// is configured, Kube-OVN writes Physical_Port.vlan_bindings for this port.
	// Immutable.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="spec.physicalPort is immutable"
	PhysicalPort string `json:"physicalPort"`

	// VlanID is the VLAN used on the physical port (0 = untagged). When
	// --vtep-db-addr is configured, this becomes the vlan_bindings map key.
	// Immutable.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=4095
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="spec.vlanID is immutable"
	VlanID int `json:"vlanID"`
}

type VtepBindingStatus struct {
	// Ready indicates the OVN VTEP Logical Switch Port has a non-empty SB chassis
	// (ovn-controller-vtep has assigned Port_Binding.chassis). Port_Binding.up is
	// not required.
	// +optional
	Ready bool `json:"ready"`

	// LogicalSwitch is the OVN Logical Switch name (the referenced Subnet).
	// +optional
	LogicalSwitch string `json:"logicalSwitch,omitempty"`

	// LogicalSwitchPort is the OVN Logical Switch Port name created for this binding.
	// +optional
	LogicalSwitchPort string `json:"logicalSwitchPort,omitempty"`

	// VtepLogicalSwitch is the resolved VTEP Logical_Switch name.
	// +optional
	VtepLogicalSwitch string `json:"vtepLogicalSwitch,omitempty"`

	// Chassis is the OVN SB Chassis name bound to the VTEP Logical Switch Port.
	// +optional
	Chassis string `json:"chassis,omitempty"`

	// Conditions represents the latest state of the object
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

func (s *VtepBindingStatus) addCondition(ctype ConditionType, status corev1.ConditionStatus, reason, message string) {
	now := metav1.Now()
	s.Conditions = append(s.Conditions, Condition{
		Type:               ctype,
		LastUpdateTime:     now,
		LastTransitionTime: now,
		Status:             status,
		Reason:             reason,
		Message:            message,
	})
}

func (s *VtepBindingStatus) setConditionValue(ctype ConditionType, status corev1.ConditionStatus, reason, message string) {
	var c *Condition
	for i := range s.Conditions {
		if s.Conditions[i].Type == ctype {
			c = &s.Conditions[i]
		}
	}
	if c == nil {
		s.addCondition(ctype, status, reason, message)
		return
	}
	if c.Status == status && c.Reason == reason && c.Message == message {
		return
	}
	now := metav1.Now()
	c.LastUpdateTime = now
	if c.Status != status {
		c.LastTransitionTime = now
	}
	c.Status = status
	c.Reason = reason
	c.Message = message
}

// GetCondition get existing condition
func (s *VtepBindingStatus) GetCondition(ctype ConditionType) *Condition {
	for i := range s.Conditions {
		if s.Conditions[i].Type == ctype {
			return &s.Conditions[i]
		}
	}
	return nil
}

// EnsureCondition useful for adding default conditions
func (s *VtepBindingStatus) EnsureCondition(ctype ConditionType) {
	if c := s.GetCondition(ctype); c != nil {
		return
	}
	s.addCondition(ctype, corev1.ConditionUnknown, ReasonInit, "Not Observed")
}

// EnsureStandardConditions - helper to inject standard conditions
func (s *VtepBindingStatus) EnsureStandardConditions() {
	s.EnsureCondition(Ready)
	s.EnsureCondition(VTEPDBReady)
}

// SetCondition updates or creates a new condition
func (s *VtepBindingStatus) SetCondition(ctype ConditionType, reason, message string) {
	s.setConditionValue(ctype, corev1.ConditionTrue, reason, message)
}

// ClearCondition updates or creates a new condition
func (s *VtepBindingStatus) ClearCondition(ctype ConditionType, reason, message string) {
	s.setConditionValue(ctype, corev1.ConditionFalse, reason, message)
}

// ReadyCondition - shortcut to set ready condition to true
func (s *VtepBindingStatus) ReadyCondition(reason, message string) {
	s.Ready = true
	s.SetCondition(Ready, reason, message)
}

// NotReady - shortcut to set ready condition to false
func (s *VtepBindingStatus) NotReady(reason, message string) {
	s.Ready = false
	s.ClearCondition(Ready, reason, message)
}

// SetError - shortcut to set error condition
func (s *VtepBindingStatus) SetError(reason, message string) {
	s.SetCondition(Error, reason, message)
}

// ClearError - shortcut to clear a previously recorded error condition
func (s *VtepBindingStatus) ClearError() {
	s.ClearCondition(Error, "Recovered", "")
}

// SetVTEPDBReady marks Hardware VTEP DB reconciliation as healthy or not required
func (s *VtepBindingStatus) SetVTEPDBReady(reason, message string) {
	s.SetCondition(VTEPDBReady, reason, message)
}

// NotVTEPDBReady marks Hardware VTEP DB reconciliation as unhealthy
func (s *VtepBindingStatus) NotVTEPDBReady(reason, message string) {
	s.ClearCondition(VTEPDBReady, reason, message)
}

// IsConditionTrue - if condition is true
func (s VtepBindingStatus) IsConditionTrue(ctype ConditionType) bool {
	if c := s.GetCondition(ctype); c != nil {
		return c.Status == corev1.ConditionTrue
	}
	return false
}

// IsReady returns true if ready condition is set
func (s VtepBindingStatus) IsReady() bool { return s.IsConditionTrue(Ready) }

func (s *VtepBindingStatus) Bytes() ([]byte, error) {
	bytes, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	newStr := fmt.Sprintf(`{"status": %s}`, string(bytes))
	klog.V(5).Info("status body", newStr)
	return []byte(newStr), nil
}
