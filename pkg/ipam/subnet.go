package ipam

import (
	"fmt"
	"net"
	"slices"
	"strings"
	"sync"

	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/internal"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

type Subnet struct {
	Name         string
	mutex        sync.RWMutex
	CIDR         string
	Protocol     string
	V4CIDR       *net.IPNet
	V4Free       *IPRangeList
	V4Reserved   *IPRangeList
	V4Available  *IPRangeList
	V4Using      *IPRangeList
	V4NicToIP    map[string]IP
	V4IPToPod    map[string]string
	V6CIDR       *net.IPNet
	V6Free       *IPRangeList
	V6Reserved   *IPRangeList
	V6Available  *IPRangeList
	V6Using      *IPRangeList
	V6NicToIP    map[string]IP
	V6IPToPod    map[string]string
	NicToMac     map[string]string
	MacToPod     map[string]string
	PodToNicList map[string][]string
	V4Gw         string
	V6Gw         string

	IPPools map[string]*IPPool
}

func NewSubnet(name, cidrStr string, excludeIps []string) (*Subnet, error) {
	var cidrs []*net.IPNet
	for _, cidrBlock := range strings.Split(cidrStr, ",") {
		_, cidr, err := net.ParseCIDR(cidrBlock)
		if err != nil {
			return nil, ErrInvalidCIDR
		}
		cidrs = append(cidrs, cidr)
	}

	// subnet.Spec.ExcludeIps contains both v4 and v6 addresses
	excludeIps = util.ExpandExcludeIPs(excludeIps, cidrStr)
	v4ExcludeIps, v6ExcludeIps := util.SplitIpsByProtocol(excludeIps)
	v4Reserved, err := NewIPRangeListFrom(v4ExcludeIps...)
	if err != nil {
		return nil, err
	}
	v6Reserved, err := NewIPRangeListFrom(v6ExcludeIps...)
	if err != nil {
		return nil, err
	}

	protocol := util.CheckProtocol(cidrStr)
	subnet := &Subnet{
		Name:         name,
		CIDR:         cidrStr,
		Protocol:     protocol,
		V4Free:       NewEmptyIPRangeList(),
		V6Free:       NewEmptyIPRangeList(),
		V4Reserved:   v4Reserved,
		V6Reserved:   v6Reserved,
		V4Using:      NewEmptyIPRangeList(),
		V6Using:      NewEmptyIPRangeList(),
		V4NicToIP:    map[string]IP{},
		V6NicToIP:    map[string]IP{},
		V4IPToPod:    map[string]string{},
		V6IPToPod:    map[string]string{},
		MacToPod:     map[string]string{},
		NicToMac:     map[string]string{},
		PodToNicList: map[string][]string{},
		IPPools:      make(map[string]*IPPool, 0),
	}
	switch protocol {
	case kubeovnv1.ProtocolIPv4:
		firstIP, _ := util.FirstIP(cidrStr)
		lastIP, _ := util.LastIP(cidrStr)
		subnet.V4CIDR = cidrs[0]
		subnet.V4Free, _ = NewIPRangeListFrom(fmt.Sprintf("%s..%s", firstIP, lastIP))
	case kubeovnv1.ProtocolIPv6:
		firstIP, _ := util.FirstIP(cidrStr)
		lastIP, _ := util.LastIP(cidrStr)
		subnet.V6CIDR = cidrs[0]
		subnet.V6Free, _ = NewIPRangeListFrom(fmt.Sprintf("%s..%s", firstIP, lastIP))
	default:
		subnet.V4CIDR = cidrs[0]
		subnet.V6CIDR = cidrs[1]
		cidrBlocks := strings.Split(cidrStr, ",")
		v4FirstIP, _ := util.FirstIP(cidrBlocks[0])
		v4LastIP, _ := util.LastIP(cidrBlocks[0])
		v6FirstIP, _ := util.FirstIP(cidrBlocks[1])
		v6LastIP, _ := util.LastIP(cidrBlocks[1])
		subnet.V4Free, _ = NewIPRangeListFrom(fmt.Sprintf("%s..%s", v4FirstIP, v4LastIP))
		subnet.V6Free, _ = NewIPRangeListFrom(fmt.Sprintf("%s..%s", v6FirstIP, v6LastIP))
	}

	pool := &IPPool{
		V4IPs:      subnet.V4Free.Clone(),
		V6IPs:      subnet.V6Free.Clone(),
		V4Released: NewEmptyIPRangeList(),
		V6Released: NewEmptyIPRangeList(),
		V4Using:    NewEmptyIPRangeList(),
		V6Using:    NewEmptyIPRangeList(),
	}
	subnet.V4Free = subnet.V4Free.Separate(subnet.V4Reserved)
	subnet.V6Free = subnet.V6Free.Separate(subnet.V6Reserved)
	subnet.V4Available = subnet.V4Free.Clone()
	subnet.V6Available = subnet.V6Free.Clone()
	pool.V4Free = subnet.V4Free.Clone()
	pool.V6Free = subnet.V6Free.Clone()
	pool.V4Available = subnet.V4Available.Clone()
	pool.V6Available = subnet.V6Available.Clone()
	pool.V4Reserved = subnet.V4Reserved.Clone()
	pool.V6Reserved = subnet.V6Reserved.Clone()
	subnet.IPPools = map[string]*IPPool{"": pool}

	return subnet, nil
}

func (s *Subnet) GetRandomMac(podName, nicName string) string {
	if mac, ok := s.NicToMac[nicName]; ok {
		return mac
	}
	for {
		mac := util.GenerateMac()
		if _, ok := s.MacToPod[mac]; !ok {
			s.MacToPod[mac] = podName
			s.NicToMac[nicName] = mac
			return mac
		}
	}
}

func (s *Subnet) GetStaticMac(podName, nicName, mac string, checkConflict bool) error {
	if mac == "" {
		return nil
	}
	if checkConflict {
		if p, ok := s.MacToPod[mac]; ok && p != podName {
			klog.Errorf("mac %s has been allocated to pod %s", mac, p)
			return ErrConflict
		}
	}
	s.MacToPod[mac] = podName
	s.NicToMac[nicName] = mac
	return nil
}

func (s *Subnet) pushPodNic(podName, nicName string) {
	if s.V4NicToIP[nicName] != nil || s.V6NicToIP[nicName] != nil || s.NicToMac[nicName] != "" {
		s.PodToNicList[podName] = slices.Compact(append(s.PodToNicList[podName], nicName))
	}
}

func (s *Subnet) popPodNic(podName, nicName string) {
	s.PodToNicList[podName] = util.RemoveString(s.PodToNicList[podName], nicName)
	if s.PodToNicList[podName] == nil {
		delete(s.PodToNicList, podName)
	}
}

func (s *Subnet) GetRandomAddress(poolName, podName, nicName string, mac *string, skippedAddrs []string, checkConflict bool) (IP, IP, string, error) {
	s.mutex.Lock()
	defer func() {
		s.pushPodNic(podName, nicName)
		s.mutex.Unlock()
	}()

	switch s.Protocol {
	case kubeovnv1.ProtocolDual:
		return s.getDualRandomAddress(poolName, podName, nicName, mac, skippedAddrs, checkConflict)
	case kubeovnv1.ProtocolIPv4:
		return s.getV4RandomAddress(poolName, podName, nicName, mac, skippedAddrs, checkConflict)
	default:
		return s.getV6RandomAddress(poolName, podName, nicName, mac, skippedAddrs, checkConflict)
	}
}

func (s *Subnet) getDualRandomAddress(poolName, podName, nicName string, mac *string, skippedAddrs []string, checkConflict bool) (IP, IP, string, error) {
	v4IP, _, _, err := s.getV4RandomAddress(poolName, podName, nicName, mac, skippedAddrs, checkConflict)
	if err != nil {
		return nil, nil, "", err
	}
	_, v6IP, macStr, err := s.getV6RandomAddress(poolName, podName, nicName, mac, skippedAddrs, checkConflict)
	if err != nil {
		return nil, nil, "", err
	}

	// allocated IPv4 address may be released in getV6RandomAddress()
	if !s.V4NicToIP[nicName].Equal(v4IP) {
		v4IP, _, _, _ = s.getV4RandomAddress(poolName, podName, nicName, mac, skippedAddrs, checkConflict)
	}

	return v4IP, v6IP, macStr, nil
}

func (s *Subnet) getV4RandomAddress(ippoolName, podName, nicName string, mac *string, skippedAddrs []string, checkConflict bool) (IP, IP, string, error) {
	// After 'macAdd' introduced to support only static mac address, pod restart will run into error mac AddressConflict
	// controller will re-enqueue the new pod then wait for old pod deleted and address released.
	// here will return only if both ip and mac exist, otherwise only ip without mac returned will trigger CreatePort error.
	if s.V4NicToIP[nicName] != nil && s.NicToMac[nicName] != "" {
		if !slices.Contains(skippedAddrs, s.V4NicToIP[nicName].String()) {
			return s.V4NicToIP[nicName], nil, s.NicToMac[nicName], nil
		}
		s.releaseAddr(podName, nicName)
	}

	pool := s.IPPools[ippoolName]
	if pool == nil {
		return nil, nil, "", ErrNoAvailable
	}

	if pool.V4Free.Len() == 0 {
		if pool.V4Released.Len() == 0 {
			return nil, nil, "", ErrNoAvailable
		}
		pool.V4Free = pool.V4Released
		pool.V4Released = NewEmptyIPRangeList()
	}

	skipped := make([]IP, 0, len(skippedAddrs))
	for _, s := range skippedAddrs {
		if ip, _ := NewIP(s); ip != nil {
			skipped = append(skipped, ip)
		}
	}
	ip := pool.V4Free.Allocate(skipped)
	if ip == nil {
		klog.Errorf("no free v4 ip in ip pool %s", ippoolName)
		return nil, nil, "", ErrConflict
	}

	pool.V4Available.Remove(ip)
	pool.V4Using.Add(ip)
	s.V4Free.Remove(ip)
	s.V4Available.Remove(ip)
	s.V4Using.Add(ip)

	s.V4NicToIP[nicName] = ip
	s.V4IPToPod[ip.String()] = podName
	s.pushPodNic(podName, nicName)
	if mac == nil {
		return ip, nil, s.GetRandomMac(podName, nicName), nil
	}
	if err := s.GetStaticMac(podName, nicName, *mac, checkConflict); err != nil {
		return nil, nil, "", err
	}
	return ip, nil, *mac, nil
}

func (s *Subnet) getV6RandomAddress(ippoolName, podName, nicName string, mac *string, skippedAddrs []string, checkConflict bool) (IP, IP, string, error) {
	// After 'macAdd' introduced to support only static mac address, pod restart will run into error mac AddressConflict
	// controller will re-enqueue the new pod then wait for old pod deleted and address released.
	// here will return only if both ip and mac exist, otherwise only ip without mac returned will trigger CreatePort error.
	if s.V6NicToIP[nicName] != nil && s.NicToMac[nicName] != "" {
		if !slices.Contains(skippedAddrs, s.V6NicToIP[nicName].String()) {
			return nil, s.V6NicToIP[nicName], s.NicToMac[nicName], nil
		}
		s.releaseAddr(podName, nicName)
	}

	pool := s.IPPools[ippoolName]
	if pool == nil {
		return nil, nil, "", ErrNoAvailable
	}

	if pool.V6Free.Len() == 0 {
		if pool.V6Released.Len() == 0 {
			return nil, nil, "", ErrNoAvailable
		}
		pool.V6Free = pool.V6Released
		pool.V6Released = NewEmptyIPRangeList()
	}

	skipped := make([]IP, 0, len(skippedAddrs))
	for _, s := range skippedAddrs {
		if ip, _ := NewIP(s); ip != nil {
			skipped = append(skipped, ip)
		}
	}
	ip := pool.V6Free.Allocate(skipped)
	if ip == nil {
		klog.Errorf("no free v6 ip in ip pool %s", ippoolName)
		return nil, nil, "", ErrConflict
	}

	pool.V6Available.Remove(ip)
	pool.V6Using.Add(ip)
	s.V6Free.Remove(ip)
	s.V6Available.Remove(ip)
	s.V6Using.Add(ip)

	s.V6NicToIP[nicName] = ip
	s.V6IPToPod[ip.String()] = podName
	s.pushPodNic(podName, nicName)
	if mac == nil {
		return nil, ip, s.GetRandomMac(podName, nicName), nil
	}
	if err := s.GetStaticMac(podName, nicName, *mac, checkConflict); err != nil {
		return nil, nil, "", err
	}
	return nil, ip, *mac, nil
}

func (s *Subnet) GetStaticAddress(podName, nicName string, ip IP, mac *string, force, checkConflict bool) (IP, string, error) {
	var v4, v6 bool
	isAllocated := false
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if ip.To4() != nil {
		v4 = s.V4CIDR != nil
	} else {
		v6 = s.V6CIDR != nil
	}
	if v4 && !s.V4CIDR.Contains(net.IP(ip)) {
		return ip, "", ErrOutOfRange
	}
	if v6 && !s.V6CIDR.Contains(net.IP(ip)) {
		return ip, "", ErrOutOfRange
	}

	var pool *IPPool
	for _, p := range s.IPPools {
		if v4 && p.V4IPs.Contains(ip) {
			pool = p
			break
		}
		if v6 && p.V6IPs.Contains(ip) {
			pool = p
			break
		}
	}

	if pool == nil {
		return ip, "", ErrOutOfRange
	}

	defer func() {
		s.pushPodNic(podName, nicName)
		if isAllocated {
			if v4 {
				s.V4Available.Remove(ip)
				s.V4Using.Add(ip)
				pool.V4Available.Remove(ip)
				pool.V4Using.Add(ip)
			}
			if v6 {
				s.V6Available.Remove(ip)
				s.V6Using.Add(ip)
				pool.V6Available.Remove(ip)
				pool.V6Using.Add(ip)
			}
		}
	}()

	var macStr string
	if mac == nil {
		if m, ok := s.NicToMac[nicName]; ok {
			macStr = m
		} else {
			macStr = s.GetRandomMac(podName, nicName)
		}
	} else {
		if err := s.GetStaticMac(podName, nicName, *mac, checkConflict); err != nil {
			return ip, macStr, err
		}
		macStr = *mac
	}

	if v4 {
		if existPod, ok := s.V4IPToPod[ip.String()]; ok {
			pods := strings.Split(existPod, ",")
			if !slices.Contains(pods, podName) {
				if !checkConflict {
					s.V4NicToIP[nicName] = ip
					s.V4IPToPod[ip.String()] = fmt.Sprintf("%s,%s", s.V4IPToPod[ip.String()], podName)
					return ip, macStr, nil
				}
				klog.Errorf("ip %s has been allocated to %v", ip.String(), pods)
				return ip, macStr, ErrConflict
			}
			if !force {
				return ip, macStr, nil
			}
		}

		if pool.V4Reserved.Contains(ip) {
			s.V4NicToIP[nicName] = ip
			s.V4IPToPod[ip.String()] = podName
			// ip allocated from excludeIPs should be recorded in usingIPs since when the ip is removed from excludeIPs, there's no way to update usingIPs
			isAllocated = true
			return ip, macStr, nil
		}

		if pool.V4Free.Remove(ip) {
			s.V4Free.Remove(ip)
			s.V4NicToIP[nicName] = ip
			s.V4IPToPod[ip.String()] = podName
			isAllocated = true
			return ip, macStr, nil
		} else if pool.V4Released.Remove(ip) {
			s.V4NicToIP[nicName] = ip
			s.V4IPToPod[ip.String()] = podName
			isAllocated = true
			return ip, macStr, nil
		}
	} else if v6 {
		if existPod, ok := s.V6IPToPod[ip.String()]; ok {
			pods := strings.Split(existPod, ",")
			if !slices.Contains(pods, podName) {
				if !checkConflict {
					s.V6NicToIP[nicName] = ip
					s.V6IPToPod[ip.String()] = fmt.Sprintf("%s,%s", s.V6IPToPod[ip.String()], podName)
					return ip, macStr, nil
				}
				klog.Errorf("ip %s has been allocated to %v", ip.String(), pods)
				return ip, macStr, ErrConflict
			}
			if !force {
				return ip, macStr, nil
			}
		}

		if pool.V6Reserved.Contains(ip) {
			s.V6NicToIP[nicName] = ip
			s.V6IPToPod[ip.String()] = podName
			isAllocated = true
			return ip, macStr, nil
		}

		if pool.V6Free.Remove(ip) {
			s.V6Free.Remove(ip)
			s.V6NicToIP[nicName] = ip
			s.V6IPToPod[ip.String()] = podName
			isAllocated = true
			return ip, macStr, nil
		} else if pool.V6Released.Remove(ip) {
			s.V6NicToIP[nicName] = ip
			s.V6IPToPod[ip.String()] = podName
			isAllocated = true
			return ip, macStr, nil
		}
	}
	return ip, macStr, ErrNoAvailable
}

func (s *Subnet) releaseAddr(podName, nicName string) {
	var ip IP
	var mac string
	var ok, changed bool
	if ip, ok = s.V4NicToIP[nicName]; ok {
		oldPods := strings.Split(s.V4IPToPod[ip.String()], ",")
		if len(oldPods) > 1 {
			newPods := util.RemoveString(oldPods, podName)
			s.V4IPToPod[ip.String()] = strings.Join(newPods, ",")
		} else {
			delete(s.V4NicToIP, nicName)
			delete(s.V4IPToPod, ip.String())
			if mac, ok = s.NicToMac[nicName]; ok {
				delete(s.NicToMac, nicName)
				delete(s.MacToPod, mac)
			}

			// When CIDR changed, do not relocate ip to CIDR list
			if !s.V4CIDR.Contains(net.IP(ip)) {
				// Continue to release IPv6 address
				klog.Infof("release v4 %s mac %s from subnet %s for %s, ignore ip", ip, mac, s.Name, podName)
				changed = true
			}

			if s.V4Reserved.Contains(ip) {
				klog.Infof("release v4 %s mac %s from subnet %s for %s, ip is in reserved list", ip, mac, s.Name, podName)
				changed = true
			}

			s.V4Available.Add(ip)
			s.V4Using.Remove(ip)
			for _, pool := range s.IPPools {
				if pool.V4Using.Remove(ip) {
					pool.V4Available.Add(ip)
					if !changed {
						if pool.V4Released.Add(ip) {
							klog.Infof("release v4 %s mac %s from subnet %s for %s, add ip to released list", ip, mac, s.Name, podName)
						}
					}
					break
				}
			}
		}
	}
	if ip, ok = s.V6NicToIP[nicName]; ok {
		oldPods := strings.Split(s.V6IPToPod[ip.String()], ",")
		if len(oldPods) > 1 {
			newPods := util.RemoveString(oldPods, podName)
			s.V6IPToPod[ip.String()] = strings.Join(newPods, ",")
		} else {
			delete(s.V6NicToIP, nicName)
			delete(s.V6IPToPod, ip.String())
			if mac, ok = s.NicToMac[nicName]; ok {
				delete(s.NicToMac, nicName)
				delete(s.MacToPod, mac)
			}
			changed = false
			// When CIDR changed, do not relocate ip to CIDR list
			if !s.V6CIDR.Contains(net.IP(ip)) {
				klog.Infof("release v6 %s mac %s from subnet %s for %s, ignore ip", ip, mac, s.Name, podName)
				changed = true
			}

			if s.V6Reserved.Contains(ip) {
				klog.Infof("release v6 %s mac %s from subnet %s for %s, ip is in reserved list", ip, mac, s.Name, podName)
				changed = true
			}

			s.V6Available.Add(ip)
			s.V6Using.Remove(ip)
			for _, pool := range s.IPPools {
				if pool.V6Using.Remove(ip) {
					pool.V6Available.Add(ip)
					if !changed {
						if pool.V6Released.Add(ip) {
							klog.Infof("release v6 %s mac %s from subnet %s for %s, add ip to released list", ip, mac, s.Name, podName)
						}
					}
					break
				}
			}
		}
	}
}

func (s *Subnet) ReleaseAddress(podName string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	for _, nicName := range s.PodToNicList[podName] {
		s.releaseAddr(podName, nicName)
		s.popPodNic(podName, nicName)
	}
}

func (s *Subnet) ReleaseAddressWithNicName(podName, nicName string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.releaseAddr(podName, nicName)
	s.popPodNic(podName, nicName)
}

func (s *Subnet) ContainAddress(address IP) bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	if _, ok := s.V4IPToPod[address.String()]; ok {
		return true
	} else if _, ok := s.V6IPToPod[address.String()]; ok {
		return true
	}
	return false
}

