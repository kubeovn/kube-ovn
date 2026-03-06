package util

import (
	"errors"
	"testing"
)

func TestSecurityGroupTierValidation(t *testing.T) {
	tests := []struct {
		name string
		tier int
		want error
	}{
		{
			name: "ValidSecurityGroupTier_0",
			tier: 0,
			want: nil,
		},
		{
			name: "ValidSecurityGroupTier_1",
			tier: 1,
			want: nil,
		},
		{
			name: "InvalidSecurityGroupTier_-1",
			tier: -1,
			want: &securityGroupTierValidationError{-1},
		},
		{
			name: "InvalidSecurityGroupTier_2",
			tier: 2,
			want: &securityGroupTierValidationError{2},
		},
	}

	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			if err := ValidateSecurityGroupTier(c.tier); !errors.Is(err, c.want) {
				t.Errorf("%v expected error %v, but got %v",
					c.name, c.want, err)
			}
		})
	}
}

func TestSecurityGroupAPITierToOVNTierConversion(t *testing.T) {
	tests := []struct {
		name      string
		inputTier int
		wantTier  int
	}{
		{
			name:      "ConvertSecurityGroupTier_0",
			inputTier: 0,
			wantTier:  2,
		},
		{
			name:      "ConvertSecurityGroupTier_1",
			inputTier: 1,
			wantTier:  3,
		},
	}

	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			if ans := ConvertSGTierToOvnTier(c.inputTier); ans != c.wantTier {
				t.Errorf("%v expected %v, but got %v",
					c.name, c.wantTier, ans)
			}
		})
	}
}
