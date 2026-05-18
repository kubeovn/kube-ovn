package webhook

import (
	"strings"
	"testing"
)

func TestCheckIPAddressFamilyUniqueness(t *testing.T) {
	tests := []struct {
		name      string
		ipAddress string
		wantErr   string
	}{
		{
			name:      "single IPv4",
			ipAddress: "10.0.0.1",
		},
		{
			name:      "single IPv6",
			ipAddress: "fd00::1",
		},
		{
			name:      "dual-stack v4 first",
			ipAddress: "10.0.0.1,fd00::1",
		},
		{
			name:      "dual-stack v6 first",
			ipAddress: "fd00::1,10.0.0.1",
		},
		{
			name:      "dual-stack with spaces",
			ipAddress: "10.0.0.1, fd00::1",
		},
		{
			name:      "single IPv4 with CIDR suffix",
			ipAddress: "10.0.0.1/24",
		},
		{
			name:      "dual-stack with CIDR suffix",
			ipAddress: "10.0.0.1/24,fd00::1/64",
		},
		{
			name:      "two IPv4 addresses rejected",
			ipAddress: "10.0.0.1,10.0.0.2",
			wantErr:   `multiple IPv4 addresses in ip_address annotation "10.0.0.1,10.0.0.2"`,
		},
		{
			name:      "two IPv6 addresses rejected",
			ipAddress: "fd00::1,fd00::2",
			wantErr:   `multiple IPv6 addresses in ip_address annotation "fd00::1,fd00::2"`,
		},
		{
			name:      "v4 + v4 + v6 rejected on v4 count",
			ipAddress: "10.0.0.1,10.0.0.2,fd00::1",
			wantErr:   `multiple IPv4 addresses in ip_address annotation "10.0.0.1,10.0.0.2,fd00::1"`,
		},
		{
			name:      "v4 + v6 + v6 rejected on v6 count",
			ipAddress: "10.0.0.1,fd00::1,fd00::2",
			wantErr:   `multiple IPv6 addresses in ip_address annotation "10.0.0.1,fd00::1,fd00::2"`,
		},
		{
			// Defer reporting to checkIPConflict for consistent error wording.
			name:      "invalid IP is left for checkIPConflict",
			ipAddress: "10.0.0.1,not-an-ip",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := checkIPAddressFamilyUniqueness(tc.ipAddress)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %q", tc.wantErr, err.Error())
			}
		})
	}
}