// This func is only called in ipam.GetPodAddress, move mutex to caller
func (s *Subnet) GetPodAddress(_, nicName string) (IP, IP, string, string) {
	switch s.Protocol {
	case kubeovnv1.ProtocolIPv4:
		ip, mac := s.V4NicToIP[nicName], s.NicToMac[nicName]
		return ip, nil, mac, kubeovnv1.ProtocolIPv4
	case kubeovnv1.ProtocolIPv6:
		ip, mac := s.V6NicToIP[nicName], s.NicToMac[nicName]
		return nil, ip, mac, kubeovnv1.ProtocolIPv6
	default:
		v4IP, v6IP, mac := s.V4NicToIP[nicName], s.V6NicToIP[nicName], s.NicToMac[nicName]
		return v4IP, v6IP, mac, kubeovnv1.ProtocolDual
	}
}

func (s *Subnet) isIPAssignedToOtherPod(ip, podName string) (string, bool) {
	if existPod, ok := s.V4IPToPod[ip]; ok {
		klog.V(4).Infof("v4 check ip assigned, existPod %s, podName %s", existPod, podName)
		pods := strings.Split(existPod, ",")
		if !slices.Contains(pods, podName) {
			return existPod, true
		}
	}
	if existPod, ok := s.V6IPToPod[ip]; ok {
		klog.V(4).Infof("v6 check ip assigned, existPod %s, podName %s", existPod, podName)
		pods := strings.Split(existPod, ",")
		if !slices.Contains(pods, podName) {
			return existPod, true
		}
	}
	return "", false
}

