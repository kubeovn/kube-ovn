package ipam

import (
	"math/big"
	"net"
	"reflect"
	"testing"

	"github.com/kubeovn/kube-ovn/pkg/types"
)

func TestNewIPRange(t *testing.T) {
	start := IP(net.ParseIP("192.168.1.1"))
	end := IP(net.ParseIP("192.168.1.10"))
	r := NewIPRange(start, end)

	if !r.Start().Equal(start) {
		t.Errorf("NewIPRange() start = %v, want %v", r.Start(), start)
	}
	if !r.End().Equal(end) {
		t.Errorf("NewIPRange() end = %v, want %v", r.End(), end)
	}
}

func TestNewIPRangeFromCIDR(t *testing.T) {
	_, ipnet, err := net.ParseCIDR("192.168.1.0/24")
	if err != nil {
		t.Fatalf("ParseCIDR failed: %v", err)
	}

	r := NewIPRangeFromCIDR(*ipnet)
	start := IP(net.ParseIP("192.168.1.0"))
	end := IP(net.ParseIP("192.168.1.255"))

	if !r.Start().Equal(start) {
		t.Errorf("NewIPRangeFromCIDR() start = %v, want %v", r.Start(), start)
	}
	if !r.End().Equal(end) {
		t.Errorf("NewIPRangeFromCIDR() end = %v, want %v", r.End(), end)
	}
}

func TestIPRangeClone(t *testing.T) {
	start := IP(net.ParseIP("192.168.1.1"))
	end := IP(net.ParseIP("192.168.1.10"))
	r := NewIPRange(start, end)
	clone := r.Clone()

	if !clone.Start().Equal(start) || !clone.End().Equal(end) {
		t.Errorf("Clone() = %v-%v, want %v-%v", clone.Start(), clone.End(), start, end)
	}

	clone.SetStart(IP(net.ParseIP("10.0.0.1")))
	clone.SetEnd(IP(net.ParseIP("10.0.0.10")))
	if r.Start().Equal(clone.Start()) {
		t.Errorf("Clone() should create a new copy, but it modified the original range start")
	}
	if r.Start().Equal(clone.End()) {
		t.Errorf("Clone() should create a new copy, but it modified the original range end")
	}
}

func TestIPRangeCount(t *testing.T) {
	tests := []struct {
		name  string
		start IP
		end   IP
		want  types.BigInt
	}{
		{
			name:  "IPv4 range",
			start: IP(net.ParseIP("192.168.1.1")),
			end:   IP(net.ParseIP("192.168.1.10")),
			want:  types.BigInt{Int: *big.NewInt(10)},
		},
		{
			name:  "IPv6 range",
			start: IP(net.ParseIP("2001:db8::1")),
			end:   IP(net.ParseIP("2001:db8::10")),
			want:  types.BigInt{Int: *big.NewInt(16)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewIPRange(tt.start, tt.end)
			got := r.Count()
			if got.Cmp(tt.want) != 0 {
				t.Errorf("Count() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIPRangeRandom(t *testing.T) {
	start := IP(net.ParseIP("192.168.1.1"))
	end := IP(net.ParseIP("192.168.1.10"))
	r := NewIPRange(start, end)

	ip := r.Random()
	if !r.Contains(ip) {
		t.Errorf("Random() = %v, not in range %v-%v", ip, start, end)
	}
}

func TestIPRangeContains(t *testing.T) {
	start := IP(net.ParseIP("192.168.1.1"))
	end := IP(net.ParseIP("192.168.1.10"))
	r := NewIPRange(start, end)

	tests := []struct {
		name string
		ip   IP
		want bool
	}{
		{
			name: "IP in range",
			ip:   IP(net.ParseIP("192.168.1.5")),
			want: true,
		},
		{
			name: "IP before range",
			ip:   IP(net.ParseIP("192.168.1.0")),
			want: false,
		},
		{
			name: "IP after range",
			ip:   IP(net.ParseIP("192.168.1.11")),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := r.Contains(tt.ip); got != tt.want {
				t.Errorf("Contains(%v) = %v, want %v", tt.ip, got, tt.want)
			}
		})
	}
}

func TestIPRangeAdd(t *testing.T) {
	start := IP(net.ParseIP("192.168.1.1"))
	end := IP(net.ParseIP("192.168.1.10"))
	r := NewIPRange(start, end)

	tests := []struct {
		name string
		ip   IP
		want bool
	}{
		{
			name: "Add IP before range",
			ip:   IP(net.ParseIP("192.168.1.0")),
			want: true,
		},
		{
			name: "Add IP after range",
			ip:   IP(net.ParseIP("192.168.1.11")),
			want: true,
		},
		{
			name: "Add IP in range",
			ip:   IP(net.ParseIP("192.168.1.5")),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := r.Add(tt.ip); got != tt.want {
				t.Errorf("Add(%v) = %v, want %v", tt.ip, got, tt.want)
			}
		})
	}
}

func TestIPRangeRemove(t *testing.T) {
	start := IP(net.ParseIP("192.168.1.1").To4())
	end := IP(net.ParseIP("192.168.1.10").To4())
	r := NewIPRange(start, end)

	tests := []struct {
		name       string
		ip         IP
		wantRanges []*IPRange
		wantOk     bool
	}{
		{
			name: "Remove IP in range",
			ip:   IP(net.ParseIP("192.168.1.5").To4()),
			wantRanges: []*IPRange{
				NewIPRange(start, IP(net.ParseIP("192.168.1.4").To4())),
				NewIPRange(IP(net.ParseIP("192.168.1.6").To4()), end),
			},
			wantOk: true,
		},
		{
			name:       "Remove IP left boundary",
			ip:         IP(net.ParseIP("192.168.1.1").To4()),
			wantRanges: []*IPRange{NewIPRange(IP(net.ParseIP("192.168.1.2").To4()), end)},
			wantOk:     true,
		},
		{
			name:       "Remove IP right boundary",
			ip:         IP(net.ParseIP("192.168.1.10").To4()),
			wantRanges: []*IPRange{NewIPRange(start, IP(net.ParseIP("192.168.1.9").To4()))},
			wantOk:     true,
		},
		{
			name:       "Remove IP that is not included",
			ip:         IP(net.ParseIP("192.168.1.254").To4()),
			wantRanges: nil,
			wantOk:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := r.Remove(tt.ip)
			if ok != tt.wantOk {
				t.Errorf("Remove(%v) ok = %v, want %v", tt.ip, ok, tt.wantOk)
			}
			if !reflect.DeepEqual(got, tt.wantRanges) {
				t.Errorf("Remove(%v) ranges = %v, want %v", tt.ip, got, tt.wantRanges)
			}
		})
	}
}

func TestIPRangeString(t *testing.T) {
	tests := []struct {
		name string
		r    *IPRange
		want string
	}{
		{
			name: "Single IP range",
			r:    NewIPRange(IP(net.ParseIP("192.168.1.1")), IP(net.ParseIP("192.168.1.1"))),
			want: "192.168.1.1",
		},
		{
			name: "IP range",
			r:    NewIPRange(IP(net.ParseIP("192.168.1.1")), IP(net.ParseIP("192.168.1.10"))),
			want: "192.168.1.1-192.168.1.10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.r.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}
