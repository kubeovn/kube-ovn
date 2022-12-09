package util

import "testing"

func TestExternalBridgeName(t *testing.T) {
	tests := []struct {
		arg  string
		want string
	}{
		{
			arg:  "ovn",
			want: "br-ovn",
		},
		{
			arg:  "Macvlan",
			want: "br-Macvlan",
		},
	}
	for _, tt := range tests {
		t.Run(tt.arg, func(t *testing.T) {
			if ret := ExternalBridgeName(tt.arg); ret != tt.want {
				t.Errorf("got %v, want %v", ret, tt.want)
			}
		})
	}
}
