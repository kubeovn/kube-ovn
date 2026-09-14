package iptables

import "testing"

func TestMatchRules(t *testing.T) {
	tests := []struct {
		name        string
		output      string
		expected    []string
		shouldExist bool
		wantMatched bool
	}{
		{
			name:        "rule present",
			output:      "-A OVN-OUTPUT -d 2001:db8::1/128 -p tcp --dport 8080 -j MARK\n",
			expected:    []string{"-A OVN-OUTPUT -d 2001:db8::1/128 -p tcp --dport 8080"},
			shouldExist: true,
			wantMatched: true,
		},
		{
			name:        "chain not created yet",
			output:      "",
			expected:    []string{"-A OVN-OUTPUT -d 2001:db8::1/128 -p tcp --dport 8080"},
			shouldExist: true,
			wantMatched: false,
		},
		{
			name:        "rule absent",
			output:      "-A OVN-OUTPUT -d 2001:db8::1/128 -p tcp --dport 8080 -j MARK\n",
			expected:    []string{"-A OVN-OUTPUT -d 2001:db8::1/128 -p tcp --dport 9090"},
			shouldExist: false,
			wantMatched: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matched, err := matchRules(tt.output, tt.expected, tt.shouldExist)
			if err != nil {
				t.Fatalf("matchRules returned error: %v", err)
			}
			if matched != tt.wantMatched {
				t.Fatalf("matchRules = %v, want %v", matched, tt.wantMatched)
			}
		})
	}
}
