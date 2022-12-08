package util

import "testing"

func TestCompareVersion(t *testing.T) {
	tests := []struct {
		name     string
		version1 string
		version2 string
		want     int
	}{
		{
			version1: "21.06.1",
			version2: "20.09",
			want:     1,
		},
		{
			version1: "1.6.2",
			version2: "1.8.4",
			want:     -1,
		},
		{
			version1: "1.8.4",
			version2: "1.8.4",
			want:     0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if ret := CompareVersion(tt.version1, tt.version2); ret != tt.want {
				t.Errorf("got %v, want %v", ret, tt.want)
			}
		})
	}
}
