package util

import (
	"errors"
	"fmt"
)

type securityGroupTierValidationError struct {
	invalidTier int
}

func (s *securityGroupTierValidationError) Error() string {
	return fmt.Sprintf("SecurityGroupTier %d invalid. It must be in range [%d,%d]", s.invalidTier, SecurityGroupAPITierMinimum, SecurityGroupAPITierMaximum)
}

func (s *securityGroupTierValidationError) Is(err error) bool {
	if sgError, ok := errors.AsType[*securityGroupTierValidationError](err); ok {
		return sgError.invalidTier == s.invalidTier
	}
	return false
}

func ValidateSecurityGroupTier(securityGroupAPITier int) error {
	if securityGroupAPITier < SecurityGroupAPITierMinimum || securityGroupAPITier > SecurityGroupAPITierMaximum {
		return &securityGroupTierValidationError{invalidTier: securityGroupAPITier}
	}
	return nil
}

// Assumes securityGroupTier is valid
func ConvertSGTierToOvnTier(securityGroupTier int) int {
	return securityGroupTier + SecurityGroupOvnTierBase
}
