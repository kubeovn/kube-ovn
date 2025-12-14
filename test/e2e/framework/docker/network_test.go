package docker

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

func TestGenerateRandomSubnets(t *testing.T) {
	tests := []struct {
		name         string
		count        int
		expectPanic  bool
		validateFunc func([]string) error
	}{
		{
			name:  "generate 1 subnet",
			count: 1,
			validateFunc: func(subnets []string) error {
				if len(subnets) != 1 {
					return fmt.Errorf("expected 1 subnet, got %d", len(subnets))
				}
				return nil
			},
		},
		{
			name:  "generate 2 subnets",
			count: 2,
			validateFunc: func(subnets []string) error {
				if len(subnets) != 2 {
					return fmt.Errorf("expected 2 subnets, got %d", len(subnets))
				}
				return nil
			},
		},
		{
			name:  "generate 5 subnets",
			count: 5,
			validateFunc: func(subnets []string) error {
				if len(subnets) != 5 {
					return fmt.Errorf("expected 5 subnets, got %d", len(subnets))
				}
				return nil
			},
		},
		{
			name:  "generate maximum (256) subnets",
			count: 256,
			validateFunc: func(subnets []string) error {
				if len(subnets) != 256 {
					return fmt.Errorf("expected 256 subnets, got %d", len(subnets))
				}
				return nil
			},
		},
		{
			name:        "invalid count: 0",
			count:       0,
			expectPanic: true,
		},
		{
			name:        "invalid count: negative",
			count:       -1,
			expectPanic: true,
		},
		{
			name:        "invalid count: > 256",
			count:       257,
			expectPanic: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.expectPanic {
				defer func() {
					if r := recover(); r == nil {
						t.Errorf("expected panic but didn't get one")
					}
				}()
				GenerateRandomSubnets(tt.count)
				return
			}

			subnets := GenerateRandomSubnets(tt.count)

			if tt.validateFunc != nil {
				if err := tt.validateFunc(subnets); err != nil {
					t.Errorf("validation failed: %v", err)
				}
			}

			// Common validations for all non-panic cases
			if err := validateSubnetFormat(subnets); err != nil {
				t.Errorf("subnet format validation failed: %v", err)
			}

			if err := validateSubnetUniqueness(subnets); err != nil {
				t.Errorf("subnet uniqueness validation failed: %v", err)
			}

			if err := validateSubnetRange(subnets); err != nil {
				t.Errorf("subnet range validation failed: %v", err)
			}
		})
	}
}

func TestGenerateRandomSubnetsUniqueness(t *testing.T) {
	// Test that multiple invocations produce different results
	iterations := 10
	allSubnets := make(map[string]int)

	for i := 0; i < iterations; i++ {
		subnets := GenerateRandomSubnets(2)
		for _, subnet := range subnets {
			allSubnets[subnet]++
		}
	}

	// We expect to see some variety (not all the same)
	if len(allSubnets) < 3 {
		t.Errorf("expected at least 3 different subnets across %d iterations, got %d: %v",
			iterations, len(allSubnets), allSubnets)
	}

	t.Logf("Generated %d unique subnets across %d iterations", len(allSubnets), iterations)
}

func TestGenerateRandomSubnetsDistribution(t *testing.T) {
	// Test randomness distribution
	count := 100
	subnets := make([]string, count)
	for i := 0; i < count; i++ {
		result := GenerateRandomSubnets(1)
		subnets[i] = result[0]
	}

	uniqueSubnets := make(map[string]bool)
	for _, subnet := range subnets {
		uniqueSubnets[subnet] = true
	}

	uniqueCount := len(uniqueSubnets)
	// Expect at least 50% uniqueness in 100 iterations
	minExpected := count / 2
	if uniqueCount < minExpected {
		t.Errorf("poor randomness: expected at least %d unique subnets in %d iterations, got %d",
			minExpected, count, uniqueCount)
	}

	t.Logf("Randomness check: %d unique subnets out of %d iterations (%.1f%%)",
		uniqueCount, count, float64(uniqueCount)/float64(count)*100)
}

