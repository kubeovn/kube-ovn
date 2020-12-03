package util

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math"
	"math/big"
	"net"
	"strconv"
	"strings"

	kubeovnv1 "github.com/alauda/kube-ovn/pkg/apis/kubeovn/v1"
	"k8s.io/klog"
)

// GenerateMac generates mac address.
func GenerateMac() string {
	prefix := "00:00:00"
	b := make([]byte, 3)
	_, err := rand.Read(b)
	if err != nil {
		klog.Errorf("generate mac error: %v", err)
	}

	mac := fmt.Sprintf("%s:%02X:%02X:%02X", prefix, b[0], b[1], b[2])
	return mac
}

func Ip2BigInt(ipStr string) *big.Int {
	ipBigInt := big.NewInt(0)
	if CheckProtocol(ipStr) == kubeovnv1.ProtocolIPv4 {
		ipBigInt.SetBytes(net.ParseIP(ipStr).To4())
	} else {
		ipBigInt.SetBytes(net.ParseIP(ipStr).To16())
	}
	return ipBigInt
}

func BigInt2Ip(ipInt *big.Int) string {
	buf := make([]byte, 4)
	if len(ipInt.Bytes()) > 4 {
		buf = make([]byte, 16)
	}
	ip := net.IP(ipInt.FillBytes(buf))
	return ip.String()
}

func SubnetNumber(subnet string) string {
	_, cidr, _ := net.ParseCIDR(subnet)
	return cidr.IP.String()
}

func SubnetBroadCast(subnet string) string {
	_, cidr, _ := net.ParseCIDR(subnet)
	var length uint
	if CheckProtocol(subnet) == kubeovnv1.ProtocolIPv4 {
		length = 32
	} else {
		length = 128
	}
	maskLength, _ := cidr.Mask.Size()
	ipInt := Ip2BigInt(cidr.IP.String())
	size := big.NewInt(0).Lsh(big.NewInt(1), length-uint(maskLength))
	size = big.NewInt(0).Sub(size, big.NewInt(1))
	return BigInt2Ip(ipInt.Add(ipInt, size))
}

func FirstSubnetIP(subnet string) (string, error) {
	_, cidr, err := net.ParseCIDR(subnet)
	if err != nil {
		return "", fmt.Errorf("%s is not a valid cidr", subnet)
	}
	ipInt := Ip2BigInt(cidr.IP.String())
	return BigInt2Ip(ipInt.Add(ipInt, big.NewInt(1))), nil
}

func LastIP(subnet string) (string, error) {
	_, cidr, err := net.ParseCIDR(subnet)
	if err != nil {
		return "", fmt.Errorf("%s is not a valid cidr", subnet)
	}
	var length uint
	if CheckProtocol(subnet) == kubeovnv1.ProtocolIPv4 {
		length = 32
	} else {
		length = 128
	}
	maskLength, _ := cidr.Mask.Size()
	ipInt := Ip2BigInt(cidr.IP.String())
	size := big.NewInt(0).Lsh(big.NewInt(1), length-uint(maskLength))
	size = big.NewInt(0).Sub(size, big.NewInt(2))
	return BigInt2Ip(ipInt.Add(ipInt, size)), nil
}

func CIDRConflict(a, b string) bool {
	aIp, aIpNet, aErr := net.ParseCIDR(a)
	bIp, bIpNet, bErr := net.ParseCIDR(b)
	if aErr != nil || bErr != nil {
		return false
	}
	return aIpNet.Contains(bIp) || bIpNet.Contains(aIp)
}

