//go:build !windows
// +build !windows

package util

import (
	"reflect"
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
			if ret := DoubleQuotedFields(tt.arg); !reflect.DeepEqual(ret, tt.want) {
				t.Errorf("got %v, want %v", ret, tt.want)
			}
		})
	}
}

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
