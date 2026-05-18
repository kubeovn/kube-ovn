package util

import (
	"sort"
	"testing"
)

func TestSha256Hash(t *testing.T) {
	tests := []struct {
		name   string
		input  []byte
		output string
	}{
		{
			name:   "Empty input",
			input:  []byte(""),
			output: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		{
			name:   "Non empty input",
			input:  []byte("hello"),
			output: "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Sha256Hash(tt.input)
			if got != tt.output {
				t.Errorf("got %v, but want %v", got, tt.output)
			}
		})
	}
}

func TestSha256HashObject(t *testing.T) {
	tests := []struct {
		name    string
		arg     any
		wantErr bool
		hash    string
	}{
		{
			name: "nil",
			arg:  nil,
			hash: "74234e98afe7498fb5daf1f36ac2d78acc339464f950703b8c019892f982b90b",
		},
		{
			name: "string slice",
			arg:  []string{"hello", "world"},
			hash: "94bedb26fb1cb9547b5b77902e89522f313c7f7fe2e9f0175cfb0a244878ee07",
		},
		{
			name: "string map",
			arg:  map[string]string{"hello": "world"},
			hash: "93a23971a914e5eacbf0a8d25154cda309c3c1c72fbb9914d47c60f3cb681588",
		},
		{
			name:    "unsupported type",
			arg:     make(chan struct{}),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash, err := Sha256HashObject(tt.arg)
			if (err != nil) != tt.wantErr {
				t.Errorf("got error = %#v, but wantErr = %v", err, tt.wantErr)
			}
			if hash != tt.hash {
				t.Errorf("got hash %v, but want %v", hash, tt.hash)
			}
		})
	}
}

func TestSha256HashGatewayChassisDistribution(t *testing.T) {
	chassises := []string{
		"efa09809-38e5-4c6d-b0a3-c8729fc40313",
		"672a26c8-c12b-4853-907c-d3243c20e77d",
		"4ac8c950-8839-4b72-93e5-3bab702657db",
	}

	getFirstChassis := func(vpcName string) string {
		sorted := make([]string, len(chassises))
		copy(sorted, chassises)
		sort.Slice(sorted, func(i, j int) bool {
			return Sha256Hash([]byte(vpcName+sorted[i])) < Sha256Hash([]byte(vpcName+sorted[j]))
		})
		return sorted[0]
	}

	// deterministic: same VPC always gets same result
	first := getFirstChassis("vpc-a")
	for range 10 {
		if getFirstChassis("vpc-a") != first {
			t.Fatal("not deterministic")
		}
	}

	// distributed: not all VPCs get the same chassis
	results := map[string]int{}
	vpcs := []string{"vpc-a", "vpc-b", "vpc-c", "vpc-d", "vpc-e", "vpc-f", "vpc-g", "vpc-h", "vpc-i", "vpc-j"}
	for _, vpc := range vpcs {
		results[getFirstChassis(vpc)]++
	}
	if len(results) == 1 {
		t.Fatalf("all VPCs got same chassis: %v", results)
	}
}
