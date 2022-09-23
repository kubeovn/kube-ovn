package util

import (
	"errors"
	"net"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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
		{"overlapped", []string{"10.16.0.0/16", "10.96.0.0/12", "10.96.0.2/16"}, errors.New("")},
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

func TestUTNet(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Kube-OVN utils/net unit test Suite")
}

var _ = Describe("[Net]", func() {
	It("AddressCount", func() {
		args := []*net.IPNet{
			{IP: net.ParseIP("10.0.0.0"), Mask: net.CIDRMask(32, 32)},
			{IP: net.ParseIP("10.0.0.0"), Mask: net.CIDRMask(31, 32)},
			{IP: net.ParseIP("10.0.0.0"), Mask: net.CIDRMask(30, 32)},
			{IP: net.ParseIP("10.0.0.0"), Mask: net.CIDRMask(24, 32)},
		}
		wants := []float64{
			0,
			0,
			2,
			254,
		}
		Expect(len(args)).To(Equal(len(args)))

		for i := range args {
			Expect(args[i].IP).NotTo(BeNil())
			Expect(AddressCount(args[i])).To(Equal(wants[i]))
		}
	})

	It("CountIpNums", func() {
		args := [][]string{
			{"10.0.0.101"},
			{"10.0.0.101..10.0.0.105"},
			{"10.0.0.101..10.0.0.105", "10.0.0.111..10.0.0.120"},
		}
		wants := []float64{
			1,
			5,
			15,
		}
		Expect(len(args)).To(Equal(len(args)))

		for i := range args {
			Expect(CountIpNums(args[i])).To(Equal(wants[i]))
		}
	})

	It("ExpandExcludeIPs", func() {
		type arg struct {
			cidr       string
			excludeIps []string
		}

		args := []arg{
			{"10.0.1.0/24", []string{"10.0.0.255"}},
			{"10.0.1.0/24", []string{"10.0.1.0"}},
			{"10.0.1.0/24", []string{"10.0.1.1"}},
			{"10.0.1.0/24", []string{"10.0.1.254"}},
			{"10.0.1.0/24", []string{"10.0.1.255"}},
			{"10.0.1.0/24", []string{"10.0.2.0"}},
			{"10.0.1.0/24", []string{"10.0.1.101..10.0.1.105"}},
			{"10.0.1.0/24", []string{"10.0.0.101..10.0.1.105"}},
			{"10.0.1.0/24", []string{"10.0.1.101..10.0.2.105"}},
			{"10.0.1.0/24", []string{"10.0.1.101..10.0.1.101"}},
			{"10.0.1.0/24", []string{"10.0.1.105..10.0.1.101"}},
			{"10.0.1.0/24", []string{"10.0.1.1", "10.0.1.101"}},
			{"10.0.1.0/24", []string{"10.0.1.1", "10.0.1.101..10.0.1.105"}},
			{"10.0.1.0/24", []string{"10.0.1.1", "10.0.1.101..10.0.1.105", "10.0.1.111..10.0.1.120"}},
			{"10.0.1.0/30", []string{"10.0.1.1", "179.17.0.0..179.17.0.10"}},
			{"10.0.1.0/31", []string{"10.0.1.1", "179.17.0.0..179.17.0.10"}},
			{"10.0.1.0/32", []string{"10.0.1.1", "179.17.0.0..179.17.0.10"}},

			{"fe00::100/120", []string{"fe00::ff"}},
			{"fe00::100/120", []string{"fe00::100"}},
			{"fe00::100/120", []string{"fe00::101"}},
			{"fe00::100/120", []string{"fe00::1fe"}},
			{"fe00::100/120", []string{"fe00::1ff"}},
			{"fe00::100/120", []string{"fe00::200"}},
			{"fe00::100/120", []string{"fe00::1a1..fe00::1a5"}},
			{"fe00::100/120", []string{"fe00::a1..fe00::1a5"}},
			{"fe00::100/120", []string{"fe00::1a1..fe00::2a5"}},
			{"fe00::100/120", []string{"fe00::1a1..fe00::1a1"}},
			{"fe00::100/120", []string{"fe00::1a5..fe00::1a1"}},
			{"fe00::100/120", []string{"fe00::101", "fe00::1a1"}},
			{"fe00::100/120", []string{"fe00::101", "fe00::1a1..fe00::1a5"}},
			{"fe00::100/120", []string{"fe00::101", "fe00::1a1..fe00::1a5", "fe00::1b1..fe00::1c0"}},
			{"fe00::100/126", []string{"fe00::101", "feff::..feff::a"}},
			{"fe00::100/127", []string{"fe00::101", "feff::..feff::a"}},
			{"fe00::100/128", []string{"fe00::101", "feff::..feff::a"}},

			{"10.0.1.0/24,fe00::100/120", []string{"10.0.1.1", "10.0.1.101..10.0.1.105"}},
			{"10.0.1.0/24,fe00::100/120", []string{"fe00::101", "fe00::1a1..fe00::1a5"}},
			{"10.0.1.0/24,fe00::100/120", []string{"10.0.1.1", "10.0.1.101..10.0.1.105", "fe00::101", "fe00::1a1..fe00::1a5"}},
		}
		wants := [][]string{
			{},
			{},
			{"10.0.1.1"},
			{"10.0.1.254"},
			{},
			{},
			{"10.0.1.101..10.0.1.105"},
			{"10.0.1.1..10.0.1.105"},
			{"10.0.1.101..10.0.1.254"},
			{"10.0.1.101"},
			{},
			{"10.0.1.1", "10.0.1.101"},
			{"10.0.1.1", "10.0.1.101..10.0.1.105"},
			{"10.0.1.1", "10.0.1.101..10.0.1.105", "10.0.1.111..10.0.1.120"},
			{"10.0.1.1"},
			{},
			{},

			{},
			{},
			{"fe00::101"},
			{"fe00::1fe"},
			{},
			{},
			{"fe00::1a1..fe00::1a5"},
			{"fe00::101..fe00::1a5"},
			{"fe00::1a1..fe00::1fe"},
			{"fe00::1a1"},
			{},
			{"fe00::101", "fe00::1a1"},
			{"fe00::101", "fe00::1a1..fe00::1a5"},
			{"fe00::101", "fe00::1a1..fe00::1a5", "fe00::1b1..fe00::1c0"},
			{"fe00::101"},
			{},
			{},

			{"10.0.1.1", "10.0.1.101..10.0.1.105"},
			{"fe00::101", "fe00::1a1..fe00::1a5"},
			{"10.0.1.1", "10.0.1.101..10.0.1.105", "fe00::101", "fe00::1a1..fe00::1a5"},
		}
		Expect(len(args)).To(Equal(len(args)))

		for i := range args {
			Expect(ExpandExcludeIPs(args[i].excludeIps, args[i].cidr)).To(Equal(wants[i]))
		}
	})
})
