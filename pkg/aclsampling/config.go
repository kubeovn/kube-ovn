package aclsampling

import (
	"errors"
	"fmt"
	"math"
)

const (
	DefaultSetID                         uint32  = 142
	DefaultLocalGroupID                  uint32  = 142
	DefaultAppIDNew                      uint32  = 102
	DefaultAppIDEstablished              uint32  = 103
	DefaultCollectorIDAllow              uint32  = 1
	DefaultCollectorIDDefaultDeny        uint32  = 2
	DefaultAllowProbabilityPercent       float64 = 1
	DefaultDefaultDenyProbabilityPercent float64 = 100

	maxOVNObjectID    uint32  = 255
	maxOVNProbability float64 = 65535
)

// ControllerConfig contains the controller-side ACL sampling configuration.
type ControllerConfig struct {
	Enabled                       bool
	SetID                         uint32
	AppIDNew                      uint32
	AppIDEstablished              uint32
	CollectorIDAllow              uint32
	CollectorIDDefaultDeny        uint32
	AllowProbabilityPercent       float64
	DefaultDenyProbabilityPercent float64
}

// NodeConfig contains the node-side ACL sampling configuration.
type NodeConfig struct {
	Enabled      bool
	SetID        uint32
	LocalGroupID uint32
}

// Validate checks whether the controller configuration can be represented by
// the OVN northbound sampling schema.
func (c ControllerConfig) Validate() error {
	if c.SetID == 0 {
		return errors.New("collector set ID must be greater than zero")
	}

	for name, id := range map[string]uint32{
		"new application ID":         c.AppIDNew,
		"established application ID": c.AppIDEstablished,
		"allow collector ID":         c.CollectorIDAllow,
		"default-deny collector ID":  c.CollectorIDDefaultDeny,
	} {
		if id == 0 || id > maxOVNObjectID {
			return fmt.Errorf("%s must be in the range 1-255", name)
		}
	}

	if c.AppIDNew == c.AppIDEstablished {
		return errors.New("new and established application IDs must be different")
	}
	if c.CollectorIDAllow == c.CollectorIDDefaultDeny {
		return errors.New("allow and default-deny collector IDs must be different")
	}
	if _, err := ProbabilityFromPercent(c.AllowProbabilityPercent); err != nil {
		return fmt.Errorf("invalid allow probability: %w", err)
	}
	if _, err := ProbabilityFromPercent(c.DefaultDenyProbabilityPercent); err != nil {
		return fmt.Errorf("invalid default-deny probability: %w", err)
	}

	return nil
}

// Validate checks whether the node configuration can identify the OVN sample
// collector set used for local delivery.
func (c NodeConfig) Validate() error {
	if c.SetID == 0 {
		return errors.New("collector set ID must be greater than zero")
	}
	return nil
}

// ProbabilityFromPercent converts a percentage to OVN's 16-bit probability
// representation using round(percent * 65535 / 100).
func ProbabilityFromPercent(percent float64) (int, error) {
	if math.IsNaN(percent) || math.IsInf(percent, 0) || percent < 0 || percent > 100 {
		return 0, errors.New("percentage must be in the range 0-100")
	}
	return int(math.Round(percent * maxOVNProbability / 100)), nil
}
