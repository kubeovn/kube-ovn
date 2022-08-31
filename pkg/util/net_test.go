package util

import (
	"errors"
	"testing"
)

func TestCheckCIDRsAll(t *testing.T) {
	cases := []struct {
		name   string
		config []string
		expect error
	}{
		// for all check
		{"1v4", []string{"10.16.0.0/16"}, nil},
		{"v4", []string{"10.16.0.0/16", "10.96.0.0/12", "100.64.0.0/16"}, nil},
		{"dual", []string{"10.16.0.0/16,fd00:10:16::/64", "10.96.0.0/12,fd00:10:96::/112", "100.64.0.0/16,fd00:100:64::/64"}, nil},
		{"v6", []string{"fd00:10:16::/64", "fd00:10:96::/112", "fd00:100:64::/64"}, nil},
		{"169254", []string{"10.16.0.0/16", "10.96.0.0/12", "169.254.0.0/16"}, errors.New("")},
		{"0000", []string{"10.16.0.0/16", "10.96.0.0/12", "0.0.0.0/16"}, errors.New("")},
		{"127", []string{"10.16.0.0/16", "10.96.0.0/12", "127.127.0.0/16"}, errors.New("")},
		{"255", []string{"10.16.0.0/16", "10.96.0.0/12", "255.255.0.0/16"}, errors.New("")},
		{"ff80", []string{"10.16.0.0/16,ff80::/64", "10.96.0.0/12,fd00:10:96::/112", "100.64.0.0/16,fd00:100:64::/64"}, errors.New("")},
		// overlap only
		{"overlaperr", []string{"10.16.0.0/16", "10.96.0.0/12", "10.96.0.2/16"}, errors.New("")},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ans := CheckSystemCIDR(c.config)
			if c.expect == nil && ans != c.expect {
				t.Fatalf("%v expected %v, but %v got",
					c.config, c.expect, ans)
			} else if c.expect != nil && ans == nil {
				t.Fatalf("%v expected error, but %v got",
					c.config, ans)
			}
		})
	}
}

func TestCheckSupCIDROverlap(t *testing.T) {
	cases := []struct {
		name    string
		subnet1 string
		subnet2 string
		expect  bool
	}{
		// for all check
		{"v4", "10.16.0.0/16", "10.96.0.0/12", false},
		{"dual", "10.16.0.0/16,fd00:10:16::/64", "10.96.0.0/12,fd00:10:96::/112", false},
		{"v6", "fd00:10:16::/64", "fd00:10:96::/112", false},
		{"overlap", "10.96.0.0/12", "10.96.0.2/16", true},
		// overlap only, spec ignore
		{"spec", "10.16.0.0/16", "169.254.0.0/12", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ans := CIDROverlap(c.subnet1, c.subnet2)
			if ans != c.expect {
				t.Fatalf("%v and %v expected %v, but %v got",
					c.subnet1, c.subnet2, c.expect, ans)
			}
		})
	}
}

func TestCheckCIDRSpec(t *testing.T) {
	cases := []struct {
		name   string
		subnet string
		expect error
	}{
		// for all check
		{"1v4", "10.16.0.0/16", nil},
		{"dual", "10.16.0.0/16,fd00:10:16::/64", nil},
		{"v6", "fd00:10:16::/64", nil},
		{"169254", "169.254.0.0/16", errors.New("")},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ans := CIDRGlobalUnicast(c.subnet)
			if c.expect == nil && ans != c.expect {
				t.Fatalf("%v expected %v, but %v got",
					c.subnet, c.expect, ans)
			} else if c.expect != nil && ans == nil {
				t.Fatalf("%v expected error, but %v got",
					c.subnet, ans)
			}
		})
	}
}
