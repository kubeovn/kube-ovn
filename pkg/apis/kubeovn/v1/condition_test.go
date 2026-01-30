package v1

import (
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/stretchr/testify/require"
)

func TestSetCondition(t *testing.T) {
	tests := []struct {
		name        string
		conditions  Conditions
		ctype       ConditionType
		status      corev1.ConditionStatus
		reason      string
		message     string
		generation  int64
		expectedLen int
	}{
		{
			name:        "add to nil conditions",
			conditions:  nil,
			ctype:       "Foo",
			status:      corev1.ConditionTrue,
			reason:      "insert",
			message:     "foo",
			generation:  1,
			expectedLen: 1,
		},
		{
			name:        "insert a new condition",
			conditions:  Conditions{{Type: "Foo", Status: corev1.ConditionTrue}},
			ctype:       "Bar",
			status:      corev1.ConditionTrue,
			reason:      "insert",
			message:     "bar",
			generation:  2,
			expectedLen: 2,
		},
		{
			name:        "update an existing condition",
			conditions:  Conditions{{Type: "Foo", Status: corev1.ConditionTrue, ObservedGeneration: 1}},
			ctype:       "Foo",
			status:      corev1.ConditionFalse,
			reason:      "update",
			message:     "bar",
			generation:  2,
			expectedLen: 1,
		},
		{
			name:        "no op",
			conditions:  Conditions{{Type: "Foo", Status: corev1.ConditionTrue, Reason: "noop", Message: "foo", ObservedGeneration: 1}},
			ctype:       "Foo",
			status:      corev1.ConditionTrue,
			reason:      "noop",
			message:     "foo",
			generation:  1,
			expectedLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.conditions.SetCondition(tt.ctype, tt.status, tt.reason, tt.message, 1)
			require.Len(t, tt.conditions, tt.expectedLen)
		})
	}
}

func TestRemoveCondition(t *testing.T) {
	tests := []struct {
		name        string
		conditions  Conditions
		ctype       ConditionType
		expectedLen int
	}{
		{
			name:        "remove from a nil conditions",
			conditions:  nil,
			ctype:       "Foo",
			expectedLen: 0,
		},
		{
			name:        "remove from an empty conditions",
			conditions:  Conditions{},
			ctype:       "Foo",
			expectedLen: 0,
		},
		{
			name: "remove an existing condition",
			conditions: Conditions{{
				Type: "Foo", Status: corev1.ConditionTrue, ObservedGeneration: 1,
			}, {
				Type: "Bar", Status: corev1.ConditionFalse, ObservedGeneration: 2,
			}},
			ctype:       "Foo",
			expectedLen: 1,
		},
		{
			name:        "remove the only condition",
			conditions:  Conditions{{Type: "Foo", Status: corev1.ConditionTrue, ObservedGeneration: 1}},
			ctype:       "Foo",
			expectedLen: 0,
		},
		{
			name:        "remove a non-existent condition",
			conditions:  Conditions{{Type: "Foo", Status: corev1.ConditionTrue, ObservedGeneration: 1}},
			ctype:       "Bar",
			expectedLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.conditions.RemoveCondition(tt.ctype)
			require.Len(t, tt.conditions, tt.expectedLen)
		})
	}
}

func TestGetCondition(t *testing.T) {
	tests := []struct {
		name       string
		conditions Conditions
		ctype      ConditionType
		expected   *Condition
	}{
		{
			name:       "get from a nil conditions",
			conditions: nil,
			ctype:      "Foo",
			expected:   nil,
		},
		{
			name:       "get from an empty conditions",
			conditions: Conditions{},
			ctype:      "Foo",
			expected:   nil,
		},
		{
			name: "get an existing condition",
			conditions: Conditions{{
				Type: "Foo", Status: corev1.ConditionTrue, ObservedGeneration: 1,
			}, {
				Type: "Bar", Status: corev1.ConditionFalse, ObservedGeneration: 2,
			}},
			ctype:    "Foo",
			expected: &Condition{Type: "Foo", Status: corev1.ConditionTrue, ObservedGeneration: 1},
		},
		{
			name:       "get the only condition",
			conditions: Conditions{{Type: "Foo", Status: corev1.ConditionTrue, ObservedGeneration: 1}},
			ctype:      "Foo",
			expected:   &Condition{Type: "Foo", Status: corev1.ConditionTrue, ObservedGeneration: 1},
		},
		{
			name:       "get a non-existent condition",
			conditions: Conditions{{Type: "Foo", Status: corev1.ConditionTrue, ObservedGeneration: 1}},
			ctype:      "Bar",
			expected:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := tt.conditions.GetCondition(tt.ctype)
			require.Equal(t, tt.expected, c)
		})
	}
}
