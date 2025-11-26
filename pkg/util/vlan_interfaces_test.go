package util

import (
	"strings"
	"testing"
)

func TestDetectVlanInterfaces(t *testing.T) {
	result := DetectVlanInterfaces("nonexistent")
	if result == nil {
		t.Errorf("Expected a non-nil slice, got nil for parent 'nonexistent'")
	}
	if len(result) != 0 {
		t.Errorf("Expected empty slice for parent 'nonexistent', got %v", result)
	}
}

func TestCheckInterfaceExists(t *testing.T) {
	tests := []struct {
		name        string
		ifaceName   string
		description string
	}{
		{
			name:        "check loopback interface",
			ifaceName:   "lo",
			description: "Loopback interface should always exist",
		},
		{
			name:        "check non-existent interface",
			ifaceName:   "nonexistent12345",
			description: "Non-existent interface should return false",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Call the actual function
			exists := CheckInterfaceExists(tt.ifaceName)

			// For loopback, we can assert it should exist
			if tt.ifaceName == "lo" && !exists {
				t.Errorf("Expected loopback interface 'lo' to exist, but CheckInterfaceExists returned false")
			}

			// For non-existent, we can assert it should not exist
			if tt.ifaceName == "nonexistent12345" && exists {
				t.Errorf("Expected interface 'nonexistent12345' to not exist, but CheckInterfaceExists returned true")
			}
		})
	}
}

func TestExtractVlanIDFromInterface(t *testing.T) {
	tests := []struct {
		name          string
		interfaceName string
		expectedID    int
		expectError   bool
		errorContains string
	}{
		{
			name:          "valid ethernet interface",
			interfaceName: "eth0.10",
			expectedID:    10,
			expectError:   false,
		},
		{
			name:          "valid bond interface",
			interfaceName: "bond0.100",
			expectedID:    100,
			expectError:   false,
		},
		{
			name:          "valid interface with high VLAN ID",
			interfaceName: "ens192.4094",
			expectedID:    4094,
			expectError:   false,
		},
		{
			name:          "valid interface with VLAN ID 1",
			interfaceName: "eth1.1",
			expectedID:    1,
			expectError:   false,
		},
		{
			name:          "no dot in interface name",
			interfaceName: "eth0",
			expectError:   true,
			errorContains: "invalid VLAN interface name format",
		},
		{
			name:          "multiple dots in interface name",
			interfaceName: "eth0.10.20",
			expectError:   true,
			errorContains: "invalid VLAN interface name format",
		},
		{
			name:          "empty interface name",
			interfaceName: "",
			expectError:   true,
			errorContains: "invalid VLAN interface name format",
		},
		{
			name:          "interface name with only dot",
			interfaceName: ".",
			expectError:   true,
			errorContains: "failed to parse VLAN ID",
		},
		{
			name:          "interface name ending with dot",
			interfaceName: "eth0.",
			expectError:   true,
			errorContains: "failed to parse VLAN ID",
		},
		{
			name:          "interface name starting with dot",
			interfaceName: ".10",
			expectedID:    10,
			expectError:   false,
		},
		{
			name:          "non-numeric VLAN ID",
			interfaceName: "eth0.abc",
			expectError:   true,
			errorContains: "failed to parse VLAN ID",
		},
		{
			name:          "negative VLAN ID",
			interfaceName: "eth0.-10",
			expectedID:    -10,
			expectError:   false,
		},
		{
			name:          "zero VLAN ID",
			interfaceName: "eth0.0",
			expectedID:    0,
			expectError:   false,
		},
		{
			name:          "VLAN ID with leading zeros",
			interfaceName: "eth0.0010",
			expectedID:    10,
			expectError:   false,
		},
		{
			name:          "very long interface name with valid VLAN",
			interfaceName: "verylonginterfacename123456.999",
			expectedID:    999,
			expectError:   false,
		},
		{
			name:          "interface with special characters",
			interfaceName: "eth-0.50",
			expectedID:    50,
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vlanID, err := ExtractVlanIDFromInterface(tt.interfaceName)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for interface name '%s', but got none", tt.interfaceName)
					return
				}
				if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error to contain '%s', but got: %v", tt.errorContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error for interface name '%s', but got: %v", tt.interfaceName, err)
					return
				}
				if vlanID != tt.expectedID {
					t.Errorf("Expected VLAN ID %d for interface '%s', but got %d", tt.expectedID, tt.interfaceName, vlanID)
				}
			}
		})
	}
}
