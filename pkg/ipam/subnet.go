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
	Name             string
	mutex            sync.RWMutex
	Protocol         string
	V4CIDR           *net.IPNet
	V4FreeIPList     IPRangeList
	V4ReleasedIPList IPRangeList
	V4ReservedIPList IPRangeList
	V4AvailIPList    IPRangeList
	V4UsingIPList    IPRangeList
	V4NicToIP        map[string]IP
	V4IPToPod        map[IP]string
	V6CIDR           *net.IPNet
	V6FreeIPList     IPRangeList
	V6ReleasedIPList IPRangeList
	V6ReservedIPList IPRangeList
	V6AvailIPList    IPRangeList
	V6UsingIPList    IPRangeList
	V6NicToIP        map[string]IP
	V6IPToPod        map[IP]string
	NicToMac         map[string]string
	MacToPod         map[string]string
	PodToNicList     map[string][]string
	V4Gw             string
	V6Gw             string
}

func NewSubnet(name, cidrStr string, excludeIps []string) (*Subnet, error) {
	excludeIps = util.ExpandExcludeIPs(excludeIps, cidrStr)

	var cidrs []*net.IPNet
	for _, cidrBlock := range strings.Split(cidrStr, ",") {
		if _, cidr, err := net.ParseCIDR(cidrBlock); err != nil {
			return nil, ErrInvalidCIDR
		} else {
			cidrs = append(cidrs, cidr)
		}
	}

	// subnet.Spec.ExcludeIps contains both v4 and v6 addresses
	v4ExcludeIps, v6ExcludeIps := util.SplitIpsByProtocol(excludeIps)

	subnet := Subnet{}
	protocol := util.CheckProtocol(cidrStr)
	if protocol == kubeovnv1.ProtocolIPv4 {
		firstIP, _ := util.FirstIP(cidrStr)
		lastIP, _ := util.LastIP(cidrStr)

		subnet = Subnet{
			Name:             name,
			mutex:            sync.RWMutex{},
			Protocol:         protocol,
			V4CIDR:           cidrs[0],
			V4FreeIPList:     IPRangeList{&IPRange{Start: IP(firstIP), End: IP(lastIP)}},
			V4ReleasedIPList: IPRangeList{},
			V4ReservedIPList: convertExcludeIps(v4ExcludeIps),
			V4AvailIPList:    IPRangeList{&IPRange{Start: IP(firstIP), End: IP(lastIP)}},
			V4UsingIPList:    IPRangeList{},
			V4NicToIP:        map[string]IP{},
			V4IPToPod:        map[IP]string{},
			V6NicToIP:        map[string]IP{},
			V6IPToPod:        map[IP]string{},
			MacToPod:         map[string]string{},
			NicToMac:         map[string]string{},
			PodToNicList:     map[string][]string{},
		}
		subnet.joinFreeWithReserve()
	} else if protocol == kubeovnv1.ProtocolIPv6 {
		firstIP, _ := util.FirstIP(cidrStr)
		lastIP, _ := util.LastIP(cidrStr)

		subnet = Subnet{
			Name:             name,
			mutex:            sync.RWMutex{},
			Protocol:         protocol,
			V6CIDR:           cidrs[0],
			V6FreeIPList:     IPRangeList{&IPRange{Start: IP(firstIP), End: IP(lastIP)}},
			V6ReleasedIPList: IPRangeList{},
			V6ReservedIPList: convertExcludeIps(v6ExcludeIps),
			V6AvailIPList:    IPRangeList{&IPRange{Start: IP(firstIP), End: IP(lastIP)}},
			V6UsingIPList:    IPRangeList{},
			V4NicToIP:        map[string]IP{},
			V4IPToPod:        map[IP]string{},
			V6NicToIP:        map[string]IP{},
			V6IPToPod:        map[IP]string{},
			MacToPod:         map[string]string{},
			NicToMac:         map[string]string{},
			PodToNicList:     map[string][]string{},
		}
		subnet.joinFreeWithReserve()
	} else {
		cidrBlocks := strings.Split(cidrStr, ",")
		v4FirstIP, _ := util.FirstIP(cidrBlocks[0])
		v4LastIP, _ := util.LastIP(cidrBlocks[0])
		v6FirstIP, _ := util.FirstIP(cidrBlocks[1])
		v6LastIP, _ := util.LastIP(cidrBlocks[1])

		subnet = Subnet{
			Name:             name,
			mutex:            sync.RWMutex{},
			Protocol:         protocol,
			V4CIDR:           cidrs[0],
			V4FreeIPList:     IPRangeList{&IPRange{Start: IP(v4FirstIP), End: IP(v4LastIP)}},
			V4ReleasedIPList: IPRangeList{},
			V4ReservedIPList: convertExcludeIps(v4ExcludeIps),
			V4AvailIPList:    IPRangeList{&IPRange{Start: IP(v4FirstIP), End: IP(v4LastIP)}},
			V4UsingIPList:    IPRangeList{},
			V4NicToIP:        map[string]IP{},
			V4IPToPod:        map[IP]string{},
			V6CIDR:           cidrs[1],
			V6FreeIPList:     IPRangeList{&IPRange{Start: IP(v6FirstIP), End: IP(v6LastIP)}},
			V6ReleasedIPList: IPRangeList{},
			V6ReservedIPList: convertExcludeIps(v6ExcludeIps),
			V6AvailIPList:    IPRangeList{&IPRange{Start: IP(v6FirstIP), End: IP(v6LastIP)}},
			V6UsingIPList:    IPRangeList{},
			V6NicToIP:        map[string]IP{},
			V6IPToPod:        map[IP]string{},
			MacToPod:         map[string]string{},
			NicToMac:         map[string]string{},
			PodToNicList:     map[string][]string{},
		}
		subnet.joinFreeWithReserve()
	}
	return &subnet, nil
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
	if subnet.V4NicToIP[nicName] != "" || subnet.V6NicToIP[nicName] != "" || subnet.NicToMac[nicName] != "" {
		subnet.PodToNicList[podName] = util.UniqString(append(subnet.PodToNicList[podName], nicName))
	}
}

