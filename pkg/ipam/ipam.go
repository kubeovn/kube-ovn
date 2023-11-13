package ipam

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"

	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/internal"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

var (
	ErrOutOfRange  = errors.New("AddressOutOfRange")
	ErrConflict    = errors.New("AddressConflict")
	ErrNoAvailable = errors.New("NoAvailableAddress")
	ErrInvalidCIDR = errors.New("CIDRInvalid")
)

type IPAM struct {
	mutex   sync.RWMutex
	Subnets map[string]*Subnet
}

type SubnetAddress struct {
	Subnet *Subnet
	IP     string
	Mac    string
}

func NewIPAM() *IPAM {
	return &IPAM{
		mutex:   sync.RWMutex{},
		Subnets: map[string]*Subnet{},
	}
}

func (ipam *IPAM) GetRandomAddress(podName, nicName string, mac *string, subnetName, poolName string, skippedAddrs []string, checkConflict bool) (string, string, string, error) {
	ipam.mutex.RLock()
	defer ipam.mutex.RUnlock()
	var v4, v6 string
	subnet, ok := ipam.Subnets[subnetName]
	if !ok {
		return "", "", "", ErrNoAvailable
	}

	v4IP, v6IP, macStr, err := subnet.GetRandomAddress(poolName, podName, nicName, mac, skippedAddrs, checkConflict)
	if v4IP != nil {
		v4 = v4IP.String()
	}
	if v6IP != nil {
		v6 = v6IP.String()
	}
	if poolName == "" {
		klog.Infof("allocate v4 %s, v6 %s, mac %s for %s from subnet %s", v4, v6, macStr, podName, subnetName)
	} else {
		klog.Infof("allocate v4 %s, v6 %s, mac %s for %s from ippool %s in subnet %s", v4, v6, macStr, podName, poolName, subnetName)
	}
	return v4, v6, macStr, err
}

func (ipam *IPAM) GetStaticAddress(podName, nicName, ip string, mac *string, subnetName string, checkConflict bool) (string, string, string, error) {
	ipam.mutex.RLock()
	defer ipam.mutex.RUnlock()
	var subnet *Subnet
	var ok bool
	klog.Infof("allocating static ip %s from subnet %s", ip, subnetName)
	if subnet, ok = ipam.Subnets[subnetName]; !ok {
		return "", "", "", ErrNoAvailable
	}

	var ips []IP
	var err error
	var ipAddr IP
	var v4, v6, macStr string
	for _, ipStr := range strings.Split(ip, ",") {
		ip, err := NewIP(ipStr)
		if err != nil {
			klog.Errorf("failed to parse ip %s", ipStr)
			return "", "", "", err
		}
		ipAddr, macStr, err = subnet.GetStaticAddress(podName, nicName, ip, mac, false, checkConflict)
		if err != nil {
			klog.Errorf("failed to allocate static ip %s for %s", ipStr, podName)
			return "", "", "", err
		}
		ips = append(ips, ipAddr)
	}
	ips, err = checkAndAppendIpsForDual(ips, macStr, podName, nicName, subnet, checkConflict)
	if err != nil {
		klog.Errorf("failed to append allocate ip %v mac %v for %s", ips, mac, podName)
		return "", "", "", err
	}

	if macStr == "" {
		err := fmt.Errorf("failed to allocate static mac for %s", podName)
		klog.Error(err)
		return "", "", "", ErrNoAvailable
	}

	switch subnet.Protocol {
	case kubeovnv1.ProtocolIPv4:
		klog.Infof("allocate v4 %s, mac %s for %s from subnet %s", ip, macStr, podName, subnetName)
		return ip, "", macStr, err
	case kubeovnv1.ProtocolIPv6:
		klog.Infof("allocate v6 %s, mac %s for %s from subnet %s", ip, macStr, podName, subnetName)
		return "", ip, macStr, err
	case kubeovnv1.ProtocolDual:
		if ips[0] != nil {
			v4 = ips[0].String()
		}
		if ips[1] != nil {
			v6 = ips[1].String()
		}
		klog.Infof("allocate v4 %s, v6 %s, mac %s for %s from subnet %s", ips[0].String(), ips[1].String(), macStr, podName, subnetName)
		return v4, v6, macStr, err
	}
	return "", "", "", ErrNoAvailable
}

