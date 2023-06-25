package ipam

import (
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

func (a IP) To4() net.IP {
	return net.IP(a).To4()
}

func (a IP) To16() net.IP {
	return net.IP(a).To16()
}

func (a IP) Equal(b IP) bool {
	return net.IP(a).Equal(net.IP(b))
}

func (a IP) LessThan(b IP) bool {
	return big.NewInt(0).SetBytes([]byte(a)).Cmp(big.NewInt(0).SetBytes([]byte(b))) < 0
}

func (a IP) GreaterThan(b IP) bool {
	return big.NewInt(0).SetBytes([]byte(a)).Cmp(big.NewInt(0).SetBytes([]byte(b))) > 0
}

func (a IP) Add(n int64) IP {
	buff := big.NewInt(0).Add(big.NewInt(0).SetBytes([]byte(a)), big.NewInt(n)).Bytes()
	if len(buff) < len(a) {
		tmp := make([]byte, len(a))
		copy(tmp[len(tmp)-len(buff):], buff)
		buff = tmp
	} else if len(buff) > len(a) {
		buff = buff[len(buff)-len(a):]
	}
	return IP(buff)
}

func (a IP) Sub(n int64) IP {
	buff := big.NewInt(0).Sub(big.NewInt(0).SetBytes([]byte(a)), big.NewInt(n)).Bytes()
	if len(buff) < len(a) {
		tmp := make([]byte, len(a))
		copy(tmp[len(tmp)-len(buff):], buff)
		buff = tmp
	} else if len(buff) > len(a) {
		buff = buff[len(buff)-len(a):]
	}
	return IP(buff)
}

func (a IP) String() string {
	return net.IP(a).String()
}