func (subnet *Subnet) popPodNic(podName, nicName string) {
	subnet.PodToNicList[podName] = util.RemoveString(subnet.PodToNicList[podName], nicName)
	if subnet.PodToNicList[podName] == nil {
		delete(subnet.PodToNicList, podName)
	}
}

func (subnet *Subnet) GetRandomAddress(podName, nicName string, mac string, skippedAddrs []string, checkConflict bool) (IP, IP, string, error) {
	subnet.mutex.Lock()
	defer func() {
		subnet.pushPodNic(podName, nicName)
		subnet.mutex.Unlock()
	}()

	if subnet.Protocol == kubeovnv1.ProtocolDual {
		return subnet.getDualRandomAddress(podName, nicName, mac, skippedAddrs, checkConflict)
	} else if subnet.Protocol == kubeovnv1.ProtocolIPv4 {
		return subnet.getV4RandomAddress(podName, nicName, mac, skippedAddrs, checkConflict)
	} else {
		return subnet.getV6RandomAddress(podName, nicName, mac, skippedAddrs, checkConflict)
	}
}

func (subnet *Subnet) getDualRandomAddress(podName, nicName string, mac string, skippedAddrs []string, checkConflict bool) (IP, IP, string, error) {
	v4IP, _, _, err := subnet.getV4RandomAddress(podName, nicName, mac, skippedAddrs, checkConflict)
	if err != nil {
		return "", "", "", err
	}
	_, v6IP, mac, err := subnet.getV6RandomAddress(podName, nicName, mac, skippedAddrs, checkConflict)
	if err != nil {
		return "", "", "", err
	}

	// allocated IPv4 address may be released in getV6RandomAddress()
	if subnet.V4NicToIP[nicName] != v4IP {
		v4IP, _, _, _ = subnet.getV4RandomAddress(podName, nicName, mac, skippedAddrs, checkConflict)
	}

	return v4IP, v6IP, mac, nil
}

