package ipam

import (
	"net"
	"testing"
)

func TestNewIP(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    IP
		wantErr bool
	}{
		{
			name:    "valid IPv4 address",
			input:   "192.168.1.1",
			want:    IP(net.ParseIP("192.168.1.1").To4()),
			wantErr: false,
		},
		{
			name:    "valid IPv6 address",
			input:   "2001:db8::1",
			want:    IP(net.ParseIP("2001:db8::1")),
			wantErr: false,
		},
		{
			name:    "invalid IP address",
			input:   "invalid",
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewIP(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewIP(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !got.Equal(tt.want) {
				t.Errorf("NewIP(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIPClone(t *testing.T) {
	ip := IP(net.ParseIP("192.168.1.1"))
	clone := ip.Clone()

	if !clone.Equal(ip) {
		t.Errorf("Clone() = %v, want %v", clone, ip)
	}

	clone[0] = 10
	if clone.Equal(ip) {
		t.Errorf("Clone() should create a new copy, but it modified the original IP")
	}
}

func TestIPLessThan(t *testing.T) {
	tests := []struct {
		name string
		a    IP
		b    IP
		want bool
	}{
		{
			name: "IPv4 less than",
			a:    IP(net.ParseIP("192.168.1.1")),
			b:    IP(net.ParseIP("192.168.1.2")),
			want: true,
		},
		{
			name: "IPv6 less than",
			a:    IP(net.ParseIP("2001:db8::1")),
			b:    IP(net.ParseIP("2001:db8::2")),
			want: true,
		},
		{
			name: "equal IPs",
			a:    IP(net.ParseIP("192.168.1.1")),
			b:    IP(net.ParseIP("192.168.1.1")),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.LessThan(tt.b); got != tt.want {
				t.Errorf("%v.LessThan(%v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestIPGreaterThan(t *testing.T) {
	tests := []struct {
		name string
		a    IP
		b    IP
		want bool
	}{
		{
			name: "IPv4 greater than",
			a:    IP(net.ParseIP("192.168.1.2")),
			b:    IP(net.ParseIP("192.168.1.1")),
			want: true,
		},
		{
			name: "IPv6 greater than",
			a:    IP(net.ParseIP("2001:db8::2")),
			b:    IP(net.ParseIP("2001:db8::1")),
			want: true,
		},
		{
			name: "equal IPs",
			a:    IP(net.ParseIP("192.168.1.1")),
			b:    IP(net.ParseIP("192.168.1.1")),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.GreaterThan(tt.b); got != tt.want {
				t.Errorf("%v.GreaterThan(%v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestIPAdd(t *testing.T) {
	tests := []struct {
		name string
		a    IP
		n    int64
		want IP
	}{
		{
			name: "IPv4 add",
			a:    IP(net.ParseIP("192.168.1.1")),
			n:    1,
			want: IP(net.ParseIP("192.168.1.2")),
		},
		{
			name: "IPv4 add 2",
			a:    IP(net.ParseIP("192.168.1.1")),
			n:    10,
			want: IP(net.ParseIP("192.168.1.11")),
		},
		{
			name: "IPv6 add",
			a:    IP(net.ParseIP("2001:db8::1")),
			n:    1,
			want: IP(net.ParseIP("2001:db8::2")),
		},
		{
			name: "IPv6 add 2",
			a:    IP(net.ParseIP("1:db8::1")),
			n:    1,
			want: IP(net.ParseIP("1:db8::2")),
		},
		{
			name: "IPv4 add overflow",
			a:    IP(net.ParseIP("255.255.255.255")),
			n:    1,
			want: IP(net.ParseIP("0.0.0.0")),
		},
		{
			name: "IPv4 add overflow 2",
			a:    IP(net.ParseIP("255.255.255.254")),
			n:    2,
			want: IP(net.ParseIP("0.0.0.0")),
		},
		{
			name: "IPv4 add overflow 3",
			a:    IP(net.ParseIP("255.255.255.254")),
			n:    3,
			want: IP(net.ParseIP("0.0.0.1")),
		},
		{
			name: "IPv6 add overflow",
			a:    IP(net.ParseIP("ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff")),
			n:    1,
			want: IP(net.ParseIP("::")),
		},
		{
			name: "IPv6 add overflow 2",
			a:    IP(net.ParseIP("ffff:ffff:ffff:ffff:ffff:ffff:ffff:fffe")),
			n:    2,
			want: IP(net.ParseIP("::")),
		},
		{
			name: "IPv6 add overflow 3",
			a:    IP(net.ParseIP("ffff:ffff:ffff:ffff:ffff:ffff:ffff:fffe")),
			n:    3,
			want: IP(net.ParseIP("::1")),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.Add(tt.n); !got.Equal(tt.want) {
				t.Errorf("%v.Add(%d) = %v, want %v", tt.a, tt.n, got, tt.want)
			}
		})
	}
}

func TestIPSub(t *testing.T) {
	tests := []struct {
		name string
		a    IP
		n    int64
		want IP
	}{
		{
			name: "IPv4 sub",
			a:    IP(net.ParseIP("192.168.1.2")),
			n:    1,
			want: IP(net.ParseIP("192.168.1.1")),
		},
		{
			name: "IPv6 sub",
			a:    IP(net.ParseIP("2001:db8::2")),
			n:    1,
			want: IP(net.ParseIP("2001:db8::1")),
		},
		{
			name: "IPv4 sub underflow",
			a:    IP(net.ParseIP("0.0.0.0")),
			n:    1,
			want: IP(net.ParseIP("255.255.255.255")),
		},
		{
			name: "IPv4 sub underflow 2",
			a:    IP(net.ParseIP("0.0.0.1")),
			n:    2,
			want: IP(net.ParseIP("255.255.255.255")),
		},
		{
			name: "IPv4 sub underflow 3",
			a:    IP(net.ParseIP("0.0.0.1")),
			n:    3,
			want: IP(net.ParseIP("255.255.255.254")),
		},
		{
			name: "IPv6 sub underflow",
			a:    IP(net.ParseIP("::")),
			n:    1,
			want: IP(net.ParseIP("ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff")),
		},
		{
			name: "IPv6 sub underflow 2",
			a:    IP(net.ParseIP("::1")),
			n:    2,
			want: IP(net.ParseIP("ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff")),
		},
		{
			name: "IPv6 sub underflow 3",
			a:    IP(net.ParseIP("::1")),
			n:    3,
			want: IP(net.ParseIP("ffff:ffff:ffff:ffff:ffff:ffff:ffff:fffe")),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.Sub(tt.n); !got.Equal(tt.want) {
				t.Errorf("%v.Sub(%d) = %v, want %v", tt.a, tt.n, got, tt.want)
			}
		})
	}
}

func TestBytes2IP(t *testing.T) {
	tests := []struct {
		name   string
		buff   []byte
		length int
		want   IP
	}{
		{
			name:   "valid IPv4 address",
			buff:   []byte{192, 168, 1, 1},
			length: 4,
			want:   IP(net.ParseIP("192.168.1.1").To4()),
		},
		{
			name:   "valid IPv6 address",
			buff:   []byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
			length: 16,
			want:   IP(net.ParseIP("2001:db8::1")),
		},
		{
			name:   "buffer shorter than length",
			buff:   []byte{192, 168, 1},
			length: 4,
			want:   IP(net.ParseIP("0.192.168.1").To4()),
		},
		{
			name:   "buffer longer than length",
			buff:   []byte{192, 168, 1, 1, 2, 3, 4},
			length: 4,
			want:   IP(net.ParseIP("1.2.3.4").To4()),
		},
		{
			name:   "empty buffer",
			buff:   []byte{},
			length: 4,
			want:   IP(net.ParseIP("0.0.0.0").To4()),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := bytes2IP(tt.buff, tt.length)
			if !got.Equal(tt.want) {
				t.Errorf("bytes2IP(%v, %d) = %v, want %v", tt.buff, tt.length, got, tt.want)
			}
		})
	}
}

func TestIPTo4(t *testing.T) {
	tests := []struct {
		name string
		ip   IP
		want net.IP
	}{
		{
			name: "IPv4 address",
			ip:   IP(net.ParseIP("192.168.1.1")),
			want: net.ParseIP("192.168.1.1").To4(),
		},
		{
			name: "IPv6 address",
			ip:   IP(net.ParseIP("2001:db8::1")),
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.ip.To4()
			if !got.Equal(tt.want) {
				t.Errorf("%v.To4() = %v, want %v", tt.ip, got, tt.want)
			}
		})
	}
}

func TestIPTo16(t *testing.T) {
	tests := []struct {
		name string
		ip   IP
		want net.IP
	}{
		{
			name: "IPv4 address",
			ip:   IP(net.ParseIP("192.168.1.1").To4()),
			want: net.ParseIP("192.168.1.1"),
		},
		{
			name: "IPv6 address",
			ip:   IP(net.ParseIP("2001:db8::1")),
			want: net.ParseIP("2001:db8::1"),
		},
		{
			name: "nil IP",
			ip:   nil,
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.ip.To16()
			if !got.Equal(tt.want) {
				t.Errorf("IP.To16() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIPString(t *testing.T) {
	tests := []struct {
		name string
		ip   IP
		want string
	}{
		{
			name: "IPv4 address",
			ip:   IP(net.ParseIP("192.168.1.1")),
			want: "192.168.1.1",
		},
		{
			name: "IPv6 address",
			ip:   IP(net.ParseIP("2001:db8::1")),
			want: "2001:db8::1",
		},
		{
			name: "nil IP",
			ip:   nil,
			want: "<nil>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.ip.String(); got != tt.want {
				t.Errorf("IP.String() = %q, want %q", got, tt.want)
			}
		})
	}
}
