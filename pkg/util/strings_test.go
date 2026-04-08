package util

import (
	"slices"
	"testing"
)

func TestSplitTrimmed(t *testing.T) {
	tests := []struct {
		name string
		s    string
		sep  string
		want []string
	}{
		{
			name: "single element",
			s:    "10.0.0.1",
			sep:  ",",
			want: []string{"10.0.0.1"},
		},
		{
			name: "dual stack",
			s:    "10.0.0.1,fd00::1",
			sep:  ",",
			want: []string{"10.0.0.1", "fd00::1"},
		},
		{
			name: "trailing separator",
			s:    "10.0.0.1,",
			sep:  ",",
			want: []string{"10.0.0.1"},
		},
		{
			name: "leading separator",
			s:    ",10.0.0.1",
			sep:  ",",
			want: []string{"10.0.0.1"},
		},
		{
			name: "consecutive separators",
			s:    "10.0.0.1,,fd00::1",
			sep:  ",",
			want: []string{"10.0.0.1", "fd00::1"},
		},
		{
			name: "spaces around elements",
			s:    " 10.0.0.1 , fd00::1 ",
			sep:  ",",
			want: []string{"10.0.0.1", "fd00::1"},
		},
		{
			name: "empty string",
			s:    "",
			sep:  ",",
			want: nil,
		},
		{
			name: "only separator",
			s:    ",",
			sep:  ",",
			want: nil,
		},
		{
			name: "only spaces and separators",
			s:    "  ,  ,  ",
			sep:  ",",
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if ret := SplitTrimmed(tt.s, tt.sep); !slices.Equal(ret, tt.want) {
				t.Errorf("SplitTrimmed(%q, %q) = %v, want %v", tt.s, tt.sep, ret, tt.want)
			}
		})
	}
}

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
