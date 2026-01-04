package util

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math"
	"math/big"
	"net"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

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
// Refer from https://github.com/cilium/cilium/blob/8c7e442ccd48b9011a10f34a128ec98751d9a80e/pkg/mac/mac.go#L106
func GenerateMac() string {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		klog.Errorf("Unable to retrieve 6 rnd bytes: %v", err)
	}

	// Set locally administered addresses bit and reset multicast bit
	buf[0] = (buf[0] | 0x02) & 0xfe

	return net.HardwareAddr(buf).String()
}

func GenerateMacWithExclusion(exclusionMACs []string) string {
	for {
		mac := GenerateMac()
		if !slices.Contains(exclusionMACs, mac) {
			return mac
		}
	}
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
	maskLength, length := cidr.Mask.Size()
	if maskLength+1 == length {
		return ""
	}
	return cidr.IP.String()
}

func SubnetBroadcast(subnet string) string {
	_, cidr, _ := net.ParseCIDR(subnet)
	ones, bits := cidr.Mask.Size()
	if ones+1 == bits {
		return ""
	}
	ipInt := IP2BigInt(cidr.IP.String())
	zeros := uint(bits - ones) // #nosec G115
	size := big.NewInt(0).Lsh(big.NewInt(1), zeros)
	size = big.NewInt(0).Sub(size, big.NewInt(1))
	return BigInt2Ip(ipInt.Add(ipInt, size))
}

// FirstIP returns first usable ip address in the subnet
func FirstIP(subnet string) (string, error) {
	_, cidr, err := net.ParseCIDR(subnet)
	if err != nil {
		klog.Error(err)
		return "", fmt.Errorf("%s is not a valid cidr", subnet)
	}
	// Handle ptp network case specially
	if ones, bits := cidr.Mask.Size(); ones+1 == bits {
		return cidr.IP.String(), nil
	}
	ipInt := IP2BigInt(cidr.IP.String())
	return BigInt2Ip(ipInt.Add(ipInt, big.NewInt(1))), nil
}

// LastIP returns last usable ip address in the subnet
func LastIP(subnet string) (string, error) {
	_, cidr, err := net.ParseCIDR(subnet)
	if err != nil {
		klog.Error(err)
		return "", fmt.Errorf("%s is not a valid cidr", subnet)
	}

	ipInt := IP2BigInt(cidr.IP.String())
	size := getCIDRSize(cidr)
	return BigInt2Ip(ipInt.Add(ipInt, size)), nil
}

func getCIDRSize(cidr *net.IPNet) *big.Int {
	ones, bits := cidr.Mask.Size()
	zeros := uint(bits - ones) // #nosec G115
	size := big.NewInt(0).Lsh(big.NewInt(1), zeros)
	if ones+1 == bits {
		return big.NewInt(0).Sub(size, big.NewInt(1))
	}
	return big.NewInt(0).Sub(size, big.NewInt(2))
}

func CIDRContainIP(cidrStr, ipStr string) bool {
	if cidrStr == "" {
		return false
	}

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
			klog.Errorf("failed to parse CIDR %q: %v", cidr, err)
			return false
		}

		for _, ip := range ips {
			if CheckProtocol(cidr) != CheckProtocol(ip) {
				continue
			}
			ipAddr := net.ParseIP(ip)
			if ipAddr == nil {
				klog.Errorf("invalid ip %q", ip)
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
	if address == "" {
		return ""
	}

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
		err := fmt.Errorf("invalid address %q", address)
		klog.Error(err)
		return ""
	}

	address = strings.Split(address, "/")[0]
	ip := net.ParseIP(address)
	if ip.To4() != nil {
		return kubeovnv1.ProtocolIPv4
	} else if ip.To16() != nil {
		return kubeovnv1.ProtocolIPv6
	}

	// cidr format error
	err := fmt.Errorf("invalid address %q", address)
	klog.Error(err)
	return ""
}

func AddressCount(network *net.IPNet) float64 {
	prefixLen, bits := network.Mask.Size()
	// Special case handling for /31 and /32 subnets
	switch bits - prefixLen {
	case 1:
		return 2 // /31 subnet
	case 0:
		return 1 // /32 subnet
	}
	return math.Pow(2, float64(bits-prefixLen)) - 2
}

