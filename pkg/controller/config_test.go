package controller

import (
	"testing"
	"time"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

func TestLeaderElectionConfigurationFlags(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected LeaderElectionConfiguration
	}{
		{
			name: "defaults",
			expected: LeaderElectionConfiguration{
				LeaseDuration: 30 * time.Second,
				RenewDeadline: 20 * time.Second,
				RetryPeriod:   6 * time.Second,
			},
		},
		{
			name: "custom durations",
			args: []string{
				"--leader-elect-lease-duration=1m",
				"--leader-elect-renew-deadline=40s",
				"--leader-elect-retry-period=10s",
			},
			expected: LeaderElectionConfiguration{
				LeaseDuration: time.Minute,
				RenewDeadline: 40 * time.Second,
				RetryPeriod:   10 * time.Second,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := defaultLeaderElectionConfiguration()
			flagSet := pflag.NewFlagSet(tt.name, pflag.ContinueOnError)
			config.addFlags(flagSet)

			require.NoError(t, flagSet.Parse(tt.args))
			require.Equal(t, tt.expected, config)
			require.NoError(t, config.validate())
		})
	}
}

func TestLeaderElectionConfigurationValidation(t *testing.T) {
	tests := []struct {
		name        string
		config      LeaderElectionConfiguration
		errorString string
	}{
		{
			name: "lease duration must be positive",
			config: LeaderElectionConfiguration{
				RenewDeadline: time.Second,
				RetryPeriod:   time.Millisecond,
			},
			errorString: "--leader-elect-lease-duration must be greater than zero",
		},
		{
			name: "renew deadline must be positive",
			config: LeaderElectionConfiguration{
				LeaseDuration: time.Second,
				RetryPeriod:   time.Millisecond,
			},
			errorString: "--leader-elect-renew-deadline must be greater than zero",
		},
		{
			name: "retry period must be positive",
			config: LeaderElectionConfiguration{
				LeaseDuration: time.Second,
				RenewDeadline: time.Millisecond,
			},
			errorString: "--leader-elect-retry-period must be greater than zero",
		},
		{
			name: "lease duration must exceed renew deadline",
			config: LeaderElectionConfiguration{
				LeaseDuration: 20 * time.Second,
				RenewDeadline: 20 * time.Second,
				RetryPeriod:   time.Second,
			},
			errorString: "--leader-elect-lease-duration must be greater than --leader-elect-renew-deadline",
		},
		{
			name: "renew deadline must allow retry jitter",
			config: LeaderElectionConfiguration{
				LeaseDuration: 30 * time.Second,
				RenewDeadline: 12 * time.Second,
				RetryPeriod:   10 * time.Second,
			},
			errorString: "--leader-elect-renew-deadline must be greater than --leader-elect-retry-period multiplied by 1.2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.EqualError(t, tt.config.validate(), tt.errorString)
		})
	}
}
