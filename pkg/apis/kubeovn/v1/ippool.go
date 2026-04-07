package v1

import (
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/internal"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type IPPoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []IPPool `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient:nonNamespaced
// +kubebuilder:resource:scope="Cluster",shortName="ippool",path="ippools"
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Subnet",type="string",JSONPath=".spec.subnet"
// +kubebuilder:printcolumn:name="enableAddressSet",type="boolean",JSONPath=".spec.enableAddressSet"
// +kubebuilder:printcolumn:name="IPs",type="string",JSONPath=".spec.ips"
// +kubebuilder:printcolumn:name="V4Used",type="number",JSONPath=".status.v4UsingIPs"
// +kubebuilder:printcolumn:name="V4Available",type="number",JSONPath=".status.v4AvailableIPs"
// +kubebuilder:printcolumn:name="V6Used",type="number",JSONPath=".status.v6UsingIPs"
// +kubebuilder:printcolumn:name="V6Available",type="number",JSONPath=".status.v6AvailableIPs"
type IPPool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   IPPoolSpec   `json:"spec"`
	Status IPPoolStatus `json:"status"`
}

type IPPoolSpec struct {
	// Subnet name for the IP pool. This field is immutable.
	// +kubebuilder:validation:Required
	Subnet string `json:"subnet,omitempty"`
	// Namespaces that can use this IP pool
	Namespaces []string `json:"namespaces,omitempty"`
	// IP addresses or ranges in the pool (IPv4/IPv6 addresses or CIDR ranges)
	// +kubebuilder:validation:Required
	IPs []string `json:"ips,omitempty"`
	// EnableAddressSet to work with policy-based routing and ACL
	// +kubebuilder:default=false
	EnableAddressSet bool `json:"enableAddressSet,omitempty"`
}

type IPPoolStatus struct {
	// Number of available IPv4 addresses
	V4AvailableIPs internal.BigInt `json:"v4AvailableIPs"`
	// Available IPv4 address range
	V4AvailableIPRange string `json:"v4AvailableIPRange"`
	// Number of using IPv4 addresses
	V4UsingIPs internal.BigInt `json:"v4UsingIPs"`
	// IPv4 address range in use
	V4UsingIPRange string `json:"v4UsingIPRange"`
	// Number of available IPv6 addresses
	V6AvailableIPs internal.BigInt `json:"v6AvailableIPs"`
	// Available IPv6 address range
	V6AvailableIPRange string `json:"v6AvailableIPRange"`
	// Number of using IPv6 addresses
	V6UsingIPs internal.BigInt `json:"v6UsingIPs"`
	// IPv6 address range in use
	V6UsingIPRange string `json:"v6UsingIPRange"`

	// Conditions represents the latest state of the object
	// +optional
	Conditions []Condition `json:"conditions,omitempty"`
}

func (s *IPPoolStatus) addCondition(ctype ConditionType, status corev1.ConditionStatus, reason, message string) {
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

// setConditionValue updates or creates a new condition
func (s *IPPoolStatus) setConditionValue(ctype ConditionType, status corev1.ConditionStatus, reason, message string) {
	var c *Condition
	for i := range s.Conditions {
		if s.Conditions[i].Type == ctype {
			c = &s.Conditions[i]
		}
	}
	if c == nil {
		s.addCondition(ctype, status, reason, message)
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

// GetCondition get existing condition
func (s *IPPoolStatus) GetCondition(ctype ConditionType) *Condition {
	for i := range s.Conditions {
		if s.Conditions[i].Type == ctype {
			return &s.Conditions[i]
		}
	}
	return nil
}

// EnsureCondition useful for adding default conditions
func (s *IPPoolStatus) EnsureCondition(ctype ConditionType) {
	if c := s.GetCondition(ctype); c != nil {
		return
	}
	s.addCondition(ctype, corev1.ConditionUnknown, ReasonInit, "Not Observed")
}

// EnsureStandardConditions - helper to inject standard conditions
func (s *IPPoolStatus) EnsureStandardConditions() {
	s.EnsureCondition(Ready)
}

// SetCondition updates or creates a new condition
func (s *IPPoolStatus) SetCondition(ctype ConditionType, reason, message string) {
	s.setConditionValue(ctype, corev1.ConditionTrue, reason, message)
}

// ClearCondition updates or creates a new condition
func (s *IPPoolStatus) ClearCondition(ctype ConditionType, reason, message string) {
	s.setConditionValue(ctype, corev1.ConditionFalse, reason, message)
}

// Ready - shortcut to set ready condition to true
func (s *IPPoolStatus) Ready(reason, message string) {
	s.SetCondition(Ready, reason, message)
}

// NotReady - shortcut to set ready condition to false
func (s *IPPoolStatus) NotReady(reason, message string) {
	s.ClearCondition(Ready, reason, message)
}

// SetError - shortcut to set error condition
func (s *IPPoolStatus) SetError(reason, message string) {
	s.SetCondition(Error, reason, message)
}

// IsConditionTrue - if condition is true
func (s IPPoolStatus) IsConditionTrue(ctype ConditionType) bool {
	if c := s.GetCondition(ctype); c != nil {
		return c.Status == corev1.ConditionTrue
	}
	return false
}

// IsReady returns true if ready condition is set
func (s IPPoolStatus) IsReady() bool { return s.IsConditionTrue(Ready) }

func (s *IPPoolStatus) Bytes() ([]byte, error) {
	bytes, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	newStr := fmt.Sprintf(`{"status": %s}`, string(bytes))
	klog.V(5).Info("status body", newStr)
	return []byte(newStr), nil
}
