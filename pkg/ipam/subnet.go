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
	V4FreeIPList     *IPRangeList
	V4ReleasedIPList *IPRangeList
	V4ReservedIPList *IPRangeList
	V4AvailIPList    *IPRangeList
	V4UsingIPList    *IPRangeList
	V4NicToIP        map[string]IP
	V4IPToPod        map[string]string
	V6CIDR           *net.IPNet
	V6FreeIPList     *IPRangeList
	V6ReleasedIPList *IPRangeList
	V6ReservedIPList *IPRangeList
	V6AvailIPList    *IPRangeList
	V6UsingIPList    *IPRangeList
	V6NicToIP        map[string]IP
	V6IPToPod        map[string]string
	NicToMac         map[string]string
	MacToPod         map[string]string
	PodToNicList     map[string][]string
	V4Gw             string
	V6Gw             string
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

	protocol := util.CheckProtocol(cidrStr)
	subnet := &Subnet{
		Name:             name,
		mutex:            sync.RWMutex{},
		Protocol:         protocol,
		V4FreeIPList:     NewIPRangeList(),
		V6FreeIPList:     NewIPRangeList(),
		V4ReservedIPList: NewIPRangeListFrom(v4ExcludeIps...),
		V6ReservedIPList: NewIPRangeListFrom(v6ExcludeIps...),
		V4ReleasedIPList: NewIPRangeList(),
		V6ReleasedIPList: NewIPRangeList(),
		V4UsingIPList:    NewIPRangeList(),
		V6UsingIPList:    NewIPRangeList(),
		V4NicToIP:        map[string]IP{},
		V6NicToIP:        map[string]IP{},
		V4IPToPod:        map[string]string{},
		V6IPToPod:        map[string]string{},
		MacToPod:         map[string]string{},
		NicToMac:         map[string]string{},
		PodToNicList:     map[string][]string{},
	}
	if protocol == kubeovnv1.ProtocolIPv4 {
		firstIP, _ := util.FirstIP(cidrStr)
		lastIP, _ := util.LastIP(cidrStr)
		subnet.V4CIDR = cidrs[0]
		subnet.V4FreeIPList = NewIPRangeListFrom(fmt.Sprintf("%s..%s", firstIP, lastIP))
	} else if protocol == kubeovnv1.ProtocolIPv6 {
		firstIP, _ := util.FirstIP(cidrStr)
		lastIP, _ := util.LastIP(cidrStr)
		subnet.V6CIDR = cidrs[0]
		subnet.V6FreeIPList = NewIPRangeListFrom(fmt.Sprintf("%s..%s", firstIP, lastIP))
	} else {
		subnet.V4CIDR = cidrs[0]
		subnet.V6CIDR = cidrs[1]
		cidrBlocks := strings.Split(cidrStr, ",")
		v4FirstIP, _ := util.FirstIP(cidrBlocks[0])
		v4LastIP, _ := util.LastIP(cidrBlocks[0])
		v6FirstIP, _ := util.FirstIP(cidrBlocks[1])
		v6LastIP, _ := util.LastIP(cidrBlocks[1])
		subnet.V4FreeIPList = NewIPRangeListFrom(fmt.Sprintf("%s..%s", v4FirstIP, v4LastIP))
		subnet.V6FreeIPList = NewIPRangeListFrom(fmt.Sprintf("%s..%s", v6FirstIP, v6LastIP))
	}
	subnet.V4FreeIPList = subnet.V4FreeIPList.Difference(subnet.V4ReservedIPList)
	subnet.V6FreeIPList = subnet.V6FreeIPList.Difference(subnet.V6ReservedIPList)
	subnet.V4AvailIPList = subnet.V4FreeIPList.Clone()
	subnet.V6AvailIPList = subnet.V6FreeIPList.Clone()

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