func checkAndAppendIpsForDual(ips []IP, mac, podName, nicName string, subnet *Subnet, checkConflict bool) ([]IP, error) {
	// IP Address for dual-stack should be format of 'IPv4,IPv6'
	if subnet.Protocol != kubeovnv1.ProtocolDual || len(ips) == 2 {
		return ips, nil
	}

	var newIps []IP
	var ipAddr IP
	var err error
	if util.CheckProtocol(ips[0].String()) == kubeovnv1.ProtocolIPv4 {
		newIps = ips
		_, ipAddr, _, err = subnet.getV6RandomAddress("", podName, nicName, &mac, nil, checkConflict)
		newIps = append(newIps, ipAddr)
	} else if util.CheckProtocol(ips[0].String()) == kubeovnv1.ProtocolIPv6 {
		ipAddr, _, _, err = subnet.getV4RandomAddress("", podName, nicName, &mac, nil, checkConflict)
		newIps = append(newIps, ipAddr)
		newIps = append(newIps, ips...)
	}

	return newIps, err
}

func (ipam *IPAM) ReleaseAddressByPod(podName, subnetName string) {
	ipam.mutex.RLock()
	defer ipam.mutex.RUnlock()
	if subnetName != "" {
		if subnet, ok := ipam.Subnets[subnetName]; ok {
			subnet.ReleaseAddress(podName)
		}
	} else {
		for _, subnet := range ipam.Subnets {
			subnet.ReleaseAddress(podName)
		}
	}
}

