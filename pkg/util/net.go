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
	for _, cidra := range strings.Split(a, ",") {
		for _, cidrb := range strings.Split(b, ",") {
			if CheckProtocol(cidra) != CheckProtocol(cidrb) {
				continue
			}
			aIp, aIpNet, aErr := net.ParseCIDR(cidra)
			bIp, bIpNet, bErr := net.ParseCIDR(cidrb)
			if aErr != nil || bErr != nil {
				return false
			}
			if aIpNet.Contains(bIp) || bIpNet.Contains(aIp) {
				return true
			}
		}
	}
	return false
}

func CIDRContainIP(cidrStr, ipStr string) bool {
	var containFlag bool
	for _, cidr := range strings.Split(cidrStr, ",") {
		_, cidrNet, err := net.ParseCIDR(cidr)
		if err != nil {
			return false
		}

		for _, ip := range strings.Split(ipStr, ",") {
			if CheckProtocol(cidr) != CheckProtocol(ip) {
				continue
			}
			ipAddr := net.ParseIP(ip)
			if ipAddr == nil {
				return false
			}

			if cidrNet.Contains(ipAddr) {
				containFlag = true
			} else {
				containFlag = false
			}
		}
	}
	// v4 and v6 address should be both matched for dualstack check
	return containFlag
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

func IPToString(ip string) string {
	ipNet, _, err := net.ParseCIDR(ip)
	if err == nil {
		return ipNet.String()
	}
	ipNet = net.ParseIP(ip)
	if ipNet != nil {
		return ipNet.String()
	}
	return ""
}

func IsValidIP(ip string) bool {
	return net.ParseIP(ip) != nil
}

func CheckCidrs(cidr string) error {
	for _, cidrBlock := range strings.Split(cidr, ",") {
		if _, _, err := net.ParseCIDR(cidrBlock); err != nil {
			return errors.New("CIDRInvalid")
		}
	}
	return nil
}

func GetGwByCidr(cidrStr string) (string, error) {
	var gws []string
	for _, cidr := range strings.Split(cidrStr, ",") {
		gw, err := FirstSubnetIP(cidr)
		if err != nil {
			return "", err
		}
		gws = append(gws, gw)
	}

	return strings.Join(gws, ","), nil
}

func AppendGwByCidr(gateway, cidrStr string) (string, error) {
	var gws []string
	for _, cidr := range strings.Split(cidrStr, ",") {
		if CheckProtocol(gateway) == CheckProtocol(cidr) {
			gws = append(gws, gateway)
			continue
		} else {
			gw, err := FirstSubnetIP(cidr)
			if err != nil {
				return "", err
			}
			var gwArray [2]string
			if CheckProtocol(gateway) == kubeovnv1.ProtocolIPv4 {
				gwArray[0] = gateway
				gwArray[1] = gw
			} else {
				gwArray[0] = gw
				gwArray[1] = gateway
			}
			gws = gwArray[:]
		}
	}

	return strings.Join(gws, ","), nil
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

func GetStringIP(v4IP, v6IP string) string {
	var ipStr string
	if IsValidIP(v4IP) && IsValidIP(v6IP) {
		ipStr = v4IP + "," + v6IP
	} else if IsValidIP(v4IP) {
		ipStr = v4IP
	} else if IsValidIP(v6IP) {
		ipStr = v6IP
	}
	return ipStr
}

func GetIpAddrWithMask(ip, cidr string) string {
	var ipAddr string
	if CheckProtocol(cidr) == kubeovnv1.ProtocolDual {
		cidrBlocks := strings.Split(cidr, ",")
		ips := strings.Split(ip, ",")
		v4IP := fmt.Sprintf("%s/%s", ips[0], strings.Split(cidrBlocks[0], "/")[1])
		v6IP := fmt.Sprintf("%s/%s", ips[1], strings.Split(cidrBlocks[1], "/")[1])
		ipAddr = v4IP + "," + v6IP
	} else {
		ipAddr = fmt.Sprintf("%s/%s", ip, strings.Split(cidr, "/")[1])
	}
	return ipAddr
}

func GetIpWithoutMask(ipStr string) string {
	var ips []string
	for _, ip := range strings.Split(ipStr, ",") {
		ips = append(ips, strings.Split(ip, "/")[0])
	}
	return strings.Join(ips, ",")
}

func SplitStringIP(ipStr string) (string, string) {
	var v4IP, v6IP string
	if CheckProtocol(ipStr) == kubeovnv1.ProtocolDual {
		v4IP = strings.Split(ipStr, ",")[0]
		v6IP = strings.Split(ipStr, ",")[1]
	} else if CheckProtocol(ipStr) == kubeovnv1.ProtocolIPv4 {
		v4IP = ipStr
	} else if CheckProtocol(ipStr) == kubeovnv1.ProtocolIPv6 {
		v6IP = ipStr
	}

	return v4IP, v6IP
}

// How to distinguish repeat values
func ExpandExcludeIPs(excludeIPs []string, cidr string) []string {
	rv := []string{}
	for _, excludeIP := range excludeIPs {
		if strings.Contains(excludeIP, "..") {
			for _, cidrBlock := range strings.Split(cidr, ",") {
				subnetNum := SubnetNumber(cidrBlock)
				broadcast := SubnetBroadCast(cidrBlock)
				parts := strings.Split(excludeIP, "..")
				s := Ip2BigInt(parts[0])
				e := Ip2BigInt(parts[1])

				// limit range in cidr
				firstIP, _ := FirstSubnetIP(cidrBlock)
				lastIP, _ := LastIP(cidrBlock)
				if s.Cmp(Ip2BigInt(firstIP)) < 0 {
					s = Ip2BigInt(firstIP)
				}
				if e.Cmp(Ip2BigInt(lastIP)) > 0 {
					e = Ip2BigInt(lastIP)
				}

				changed := false
				// exclude cidr and broadcast address
				if ContainsIPs(excludeIP, subnetNum) {
					v := Ip2BigInt(subnetNum)
					if s.Cmp(v) == 0 {
						s.Add(s, big.NewInt(1))
						rv = append(rv, BigInt2Ip(s)+".."+BigInt2Ip(e))
					} else if e.Cmp(v) == 0 {
						e.Sub(e, big.NewInt(1))
						rv = append(rv, BigInt2Ip(s)+".."+BigInt2Ip(e))
					} else {
						var low, high big.Int
						lowp := (&low).Sub(v, big.NewInt(1))
						highp := (&high).Add(v, big.NewInt(1))
						rv = append(rv, BigInt2Ip(s)+".."+BigInt2Ip(lowp))
						rv = append(rv, BigInt2Ip(highp)+".."+BigInt2Ip(e))
					}
					changed = true
				}
				if ContainsIPs(excludeIP, broadcast) {
					v := Ip2BigInt(broadcast)
					v.Sub(v, big.NewInt(1))
					rv = append(rv, BigInt2Ip(s)+".."+BigInt2Ip(v))
					changed = true
				}
				if !changed && s.Cmp(e) < 0 {
					rv = append(rv, BigInt2Ip(s)+".."+BigInt2Ip(e))
				}
			}
		} else {
			rv = append(rv, excludeIP)
		}
	}
	klog.V(3).Infof("expand exclude ips %v", rv)
	return rv
}

func ContainsIPs(excludeIP string, ip string) bool {
	if strings.Contains(excludeIP, "..") {
		parts := strings.Split(excludeIP, "..")
		s := Ip2BigInt(parts[0])
		e := Ip2BigInt(parts[1])
		ipv := Ip2BigInt(ip)
		if s.Cmp(ipv) <= 0 && e.Cmp(ipv) >= 0 {
			return true
		}
	} else {
		if excludeIP == ip {
			return true
		}
	}
	return false
}

func CountIpNums(excludeIPs []string) int64 {
	var count int64
	for _, excludeIP := range excludeIPs {
		if strings.Contains(excludeIP, "..") {
			var val big.Int
			parts := strings.Split(excludeIP, "..")
			s := Ip2BigInt(parts[0])
			e := Ip2BigInt(parts[1])
			count = val.Add(val.Sub(e, s), big.NewInt(1)).Int64()
		} else {
			count++
		}
	}
	return count
}
