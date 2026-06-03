package ipam

import (
	"bytes"
	"fmt"
	"math/big"
	"net"
	"strings"
)

type IP net.IP

func NewIP(s string) (IP, error) {
	ip := net.ParseIP(s)
	if ip == nil {
		return nil, fmt.Errorf("invalid IP address %q", s)
	}

	if !strings.ContainsRune(s, ':') {
		ip = ip.To4()
	}

	return IP(ip), nil
}

func (a IP) Clone() IP {
	v := make(IP, len(a))
	copy(v, a)
	return v
}

func (a IP) To4() net.IP {
	return net.IP(a).To4()
}

func (a IP) To16() net.IP {
	return net.IP(a).To16()
}

func (a IP) Equal(b IP) bool {
	return net.IP(a).Equal(net.IP(b))
}

// cmp compares a and b, returning -1, 0 or 1 if a is less than, equal to or
// greater than b. It is allocation-free and normalizes per address family so
// that IPv4 addresses compare as their 4-byte value regardless of whether they
// are stored in 4-byte or 16-byte (v4-in-v6) representation. This keeps the
// result consistent with Equal and avoids the big.Int allocations that the
// range engine would otherwise pay on every comparison.
func (a IP) cmp(b IP) int {
	if a4, b4 := net.IP(a).To4(), net.IP(b).To4(); a4 != nil && b4 != nil {
		return bytes.Compare(a4, b4)
	}
	return bytes.Compare(net.IP(a).To16(), net.IP(b).To16())
}

func (a IP) LessThan(b IP) bool {
	return a.cmp(b) < 0
}

func (a IP) GreaterThan(b IP) bool {
	return a.cmp(b) > 0
}

func bytes2IP(buff []byte, length int) IP {
	if len(buff) < length {
		tmp := make([]byte, length)
		copy(tmp[len(tmp)-len(buff):], buff)
		buff = tmp
	} else if len(buff) > length {
		buff = buff[len(buff)-length:]
	}
	return IP(buff)
}

func (a IP) Add(n int64) IP {
	if a.To4() != nil {
		return bytes2IP(big.NewInt(0).Add(big.NewInt(0).SetBytes([]byte(a.To4())), big.NewInt(n)).Bytes(), len(a.To4()))
	}
	return bytes2IP(big.NewInt(0).Add(big.NewInt(0).SetBytes([]byte(a.To16())), big.NewInt(n)).Bytes(), len(a.To16()))
}

func (a IP) Sub(n int64) IP {
	ipInt := big.NewInt(0).SetBytes(a.To16())
	ipInt.Sub(ipInt, big.NewInt(n))
	if ipInt.Sign() < 0 {
		ipInt.Add(ipInt, big.NewInt(0).Exp(big.NewInt(2), big.NewInt(128), nil))
	}

	if a.To4() != nil {
		return bytes2IP(ipInt.Bytes(), len(a.To4()))
	}
	return bytes2IP(ipInt.Bytes(), len(a.To16()))
}

func (a IP) String() string {
	return net.IP(a).String()
}
