package util

import (
	"crypto/rand"
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

func CheckProtocol(address string) string {
	if strings.Contains(address, ",") && (strings.Count(address, "/") == 2) {
		return kubeovnv1.ProtocolDual
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
