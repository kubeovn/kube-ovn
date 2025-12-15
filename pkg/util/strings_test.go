package util

import (
	"slices"
	"testing"
)

func TestDoubleQuotedFields(t *testing.T) {
	tests := []struct {
		name string
		arg  string
		want []string
	}{
		{
			name: "simple",
			arg:  "abc def 123",
			want: []string{"abc", "def", "123"},
		},
		{
			name: "with quotes 1",
			arg:  `abc "def xyz" 123`,
			want: []string{"abc", "def xyz", "123"},
		},
		{
			name: "with quotes 2",
			arg:  `abc "def xyz " 123`,
			want: []string{"abc", "def xyz ", "123"},
		},
		{
			name: "with quotes 3",
			arg:  `abc " def xyz" 123`,
			want: []string{"abc", " def xyz", "123"},
		},
		{
			name: "with quotes 4",
			arg:  `abc " def xyz " 123`,
			want: []string{"abc", " def xyz ", "123"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if ret := DoubleQuotedFields(tt.arg); !slices.Equal(ret, tt.want) {
				t.Errorf("got %v, want %v", ret, tt.want)
			}
		})
	}
}
