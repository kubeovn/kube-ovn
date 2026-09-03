package aclsampling

import (
	"math"
	"testing"
)

func validControllerConfig() ControllerConfig {
	return ControllerConfig{
		Enabled:                       true,
		SetID:                         DefaultSetID,
		AppIDNew:                      DefaultAppIDNew,
		AppIDEstablished:              DefaultAppIDEstablished,
		CollectorIDAllow:              DefaultCollectorIDAllow,
		CollectorIDDefaultDeny:        DefaultCollectorIDDefaultDeny,
		AllowProbabilityPercent:       DefaultAllowProbabilityPercent,
		DefaultDenyProbabilityPercent: DefaultDefaultDenyProbabilityPercent,
	}
}

func TestControllerConfigValidate(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ControllerConfig)
	}{
		{name: "valid", mutate: func(*ControllerConfig) {}},
		{name: "zero set ID", mutate: func(c *ControllerConfig) { c.SetID = 0 }},
		{name: "zero application ID", mutate: func(c *ControllerConfig) { c.AppIDNew = 0 }},
		{name: "large application ID", mutate: func(c *ControllerConfig) { c.AppIDEstablished = 256 }},
		{name: "duplicate application ID", mutate: func(c *ControllerConfig) { c.AppIDEstablished = c.AppIDNew }},
		{name: "duplicate collector ID", mutate: func(c *ControllerConfig) { c.CollectorIDDefaultDeny = c.CollectorIDAllow }},
		{name: "negative allow probability", mutate: func(c *ControllerConfig) { c.AllowProbabilityPercent = -1 }},
		{name: "large default-deny probability", mutate: func(c *ControllerConfig) { c.DefaultDenyProbabilityPercent = 101 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := validControllerConfig()
			tt.mutate(&config)
			err := config.Validate()
			if tt.name == "valid" && err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
			if tt.name != "valid" && err == nil {
				t.Fatal("Validate() expected an error")
			}
		})
	}
}

func TestNodeConfigValidate(t *testing.T) {
	if err := (NodeConfig{SetID: DefaultSetID, LocalGroupID: 0}).Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if err := (NodeConfig{SetID: 0, LocalGroupID: DefaultLocalGroupID}).Validate(); err == nil {
		t.Fatal("Validate() expected an error")
	}
}

func TestProbabilityFromPercent(t *testing.T) {
	tests := []struct {
		name    string
		percent float64
		want    int
		wantErr bool
	}{
		{name: "disabled", percent: 0, want: 0},
		{name: "default allow", percent: 1, want: 655},
		{name: "fractional", percent: 12.5, want: 8192},
		{name: "always", percent: 100, want: 65535},
		{name: "negative", percent: -0.1, wantErr: true},
		{name: "too large", percent: 100.1, wantErr: true},
		{name: "not a number", percent: math.NaN(), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ProbabilityFromPercent(tt.percent)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ProbabilityFromPercent() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("ProbabilityFromPercent() = %d, want %d", got, tt.want)
			}
		})
	}
}
