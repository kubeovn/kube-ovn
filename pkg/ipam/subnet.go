package ipam

import (
	"fmt"
	"net"
	"strings"
	"sync"

	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
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
		if _, cidr, err := net.ParseCIDR(cidrBlock); err != nil {
			return nil, ErrInvalidCIDR
		} else {
			cidrs = append(cidrs, cidr)
		}
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
		V4Free:       NewIPRangeList(),
		V6Free:       NewIPRangeList(),
		V4Reserved:   v4Reserved,
		V6Reserved:   v6Reserved,
		V4Using:      NewIPRangeList(),
		V6Using:      NewIPRangeList(),
		V4NicToIP:    map[string]IP{},
		V6NicToIP:    map[string]IP{},
		V4IPToPod:    map[string]string{},
		V6IPToPod:    map[string]string{},
		MacToPod:     map[string]string{},
		NicToMac:     map[string]string{},
		PodToNicList: map[string][]string{},
		IPPools:      make(map[string]*IPPool, 0),
	}
	if protocol == kubeovnv1.ProtocolIPv4 {
		firstIP, _ := util.FirstIP(cidrStr)
		lastIP, _ := util.LastIP(cidrStr)
		subnet.V4CIDR = cidrs[0]
		subnet.V4Free, _ = NewIPRangeListFrom(fmt.Sprintf("%s..%s", firstIP, lastIP))
	} else if protocol == kubeovnv1.ProtocolIPv6 {
		firstIP, _ := util.FirstIP(cidrStr)
		lastIP, _ := util.LastIP(cidrStr)
		subnet.V6CIDR = cidrs[0]
		subnet.V6Free, _ = NewIPRangeListFrom(fmt.Sprintf("%s..%s", firstIP, lastIP))
	} else {
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
		V4Released: NewIPRangeList(),
		V6Released: NewIPRangeList(),
		V4Using:    NewIPRangeList(),
		V6Using:    NewIPRangeList(),
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

func (subnet *Subnet) GetRandomMac(podName, nicName string) string {
	if mac, ok := subnet.NicToMac[nicName]; ok {
		return mac
	}
	for {
		mac := util.GenerateMac()
		if _, ok := subnet.MacToPod[mac]; !ok {
			subnet.MacToPod[mac] = podName
			subnet.NicToMac[nicName] = mac
			return mac
		}
	}
}

func (subnet *Subnet) GetStaticMac(podName, nicName, mac string, checkConflict bool) error {
	if mac == "" {
		return nil
	}
	if checkConflict {
		if p, ok := subnet.MacToPod[mac]; ok && p != podName {
			return ErrConflict
		}
	}
	subnet.MacToPod[mac] = podName
	subnet.NicToMac[nicName] = mac
	return nil
}

func (subnet *Subnet) pushPodNic(podName, nicName string) {
	if subnet.V4NicToIP[nicName] != nil || subnet.V6NicToIP[nicName] != nil || subnet.NicToMac[nicName] != "" {
		subnet.PodToNicList[podName] = util.UniqString(append(subnet.PodToNicList[podName], nicName))
	}
}

func (subnet *Subnet) popPodNic(podName, nicName string) {
	subnet.PodToNicList[podName] = util.RemoveString(subnet.PodToNicList[podName], nicName)
	if subnet.PodToNicList[podName] == nil {
		delete(subnet.PodToNicList, podName)
	}
}

func (subnet *Subnet) GetRandomAddress(poolName, podName, nicName string, mac *string, skippedAddrs []string, checkConflict bool) (IP, IP, string, error) {
	subnet.mutex.Lock()
	defer func() {
		subnet.pushPodNic(podName, nicName)
		subnet.mutex.Unlock()
	}()

	if subnet.Protocol == kubeovnv1.ProtocolDual {
		return subnet.getDualRandomAddress(poolName, podName, nicName, mac, skippedAddrs, checkConflict)
	} else if subnet.Protocol == kubeovnv1.ProtocolIPv4 {
		return subnet.getV4RandomAddress(poolName, podName, nicName, mac, skippedAddrs, checkConflict)
	} else {
		return subnet.getV6RandomAddress(poolName, podName, nicName, mac, skippedAddrs, checkConflict)
	}
}

func (subnet *Subnet) getDualRandomAddress(poolName, podName, nicName string, mac *string, skippedAddrs []string, checkConflict bool) (IP, IP, string, error) {
	v4IP, _, _, err := subnet.getV4RandomAddress(poolName, podName, nicName, mac, skippedAddrs, checkConflict)
	if err != nil {
		return nil, nil, "", err
	}
	_, v6IP, macStr, err := subnet.getV6RandomAddress(poolName, podName, nicName, mac, skippedAddrs, checkConflict)
	if err != nil {
		return nil, nil, "", err
	}

	// allocated IPv4 address may be released in getV6RandomAddress()
	if !subnet.V4NicToIP[nicName].Equal(v4IP) {
		v4IP, _, _, _ = subnet.getV4RandomAddress(poolName, podName, nicName, mac, skippedAddrs, checkConflict)
	}

	return v4IP, v6IP, macStr, nil
}

func (subnet *Subnet) getV4RandomAddress(ippoolName, podName, nicName string, mac *string, skippedAddrs []string, checkConflict bool) (IP, IP, string, error) {
	// After 'macAdd' introduced to support only static mac address, pod restart will run into error mac AddressConflict
	// controller will re-enqueue the new pod then wait for old pod deleted and address released.
	// here will return only if both ip and mac exist, otherwise only ip without mac returned will trigger CreatePort error.
	if subnet.V4NicToIP[nicName] != nil && subnet.NicToMac[nicName] != "" {
		if !util.ContainsString(skippedAddrs, subnet.V4NicToIP[nicName].String()) {
			return subnet.V4NicToIP[nicName], nil, subnet.NicToMac[nicName], nil
		}
		subnet.releaseAddr(podName, nicName)
	}

	pool := subnet.IPPools[ippoolName]
	if pool == nil {
		return nil, nil, "", ErrNoAvailable
	}

	if pool.V4Free.Len() == 0 {
		if pool.V4Released.Len() == 0 {
			return nil, nil, "", ErrNoAvailable
		}
		pool.V4Free = pool.V4Released
		pool.V4Released = NewIPRangeList()
	}

	skipped := make([]IP, 0, len(skippedAddrs))
	for _, s := range skippedAddrs {
		if ip, _ := NewIP(s); ip != nil {
			skipped = append(skipped, ip)
		}
	}
	ip := pool.V4Free.Allocate(skipped)
	if ip == nil {
		return nil, nil, "", ErrConflict
	}

	pool.V4Available.Remove(ip)
	pool.V4Using.Add(ip)
	subnet.V4Free.Remove(ip)
	subnet.V4Available.Remove(ip)
	subnet.V4Using.Add(ip)

	subnet.V4NicToIP[nicName] = ip
	subnet.V4IPToPod[ip.String()] = podName
	subnet.pushPodNic(podName, nicName)
	if mac == nil {
		return ip, nil, subnet.GetRandomMac(podName, nicName), nil
	} else {
		if err := subnet.GetStaticMac(podName, nicName, *mac, checkConflict); err != nil {
			return nil, nil, "", err
		}
		return ip, nil, *mac, nil
	}
}

func (subnet *Subnet) getV6RandomAddress(ippoolName, podName, nicName string, mac *string, skippedAddrs []string, checkConflict bool) (IP, IP, string, error) {
	// After 'macAdd' introduced to support only static mac address, pod restart will run into error mac AddressConflict
	// controller will re-enqueue the new pod then wait for old pod deleted and address released.
	// here will return only if both ip and mac exist, otherwise only ip without mac returned will trigger CreatePort error.
	if subnet.V6NicToIP[nicName] != nil && subnet.NicToMac[nicName] != "" {
		if !util.ContainsString(skippedAddrs, subnet.V6NicToIP[nicName].String()) {
			return nil, subnet.V6NicToIP[nicName], subnet.NicToMac[nicName], nil
		}
		subnet.releaseAddr(podName, nicName)
	}

	pool := subnet.IPPools[ippoolName]
	if pool == nil {
		return nil, nil, "", ErrNoAvailable
	}

	if pool.V6Free.Len() == 0 {
		if pool.V6Released.Len() == 0 {
			return nil, nil, "", ErrNoAvailable
		}
		pool.V6Free = pool.V6Released
		pool.V6Released = NewIPRangeList()
	}

	skipped := make([]IP, 0, len(skippedAddrs))
	for _, s := range skippedAddrs {
		if ip, _ := NewIP(s); ip != nil {
			skipped = append(skipped, ip)
		}
	}
	ip := pool.V6Free.Allocate(skipped)
	if ip == nil {
		return nil, nil, "", ErrConflict
	}

	pool.V6Available.Remove(ip)
	pool.V6Using.Add(ip)
	subnet.V6Free.Remove(ip)
	subnet.V6Available.Remove(ip)
	subnet.V6Using.Add(ip)

	subnet.V6NicToIP[nicName] = ip
	subnet.V6IPToPod[ip.String()] = podName
	subnet.pushPodNic(podName, nicName)
	if mac == nil {
		return nil, ip, subnet.GetRandomMac(podName, nicName), nil
	} else {
		if err := subnet.GetStaticMac(podName, nicName, *mac, checkConflict); err != nil {
			return nil, nil, "", err
		}
		return nil, ip, *mac, nil
	}
}

func (subnet *Subnet) GetStaticAddress(podName, nicName string, ip IP, mac *string, force bool, checkConflict bool) (IP, string, error) {
	var v4, v6 bool
	isAllocated := false
	subnet.mutex.Lock()

	if ip.To4() != nil {
		v4 = subnet.V4CIDR != nil
	} else {
		v6 = subnet.V6CIDR != nil
	}
	if v4 && !subnet.V4CIDR.Contains(net.IP(ip)) {
		return ip, "", ErrOutOfRange
	}
	if v6 && !subnet.V6CIDR.Contains(net.IP(ip)) {
		return ip, "", ErrOutOfRange
	}

	var pool *IPPool
	for _, p := range subnet.IPPools {
		if v4 && p.V4IPs.Contains(ip) {
			pool = p
			break
		}
		if v6 && p.V6IPs.Contains(ip) {
			pool = p
			break
		}
	}

	defer func() {
		subnet.pushPodNic(podName, nicName)
		if isAllocated {
			if v4 {
				subnet.V4Available.Remove(ip)
				subnet.V4Using.Add(ip)
				pool.V4Available.Remove(ip)
				pool.V4Using.Add(ip)
			}
			if v6 {
				subnet.V6Available.Remove(ip)
				subnet.V6Using.Add(ip)
				pool.V6Available.Remove(ip)
				pool.V6Using.Add(ip)
			}
		}
		subnet.mutex.Unlock()
	}()

	var macStr string
	if mac == nil {
		if m, ok := subnet.NicToMac[nicName]; ok {
			macStr = m
		} else {
			macStr = subnet.GetRandomMac(podName, nicName)
		}
	} else {
		if err := subnet.GetStaticMac(podName, nicName, *mac, checkConflict); err != nil {
			return ip, macStr, err
		}
		macStr = *mac
	}

	if v4 {
		if existPod, ok := subnet.V4IPToPod[ip.String()]; ok {
			pods := strings.Split(existPod, ",")
			if !util.ContainsString(pods, podName) {
				if !checkConflict {
					subnet.V4NicToIP[nicName] = ip
					subnet.V4IPToPod[ip.String()] = fmt.Sprintf("%s,%s", subnet.V4IPToPod[ip.String()], podName)
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
			subnet.V4NicToIP[nicName] = ip
			subnet.V4IPToPod[ip.String()] = podName
			return ip, macStr, nil
		}

		if pool.V4Free.Remove(ip) {
			subnet.V4Free.Remove(ip)
			subnet.V4NicToIP[nicName] = ip
			subnet.V4IPToPod[ip.String()] = podName
			isAllocated = true
			return ip, macStr, nil
		} else if pool.V4Released.Remove(ip) {
			subnet.V4NicToIP[nicName] = ip
			subnet.V4IPToPod[ip.String()] = podName
			isAllocated = true
			return ip, macStr, nil
		}
	} else if v6 {
		if existPod, ok := subnet.V6IPToPod[ip.String()]; ok {
			pods := strings.Split(existPod, ",")
			if !util.ContainsString(pods, podName) {
				if !checkConflict {
					subnet.V6NicToIP[nicName] = ip
					subnet.V6IPToPod[ip.String()] = fmt.Sprintf("%s,%s", subnet.V6IPToPod[ip.String()], podName)
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
			subnet.V6NicToIP[nicName] = ip
			subnet.V6IPToPod[ip.String()] = podName
			return ip, macStr, nil
		}

		if pool.V6Free.Remove(ip) {
			subnet.V6Free.Remove(ip)
			subnet.V6NicToIP[nicName] = ip
			subnet.V6IPToPod[ip.String()] = podName
			isAllocated = true
			return ip, macStr, nil
		} else if pool.V6Released.Remove(ip) {
			subnet.V6NicToIP[nicName] = ip
			subnet.V6IPToPod[ip.String()] = podName
			isAllocated = true
			return ip, macStr, nil
		}
	}
	return ip, macStr, ErrNoAvailable
}

func (subnet *Subnet) releaseAddr(podName, nicName string) {
	var ip IP
	var mac string
	var ok, changed bool
	if ip, ok = subnet.V4NicToIP[nicName]; ok {
		oldPods := strings.Split(subnet.V4IPToPod[ip.String()], ",")
		if len(oldPods) > 1 {
			newPods := util.RemoveString(oldPods, podName)
			subnet.V4IPToPod[ip.String()] = strings.Join(newPods, ",")
		} else {
			delete(subnet.V4NicToIP, nicName)
			delete(subnet.V4IPToPod, ip.String())
			if mac, ok = subnet.NicToMac[nicName]; ok {
				delete(subnet.NicToMac, nicName)
				delete(subnet.MacToPod, mac)
			}

			// When CIDR changed, do not relocate ip to CIDR list
			if !subnet.V4CIDR.Contains(net.IP(ip)) {
				// Continue to release IPv6 address
				klog.Infof("release v4 %s mac %s from subnet %s for %s, ignore ip", ip, mac, subnet.Name, podName)
				changed = true
			}

			if subnet.V4Reserved.Contains(ip) {
				klog.Infof("release v4 %s mac %s from subnet %s for %s, ip is in reserved list", ip, mac, subnet.Name, podName)
				changed = true
			}

			subnet.V4Available.Add(ip)
			subnet.V4Using.Remove(ip)
			for _, pool := range subnet.IPPools {
				if pool.V4Using.Remove(ip) {
					pool.V4Available.Add(ip)
					if !changed {
						if pool.V4Released.Add(ip) {
							klog.Infof("release v4 %s mac %s from subnet %s for %s, add ip to released list", ip, mac, subnet.Name, podName)
						}
					}
					break
				}
			}
		}
	}
	if ip, ok = subnet.V6NicToIP[nicName]; ok {
		oldPods := strings.Split(subnet.V6IPToPod[ip.String()], ",")
		if len(oldPods) > 1 {
			newPods := util.RemoveString(oldPods, podName)
			subnet.V6IPToPod[ip.String()] = strings.Join(newPods, ",")
		} else {
			delete(subnet.V6NicToIP, nicName)
			delete(subnet.V6IPToPod, ip.String())
			if mac, ok = subnet.NicToMac[nicName]; ok {
				delete(subnet.NicToMac, nicName)
				delete(subnet.MacToPod, mac)
			}
			changed = false
			// When CIDR changed, do not relocate ip to CIDR list
			if !subnet.V6CIDR.Contains(net.IP(ip)) {
				klog.Infof("release v6 %s mac %s from subnet %s for %s, ignore ip", ip, mac, subnet.Name, podName)
				changed = true
			}

			if subnet.V6Reserved.Contains(ip) {
				klog.Infof("release v6 %s mac %s from subnet %s for %s, ip is in reserved list", ip, mac, subnet.Name, podName)
				changed = true
			}

			subnet.V6Available.Add(ip)
			subnet.V6Using.Remove(ip)
			for _, pool := range subnet.IPPools {
				if pool.V6Using.Remove(ip) {
					pool.V6Available.Add(ip)
					if !changed {
						if pool.V6Released.Add(ip) {
							klog.Infof("release v6 %s mac %s from subnet %s for %s, add ip to released list", ip, mac, subnet.Name, podName)
						}
					}
					break
				}
			}
		}
	}
}

func (subnet *Subnet) ReleaseAddress(podName string) {
	subnet.mutex.Lock()
	defer subnet.mutex.Unlock()
	for _, nicName := range subnet.PodToNicList[podName] {
		subnet.releaseAddr(podName, nicName)
		subnet.popPodNic(podName, nicName)
	}
}

func (subnet *Subnet) ReleaseAddressWithNicName(podName, nicName string) {
	subnet.mutex.Lock()
	defer subnet.mutex.Unlock()

	subnet.releaseAddr(podName, nicName)
	subnet.popPodNic(podName, nicName)
}

func (subnet *Subnet) ContainAddress(address IP) bool {
	subnet.mutex.RLock()
	defer subnet.mutex.RUnlock()

	if _, ok := subnet.V4IPToPod[address.String()]; ok {
		return true
	} else if _, ok := subnet.V6IPToPod[address.String()]; ok {
		return true
	}
	return false
}

// This func is only called in ipam.GetPodAddress, move mutex to caller
func (subnet *Subnet) GetPodAddress(podName, nicName string) (IP, IP, string, string) {
	if subnet.Protocol == kubeovnv1.ProtocolIPv4 {
		ip, mac := subnet.V4NicToIP[nicName], subnet.NicToMac[nicName]
		return ip, nil, mac, kubeovnv1.ProtocolIPv4
	} else if subnet.Protocol == kubeovnv1.ProtocolIPv6 {
		ip, mac := subnet.V6NicToIP[nicName], subnet.NicToMac[nicName]
		return nil, ip, mac, kubeovnv1.ProtocolIPv6
	} else {
		v4IP, v6IP, mac := subnet.V4NicToIP[nicName], subnet.V6NicToIP[nicName], subnet.NicToMac[nicName]
		return v4IP, v6IP, mac, kubeovnv1.ProtocolDual
	}
}

func (subnet *Subnet) isIPAssignedToOtherPod(ip, podName string) (string, bool) {
	if existPod, ok := subnet.V4IPToPod[ip]; ok {
		klog.V(4).Infof("v4 check ip assigned, existPod %s, podName %s", existPod, podName)
		pods := strings.Split(existPod, ",")
		if !util.ContainsString(pods, podName) {
			return existPod, true
		}
	}
	if existPod, ok := subnet.V6IPToPod[ip]; ok {
		klog.V(4).Infof("v6 check ip assigned, existPod %s, podName %s", existPod, podName)
		pods := strings.Split(existPod, ",")
		if !util.ContainsString(pods, podName) {
			return existPod, true
		}
	}
	return "", false
}

func (s *Subnet) AddOrUpdateIPPool(name string, ips []string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	pool := &IPPool{
		V4IPs:       NewIPRangeList(),
		V6IPs:       NewIPRangeList(),
		V4Free:      NewIPRangeList(),
		V6Free:      NewIPRangeList(),
		V4Available: NewIPRangeList(),
		V6Available: NewIPRangeList(),
		V4Reserved:  NewIPRangeList(),
		V6Reserved:  NewIPRangeList(),
		V4Released:  NewIPRangeList(),
		V6Released:  NewIPRangeList(),
		V4Using:     NewIPRangeList(),
		V6Using:     NewIPRangeList(),
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
	defaultPool.V4Released = NewIPRangeList()
	defaultPool.V6Released = NewIPRangeList()
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
	v4Available, v4Using, v6Available, v6Using float64,
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