func (ipam *IPAM) AddOrUpdateSubnet(name, cidrStr, gw string, excludeIps []string) error {
	excludeIps = util.ExpandExcludeIPs(excludeIps, cidrStr)

	ipam.mutex.Lock()
	defer ipam.mutex.Unlock()

	var v4cidrStr, v6cidrStr, v4Gw, v6Gw string
	var cidrs []*net.IPNet
	for _, cidrBlock := range strings.Split(cidrStr, ",") {
		_, cidr, err := net.ParseCIDR(cidrBlock)
		if err != nil {
			return ErrInvalidCIDR
		}
		cidrs = append(cidrs, cidr)
	}
	protocol := util.CheckProtocol(cidrStr)
	switch protocol {
	case kubeovnv1.ProtocolDual:
		v4cidrStr = cidrs[0].String()
		v6cidrStr = cidrs[1].String()
		gws := strings.Split(gw, ",")
		v4Gw = gws[0]
		v6Gw = gws[1]
	case kubeovnv1.ProtocolIPv4:
		v4cidrStr = cidrs[0].String()
		v4Gw = gw
	case kubeovnv1.ProtocolIPv6:
		v6cidrStr = cidrs[0].String()
		v6Gw = gw
	}

	// subnet.Spec.ExcludeIps contains both v4 and v6 addresses
	v4ExcludeIps, v6ExcludeIps := util.SplitIpsByProtocol(excludeIps)

	if subnet, ok := ipam.Subnets[name]; ok {
		subnet.Protocol = protocol
		v4Reserved, err := NewIPRangeListFrom(v4ExcludeIps...)
		if err != nil {
			klog.Errorf("failed to parse v4 exclude ips %v", v4ExcludeIps)
			return err
		}
		v6Reserved, err := NewIPRangeListFrom(v6ExcludeIps...)
		if err != nil {
			klog.Errorf("failed to parse v6 exclude ips %v", v6ExcludeIps)
			return err
		}
		if (protocol == kubeovnv1.ProtocolDual || protocol == kubeovnv1.ProtocolIPv4) &&
			(subnet.V4CIDR.String() != v4cidrStr || subnet.V4Gw != v4Gw || !subnet.V4Reserved.Equal(v4Reserved)) {
			_, cidr, _ := net.ParseCIDR(v4cidrStr)
			subnet.V4CIDR = cidr
			subnet.V4Reserved = v4Reserved
			firstIP, _ := util.FirstIP(v4cidrStr)
			lastIP, _ := util.LastIP(v4cidrStr)
			ips, _ := NewIPRangeListFrom(fmt.Sprintf("%s..%s", firstIP, lastIP))
			subnet.V4Using = subnet.V4Using.Intersect(ips)
			subnet.V4Free = ips.Separate(subnet.V4Reserved).Separate(subnet.V4Using)
			subnet.V4Available = subnet.V4Free.Clone()
			subnet.V4Gw = v4Gw

			pool := subnet.IPPools[""]
			pool.V4IPs = ips
			pool.V4Free = subnet.V4Free.Clone()
			pool.V4Reserved = subnet.V4Reserved.Clone()
			pool.V4Released = NewEmptyIPRangeList()
			pool.V4Using = subnet.V4Using.Clone()

			for name, p := range subnet.IPPools {
				if name == "" {
					continue
				}
				p.V4Free = ips.Intersect(p.V4IPs)
				p.V4Reserved = subnet.V4Reserved.Intersect(p.V4IPs)
				p.V4Available = p.V4Free.Clone()
				p.V4Released = NewEmptyIPRangeList()
				pool.V4Free = pool.V4Free.Separate(p.V4IPs)
				pool.V4Reserved = pool.V4Reserved.Separate(p.V4Reserved)
			}
			pool.V4Available = pool.V4Free.Clone()

			for nicName, ip := range subnet.V4NicToIP {
				if !ips.Contains(ip) {
					podName := subnet.V4IPToPod[ip.String()]
					klog.Errorf("%s address %s not in subnet %s new cidr %s", podName, ip, name, cidrStr)
					delete(subnet.V4NicToIP, nicName)
					delete(subnet.V4IPToPod, ip.String())
				}
			}

			for nicName, ip := range subnet.V4NicToIP {
				klog.Infof("already assigned ip %s to nic %s in subnet %s", ip, nicName, name)
			}
		}
		if (protocol == kubeovnv1.ProtocolDual || protocol == kubeovnv1.ProtocolIPv6) &&
			(subnet.V6CIDR.String() != v6cidrStr || subnet.V6Gw != v6Gw || !subnet.V6Reserved.Equal(v6Reserved)) {
			_, cidr, _ := net.ParseCIDR(v6cidrStr)
			subnet.V6CIDR = cidr
			subnet.V6Reserved = v6Reserved
			firstIP, _ := util.FirstIP(v6cidrStr)
			lastIP, _ := util.LastIP(v6cidrStr)
			ips, _ := NewIPRangeListFrom(fmt.Sprintf("%s..%s", firstIP, lastIP))
			subnet.V6Using = subnet.V6Using.Intersect(ips)
			subnet.V6Free = ips.Separate(subnet.V6Reserved).Separate(subnet.V6Using)
			subnet.V6Available = subnet.V6Free.Clone()
			subnet.V6Gw = v6Gw

			pool := subnet.IPPools[""]
			pool.V6IPs = ips
			pool.V6Free = subnet.V6Free.Clone()
			pool.V6Reserved = subnet.V6Reserved.Clone()
			pool.V6Released = NewEmptyIPRangeList()
			pool.V6Using = subnet.V6Using.Clone()

			for name, p := range subnet.IPPools {
				if name == "" {
					continue
				}
				p.V6Free = ips.Intersect(p.V6IPs)
				p.V6Reserved = subnet.V6Reserved.Intersect(p.V6IPs)
				p.V6Available = p.V6Free.Clone()
				p.V6Released = NewEmptyIPRangeList()
				pool.V6Free = pool.V6Free.Separate(p.V6IPs)
				pool.V6Reserved = pool.V6Reserved.Separate(p.V6Reserved)
			}
			pool.V6Available = pool.V6Free.Clone()

			for nicName, ip := range subnet.V6NicToIP {
				if !ips.Contains(ip) {
					podName := subnet.V6IPToPod[ip.String()]
					klog.Errorf("%s address %s not in subnet %s new cidr %s", podName, ip, name, cidrStr)
					delete(subnet.V6NicToIP, nicName)
					delete(subnet.V6IPToPod, ip.String())
				}
			}

			for nicName, ip := range subnet.V6NicToIP {
				klog.Infof("already assigned ip %s to nic %s in subnet %s", ip, nicName, name)
			}
		}

		for nicName, mac := range subnet.NicToMac {
			if subnet.V4NicToIP[nicName] == nil && subnet.V6NicToIP[nicName] == nil {
				delete(subnet.NicToMac, nicName)
				delete(subnet.MacToPod, mac)
			}
		}
		return nil
	}

	subnet, err := NewSubnet(name, cidrStr, excludeIps)
	if err != nil {
		klog.Errorf("failed to create subnet %s, %v", name, err)
		return err
	}
	subnet.V4Gw = v4Gw
	subnet.V6Gw = v6Gw
	klog.Infof("adding new subnet %s", name)
	ipam.Subnets[name] = subnet
	return nil
}

func (ipam *IPAM) DeleteSubnet(subnetName string) {
	ipam.mutex.Lock()
	defer ipam.mutex.Unlock()
	klog.Infof("delete subnet %s", subnetName)
	delete(ipam.Subnets, subnetName)
}