func (subnet *Subnet) GetRandomAddress(podName, nicName string, mac *string, skippedAddrs []string, checkConflict bool) (IP, IP, string, error) {
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

func (subnet *Subnet) getDualRandomAddress(podName, nicName string, mac *string, skippedAddrs []string, checkConflict bool) (IP, IP, string, error) {
	v4IP, _, _, err := subnet.getV4RandomAddress(podName, nicName, mac, skippedAddrs, checkConflict)
	if err != nil {
		return nil, nil, "", err
	}
	_, v6IP, macStr, err := subnet.getV6RandomAddress(podName, nicName, mac, skippedAddrs, checkConflict)
	if err != nil {
		return nil, nil, "", err
	}

	// allocated IPv4 address may be released in getV6RandomAddress()
	if !subnet.V4NicToIP[nicName].Equal(v4IP) {
		v4IP, _, _, _ = subnet.getV4RandomAddress(podName, nicName, mac, skippedAddrs, checkConflict)
	}

	return v4IP, v6IP, macStr, nil
}

func (subnet *Subnet) getV4RandomAddress(podName, nicName string, mac *string, skippedAddrs []string, checkConflict bool) (IP, IP, string, error) {
	// After 'macAdd' introduced to support only static mac address, pod restart will run into error mac AddressConflict
	// controller will re-enqueue the new pod then wait for old pod deleted and address released.
	// here will return only if both ip and mac exist, otherwise only ip without mac returned will trigger CreatePort error.
	if subnet.V4NicToIP[nicName] != nil && subnet.NicToMac[nicName] != "" {
		if !util.ContainsString(skippedAddrs, subnet.V4NicToIP[nicName].String()) {
			return subnet.V4NicToIP[nicName], nil, subnet.NicToMac[nicName], nil
		}
		subnet.releaseAddr(podName, nicName)
	}

	if subnet.V4FreeIPList.Len() == 0 {
		if subnet.V4ReleasedIPList.Len() == 0 {
			return nil, nil, "", ErrNoAvailable
		}
		subnet.V4FreeIPList = subnet.V4ReleasedIPList
		subnet.V4ReleasedIPList = NewIPRangeList()
	}

	skipped := make([]IP, 0, len(skippedAddrs))
	for _, s := range skippedAddrs {
		skipped = append(skipped, NewIP(s))
	}
	ip := subnet.V4FreeIPList.Allocate(skipped)
	if ip == nil {
		return nil, nil, "", ErrConflict
	}

	subnet.V4AvailIPList.Remove(ip)
	subnet.V4UsingIPList.Add(ip)

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

func (subnet *Subnet) getV6RandomAddress(podName, nicName string, mac *string, skippedAddrs []string, checkConflict bool) (IP, IP, string, error) {
	// After 'macAdd' introduced to support only static mac address, pod restart will run into error mac AddressConflict
	// controller will re-enqueue the new pod then wait for old pod deleted and address released.
	// here will return only if both ip and mac exist, otherwise only ip without mac returned will trigger CreatePort error.
	if subnet.V6NicToIP[nicName] != nil && subnet.NicToMac[nicName] != "" {
		if !util.ContainsString(skippedAddrs, subnet.V6NicToIP[nicName].String()) {
			return nil, subnet.V6NicToIP[nicName], subnet.NicToMac[nicName], nil
		}
		subnet.releaseAddr(podName, nicName)
	}

	if subnet.V6FreeIPList.Len() == 0 {
		if subnet.V6ReleasedIPList.Len() == 0 {
			return nil, nil, "", ErrNoAvailable
		}
		subnet.V6FreeIPList = subnet.V6ReleasedIPList
		subnet.V6ReleasedIPList = NewIPRangeList()
	}

	skipped := make([]IP, 0, len(skippedAddrs))
	for _, s := range skippedAddrs {
		skipped = append(skipped, NewIP(s))
	}
	ip := subnet.V6FreeIPList.Allocate(skipped)
	if ip == nil {
		return nil, nil, "", ErrConflict
	}

	subnet.V6AvailIPList.Remove(ip)
	subnet.V6UsingIPList.Add(ip)

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
	defer func() {
		subnet.pushPodNic(podName, nicName)
		if isAllocated {
			if v4 {
				subnet.V4AvailIPList.Remove(ip)
				subnet.V4UsingIPList.Add(ip)
			}
			if v6 {
				subnet.V6AvailIPList.Remove(ip)
				subnet.V6UsingIPList.Add(ip)
			}
		}
		subnet.mutex.Unlock()
	}()

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
				return ip, macStr, ErrConflict
			}
			if !force {
				return ip, macStr, nil
			}
		}

		if subnet.V4ReservedIPList.Contains(ip) {
			subnet.V4NicToIP[nicName] = ip
			subnet.V4IPToPod[ip.String()] = podName
			return ip, macStr, nil
		}

		if subnet.V4FreeIPList.Remove(ip) {
			subnet.V4NicToIP[nicName] = ip
			subnet.V4IPToPod[ip.String()] = podName
			isAllocated = true
			return ip, macStr, nil
		} else if subnet.V4ReleasedIPList.Remove(ip) {
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
				return ip, macStr, ErrConflict
			}
			if !force {
				return ip, macStr, nil
			}
		}

		if subnet.V6ReservedIPList.Contains(ip) {
			subnet.V6NicToIP[nicName] = ip
			subnet.V6IPToPod[ip.String()] = podName
			return ip, macStr, nil
		}

		if subnet.V6FreeIPList.Remove(ip) {
			subnet.V6NicToIP[nicName] = ip
			subnet.V6IPToPod[ip.String()] = podName
			isAllocated = true
			return ip, macStr, nil
		} else if subnet.V6ReleasedIPList.Remove(ip) {
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
				klog.Infof("release v4 %s mac %s for %s, ignore ip", ip, mac, podName)
				changed = true
			}

			if subnet.V4ReservedIPList.Contains(ip) {
				klog.Infof("release v4 %s mac %s for %s, ip is in reserved list", ip, mac, podName)
				changed = true
			}

			if !changed {
				if subnet.V4ReleasedIPList.Add(ip) {
					klog.Infof("release v4 %s mac %s for %s, add ip to released list", ip, mac, podName)
				}
			}

			subnet.V4AvailIPList.Add(ip)
			subnet.V4UsingIPList.Remove(ip)
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
				klog.Infof("release v6 %s mac %s for %s, ignore ip", ip, mac, podName)
				changed = true
			}

			if subnet.V6ReservedIPList.Contains(ip) {
				klog.Infof("release v6 %s mac %s for %s, ip is in reserved list", ip, mac, podName)
				changed = true
			}

			if !changed {
				if subnet.V6ReleasedIPList.Add(ip) {
					klog.Infof("release v6 %s mac %s for %s, add ip to released list", ip, mac, podName)
				}
			}

			subnet.V6AvailIPList.Add(ip)
			subnet.V6UsingIPList.Remove(ip)
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
