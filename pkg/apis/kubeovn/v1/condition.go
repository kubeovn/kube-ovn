package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (m *SubnetStatus) addCondition(ctype ConditionType, status corev1.ConditionStatus, reason, message string) {
	now := metav1.Now()
	c := &SubnetCondition{
		Type:               ctype,
		LastUpdateTime:     now,
		LastTransitionTime: now,
		Status:             status,
		Reason:             reason,
		Message:            message,
	}
	m.Conditions = append(m.Conditions, *c)
}

// setConditionValue updates or creates a new condition
func (m *SubnetStatus) setConditionValue(ctype ConditionType, status corev1.ConditionStatus, reason, message string) {
	var c *SubnetCondition
	for i := range m.Conditions {
		if m.Conditions[i].Type == ctype {
			c = &m.Conditions[i]
		}
	}
	if c == nil {
		m.addCondition(ctype, status, reason, message)
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

// RemoveCondition removes the condition with the provided type.
func (m *SubnetStatus) RemoveCondition(ctype ConditionType) {
	for i, c := range m.Conditions {
		if c.Type == ctype {
			m.Conditions[i] = m.Conditions[len(m.Conditions)-1]
			m.Conditions = m.Conditions[:len(m.Conditions)-1]
			break
		}
	}
}

// GetCondition get existing condition
func (m *SubnetStatus) GetCondition(ctype ConditionType) *SubnetCondition {
	for i := range m.Conditions {
		if m.Conditions[i].Type == ctype {
			return &m.Conditions[i]
		}
	}
	return nil
}

// IsConditionTrue - if condition is true
func (m *SubnetStatus) IsConditionTrue(ctype ConditionType) bool {
	if c := m.GetCondition(ctype); c != nil {
		return c.Status == corev1.ConditionTrue
	}
	return false
}

// IsReady returns true if ready condition is set
func (m *SubnetStatus) IsReady() bool { return m.IsConditionTrue(Ready) }

// IsNotReady returns true if ready condition is set
func (m *SubnetStatus) IsNotReady() bool { return !m.IsConditionTrue(Ready) }

// IsValidated returns true if ready condition is set
func (m *SubnetStatus) IsValidated() bool { return m.IsConditionTrue(Validated) }

// IsNotValidated returns true if ready condition is set
func (m *SubnetStatus) IsNotValidated() bool { return !m.IsConditionTrue(Validated) }

// ConditionReason - return condition reason
func (m *SubnetStatus) ConditionReason(ctype ConditionType) string {
	if c := m.GetCondition(ctype); c != nil {
		return c.Reason
	}
	return ""
}

// Ready - shortcut to set ready condition to true
func (m *SubnetStatus) Ready(reason, message string) {
	m.SetCondition(Ready, reason, message)
}

// NotReady - shortcut to set ready condition to false
func (m *SubnetStatus) NotReady(reason, message string) {
	m.ClearCondition(Ready, reason, message)
}

// Validated - shortcut to set validated condition to true
func (m *SubnetStatus) Validated(reason, message string) {
	m.SetCondition(Validated, reason, message)
}

// NotValidated - shortcut to set validated condition to false
func (m *SubnetStatus) NotValidated(reason, message string) {
	m.ClearCondition(Validated, reason, message)
}

// SetError - shortcut to set error condition
func (m *SubnetStatus) SetError(reason, message string) {
	m.SetCondition(Error, reason, message)
}

// ClearError - shortcut to set error condition
func (m *SubnetStatus) ClearError() {
	m.ClearCondition(Error, "NoError", "No error seen")
}

// EnsureCondition useful for adding default conditions
func (m *SubnetStatus) EnsureCondition(ctype ConditionType) {
	if c := m.GetCondition(ctype); c != nil {
		return
	}
	m.addCondition(ctype, corev1.ConditionUnknown, ReasonInit, "Not Observed")
}

// EnsureStandardConditions - helper to inject standard conditions
func (m *SubnetStatus) EnsureStandardConditions() {
	m.EnsureCondition(Ready)
	m.EnsureCondition(Validated)
	m.EnsureCondition(Error)
}

// ClearCondition updates or creates a new condition
func (m *SubnetStatus) ClearCondition(ctype ConditionType, reason, message string) {
	m.setConditionValue(ctype, corev1.ConditionFalse, reason, message)
}

// SetCondition updates or creates a new condition
func (m *SubnetStatus) SetCondition(ctype ConditionType, reason, message string) {
	m.setConditionValue(ctype, corev1.ConditionTrue, reason, message)
}

// RemoveAllConditions updates or creates a new condition
func (m *SubnetStatus) RemoveAllConditions() {
	m.Conditions = []SubnetCondition{}
}

// ClearAllConditions updates or creates a new condition
func (m *SubnetStatus) ClearAllConditions() {
	for i := range m.Conditions {
		m.Conditions[i].Status = corev1.ConditionFalse
	}
}

func (s *IPPoolStatus) addCondition(ctype ConditionType, status corev1.ConditionStatus, reason, message string) {
	now := metav1.Now()
	s.Conditions = append(s.Conditions, IPPoolCondition{
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
	var c *IPPoolCondition
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
func (s *IPPoolStatus) GetCondition(ctype ConditionType) *IPPoolCondition {
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
	s.EnsureCondition(Error)
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

// ClearError - shortcut to set error condition
func (s *IPPoolStatus) ClearError() {
	s.ClearCondition(Error, "NoError", "No error seen")
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

// SetVlanError - shortcut to set error condition
func (v *VlanStatus) SetVlanError(reason, message string) {
	v.SetVlanCondition(Error, reason, message)
}

// SetVlanCondition updates or creates a new condition
func (v *VlanStatus) SetVlanCondition(ctype ConditionType, reason, message string) {
	v.setVlanConditionValue(ctype, corev1.ConditionTrue, reason, message)
}

func (v *VlanStatus) setVlanConditionValue(ctype ConditionType, status corev1.ConditionStatus, reason, message string) {
	var c *VlanCondition
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
	c := &VlanCondition{
		Type:               ctype,
		LastUpdateTime:     now,
		LastTransitionTime: now,
		Status:             status,
		Reason:             reason,
		Message:            message,
	}
	v.Conditions = append(v.Conditions, *c)
}

func (s *ProviderNetworkStatus) addNodeCondition(node string, ctype ConditionType, status corev1.ConditionStatus, reason, message string) {
	now := metav1.Now()
	c := &ProviderNetworkCondition{
		Node: node,
		Condition: Condition{
			Type:               ctype,
			LastUpdateTime:     now,
			LastTransitionTime: now,
			Status:             status,
			Reason:             reason,
			Message:            message,
		},
	}
	s.Conditions = append(s.Conditions, *c)
}

// setConditionValue updates or creates a new condition
func (s *ProviderNetworkStatus) setNodeConditionValue(node string, ctype ConditionType, status corev1.ConditionStatus, reason, message string) bool {
	var c *ProviderNetworkCondition
	for i := range s.Conditions {
		if s.Conditions[i].Node == node && s.Conditions[i].Type == ctype {
			c = &s.Conditions[i]
		}
	}
	if c == nil {
		s.addNodeCondition(node, ctype, status, reason, message)
	} else {
		// check message ?
		if c.Status == status && c.Reason == reason && c.Message == message {
			return false
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

	return true
}

// RemoveNodeCondition removes the condition with the provided type.
func (s *ProviderNetworkStatus) RemoveNodeCondition(node string, ctype ConditionType) {
	for i, c := range s.Conditions {
		if c.Node == node && c.Type == ctype {
			s.Conditions[i] = s.Conditions[len(s.Conditions)-1]
			s.Conditions = s.Conditions[:len(s.Conditions)-1]
			break
		}
	}
}

// GetNodeCondition get existing condition
func (s *ProviderNetworkStatus) GetNodeCondition(node string, ctype ConditionType) *ProviderNetworkCondition {
	for i := range s.Conditions {
		if s.Conditions[i].Node == node && s.Conditions[i].Type == ctype {
			return &s.Conditions[i]
		}
	}
	return nil
}

// IsNodeConditionTrue - if condition is true
func (s *ProviderNetworkStatus) IsNodeConditionTrue(node string, ctype ConditionType) bool {
	if c := s.GetNodeCondition(node, ctype); c != nil {
		return c.Status == corev1.ConditionTrue
	}
	return false
}

// NodeIsReady returns true if ready condition is set
func (s *ProviderNetworkStatus) NodeIsReady(node string) bool {
	for _, c := range s.Conditions {
		if c.Node == node && c.Type == Ready && c.Status != corev1.ConditionTrue {
			return false
		}
	}
	return true
}

// IsReady returns true if ready condition is set
func (s *ProviderNetworkStatus) IsReady() bool {
	for _, c := range s.Conditions {
		if c.Type == Ready && c.Status != corev1.ConditionTrue {
			return false
		}
	}
	return true
}

// ConditionReason - return condition reason
func (s *ProviderNetworkStatus) ConditionReason(node string, ctype ConditionType) string {
	if c := s.GetNodeCondition(node, ctype); c != nil {
		return c.Reason
	}
	return ""
}

// SetNodeReady - shortcut to set ready condition to true
func (s *ProviderNetworkStatus) SetNodeReady(node, reason, message string) bool {
	return s.SetNodeCondition(node, Ready, reason, message)
}

// SetNodeNotReady - shortcut to set ready condition to false
func (s *ProviderNetworkStatus) SetNodeNotReady(node, reason, message string) bool {
	return s.ClearNodeCondition(node, Ready, reason, message)
}

// EnsureNodeCondition useful for adding default conditions
func (s *ProviderNetworkStatus) EnsureNodeCondition(node string, ctype ConditionType) bool {
	if c := s.GetNodeCondition(node, ctype); c != nil {
		return false
	}
	s.addNodeCondition(node, ctype, corev1.ConditionUnknown, ReasonInit, "Not Observed")
	return true
}

// EnsureNodeStandardConditions - helper to inject standard conditions
func (s *ProviderNetworkStatus) EnsureNodeStandardConditions(node string) bool {
	return s.EnsureNodeCondition(node, Ready)
}

// ClearNodeCondition updates or creates a new condition
func (s *ProviderNetworkStatus) ClearNodeCondition(node string, ctype ConditionType, reason, message string) bool {
	return s.setNodeConditionValue(node, ctype, corev1.ConditionFalse, reason, message)
}

// SetNodeCondition updates or creates a new condition
func (s *ProviderNetworkStatus) SetNodeCondition(node string, ctype ConditionType, reason, message string) bool {
	return s.setNodeConditionValue(node, ctype, corev1.ConditionTrue, reason, message)
}

// RemoveNodeConditions updates or creates a new condition
func (s *ProviderNetworkStatus) RemoveNodeConditions(node string) bool {
	var changed bool
	for i := 0; i < len(s.Conditions); {
		if s.Conditions[i].Node == node {
			changed = true
			s.Conditions = append(s.Conditions[:i], s.Conditions[i+1:]...)
		} else {
			i++
		}
	}
	return changed
}
