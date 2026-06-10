package controller

import (
	"testing"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestParseEnableSSLFromEnv(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want bool
	}{
		{name: "enabled", env: "true", want: true},
		{name: "disabled", env: "false", want: false},
		{name: "unset", env: "", want: false},
		{name: "garbage", env: "yes", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(util.EnvSSLEnabled, tt.env)
			if got := parseEnableSSLFromEnv(); got != tt.want {
				t.Fatalf("parseEnableSSLFromEnv() = %v, want %v", got, tt.want)
			}
		})
	}
}