func (subnet *Subnet) getV4RandomAddress(podName, nicName string, mac string, skippedAddrs []string, checkConflict bool) (IP, IP, string, error) {
	// After 'macAdd' introduced to support only static mac address, pod restart will run into error mac AddressConflict
	// controller will re-enqueue the new pod then wait for old pod deleted and address released.
	// here will return only if both ip and mac exist, otherwise only ip without mac returned will trigger CreatePort error.
	if subnet.V4NicToIP[nicName] != "" && subnet.NicToMac[nicName] != "" {
		if !util.ContainsString(skippedAddrs, string(subnet.V4NicToIP[nicName])) {
			return subnet.V4NicToIP[nicName], "", subnet.NicToMac[nicName], nil
		}
		subnet.releaseAddr(podName, nicName)
	}
	if len(subnet.V4FreeIPList) == 0 {
		if len(subnet.V4ReleasedIPList) == 0 {
			return "", "", "", ErrNoAvailable
		}
		subnet.V4FreeIPList = subnet.V4ReleasedIPList
		subnet.V4ReleasedIPList = IPRangeList{}
	}

	var ip IP
	var idx int
	for i, ipr := range subnet.V4FreeIPList {
		for next := ipr.Start; !next.GreaterThan(ipr.End); next = next.Add(1) {
			if !util.ContainsString(skippedAddrs, string(next)) {
				ip = next
				break
			}
		}
		if ip != "" {
			idx = i
			break
		}
	}
	if ip == "" {
		return "", "", "", ErrConflict
	}

	ipr := subnet.V4FreeIPList[idx]
	part1 := &IPRange{Start: ipr.Start, End: ip.Sub(1)}
	part2 := &IPRange{Start: ip.Add(1), End: ipr.End}
	subnet.V4FreeIPList = append(subnet.V4FreeIPList[:idx], subnet.V4FreeIPList[idx+1:]...)
	if !part1.Start.GreaterThan(part1.End) {
		subnet.V4FreeIPList = append(subnet.V4FreeIPList, part1)
	}
	if !part2.Start.GreaterThan(part2.End) {
		subnet.V4FreeIPList = append(subnet.V4FreeIPList, part2)
	}
	if split, NewV4AvailIPRangeList := splitIPRangeList(subnet.V4AvailIPList, ip); split {
		subnet.V4AvailIPList = NewV4AvailIPRangeList
	}

	if merged, NewV4UsingIPRangeList := mergeIPRangeList(subnet.V4UsingIPList, ip); merged {
		subnet.V4UsingIPList = NewV4UsingIPRangeList
	}

	subnet.V4NicToIP[nicName] = ip
	subnet.V4IPToPod[ip] = podName
	subnet.pushPodNic(podName, nicName)
	if mac == "" {
		return ip, "", subnet.GetRandomMac(podName, nicName), nil
	} else {
		if err := subnet.GetStaticMac(podName, nicName, mac, checkConflict); err != nil {
			return "", "", "", err
		}
		return ip, "", mac, nil
	}
}

func (subnet *Subnet) getV6RandomAddress(podName, nicName string, mac string, skippedAddrs []string, checkConflict bool) (IP, IP, string, error) {
	// After 'macAdd' introduced to support only static mac address, pod restart will run into error mac AddressConflict
	// controller will re-enqueue the new pod then wait for old pod deleted and address released.
	// here will return only if both ip and mac exist, otherwise only ip without mac returned will trigger CreatePort error.
	if subnet.V6NicToIP[nicName] != "" && subnet.NicToMac[nicName] != "" {
		if !util.ContainsString(skippedAddrs, string(subnet.V6NicToIP[nicName])) {
			return "", subnet.V6NicToIP[nicName], subnet.NicToMac[nicName], nil
		}
		subnet.releaseAddr(podName, nicName)
	}

	if len(subnet.V6FreeIPList) == 0 {
		if len(subnet.V6ReleasedIPList) == 0 {
			return "", "", "", ErrNoAvailable
		}
		subnet.V6FreeIPList = subnet.V6ReleasedIPList
		subnet.V6ReleasedIPList = IPRangeList{}
	}

	var ip IP
	var idx int
	for i, ipr := range subnet.V6FreeIPList {
		for next := ipr.Start; !next.GreaterThan(ipr.End); next = next.Add(1) {
			if !util.ContainsString(skippedAddrs, string(next)) {
				ip = next
				break
			}
		}
		if ip != "" {
			idx = i
			break
		}
	}
	if ip == "" {
		return "", "", "", ErrConflict
	}

	ipr := subnet.V6FreeIPList[idx]
	part1 := &IPRange{Start: ipr.Start, End: ip.Sub(1)}
	part2 := &IPRange{Start: ip.Add(1), End: ipr.End}
	subnet.V6FreeIPList = append(subnet.V6FreeIPList[:idx], subnet.V6FreeIPList[idx+1:]...)
	if !part1.Start.GreaterThan(part1.End) {
		subnet.V6FreeIPList = append(subnet.V6FreeIPList, part1)
	}
	if !part2.Start.GreaterThan(part2.End) {
		subnet.V6FreeIPList = append(subnet.V6FreeIPList, part2)
	}
	if split, NewV6AvailIPRangeList := splitIPRangeList(subnet.V6AvailIPList, ip); split {
		subnet.V6AvailIPList = NewV6AvailIPRangeList
	}

	if merged, NewV6UsingIPRangeList := mergeIPRangeList(subnet.V6UsingIPList, ip); merged {
		subnet.V6UsingIPList = NewV6UsingIPRangeList
	}

	subnet.V6NicToIP[nicName] = ip
	subnet.V6IPToPod[ip] = podName
	subnet.pushPodNic(podName, nicName)
	if mac == "" {
		return "", ip, subnet.GetRandomMac(podName, nicName), nil
	} else {
		if err := subnet.GetStaticMac(podName, nicName, mac, checkConflict); err != nil {
			return "", "", "", err
		}
		return "", ip, mac, nil
	}
}

