package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type VlanList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Vlan `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient:nonNamespaced
// +kubebuilder:resource:scope="Cluster",shortName="vlan",path="vlans"
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="ID",type="string",JSONPath=".spec.id"
// +kubebuilder:printcolumn:name="Provider",type="string",JSONPath=".spec.provider"
// +kubebuilder:printcolumn:name="conflict",type="boolean",JSONPath=".status.conflict"
type Vlan struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   VlanSpec   `json:"spec"`
	Status VlanStatus `json:"status"`
}

type VlanSpec struct {
	// deprecated fields, use ID & Provider instead
	VlanID int `json:"vlanId,omitempty"`
	// Deprecated: in favor of provider
	// +kubebuilder:validation:Optional
	ProviderInterfaceName string `json:"providerInterfaceName,omitempty"`

	// VLAN ID (0-4095). This field is immutable after creation.
	ID int `json:"id"`
	// Provider network name. This field is immutable after creation.
	// +kubebuilder:validation:Required
	Provider string `json:"provider,omitempty"`
}

type VlanStatus struct {
	// List of subnet names using this VLAN
	// +optional
	// +patchStrategy=merge
	Subnets []string `json:"subnets,omitempty"`

	// Whether there is a conflict with this VLAN
	Conflict bool `json:"conflict,omitempty"`

	// Conditions represents the latest state of the object
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// SetVlanError - shortcut to set error condition
func (v *VlanStatus) SetVlanError(reason, message string) {
	v.SetVlanCondition(Error, reason, message)
}

// SetVlanCondition updates or creates a new condition
func (v *VlanStatus) SetVlanCondition(ctype ConditionType, reason, message string) {
	v.setVlanConditionValue(ctype, corev1.ConditionTrue, reason, message)
}

func (v *VlanStatus) setVlanConditionValue(ctype ConditionType, status corev1.ConditionStatus, reason, message string) {
	var c *Condition
	for i := range v.Conditions {
		if v.Conditions[i].Type == ctype {
			c = &v.Conditions[i]
		}
	}
	if c == nil {
		v.addVlanCondition(ctype, status, reason, message)
	} else {
		// check message ?
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
}

func (v *VlanStatus) addVlanCondition(ctype ConditionType, status corev1.ConditionStatus, reason, message string) {
	now := metav1.Now()
	c := &Condition{
		Type:               ctype,
		LastUpdateTime:     now,
		LastTransitionTime: now,
		Status:             status,
		Reason:             reason,
		Message:            message,
	}
	v.Conditions = append(v.Conditions, *c)
}
