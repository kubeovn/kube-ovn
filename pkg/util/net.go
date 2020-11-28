package util

import (
	"fmt"
	"math"
	"math/big"
	"math/rand"
	"net"
	"strings"
	"time"

	kubeovnv1 "github.com/alauda/kube-ovn/pkg/apis/kubeovn/v1"
)

// GenerateMac generates mac address.
/* #nosec */
func GenerateMac() string {
	prefix := "00:00:00"
	newRand := rand.New(rand.NewSource(time.Now().UnixNano()))
	mac := fmt.Sprintf("%s:%02X:%02X:%02X", prefix, newRand.Intn(255), newRand.Intn(255), newRand.Intn(255))
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
	ip := net.IP(ipInt.Bytes())
	return ip.String()
}

func FirstSubnetIP(cidrBlock kubeovnv1.DualStack) (kubeovnv1.DualStack, error) {
	ip := kubeovnv1.DualStack{}
	for protocol, subnet := range cidrBlock {
		_, cidr, err := net.ParseCIDR(subnet)
		if err != nil {
			return nil, fmt.Errorf("%s is not a valid cidr", subnet)
		}
		ipInt := Ip2BigInt(cidr.IP.String())
		ip[protocol] = BigInt2Ip(ipInt.Add(ipInt, big.NewInt(1)))
	}
	return ip, nil
}

func LastSubnetIP(cidrBlock kubeovnv1.DualStack) (kubeovnv1.DualStack, error) {
	ip := kubeovnv1.DualStack{}
	for protocol, c := range cidrBlock {
		_, cidr, err := net.ParseCIDR(c)
		if err != nil {
			return nil, fmt.Errorf("%s is not a valid cidr", c)
		}
		var length uint
		if CheckProtocol(c) == kubeovnv1.ProtocolIPv4 {
			length = 32
		} else {
			length = 128
		}
		maskLength, _ := cidr.Mask.Size()
		ipInt := Ip2BigInt(cidr.IP.String())
		size := big.NewInt(0).Lsh(big.NewInt(1), length-uint(maskLength))
		size = big.NewInt(0).Sub(size, big.NewInt(2))
		ip[protocol] = BigInt2Ip(ipInt.Add(ipInt, size))
	}
	return ip, nil
}

func CIDRConflict(a, b string) bool {
	aIp, aIpNet, aErr := net.ParseCIDR(a)
	bIp, bIpNet, bErr := net.ParseCIDR(b)
	if aErr != nil || bErr != nil {
		return false
	}
	return aIpNet.Contains(bIp) || bIpNet.Contains(aIp)
}

func SubnetConflict(cidrBlockA, cidrBlockB kubeovnv1.DualStack) bool {
	for protocol, a := range cidrBlockA {
		if CIDRConflict(a, cidrBlockB[protocol]) {
			return true
		}
	}
	return false
}

func CIDRContainIP(cidrStr, ipStr string) bool {
	_, cidr, err := net.ParseCIDR(cidrStr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	return cidr.Contains(ip)
}

func SubnetContainIp(cidrBlock kubeovnv1.DualStack, ipStr string) bool {
	for _, cidrStr := range cidrBlock {
		if CIDRContainIP(cidrStr, ipStr) {
			return true
		}
	}
	return false
}

// check more then one address protocol
func CheckProtocol(addresses ...string) kubeovnv1.Protocol {
	protoV4 := false
	protoV6 := false
	for _, address := range addresses {
		address = strings.Split(address, "/")[0]
		ip := net.ParseIP(address)
		if ip.To4() != nil {
			protoV4 = true
		} else {
			protoV6 = true
		}
	}

	if protoV4 && protoV6 {
		return kubeovnv1.ProtocolDual
	} else if protoV4 {
		return kubeovnv1.ProtocolIPv4
	} else {
		return kubeovnv1.ProtocolIPv6
	}
}

func CheckProtocolDual(dual kubeovnv1.DualStack) kubeovnv1.Protocol {
	protoV4 := false
	protoV6 := false
	for _, address := range dual {
		address = strings.Split(address, "/")[0]
		ip := net.ParseIP(address)
		if ip.To4() != nil {
			protoV4 = true
		} else {
			protoV6 = true
		}
	}

	if protoV4 && protoV6 {
		return kubeovnv1.ProtocolDual
	} else if protoV4 {
		return kubeovnv1.ProtocolIPv4
	} else {
		return kubeovnv1.ProtocolIPv6
	}
}

func AddressCount(network *net.IPNet) float64 {
	prefixLen, bits := network.Mask.Size()
	if bits-prefixLen < 2 {
		return 0
	}
	return math.Pow(2, float64(bits-prefixLen)) - 2
}

func DualStackToString(cidrBlock kubeovnv1.DualStack) string {
	var l []string
	for _, proto := range []kubeovnv1.Protocol{kubeovnv1.ProtocolIPv4, kubeovnv1.ProtocolIPv6} {
		if cidrBlock[proto] != "" {
			l = append(l, cidrBlock[proto])
		}
	}
	return strings.Join(l, ",")
}

func StringToDualStack(s string) (kubeovnv1.DualStack, error) {
	addresses := strings.Split(s, ",")
	if len(addresses) > 2 {
		return nil, fmt.Errorf("convert %v to dualstack fail; more then two addresses", addresses)
	}

	var (
		dualStack = kubeovnv1.DualStack{}
		protoV4   = false
		protoV6   = false
	)

	for _, address := range addresses {
		if address == "" {
			continue
		}
		ipStr := strings.Split(address, "/")[0]
		ip := net.ParseIP(ipStr)
		if ip.To4() != nil && protoV4 != true {
			dualStack[kubeovnv1.ProtocolIPv4] = address
			protoV4 = true
		} else if ip.To16() != nil && protoV6 != true {
			dualStack[kubeovnv1.ProtocolIPv6] = address
			protoV6 = true
		} else {
			return nil, fmt.Errorf("convert %v to dualstack fail: same ip family", s)
		}
	}
	return dualStack, nil
}

func DualStackListToString(cidrBlockList kubeovnv1.DualStackList) string {
	var l []string
	for _, proto := range []kubeovnv1.Protocol{kubeovnv1.ProtocolIPv4, kubeovnv1.ProtocolIPv6} {
		if len(cidrBlockList[proto]) > 0 {
			l = append(l, cidrBlockList[proto]...)
		}
	}
	return strings.Join(l, ",")
}

func StringToDualStackList(s string) (kubeovnv1.DualStackList, error) {
	var dualStackList = kubeovnv1.DualStackList{}

	for _, address := range strings.Split(s, ",") {
		if address == "" {
			continue
		}
		if strings.Contains(address, ":") {
			v6List, ok := dualStackList[kubeovnv1.ProtocolIPv6]
			if !ok {
				v6List = []string{}
			}
			dualStackList[kubeovnv1.ProtocolIPv6] = append(v6List, address)
		} else if strings.Contains(address, ".") {
			v4List, ok := dualStackList[kubeovnv1.ProtocolIPv4]
			if !ok {
				v4List = []string{}
			}
			dualStackList[kubeovnv1.ProtocolIPv4] = append(v4List, address)
		} else {
			return nil, fmt.Errorf("convert %v to dualstack list fail", s)
		}
	}
	return dualStackList, nil
}

func GenerateRouterGW(gateway, subnet kubeovnv1.DualStack) string {
	var gatewayList []string
	for proto, cidr := range subnet {
		mask := strings.Split(cidr, "/")[1]
		gatewayList = append(gatewayList, gateway[proto]+"/"+mask)
	}
	return strings.Join(gatewayList, " ")
}