func (subnet *Subnet) GetStaticAddress(podName, nicName string, ip IP, mac string, force bool, checkConflict bool) (IP, string, error) {
	var v4, v6 bool
	isAllocated := false
	subnet.mutex.Lock()
	defer func() {
		subnet.pushPodNic(podName, nicName)
		if isAllocated {
			if v4 {
				if split, NewV4AvailIPRangeList := splitIPRangeList(subnet.V4AvailIPList, ip); split {
					subnet.V4AvailIPList = NewV4AvailIPRangeList
				}

				if merged, NewV4UsingIPRangeList := mergeIPRangeList(subnet.V4UsingIPList, ip); merged {
					subnet.V4UsingIPList = NewV4UsingIPRangeList
				}
			}

			if v6 {
				if split, NewV6AvailIPRangeList := splitIPRangeList(subnet.V6AvailIPList, ip); split {
					subnet.V6AvailIPList = NewV6AvailIPRangeList
				}

				if merged, NewV6UsingIPRangeList := mergeIPRangeList(subnet.V6UsingIPList, ip); merged {
					subnet.V6UsingIPList = NewV6UsingIPRangeList
				}
			}
		}
		subnet.mutex.Unlock()
	}()

	if net.ParseIP(string(ip)).To4() != nil {
		v4 = subnet.V4CIDR != nil
	} else {
		v6 = subnet.V6CIDR != nil
	}
	if v4 && !subnet.V4CIDR.Contains(net.ParseIP(string(ip))) {
		return ip, mac, ErrOutOfRange
	}

	if v6 {
		ip = IP(net.ParseIP(string(ip)).String())
	}

	if v6 && !subnet.V6CIDR.Contains(net.ParseIP(string(ip))) {
		return ip, mac, ErrOutOfRange
	}

	if mac == "" {
		if m, ok := subnet.NicToMac[nicName]; ok {
			mac = m
		} else {
			mac = subnet.GetRandomMac(podName, nicName)
		}
	} else {
		if err := subnet.GetStaticMac(podName, nicName, mac, checkConflict); err != nil {
			return ip, mac, err
		}
	}

	if v4 {
		if existPod, ok := subnet.V4IPToPod[ip]; ok {
			pods := strings.Split(existPod, ",")
			if !util.ContainsString(pods, podName) {
				if !checkConflict {
					subnet.V4NicToIP[nicName] = ip
					subnet.V4IPToPod[ip] = fmt.Sprintf("%s,%s", subnet.V4IPToPod[ip], podName)
					return ip, mac, nil
				}
				return ip, mac, ErrConflict
			}
			if !force {
				return ip, mac, nil
			}
		}

		if subnet.V4ReservedIPList.Contains(ip) {
			subnet.V4NicToIP[nicName] = ip
			subnet.V4IPToPod[ip] = podName
			return ip, mac, nil
		}

		if split, newFreeList := splitIPRangeList(subnet.V4FreeIPList, ip); split {
			subnet.V4FreeIPList = newFreeList
			subnet.V4NicToIP[nicName] = ip
			subnet.V4IPToPod[ip] = podName
			isAllocated = true
			return ip, mac, nil
		} else {
			if split, newReleasedList := splitIPRangeList(subnet.V4ReleasedIPList, ip); split {
				subnet.V4ReleasedIPList = newReleasedList
				subnet.V4NicToIP[nicName] = ip
				subnet.V4IPToPod[ip] = podName
				isAllocated = true
				return ip, mac, nil
			}
		}
	} else if v6 {
		if existPod, ok := subnet.V6IPToPod[ip]; ok {
			pods := strings.Split(existPod, ",")
			if !util.ContainsString(pods, podName) {
				if !checkConflict {
					subnet.V6NicToIP[nicName] = ip
					subnet.V6IPToPod[ip] = fmt.Sprintf("%s,%s", subnet.V6IPToPod[ip], podName)
					return ip, mac, nil
				}
				return ip, mac, ErrConflict
			}
			if !force {
				return ip, mac, nil
			}
		}

		if subnet.V6ReservedIPList.Contains(ip) {
			subnet.V6NicToIP[nicName] = ip
			subnet.V6IPToPod[ip] = podName
			return ip, mac, nil
		}

		if split, newFreeList := splitIPRangeList(subnet.V6FreeIPList, ip); split {
			subnet.V6FreeIPList = newFreeList
			subnet.V6NicToIP[nicName] = ip
			subnet.V6IPToPod[ip] = podName
			isAllocated = true
			return ip, mac, nil
		} else {
			if split, newReleasedList := splitIPRangeList(subnet.V6ReleasedIPList, ip); split {
				subnet.V6ReleasedIPList = newReleasedList
				subnet.V6NicToIP[nicName] = ip
				subnet.V6IPToPod[ip] = podName
				isAllocated = true
				return ip, mac, nil
			}
		}
	}
	return ip, mac, ErrNoAvailable
}