func CIDRContainIP(cidrStr, ipStr string) bool {
	protocol := CheckProtocol(cidrStr)
	if protocol == kubeovnv1.ProtocolDual {
		if _, _, err := CheckDualCidrs(cidrStr); err != nil {
			return false
		}
		cidrBlocks := strings.Split(cidrStr, ",")
		_, v4CIDR, _ := net.ParseCIDR(cidrBlocks[0])
		_, v6CIDR, _ := net.ParseCIDR(cidrBlocks[1])

		ips := strings.Split(ipStr, ",")
		if len(ips) == 2 {
			// The format of ipStr may be 10.244.0.0/16,fd00:10:244::/64 when protocol is dualstack
			if CheckProtocol(cidrBlocks[0]) != CheckProtocol(ips[0]) || CheckProtocol(cidrBlocks[1]) != CheckProtocol(ips[1]) {
				return false
			}

			v4IP := net.ParseIP(ips[0])
			v6IP := net.ParseIP(ips[1])
			if v4IP == nil || v6IP == nil {
				return false
			}
			return v4CIDR.Contains(v4IP) && v6CIDR.Contains(v6IP)
		} else {
			if CheckProtocol(cidrBlocks[0]) != CheckProtocol(ipStr) && CheckProtocol(cidrBlocks[1]) != CheckProtocol(ipStr) {
				return false
			}

			ip := net.ParseIP(ipStr)
			if ip == nil {
				return false
			}
			return v4CIDR.Contains(ip) || v6CIDR.Contains(ip)
		}
	} else {
		_, cidr, err := net.ParseCIDR(cidrStr)
		if err != nil {
			return false
		}
		if CheckProtocol(cidrStr) != CheckProtocol(ipStr) {
			return false
		}
		ip := net.ParseIP(ipStr)
		if ip == nil {
			return false
		}
		return cidr.Contains(ip)
	}
}

func CheckProtocol(address string) string {
	ips := strings.Split(address, ",")
	if len(ips) == 2 {
		v4IP := net.ParseIP(strings.Split(ips[0], "/")[0])
		v6IP := net.ParseIP(strings.Split(ips[1], "/")[0])
		if v4IP.To4() != nil && v6IP.To16() != nil {
			return kubeovnv1.ProtocolDual
		}
	}

	address = strings.Split(address, "/")[0]
	ip := net.ParseIP(address)
	if ip.To4() != nil {
		return kubeovnv1.ProtocolIPv4
	}
	return kubeovnv1.ProtocolIPv6
}

func AddressCount(network *net.IPNet) float64 {
	prefixLen, bits := network.Mask.Size()
	if bits-prefixLen < 2 {
		return 0
	}
	return math.Pow(2, float64(bits-prefixLen)) - 2
}

func GenerateRandomV4IP(cidr string) string {
	ip := strings.Split(cidr, "/")[0]
	netMask, _ := strconv.Atoi(strings.Split(cidr, "/")[1])
	hostNum := 32 - netMask
	add, err := rand.Int(rand.Reader, big.NewInt(1<<(uint(hostNum)-1)))
	if err != nil {
		klog.Fatalf("failed to generate random ip, %v", err)
	}
	t := big.NewInt(0).Add(Ip2BigInt(ip), add)
	return fmt.Sprintf("%s/%d", BigInt2Ip(t), netMask)
}

func IsValidIP(ip string) bool {
	if net.ParseIP(ip) != nil {
		return true
	}
	return false
}

func CheckDualCidrs(cidr string) (string, string, error) {
	cidrBlocks := strings.Split(cidr, ",")
	_, _, err := net.ParseCIDR(cidrBlocks[0])
	if err != nil {
		return "", "", errors.New("CIDRInvalid")
	}
	_, _, err = net.ParseCIDR(cidrBlocks[1])
	if err != nil {
		return "", "", errors.New("CIDRInvalid")
	}

	return cidrBlocks[0], cidrBlocks[1], nil
}

func ParseDualGw(cidr string) (string, error) {
	cidrBlocks := strings.Split(cidr, ",")
	v4gw, err := FirstSubnetIP(cidrBlocks[0])
	if err != nil {
		return "", err
	}
	v6gw, err := FirstSubnetIP(cidrBlocks[1])
	if err != nil {
		return "", err
	}
	return v4gw + "," + v6gw, nil
}

func SplitIpsByProtocol(excludeIps []string) ([]string, []string) {
	var v4ExcludeIps, v6ExcludeIps []string
	for _, ex := range excludeIps {
		ips := strings.Split(ex, "..")
		if len(ips) == 1 {
			if net.ParseIP(ips[0]).To4() != nil {
				v4ExcludeIps = append(v4ExcludeIps, ips[0])
			} else {
				v6ExcludeIps = append(v6ExcludeIps, ips[0])
			}
		} else {
			if net.ParseIP(ips[0]).To4() != nil {
				v4ExcludeIps = append(v4ExcludeIps, ex)
			} else {
				v6ExcludeIps = append(v6ExcludeIps, ex)
			}
		}
	}

	return v4ExcludeIps, v6ExcludeIps
}
