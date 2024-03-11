package util

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math"
	"math/big"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

// #nosec G101
const vpcExternalNet = "ovn-vpc-external-network"

const (
	IPv4Multicast        = "224.0.0.0/4"
	IPv4Loopback         = "127.0.0.1/8"
	IPv4Broadcast        = "255.255.255.255/32"
	IPv4Zero             = "0.0.0.0/32"
	IPv4LinkLocalUnicast = "169.254.0.0/16"

	IPv6Unspecified      = "::/128"
	IPv6Loopback         = "::1/128"
	IPv6Multicast        = "ff00::/8"
	IPv6LinkLocalUnicast = "FE80::/10"
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

func IP2BigInt(ipStr string) *big.Int {
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

func SubnetBroadcast(subnet string) string {
	_, cidr, _ := net.ParseCIDR(subnet)
	var length uint
	if CheckProtocol(subnet) == kubeovnv1.ProtocolIPv4 {
		length = 32
	} else {
		length = 128
	}
	maskLength, _ := cidr.Mask.Size()
	ipInt := IP2BigInt(cidr.IP.String())
	size := big.NewInt(0).Lsh(big.NewInt(1), length-uint(maskLength))
	size = big.NewInt(0).Sub(size, big.NewInt(1))
	return BigInt2Ip(ipInt.Add(ipInt, size))
}

func FirstIP(subnet string) (string, error) {
	_, cidr, err := net.ParseCIDR(subnet)
	if err != nil {
		return "", fmt.Errorf("%s is not a valid cidr", subnet)
	}
	ipInt := IP2BigInt(cidr.IP.String())
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
	ipInt := IP2BigInt(cidr.IP.String())
	size := big.NewInt(0).Lsh(big.NewInt(1), length-uint(maskLength))
	size = big.NewInt(0).Sub(size, big.NewInt(2))
	return BigInt2Ip(ipInt.Add(ipInt, size)), nil
}

func CIDRContainIP(cidrStr, ipStr string) bool {
	cidrs := strings.Split(cidrStr, ",")
	ips := strings.Split(ipStr, ",")

	if len(cidrs) == 1 {
		for _, ip := range ips {
			if CheckProtocol(cidrStr) != CheckProtocol(ip) {
				return false
			}
		}
	}

	for _, cidr := range cidrs {
		_, cidrNet, err := net.ParseCIDR(cidr)
		if err != nil {
			return false
		}

		for _, ip := range ips {
			if CheckProtocol(cidr) != CheckProtocol(ip) {
				continue
			}
			ipAddr := net.ParseIP(ip)
			if ipAddr == nil {
				return false
			}

			if !cidrNet.Contains(ipAddr) {
				return false
			}
		}
	}
	// v4 and v6 address should be both matched for dualstack check
	return true
}

func CheckProtocol(address string) string {
	ips := strings.Split(address, ",")
	if len(ips) == 2 {
		IP1 := net.ParseIP(strings.Split(ips[0], "/")[0])
		IP2 := net.ParseIP(strings.Split(ips[1], "/")[0])
		if IP1.To4() != nil && IP2.To4() == nil && IP2.To16() != nil {
			return kubeovnv1.ProtocolDual
		}
		if IP2.To4() != nil && IP1.To4() == nil && IP1.To16() != nil {
			return kubeovnv1.ProtocolDual
		}
		return ""
	}

	address = strings.Split(address, "/")[0]
	ip := net.ParseIP(address)
	if ip.To4() != nil {
		return kubeovnv1.ProtocolIPv4
	} else if ip.To16() != nil {
		return kubeovnv1.ProtocolIPv6
	}

	// cidr formal error
	return ""
}

func AddressCount(network *net.IPNet) float64 {
	prefixLen, bits := network.Mask.Size()
	if bits-prefixLen < 2 {
		return 0
	}
	return math.Pow(2, float64(bits-prefixLen)) - 2
}

func GenerateRandomV4IP(cidr string) string {
	return genRandomIP(cidr, false)
}

func GenerateRandomV6IP(cidr string) string {
	return genRandomIP(cidr, true)
}

func genRandomIP(cidr string, isIPv6 bool) string {
	if len(strings.Split(cidr, "/")) != 2 {
		return ""
	}
	ip := strings.Split(cidr, "/")[0]
	netMask, _ := strconv.Atoi(strings.Split(cidr, "/")[1])
	hostBits := 32 - netMask
	if isIPv6 {
		hostBits = 128 - netMask
	}
	add, err := rand.Int(rand.Reader, big.NewInt(1<<(uint(hostBits)-1)))
	if err != nil {
		LogFatalAndExit(err, "failed to generate random ip")
	}
	t := big.NewInt(0).Add(IP2BigInt(ip), add)
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
		gw, err := FirstIP(cidr)
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
		}
		gw, err := FirstIP(cidr)
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

	return strings.Join(gws, ","), nil
}

func SplitIpsByProtocol(excludeIps []string) ([]string, []string) {
	var v4ExcludeIps, v6ExcludeIps []string
	for _, ex := range excludeIps {
		ips := strings.Split(ex, "..")
		if CheckProtocol(ips[0]) == kubeovnv1.ProtocolIPv4 {
			v4ExcludeIps = append(v4ExcludeIps, ex)
		} else {
			v6ExcludeIps = append(v6ExcludeIps, ex)
		}
	}

	return v4ExcludeIps, v6ExcludeIps
}

func GetStringIP(v4IP, v6IP string) string {
	var ipList []string
	if IsValidIP(v4IP) {
		ipList = append(ipList, v4IP)
	}
	if IsValidIP(v6IP) {
		ipList = append(ipList, v6IP)
	}
	return strings.Join(ipList, ",")
}

func GetIPAddrWithMask(ip, cidr string) (string, error) {
	var ipAddr string
	if CheckProtocol(cidr) == kubeovnv1.ProtocolDual {
		cidrBlocks := strings.Split(cidr, ",")
		ips := strings.Split(ip, ",")
		if len(cidrBlocks) == 2 {
			if len(ips) == 2 {
				v4IP := fmt.Sprintf("%s/%s", ips[0], strings.Split(cidrBlocks[0], "/")[1])
				v6IP := fmt.Sprintf("%s/%s", ips[1], strings.Split(cidrBlocks[1], "/")[1])
				ipAddr = v4IP + "," + v6IP
			} else {
				err := fmt.Errorf("ip %s should be dualstack", ip)
				klog.Error(err)
				return "", err
			}
		}
	} else {
		ipAddr = fmt.Sprintf("%s/%s", ip, strings.Split(cidr, "/")[1])
	}
	return ipAddr, nil
}

func GetIPWithoutMask(ipStr string) string {
	var ips []string
	for _, ip := range strings.Split(ipStr, ",") {
		ips = append(ips, strings.Split(ip, "/")[0])
	}
	return strings.Join(ips, ",")
}

func SplitStringIP(ipStr string) (string, string) {
	var v4IP, v6IP string
	switch CheckProtocol(ipStr) {
	case kubeovnv1.ProtocolDual:
		for _, ipTmp := range strings.Split(ipStr, ",") {
			if CheckProtocol(ipTmp) == kubeovnv1.ProtocolIPv4 {
				v4IP = ipTmp
			} else {
				v6IP = ipTmp
			}
		}
	case kubeovnv1.ProtocolIPv4:
		v4IP = ipStr
	case kubeovnv1.ProtocolIPv6:
		v6IP = ipStr
	}

	return v4IP, v6IP
}

// ExpandExcludeIPs used to get exclude ips in range of subnet cidr, excludes cidr addr and broadcast addr
func ExpandExcludeIPs(excludeIPs []string, cidr string) []string {
	rv := []string{}
	for _, excludeIP := range excludeIPs {
		if strings.Contains(excludeIP, "..") {
			parts := strings.Split(excludeIP, "..")
			if len(parts) != 2 || CheckProtocol(parts[0]) != CheckProtocol(parts[1]) {
				klog.Errorf("invalid exclude IP: %s", excludeIP)
				continue
			}
			s := IP2BigInt(parts[0])
			e := IP2BigInt(parts[1])
			if s.Cmp(e) > 0 {
				continue
			}

			for _, cidrBlock := range strings.Split(cidr, ",") {
				if CheckProtocol(cidrBlock) != CheckProtocol(parts[0]) {
					continue
				}

				firstIP, err := FirstIP(cidrBlock)
				if err != nil {
					klog.Error(err)
					continue
				}
				if firstIP == SubnetBroadcast(cidrBlock) {
					klog.Errorf("no available IP address in CIDR %s", cidrBlock)
					continue
				}
				lastIP, _ := LastIP(cidrBlock)
				s1, e1 := s, e
				if s1.Cmp(IP2BigInt(firstIP)) < 0 {
					s1 = IP2BigInt(firstIP)
				}
				if e1.Cmp(IP2BigInt(lastIP)) > 0 {
					e1 = IP2BigInt(lastIP)
				}
				if c := s1.Cmp(e1); c == 0 {
					rv = append(rv, BigInt2Ip(s1))
				} else if c < 0 {
					rv = append(rv, BigInt2Ip(s1)+".."+BigInt2Ip(e1))
				}
			}
		} else {
			for _, cidrBlock := range strings.Split(cidr, ",") {
				if CIDRContainIP(cidrBlock, excludeIP) && excludeIP != SubnetNumber(cidrBlock) && excludeIP != SubnetBroadcast(cidrBlock) {
					rv = append(rv, excludeIP)
					break
				}
			}
		}
	}
	klog.V(3).Infof("expand exclude ips %v", rv)
	return rv
}

func ContainsIPs(excludeIP, ip string) bool {
	if strings.Contains(excludeIP, "..") {
		parts := strings.Split(excludeIP, "..")
		s := IP2BigInt(parts[0])
		e := IP2BigInt(parts[1])
		ipv := IP2BigInt(ip)
		if s.Cmp(ipv) <= 0 && e.Cmp(ipv) >= 0 {
			return true
		}
	} else if excludeIP == ip {
		return true
	}
	return false
}

func CountIPNums(excludeIPs []string) float64 {
	var count float64
	for _, excludeIP := range excludeIPs {
		if strings.Contains(excludeIP, "..") {
			var val big.Int
			parts := strings.Split(excludeIP, "..")
			s := IP2BigInt(parts[0])
			e := IP2BigInt(parts[1])
			v, _ := new(big.Float).SetInt(val.Add(val.Sub(e, s), big.NewInt(1))).Float64()
			count += v
		} else {
			count++
		}
	}
	return count
}

func GatewayContains(gatewayNodeStr, gateway string) bool {
	// the format of gatewayNodeStr can be like 'kube-ovn-worker:172.18.0.2, kube-ovn-control-plane:172.18.0.3', which consists of node name and designative egress ip
	for _, gw := range strings.Split(gatewayNodeStr, ",") {
		if strings.Contains(gw, ":") {
			gw = strings.TrimSpace(strings.Split(gw, ":")[0])
		} else {
			gw = strings.TrimSpace(gw)
		}
		if gw == strings.TrimSpace(gateway) {
			return true
		}
	}
	return false
}

func JoinHostPort(host string, port int32) string {
	return net.JoinHostPort(host, strconv.FormatInt(int64(port), 10))
}

func CIDROverlap(a, b string) bool {
	for _, cidrA := range strings.Split(a, ",") {
		for _, cidrB := range strings.Split(b, ",") {
			if CheckProtocol(cidrA) != CheckProtocol(cidrB) {
				continue
			}
			aIP, aIPNet, aErr := net.ParseCIDR(cidrA)
			bIP, bIPNet, bErr := net.ParseCIDR(cidrB)
			if aErr != nil || bErr != nil {
				return false
			}
			if aIPNet.Contains(bIP) || bIPNet.Contains(aIP) {
				return true
			}
		}
	}
	return false
}

func CIDRGlobalUnicast(cidr string) error {
	for _, cidrBlock := range strings.Split(cidr, ",") {
		if CIDROverlap(cidrBlock, IPv4Broadcast) {
			return fmt.Errorf("%s conflict with v4 broadcast cidr %s", cidr, IPv4Broadcast)
		}
		if CIDROverlap(cidrBlock, IPv4Multicast) {
			return fmt.Errorf("%s conflict with v4 multicast cidr %s", cidr, IPv4Multicast)
		}
		if CIDROverlap(cidrBlock, IPv4Loopback) {
			return fmt.Errorf("%s conflict with v4 loopback cidr %s", cidr, IPv4Loopback)
		}
		if CIDROverlap(cidrBlock, IPv4Zero) {
			return fmt.Errorf("%s conflict with v4 localnet cidr %s", cidr, IPv4Zero)
		}
		if CIDROverlap(cidrBlock, IPv4LinkLocalUnicast) {
			return fmt.Errorf("%s conflict with v4 link local cidr %s", cidr, IPv4LinkLocalUnicast)
		}

		if CIDROverlap(cidrBlock, IPv6Unspecified) {
			return fmt.Errorf("%s conflict with v6 unspecified cidr %s", cidr, IPv6Unspecified)
		}
		if CIDROverlap(cidrBlock, IPv6Loopback) {
			return fmt.Errorf("%s conflict with v6 loopback cidr %s", cidr, IPv6Loopback)
		}
		if CIDROverlap(cidrBlock, IPv6Multicast) {
			return fmt.Errorf("%s conflict with v6 multicast cidr %s", cidr, IPv6Multicast)
		}
		if CIDROverlap(cidrBlock, IPv6LinkLocalUnicast) {
			return fmt.Errorf("%s conflict with v6 link local cidr %s", cidr, IPv6LinkLocalUnicast)
		}
	}
	return nil
}

func CheckSystemCIDR(cidrs []string) error {
	for i, cidr := range cidrs {
		if err := CIDRGlobalUnicast(cidr); err != nil {
			return err
		}
		for j, nextCidr := range cidrs {
			if j <= i {
				continue
			}
			if CIDROverlap(cidr, nextCidr) {
				err := fmt.Errorf("cidr %s is conflict with cidr %s", cidr, nextCidr)
				return err
			}
		}
	}
	return nil
}

// GetExternalNetwork returns the external network name
// if the external network is not specified, return the default external network name
func GetExternalNetwork(externalNet string) string {
	if externalNet == "" {
		return vpcExternalNet
	}
	return externalNet
}

func GetNatGwExternalNetwork(externalNets []string) string {
	if len(externalNets) == 0 {
		return vpcExternalNet
	}
	return externalNets[0]
}

func TCPConnectivityCheck(address string) error {
	conn, err := net.DialTimeout("tcp", address, 3*time.Second)
	if err != nil {
		return err
	}

	_ = conn.Close()

	return nil
}

func TCPConnectivityListen(address string) error {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("listen failed with err %v", err)
	}

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				continue
			}
			_ = conn.Close()
		}
	}()

	return nil
}