func (subnet *Subnet) releaseAddr(podName, nicName string) {
	ip, mac := IP(""), ""
	var ok, changed bool
	if ip, ok = subnet.V4NicToIP[nicName]; ok {
		oldPods := strings.Split(subnet.V4IPToPod[ip], ",")
		if len(oldPods) > 1 {
			newPods := util.RemoveString(oldPods, podName)
			subnet.V4IPToPod[ip] = strings.Join(newPods, ",")
		} else {
			delete(subnet.V4NicToIP, nicName)
			delete(subnet.V4IPToPod, ip)
			if mac, ok = subnet.NicToMac[nicName]; ok {
				delete(subnet.NicToMac, nicName)
				delete(subnet.MacToPod, mac)
			}

			// When CIDR changed, do not relocate ip to CIDR list
			if !subnet.V4CIDR.Contains(net.ParseIP(string(ip))) {
				// Continue to release IPv6 address
				klog.Infof("release v4 %s mac %s for %s, ignore ip", ip, mac, podName)
				changed = true
			}

			if subnet.V4ReservedIPList.Contains(ip) {
				klog.Infof("release v4 %s mac %s for %s, ip is in reserved list", ip, mac, podName)
				changed = true
			}

			if merged, newReleasedList := mergeIPRangeList(subnet.V4ReleasedIPList, ip); !changed && merged {
				subnet.V4ReleasedIPList = newReleasedList
				klog.Infof("release v4 %s mac %s for %s, add ip to released list", ip, mac, podName)
			}

			if merged, NewV4AvailIPRangeList := mergeIPRangeList(subnet.V4AvailIPList, ip); merged {
				subnet.V4AvailIPList = NewV4AvailIPRangeList
			}

			if split, NewV4UsingIPList := splitIPRangeList(subnet.V4UsingIPList, ip); split {
				subnet.V4UsingIPList = NewV4UsingIPList
			}
		}
	}
	if ip, ok = subnet.V6NicToIP[nicName]; ok {
		oldPods := strings.Split(subnet.V6IPToPod[ip], ",")
		if len(oldPods) > 1 {
			newPods := util.RemoveString(oldPods, podName)
			subnet.V6IPToPod[ip] = strings.Join(newPods, ",")
		} else {
			delete(subnet.V6NicToIP, nicName)
			delete(subnet.V6IPToPod, ip)
			if mac, ok = subnet.NicToMac[nicName]; ok {
				delete(subnet.NicToMac, nicName)
				delete(subnet.MacToPod, mac)
			}
			changed = false
			// When CIDR changed, do not relocate ip to CIDR list
			if !subnet.V6CIDR.Contains(net.ParseIP(string(ip))) {
				klog.Infof("release v6 %s mac %s for %s, ignore ip", ip, mac, podName)
				changed = true
			}

			if subnet.V6ReservedIPList.Contains(ip) {
				klog.Infof("release v6 %s mac %s for %s, ip is in reserved list", ip, mac, podName)
				changed = true
			}

			if merged, newReleasedList := mergeIPRangeList(subnet.V6ReleasedIPList, ip); !changed && merged {
				subnet.V6ReleasedIPList = newReleasedList
				klog.Infof("release v6 %s mac %s for %s, add ip to released list", ip, mac, podName)
			}

			if merged, NewV6AvailIPRangeList := mergeIPRangeList(subnet.V6AvailIPList, ip); merged {
				subnet.V6AvailIPList = NewV6AvailIPRangeList
			}

			if split, NewV6UsingIPList := splitIPRangeList(subnet.V6UsingIPList, ip); split {
				subnet.V6UsingIPList = NewV6UsingIPList
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

	if _, ok := subnet.V4IPToPod[address]; ok {
		return true
	} else if _, ok := subnet.V6IPToPod[address]; ok {
		return true
	}
	return false
}

func (subnet *Subnet) joinFreeWithReserve() {
	protocol := subnet.Protocol
	if protocol == kubeovnv1.ProtocolDual || protocol == kubeovnv1.ProtocolIPv4 {
		for _, reserveIpr := range subnet.V4ReservedIPList {
			newFreeList := IPRangeList{}
			for _, freeIpr := range subnet.V4FreeIPList {
				if iprl := splitRange(freeIpr, reserveIpr); iprl != nil {
					newFreeList = append(newFreeList, iprl...)
				}
			}
			subnet.V4FreeIPList = newFreeList

			newAvailableList := IPRangeList{}
			for _, availIpr := range subnet.V4AvailIPList {
				if iprl := splitRange(availIpr, reserveIpr); iprl != nil {
					newAvailableList = append(newAvailableList, iprl...)
				}
			}
			subnet.V4AvailIPList = newAvailableList
		}
	}
	if protocol == kubeovnv1.ProtocolDual || protocol == kubeovnv1.ProtocolIPv6 {
		for _, reserveIpr := range subnet.V6ReservedIPList {
			newFreeList := IPRangeList{}
			for _, freeIpr := range subnet.V6FreeIPList {
				if iprl := splitRange(freeIpr, reserveIpr); iprl != nil {
					newFreeList = append(newFreeList, iprl...)
				}
			}
			subnet.V6FreeIPList = newFreeList

			newAvailableList := IPRangeList{}
			for _, availIpr := range subnet.V6AvailIPList {
				if iprl := splitRange(availIpr, reserveIpr); iprl != nil {
					newAvailableList = append(newAvailableList, iprl...)
				}
			}
			subnet.V6AvailIPList = newAvailableList
		}
	}
}

// This func is only called in ipam.GetPodAddress, move mutex to caller
func (subnet *Subnet) GetPodAddress(podName, nicName string) (IP, IP, string, string) {
	if subnet.Protocol == kubeovnv1.ProtocolIPv4 {
		ip, mac := subnet.V4NicToIP[nicName], subnet.NicToMac[nicName]
		return ip, "", mac, kubeovnv1.ProtocolIPv4
	} else if subnet.Protocol == kubeovnv1.ProtocolIPv6 {
		ip, mac := subnet.V6NicToIP[nicName], subnet.NicToMac[nicName]
		return "", ip, mac, kubeovnv1.ProtocolIPv6
	} else {
		v4IP, v6IP, mac := subnet.V4NicToIP[nicName], subnet.V6NicToIP[nicName], subnet.NicToMac[nicName]
		return v4IP, v6IP, mac, kubeovnv1.ProtocolDual
	}
}

func (subnet *Subnet) isIPAssignedToOtherPod(ip, podName string) (string, bool) {
	if existPod, ok := subnet.V4IPToPod[IP(ip)]; ok {
		klog.V(4).Infof("v4 check ip assigned, existPod %s, podName %s", existPod, podName)
		pods := strings.Split(existPod, ",")
		if !util.ContainsString(pods, podName) {
			return existPod, true
		}
	}
	if existPod, ok := subnet.V6IPToPod[IP(ip)]; ok {
		klog.V(4).Infof("v6 check ip assigned, existPod %s, podName %s", existPod, podName)
		pods := strings.Split(existPod, ",")
		if !util.ContainsString(pods, podName) {
			return existPod, true
		}
	}
	return "", false
}
