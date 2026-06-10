package daemon

import (
	"testing"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestParseEnableSSLDaemon(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want bool
	}{
		{name: "true", env: "true", want: true},
		{name: "false", env: "false", want: false},
		{name: "empty", env: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(util.EnvSSLEnabled, tt.env)
			if got := parseEnableSSLFromEnv(); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}
