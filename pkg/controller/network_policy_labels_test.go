package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

func TestLabelsSetNilMatchesSelector(t *testing.T) {
	tests := []struct {
		name     string
		labels   map[string]string
		selector *metav1.LabelSelector
		expected bool
	}{
		{
			name:     "nil labels with empty selector should match",
			labels:   nil,
			selector: &metav1.LabelSelector{},
			expected: true,
		},
		{
			name:     "nil labels with matchLabels selector should not match",
			labels:   nil,
			selector: &metav1.LabelSelector{MatchLabels: map[string]string{"env": "prod"}},
			expected: false,
		},
		{
			name:     "nil labels with exists expression should not match",
			labels:   nil,
			selector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "env", Operator: metav1.LabelSelectorOpExists}}},
			expected: false,
		},
		{
			name:     "nil labels with doesNotExist expression should match",
			labels:   nil,
			selector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "env", Operator: metav1.LabelSelectorOpDoesNotExist}}},
			expected: true,
		},
		{
			name:     "empty labels with empty selector should match",
			labels:   map[string]string{},
			selector: &metav1.LabelSelector{},
			expected: true,
		},
		{
			name:     "empty labels with matchLabels selector should not match",
			labels:   map[string]string{},
			selector: &metav1.LabelSelector{MatchLabels: map[string]string{"env": "prod"}},
			expected: false,
		},
		{
			name:     "matching labels should match",
			labels:   map[string]string{"env": "prod"},
			selector: &metav1.LabelSelector{MatchLabels: map[string]string{"env": "prod"}},
			expected: true,
		},
		{
			name:     "nil labels with notIn expression should match",
			labels:   nil,
			selector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "env", Operator: metav1.LabelSelectorOpNotIn, Values: []string{"prod"}}}},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sel, err := metav1.LabelSelectorAsSelector(tt.selector)
			assert.NoError(t, err)

			result := sel.Matches(labels.Set(tt.labels))
			assert.Equal(t, tt.expected, result,
				"labels.Set(%v).Matches(%v) = %v, want %v",
				tt.labels, tt.selector, result, tt.expected)
		})
	}
}
