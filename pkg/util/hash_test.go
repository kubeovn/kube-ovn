package util

import "testing"

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
