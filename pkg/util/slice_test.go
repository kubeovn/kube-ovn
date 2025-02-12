package util

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDiffStringSlice(t *testing.T) {
	tests := []struct {
		name   string
		slice1 []string
		slice2 []string
		want   []string
	}{
		{
			name:   "base",
			slice1: []string{"a", "b", "c"},
			slice2: []string{"a", "b", "f"},
			want:   []string{"c", "f"},
		},
		{
			name:   "baseWithBlank",
			slice1: []string{"a ", " b", " c "},
			slice2: []string{"a", " b", "f"},
			want:   []string{"a ", " c ", "a", "f"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if ret := DiffStringSlice(tt.slice1, tt.slice2); !slices.Equal(ret, tt.want) {
				t.Errorf("got %v, want %v", ret, tt.want)
			}
		})
	}
}

func TestUnionStringSlice(t *testing.T) {
	t.Parallel()
	tests := []struct {
		desc   string
		slice1 []string
		slice2 []string
		want   []string
	}{
		{
			desc:   "both slices nil",
			slice1: nil,
			slice2: nil,
			want:   []string{},
		},
		{
			desc:   "first slice nil",
			slice1: nil,
			slice2: []string{"a", "b", "c"},
			want:   []string{"a", "b", "c"},
		},
		{
			desc:   "second slice nil",
			slice1: []string{"x", "y", "z"},
			slice2: nil,
			want:   []string{"x", "y", "z"},
		},
		{
			desc:   "duplicate elements",
			slice1: []string{"a", "b", "a", "c"},
			slice2: []string{"b", "c", "c", "d"},
			want:   []string{"a", "b", "c", "d"},
		},
		{
			desc:   "empty slices",
			slice1: []string{},
			slice2: []string{},
			want:   []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			result := UnionStringSlice(tt.slice1, tt.slice2)
			require.ElementsMatch(t, tt.want, result)
		})
	}
}

func TestIsStringsOverlap(t *testing.T) {
	tests := []struct {
		name string
		a    []string
		b    []string
		want bool
	}{
		{
			name: "base",
			a:    []string{"a", "b", "c"},
			b:    []string{"a", "e", "f"},
			want: true,
		},
		{
			name: "baseWithBlank",
			a:    []string{"a", "b", "c"},
			b:    []string{" a", "e", "f"},
			want: false,
		},
		{
			name: "baseWithDiffString",
			a:    []string{"a", "b", "c"},
			b:    []string{"d", "e", "f"},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if ret := IsStringsOverlap(tt.a, tt.b); ret != tt.want {
				t.Errorf("got %v, want %v", ret, tt.want)
			}
		})
	}
}

func TestRemoveString(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    []string
		want []string
	}{
		{
			name: "base",
			a:    "a",
			b:    []string{"a", "b", "c"},
			want: []string{"b", "c"},
		},
		{
			name: "baseWithDiff",
			a:    "c",
			b:    []string{"a", "b"},
			want: []string{"a", "b"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if ret := RemoveString(tt.b, tt.a); !slices.Equal(ret, tt.want) {
				t.Errorf("got %v, want %v", ret, tt.want)
			}
		})
	}
}

func Test_DiffStringSlice(t *testing.T) {
	t.Parallel()
	tests := []struct {
		desc string
		s1   []string
		s2   []string
		diff []string
	}{
		{
			"slice1 is subset of slice2",
			[]string{"1", "2", "3"},
			[]string{"1", "2", "3", "4", "5"},
			[]string{"4", "5"},
		},
		{
			"slice2 is subset of slice1",
			[]string{"1", "2", "3", "4", "5"},
			[]string{"1", "2", "3"},
			[]string{"4", "5"},
		},
		{
			"slice1 is empty",
			nil,
			[]string{"1", "2", "3", "4", "5"},
			[]string{"1", "2", "3", "4", "5"},
		},
		{
			"slice2 is empty",
			[]string{"1", "2", "3", "4", "5"},
			nil,
			[]string{"1", "2", "3", "4", "5"},
		},
		{
			"slice1 and slice2 have intersection",
			[]string{"1", "2", "3"},
			[]string{"6", "7", "3", "1", "5"},
			[]string{"2", "5", "6", "7"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			diff := DiffStringSlice(tt.s1, tt.s2)
			require.ElementsMatch(t, tt.diff, diff)
		})
	}
}