func GenerateRandomIP(cidr string) string {
	ip, network, err := net.ParseCIDR(cidr)
	if err != nil {
		klog.Errorf("failed to parse cidr %q: %v", cidr, err)
		return ""
	}
	ones, bits := network.Mask.Size()
	zeros := uint(bits - ones) // #nosec G115
	add, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), zeros-1))
	if err != nil {
		klog.Errorf("failed to generate random big int with bits %d: %v", zeros, err)
		return ""
	}
	t := big.NewInt(0).Add(IP2BigInt(ip.String()), add)
	return fmt.Sprintf("%s/%d", BigInt2Ip(t), ones)
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
	for cidrBlock := range strings.SplitSeq(cidr, ",") {
		if _, _, err := net.ParseCIDR(cidrBlock); err != nil {
			klog.Error(err)
			return errors.New("CIDRInvalid")
		}
	}
	return nil
}

func GetGwByCidr(cidrStr string) (string, error) {
	var gws []string
	for cidr := range strings.SplitSeq(cidrStr, ",") {
		gw, err := FirstIP(cidr)
		if err != nil {
			klog.Error(err)
			return "", err
		}
		gws = append(gws, gw)
	}

	return strings.Join(gws, ","), nil
}

func AppendGwByCidr(gateway, cidrStr string) (string, error) {
	var gws []string
	for cidr := range strings.SplitSeq(cidrStr, ",") {
		if CheckProtocol(gateway) == CheckProtocol(cidr) {
			gws = append(gws, gateway)
			continue
		}
		gw, err := FirstIP(cidr)
		if err != nil {
			klog.Error(err)
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

// GetIPAddrWithMaskForCNI returns IP address with mask for CNI plugin.
// When ip is empty, it indicates no-IPAM mode (e.g., NAT gateway macvlan without default EIP).
// Returns (ipAddr, noIPAM, error) where noIPAM is true when IP allocation is skipped.
func GetIPAddrWithMaskForCNI(ip, cidr string) (string, bool, error) {
	if ip == "" {
		// Network attachment definition using no-IPAM plugin (e.g., NAT gateway net1 macvlan with no default EIP)
		// IP is not allocated by Kube-OVN, but cidr still comes from subnet configuration
		klog.V(3).Infof("skipping IP allocation: ip is empty for cidr %s (no-IPAM mode)", cidr)
		return "", true, nil
	}
	ipAddr, err := GetIPAddrWithMask(ip, cidr)
	return ipAddr, false, err
}

func GetIPAddrWithMask(ip, cidr string) (string, error) {
	var ipAddr string
	ips := strings.Split(ip, ",")
	if CheckProtocol(cidr) == kubeovnv1.ProtocolDual {
		cidrBlocks := strings.Split(cidr, ",")
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
		if len(ips) == 1 {
			ipAddr = fmt.Sprintf("%s/%s", ip, strings.Split(cidr, "/")[1])
		} else {
			err := fmt.Errorf("ip %s should be singlestack", ip)
			klog.Error(err)
			return ipAddr, err
		}
	}
	return ipAddr, nil
}

func GetIPWithoutMask(ipStr string) string {
	var ips []string
	for ip := range strings.SplitSeq(ipStr, ",") {
		ips = append(ips, strings.Split(ip, "/")[0])
	}
	return strings.Join(ips, ",")
}

func SplitStringIP(ipStr string) (string, string) {
	var v4IP, v6IP string
	switch CheckProtocol(ipStr) {
	case kubeovnv1.ProtocolDual:
		for ipTmp := range strings.SplitSeq(ipStr, ",") {
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

			for cidrBlock := range strings.SplitSeq(cidr, ",") {
				if CheckProtocol(cidrBlock) != CheckProtocol(parts[0]) {
					continue
				}

				firstIP, err := FirstIP(cidrBlock)
				if err != nil {
					klog.Error(err)
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
				} else {
					klog.Errorf("invalid exclude ip range %s, start ip %s should smaller than end %s", excludeIP, BigInt2Ip(s1), BigInt2Ip(e1))
				}
			}
		} else {
			for cidrBlock := range strings.SplitSeq(cidr, ",") {
				// exclude ip should be the same protocol with cidr
				if CheckProtocol(cidrBlock) == CheckProtocol(excludeIP) {
					// exclude ip should be in the range of cidr and not cidr addr and broadcast addr
					if CIDRContainIP(cidrBlock, excludeIP) && excludeIP != SubnetNumber(cidrBlock) && excludeIP != SubnetBroadcast(cidrBlock) {
						rv = append(rv, excludeIP)
						break
					}
					klog.Errorf("CIDR %s not contains the exclude ip %s", cidrBlock, excludeIP)
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
	for gw := range strings.SplitSeq(gatewayNodeStr, ",") {
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
	for cidrA := range strings.SplitSeq(a, ",") {
		for cidrB := range strings.SplitSeq(b, ",") {
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

// CIDRContainsCIDR checks whether CIDR a contains CIDR b
// if a and b are not the same protocol, return an error directly
// if a and b are the same protocol, but a doesn't contain b, return false
// if a contains b, return true
// if a and b are the same CIDR, return true
func CIDRContainsCIDR(a, b string) (bool, error) {
	_, ca, err := net.ParseCIDR(a)
	if err != nil {
		return false, err
	}
	_, cb, err := net.ParseCIDR(b)
	if err != nil {
		return false, err
	}
	oa, ba := ca.Mask.Size()
	ob, bb := cb.Mask.Size()
	if ba != bb {
		return false, fmt.Errorf("protocol of cidr %s and %s not match", a, b)
	}
	return oa <= ob && ca.Contains(cb.IP), nil
}

func CIDRGlobalUnicast(cidr string) error {
	for cidrBlock := range strings.SplitSeq(cidr, ",") {
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
			klog.Error(err)
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

func CheckNodeDNSIP(nodeLocalDNSIP string) error {
	if nodeLocalDNSIP != "" && !IsValidIP(nodeLocalDNSIP) {
		return fmt.Errorf("invalid node local dns ip: %q", nodeLocalDNSIP)
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

func TCPConnectivityCheck(endpoint string) error {
	conn, err := net.DialTimeout("tcp", endpoint, 3*time.Second)
	if err != nil {
		klog.Error(err)
		return err
	}

	_ = conn.Close()

	return nil
}

func TCPConnectivityListen(endpoint string) error {
	listener, err := net.Listen("tcp", endpoint)
	if err != nil {
		err := fmt.Errorf("failed to listen %s, %w", endpoint, err)
		klog.Error(err)
		return err
	}

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				klog.Error(err)
				continue
			}
			_ = conn.Close()
		}
	}()

	return nil
}

func UDPConnectivityCheck(endpoint string) error {
	udpAddr, err := net.ResolveUDPAddr("udp", endpoint)
	if err != nil {
		err := fmt.Errorf("failed to resolve %s, %w", endpoint, err)
		klog.Error(err)
		return err
	}

	conn, err := net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		klog.Error(err)
		return err
	}

	defer conn.Close()

	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		klog.Error(err)
		return err
	}

	_, err = conn.Write([]byte("health check"))
	if err != nil {
		err := fmt.Errorf("failed to send udp packet, %w", err)
		klog.Error(err)
		return err
	}

	buffer := make([]byte, 1024)
	_, err = conn.Read(buffer)
	if err != nil {
		err := fmt.Errorf("failed to read udp packet from remote, %w", err)
		klog.Error(err)
		return err
	}

	return nil
}

func UDPConnectivityListen(endpoint string) error {
	listenAddr, err := net.ResolveUDPAddr("udp", endpoint)
	if err != nil {
		err := fmt.Errorf("failed to resolve udp addr: %w", err)
		klog.Error(err)
		return err
	}

	conn, err := net.ListenUDP("udp", listenAddr)
	if err != nil {
		err := fmt.Errorf("failed to listen udp address: %w", err)
		klog.Error(err)
		return err
	}

	buffer := make([]byte, 1024)

	go func() {
		for {
			_, clientAddr, err := conn.ReadFromUDP(buffer)
			if err != nil {
				klog.Error(err)
				continue
			}

			_, err = conn.WriteToUDP([]byte("health check"), clientAddr)
			if err != nil {
				klog.Error(err)
				continue
			}
		}
	}()

	return nil
}

func GetDefaultListenAddr() []string {
	if os.Getenv("ENABLE_BIND_LOCAL_IP") == "true" {
		if podIPs := os.Getenv("POD_IPS"); podIPs != "" {
			return strings.Split(podIPs, ",")
		}
		klog.Error("environment variable POD_IPS is not set, cannot bind to local ip")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}
	return []string{"0.0.0.0"}
}

func ContainsUppercase(s string) bool {
	for _, char := range s {
		if unicode.IsUpper(char) {
			return true
		}
	}
	return false
}

func InvalidSpecialCIDR(s string) error {
	// 0.0.0.0 and 255.255.255.255 only using in special case
	if strings.HasPrefix(s, "0.0.0.0") {
		err := fmt.Errorf("invalid zero cidr %q", s)
		klog.Error(err)
		return err
	}
	if strings.Contains(s, "255.255.255.255") {
		err := fmt.Errorf("invalid broadcast cidr %q", s)
		klog.Error(err)
		return err
	}
	return nil
}

func InvalidNetworkMask(network *net.IPNet) error {
	if ones, bits := network.Mask.Size(); ones == bits {
		err := fmt.Errorf("invalid network mask %d", ones)
		klog.Error(err)
		return err
	}
	return nil
}
