package controller

import "testing"

func TestMatchesExternalIDs(t *testing.T) {
	tests := []struct {
		name     string
		actual   map[string]string
		expected map[string]string
		want     bool
	}{
		{name: "empty selector", actual: map[string]string{"vendor": "kube-ovn"}, want: true},
		{name: "exact value", actual: map[string]string{"vendor": "kube-ovn"}, expected: map[string]string{"vendor": "kube-ovn"}, want: true},
		{name: "key only", actual: map[string]string{"node": "node-1"}, expected: map[string]string{"node": ""}, want: true},
		{name: "missing key", actual: map[string]string{}, expected: map[string]string{"node": ""}, want: false},
		{name: "different value", actual: map[string]string{"vendor": "other"}, expected: map[string]string{"vendor": "kube-ovn"}, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := matchesExternalIDs(test.actual, test.expected); got != test.want {
				t.Fatalf("matchesExternalIDs(%v, %v) = %v, want %v", test.actual, test.expected, got, test.want)
			}
		})
	}
}
