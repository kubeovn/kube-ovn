package controller

import (
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

func TestConfigurationValidateModeFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		enableBgpLb   bool
		enableLbSvc   bool
		expectErr     bool
		errorContains string
	}{
		{
			name:        "both disabled is valid",
			enableBgpLb: false,
			enableLbSvc: false,
			expectErr:   false,
		},
		{
			name:        "only bgp lb eip enabled is valid",
			enableBgpLb: true,
			enableLbSvc: false,
			expectErr:   false,
		},
		{
			name:        "only lb svc enabled is valid",
			enableBgpLb: false,
			enableLbSvc: true,
			expectErr:   false,
		},
		{
			name:          "both enabled is invalid",
			enableBgpLb:   true,
			enableLbSvc:   true,
			expectErr:     true,
			errorContains: "mutually exclusive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := &Configuration{
				EnableBgpLbVip: tt.enableBgpLb,
				EnableLbSvc:    tt.enableLbSvc,
			}

			err := cfg.validateModeFlags()
			if tt.expectErr {
				require.Error(t, err)
				require.ErrorContains(t, err, tt.errorContains)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestRegisterBgpLbVipFlags(t *testing.T) {
	t.Parallel()

	t.Run("enable via vip flag", func(t *testing.T) {
		t.Parallel()

		fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
		enabled := fs.Bool("enable-bgp-lb-vip", false, "test flag")

		require.NoError(t, fs.Parse([]string{"--enable-bgp-lb-vip=true"}))
		require.True(t, *enabled)
	})

	t.Run("unknown bgp lb flag is rejected", func(t *testing.T) {
		t.Parallel()

		fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
		enabled := fs.Bool("enable-bgp-lb-vip", false, "test flag")

		err := fs.Parse([]string{"--enable-bgp-lb-unknown=true"})
		require.Error(t, err)
		require.False(t, *enabled)
	})
}
