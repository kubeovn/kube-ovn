package multus

import (
	"strings"
	"testing"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

func TestVpcEgressGatewayPolicyCount(t *testing.T) {
	tests := []struct {
		name      string
		selectors []apiv1.VpcEgressGatewaySelector
		want      int
	}{
		{name: "subnet-only gateway", want: 2},
		{name: "selector gateway", selectors: []apiv1.VpcEgressGatewaySelector{{}}, want: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			veg := &apiv1.VpcEgressGateway{Spec: apiv1.VpcEgressGatewaySpec{Selectors: tt.selectors}}
			if got := vpcEgressGatewayPolicyCount(veg); got != tt.want {
				t.Fatalf("expected %d policies, got %d", tt.want, got)
			}
		})
	}
}

func TestValidateVpcEgressGatewayPolicyNexthops(t *testing.T) {
	const nexthops = "10.241.187.2 10.241.187.3"
	tests := []struct {
		name                string
		output              string
		expected            []string
		expectedPolicyCount int
		wantErr             string
	}{
		{
			name:                "subnet-only policy",
			output:              nexthops + "\n",
			expected:            []string{"10.241.187.2", "10.241.187.3"},
			expectedPolicyCount: 1,
		},
		{
			name:                "address set and port group policies",
			output:              nexthops + "\n" + nexthops + "\n",
			expected:            []string{"10.241.187.2", "10.241.187.3"},
			expectedPolicyCount: 2,
		},
		{
			name:                "unexpected policy count",
			output:              nexthops + "\n",
			expected:            []string{"10.241.187.2", "10.241.187.3"},
			expectedPolicyCount: 2,
			wantErr:             "got 1 matching policies, expected 2",
		},
		{
			name:                "unexpected nexthops",
			output:              "10.241.187.2\n",
			expected:            []string{"10.241.187.2", "10.241.187.3"},
			expectedPolicyCount: 1,
			wantErr:             "got policy nexthops",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateVpcEgressGatewayPolicyNexthops(tt.output, tt.expected, tt.expectedPolicyCount)
			if tt.wantErr == "" && err != nil {
				t.Fatal(err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}