func (s *Subnet) AddOrUpdateIPPool(name string, ips []string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	pool := &IPPool{
		V4IPs:       NewEmptyIPRangeList(),
		V6IPs:       NewEmptyIPRangeList(),
		V4Free:      NewEmptyIPRangeList(),
		V6Free:      NewEmptyIPRangeList(),
		V4Available: NewEmptyIPRangeList(),
		V6Available: NewEmptyIPRangeList(),
		V4Reserved:  NewEmptyIPRangeList(),
		V6Reserved:  NewEmptyIPRangeList(),
		V4Released:  NewEmptyIPRangeList(),
		V6Released:  NewEmptyIPRangeList(),
		V4Using:     NewEmptyIPRangeList(),
		V6Using:     NewEmptyIPRangeList(),
	}

	var err error
	v4IPs, v6IPs := util.SplitIpsByProtocol(ips)
	if s.V4CIDR != nil {
		if pool.V4IPs, err = NewIPRangeListFrom(v4IPs...); err != nil {
			return err
		}
		for k, v := range s.IPPools {
			if k == "" || k == name {
				continue
			}
			if r := pool.V4IPs.Intersect(v.V4IPs); r.Len() != 0 {
				return fmt.Errorf("ippool %s has conflict IPs with ippool %s: %s", name, k, r.String())
			}
		}

		firstIP, _ := util.FirstIP(s.V4CIDR.String())
		lastIP, _ := util.LastIP(s.V4CIDR.String())
		pool.V4Reserved = s.V4Reserved.Intersect(pool.V4IPs)
		pool.V4Using = s.V4Using.Intersect(pool.V4IPs)
		pool.V4Free, _ = NewIPRangeListFrom(fmt.Sprintf("%s..%s", firstIP, lastIP))
		pool.V4Free = pool.V4Free.Intersect(pool.V4IPs).Separate(pool.V4Using).Separate(pool.V4Reserved)
	}
	if s.V6CIDR != nil {
		if pool.V6IPs, err = NewIPRangeListFrom(v6IPs...); err != nil {
			return err
		}
		for k, v := range s.IPPools {
			if k == "" || k == name {
				continue
			}
			if r := pool.V6IPs.Intersect(v.V6IPs); r.Len() != 0 {
				return fmt.Errorf("ippool %s has conflict IPs with ippool %s: %s", name, k, r.String())
			}
		}

		firstIP, _ := util.FirstIP(s.V6CIDR.String())
		lastIP, _ := util.LastIP(s.V6CIDR.String())
		pool.V6Reserved = s.V6Reserved.Intersect(pool.V6IPs)
		pool.V6Using = s.V6Using.Intersect(pool.V6IPs)
		pool.V6Free, _ = NewIPRangeListFrom(fmt.Sprintf("%s..%s", firstIP, lastIP))
		pool.V6Free = pool.V6Free.Intersect(pool.V6IPs).Separate(pool.V6Using).Separate(pool.V6Reserved)
	}

	defaultPool := s.IPPools[""]
	if p := s.IPPools[name]; p != nil {
		defaultPool.V4IPs = defaultPool.V4IPs.Merge(p.V4IPs).Separate(pool.V4IPs)
		defaultPool.V6IPs = defaultPool.V6IPs.Merge(p.V6IPs).Separate(pool.V6IPs)
		defaultPool.V4Free = defaultPool.V4Available.Merge(p.V4Available).Separate(pool.V4Free)
		defaultPool.V6Free = defaultPool.V6Available.Merge(p.V6Available).Separate(pool.V6Free)
		defaultPool.V4Using = defaultPool.V4Using.Merge(p.V4Using).Separate(pool.V4Using)
		defaultPool.V6Using = defaultPool.V6Using.Merge(p.V6Using).Separate(pool.V6Using)
		defaultPool.V4Reserved = defaultPool.V4Reserved.Merge(p.V4Reserved).Separate(pool.V4Reserved)
		defaultPool.V6Reserved = defaultPool.V6Reserved.Merge(p.V6Reserved).Separate(pool.V6Reserved)
		defaultPool.V4Available = defaultPool.V4Free.Clone()
		defaultPool.V6Available = defaultPool.V6Free.Clone()
	} else {
		defaultPool.V4IPs = defaultPool.V4IPs.Separate(pool.V4IPs)
		defaultPool.V6IPs = defaultPool.V6IPs.Separate(pool.V6IPs)
		defaultPool.V4Free = defaultPool.V4Available.Separate(pool.V4Free)
		defaultPool.V6Free = defaultPool.V6Available.Separate(pool.V6Free)
		defaultPool.V4Using = defaultPool.V4Using.Separate(pool.V4Using)
		defaultPool.V6Using = defaultPool.V6Using.Separate(pool.V6Using)
		defaultPool.V4Reserved = defaultPool.V4Reserved.Separate(pool.V4Reserved)
		defaultPool.V6Reserved = defaultPool.V6Reserved.Separate(pool.V6Reserved)
		defaultPool.V4Available = defaultPool.V4Free.Clone()
		defaultPool.V6Available = defaultPool.V6Free.Clone()
	}
	defaultPool.V4Released = NewEmptyIPRangeList()
	defaultPool.V6Released = NewEmptyIPRangeList()
	pool.V4Available = pool.V4Free.Clone()
	pool.V6Available = pool.V6Free.Clone()
	s.IPPools[name] = pool

	return nil
}