func (ipam *IPAM) GetPodAddress(podName string) []*SubnetAddress {
	ipam.mutex.RLock()
	defer ipam.mutex.RUnlock()
	addresses := []*SubnetAddress{}
	for _, subnet := range ipam.Subnets {
		subnet.mutex.RLock()
		for _, nicName := range subnet.PodToNicList[podName] {
			v4IP, v6IP, mac, protocol := subnet.GetPodAddress(podName, nicName)
			switch protocol {
			case kubeovnv1.ProtocolIPv4:
				addresses = append(addresses, &SubnetAddress{Subnet: subnet, IP: v4IP.String(), Mac: mac})
			case kubeovnv1.ProtocolIPv6:
				addresses = append(addresses, &SubnetAddress{Subnet: subnet, IP: v6IP.String(), Mac: mac})
			case kubeovnv1.ProtocolDual:
				addresses = append(addresses, &SubnetAddress{Subnet: subnet, IP: v4IP.String(), Mac: mac})
				addresses = append(addresses, &SubnetAddress{Subnet: subnet, IP: v6IP.String(), Mac: mac})
			}
		}
		subnet.mutex.RUnlock()
	}
	return addresses
}

func (ipam *IPAM) ContainAddress(address string) bool {
	ipam.mutex.RLock()
	defer ipam.mutex.RUnlock()
	ip, err := NewIP(address)
	if ip == nil {
		klog.Error(err)
		return false
	}

	for _, subnet := range ipam.Subnets {
		if subnet.ContainAddress(ip) {
			return true
		}
	}
	return false
}

func (ipam *IPAM) IsIPAssignedToOtherPod(ip, subnetName, podName string) (string, bool) {
	ipam.mutex.RLock()
	defer ipam.mutex.RUnlock()

	subnet, ok := ipam.Subnets[subnetName]
	if !ok {
		return "", false
	}
	return subnet.isIPAssignedToOtherPod(ip, podName)
}

func (ipam *IPAM) GetSubnetV4Mask(subnetName string) (string, error) {
	subnet, ok := ipam.Subnets[subnetName]
	if ok {
		mask, _ := subnet.V4CIDR.Mask.Size()
		return strconv.Itoa(mask), nil
	}
	return "", ErrNoAvailable
}

func (ipam *IPAM) GetSubnetIPRangeString(subnetName string, excludeIps []string) (string, string, string, string) {
	ipam.mutex.RLock()
	defer ipam.mutex.RUnlock()

	// subnet.Spec.ExcludeIps contains both v4 and v6 addresses
	v4ExcludeIps, v6ExcludeIps := util.SplitIpsByProtocol(excludeIps)

	var v4UsingIPStr, v6UsingIPStr, v4AvailableIPStr, v6AvailableIPStr string
	if subnet, ok := ipam.Subnets[subnetName]; ok {
		v4Reserved, _ := NewIPRangeListFrom(v4ExcludeIps...)
		v6Reserved, _ := NewIPRangeListFrom(v6ExcludeIps...)

		// do not count ips in excludeIPs as available and using IPs
		v4AvailableIPStr = subnet.V4Available.Separate(v4Reserved).String()
		v6AvailableIPStr = subnet.V6Available.Separate(v6Reserved).String()
		v4UsingIPStr = subnet.V4Using.Separate(v4Reserved).String()
		v6UsingIPStr = subnet.V6Using.Separate(v6Reserved).String()
	}

	return v4UsingIPStr, v6UsingIPStr, v4AvailableIPStr, v6AvailableIPStr
}

func (ipam *IPAM) AddOrUpdateIPPool(subnet, ippool string, ips []string) error {
	ipam.mutex.RLock()
	defer ipam.mutex.RUnlock()

	s := ipam.Subnets[subnet]
	if s == nil {
		return fmt.Errorf("subnet %s does not exist in IPAM", subnet)
	}

	return s.AddOrUpdateIPPool(ippool, ips)
}

func (ipam *IPAM) RemoveIPPool(subnet, ippool string) {
	ipam.mutex.RLock()
	if s := ipam.Subnets[subnet]; s != nil {
		s.RemoveIPPool(ippool)
	}
	ipam.mutex.RUnlock()
}

func (ipam *IPAM) IPPoolStatistics(subnet, ippool string) (
	v4Available, v4Using, v6Available, v6Using internal.BigInt,
	v4AvailableRange, v4UsingRange, v6AvailableRange, v6UsingRange string,
) {
	ipam.mutex.RLock()
	defer ipam.mutex.RUnlock()

	s := ipam.Subnets[subnet]
	if s == nil {
		return
	}
	return s.IPPoolStatistics(ippool)
}