func TestGenerateRandomSubnetsNoCollision(t *testing.T) {
	// Test that generating 2 subnets never produces collision within a single call
	iterations := 100
	for i := 0; i < iterations; i++ {
		subnets := GenerateRandomSubnets(2)
		if subnets[0] == subnets[1] {
			t.Errorf("iteration %d: collision detected: %s == %s", i, subnets[0], subnets[1])
		}
	}
	t.Logf("No collisions detected in %d iterations", iterations)
}

func TestSubnetFormatConsistency(t *testing.T) {
	// Test that all generated subnets follow the expected format
	subnets := GenerateRandomSubnets(10)
	expectedPattern := regexp.MustCompile(`^172\.28\.\d{1,3}\.0/24$`)

	for i, subnet := range subnets {
		if !expectedPattern.MatchString(subnet) {
			t.Errorf("subnet %d has invalid format: %s", i, subnet)
		}

		// Verify the third octet is in valid range (0-255)
		parts := strings.Split(strings.TrimSuffix(subnet, "/24"), ".")
		if len(parts) != 4 {
			t.Errorf("subnet %d has invalid structure: %s", i, subnet)
			continue
		}

		var thirdOctet int
		if _, err := fmt.Sscanf(parts[2], "%d", &thirdOctet); err != nil {
			t.Errorf("subnet %d has invalid third octet: %s", i, parts[2])
			continue
		}

		if thirdOctet < 0 || thirdOctet > 255 {
			t.Errorf("subnet %d has third octet out of range: %d", i, thirdOctet)
		}
	}
}

// Helper functions for validation

func validateSubnetFormat(subnets []string) error {
	pattern := regexp.MustCompile(`^172\.28\.\d{1,3}\.0/24$`)
	for i, subnet := range subnets {
		if !pattern.MatchString(subnet) {
			return fmt.Errorf("subnet %d has invalid format: %s (expected 172.28.X.0/24)", i, subnet)
		}
	}
	return nil
}

func validateSubnetUniqueness(subnets []string) error {
	seen := make(map[string]bool)
	for i, subnet := range subnets {
		if seen[subnet] {
			return fmt.Errorf("duplicate subnet found at index %d: %s", i, subnet)
		}
		seen[subnet] = true
	}
	return nil
}

func validateSubnetRange(subnets []string) error {
	for i, subnet := range subnets {
		parts := strings.Split(strings.TrimSuffix(subnet, "/24"), ".")
		if len(parts) != 4 {
			return fmt.Errorf("subnet %d has invalid structure: %s", i, subnet)
		}

		// Validate base (172.28)
		if parts[0] != "172" || parts[1] != "28" {
			return fmt.Errorf("subnet %d has invalid base: %s.%s (expected 172.28)", i, parts[0], parts[1])
		}

		// Validate third octet (0-255)
		var thirdOctet int
		if _, err := fmt.Sscanf(parts[2], "%d", &thirdOctet); err != nil {
			return fmt.Errorf("subnet %d has invalid third octet: %s", i, parts[2])
		}
		if thirdOctet < 0 || thirdOctet > 255 {
			return fmt.Errorf("subnet %d has third octet out of range: %d (must be 0-255)", i, thirdOctet)
		}

		// Validate fourth octet (must be 0)
		if parts[3] != "0" {
			return fmt.Errorf("subnet %d has invalid fourth octet: %s (expected 0)", i, parts[3])
		}
	}
	return nil
}

// Benchmark tests

func BenchmarkGenerateRandomSubnets1(b *testing.B) {
	for i := 0; i < b.N; i++ {
		GenerateRandomSubnets(1)
	}
}

func BenchmarkGenerateRandomSubnets2(b *testing.B) {
	for i := 0; i < b.N; i++ {
		GenerateRandomSubnets(2)
	}
}

func BenchmarkGenerateRandomSubnets10(b *testing.B) {
	for i := 0; i < b.N; i++ {
		GenerateRandomSubnets(10)
	}
}

func BenchmarkGenerateRandomSubnets100(b *testing.B) {
	for i := 0; i < b.N; i++ {
		GenerateRandomSubnets(100)
	}
}