func UDPConnectivityCheck(address string) error {
	udpAddr, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		return fmt.Errorf("resolve udp addr failed with err %v", err)
	}

	conn, err := net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		klog.Error(err)
		return err
	}

	defer conn.Close()

	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		return err
	}

	_, err = conn.Write([]byte("health check"))
	if err != nil {
		return fmt.Errorf("send udp packet failed with err %v", err)
	}

	buffer := make([]byte, 1024)
	_, err = conn.Read(buffer)
	if err != nil {
		return fmt.Errorf("read udp packet from remote failed %v", err)
	}

	return nil
}

func UDPConnectivityListen(address string) error {
	listenAddr, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		return fmt.Errorf("resolve udp addr failed with err %v", err)
	}

	conn, err := net.ListenUDP("udp", listenAddr)
	if err != nil {
		return fmt.Errorf("listen udp address failed with %v", err)
	}

	buffer := make([]byte, 1024)

	go func() {
		for {
			_, clientAddr, err := conn.ReadFromUDP(buffer)
			if err != nil {
				continue
			}

			_, err = conn.WriteToUDP([]byte("health check"), clientAddr)
			if err != nil {
				continue
			}
		}
	}()

	return nil
}

func GetDefaultListenAddr() string {
	addr := "0.0.0.0"
	if os.Getenv("ENABLE_BIND_LOCAL_IP") == "true" {
		podIpsEnv := os.Getenv("POD_IPS")
		podIps := strings.Split(podIpsEnv, ",")
		// when pod in dual mode, golang can't support bind v4 and v6 address in the same time,
		// so not support bind local ip when in dual mode
		if len(podIps) == 1 {
			addr = podIps[0]
			if CheckProtocol(podIps[0]) == kubeovnv1.ProtocolIPv6 {
				addr = fmt.Sprintf("[%s]", podIps[0])
			}
		}
	}
	return addr
}