func (s *Subnet) RemoveIPPool(name string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	p := s.IPPools[name]
	if p == nil {
		return
	}

	defaultPool := s.IPPools[""]
	defaultPool.V4Free = defaultPool.V4Free.Merge(p.V4Free)
	defaultPool.V6Free = defaultPool.V6Free.Merge(p.V6Free)
	defaultPool.V4Available = defaultPool.V4Available.Merge(p.V4Available)
	defaultPool.V6Available = defaultPool.V6Available.Merge(p.V6Available)
	defaultPool.V4Using = defaultPool.V4Using.Merge(p.V4Using)
	defaultPool.V6Using = defaultPool.V6Using.Merge(p.V6Using)
	defaultPool.V4Reserved = defaultPool.V4Reserved.Merge(p.V4Reserved)
	defaultPool.V6Reserved = defaultPool.V6Reserved.Merge(p.V6Reserved)
	defaultPool.V4Released = defaultPool.V4Released.Merge(p.V4Released)
	defaultPool.V6Released = defaultPool.V6Released.Merge(p.V6Released)

	delete(s.IPPools, name)
}

func (s *Subnet) IPPoolStatistics(ippool string) (
	v4Available, v4Using, v6Available, v6Using internal.BigInt,
	v4AvailableRange, v4UsingRange, v6AvailableRange, v6UsingRange string,
) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	p := s.IPPools[ippool]
	if p == nil {
		return
	}

	v4Available = p.V4Available.Count()
	v6Available = p.V6Available.Count()
	v4Using = p.V4Using.Count()
	v6Using = p.V6Using.Count()
	v4AvailableRange = p.V4Available.String()
	v6AvailableRange = p.V6Available.String()
	v4UsingRange = p.V4Using.String()
	v6UsingRange = p.V6Using.String()

	return
}
