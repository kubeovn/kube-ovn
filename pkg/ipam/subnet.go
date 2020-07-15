package ipam

import (
	"github.com/alauda/kube-ovn/pkg/util"
	"net"
	"sync"
)

type Subnet struct {
	Name           string
	mutex          sync.RWMutex
	CIDR           *net.IPNet
	FreeIPList     IPRangeList
	ReleasedIPList IPRangeList
	ReservedIPList IPRangeList
	PodToIP        map[string]IP
	IPToPod        map[IP]string
	PodToMac       map[string]string
	MacToPod       map[string]string
}

func NewSubnet(name, cidrStr string, excludeIps []string) (*Subnet, error) {
	_, cidr, err := net.ParseCIDR(cidrStr)
	if err != nil {
		return nil, InvalidCIDRError
	}

	firstIP, _ := util.FirstSubnetIP(cidrStr)
	lastIP, _ := util.LastIP(cidrStr)

	subnet := Subnet{
		Name:           name,
		mutex:          sync.RWMutex{},
		CIDR:           cidr,
		FreeIPList:     IPRangeList{&IPRange{Start: IP(firstIP), End: IP(lastIP)}},
		ReleasedIPList: IPRangeList{},
		ReservedIPList: convertExcludeIps(excludeIps),
		PodToIP:        map[string]IP{},
		IPToPod:        map[IP]string{},
		MacToPod:       map[string]string{},
		PodToMac:       map[string]string{},
	}
	subnet.joinFreeWithReserve()
	return &subnet, nil
}

func (subnet *Subnet) GetRandomMac(podName string) string {
	if mac, ok := subnet.PodToMac[podName]; ok {
		return mac
	}
	for {
		mac := util.GenerateMac()
		if _, ok := subnet.MacToPod[mac]; !ok {
			subnet.MacToPod[mac] = podName
			subnet.PodToMac[podName] = mac
			return mac
		}
	}
}

func (subnet *Subnet) GetStaticMac(podName, mac string) error {
	if p, ok := subnet.MacToPod[mac]; ok && p != podName {
		return ConflictError
	}
	subnet.MacToPod[mac] = podName
	subnet.PodToMac[podName] = mac
	return nil
}

func (subnet *Subnet) GetRandomAddress(podName string) (IP, string, error) {
	subnet.mutex.Lock()
	defer subnet.mutex.Unlock()
	if ip, ok := subnet.PodToIP[podName]; ok {
		return ip, subnet.PodToMac[podName], nil
	}
	if len(subnet.FreeIPList) == 0 {
		if len(subnet.ReleasedIPList) != 0 {
			subnet.FreeIPList = subnet.ReleasedIPList
			subnet.ReleasedIPList = IPRangeList{}
		} else {
			return "", "", NoAvailableError
		}
	}
	freeList := subnet.FreeIPList
	ipr := freeList[0]
	ip := ipr.Start
	newStart := ip.Add(1)
	if newStart.LessThan(ipr.End) || newStart.Equal(ipr.End) {
		ipr.Start = newStart
	} else {
		subnet.FreeIPList = subnet.FreeIPList[1:]
	}
	subnet.PodToIP[podName] = ip
	subnet.IPToPod[ip] = podName
	return ip, subnet.GetRandomMac(podName), nil
}

func (subnet *Subnet) GetStaticAddress(podName string, ip IP, mac string, force bool) (IP, string, error) {
	subnet.mutex.Lock()
	subnet.mutex.Unlock()
	if !subnet.CIDR.Contains(net.ParseIP(string(ip))) {
		return ip, mac, OutOfRangeError
	}

	if mac == "" {
		if m, ok := subnet.PodToMac[mac]; ok {
			mac = m
		} else {
			mac = subnet.GetRandomMac(podName)
		}
	} else {
		if err := subnet.GetStaticMac(podName, mac); err != nil {
			return ip, mac, err
		}
	}

	if existPod, ok := subnet.IPToPod[ip]; ok {
		if existPod != podName {
			return ip, mac, ConflictError
		}
		if !force {
			return ip, mac, nil
		}
	}

	if subnet.ReservedIPList.Contains(ip) {
		subnet.PodToIP[podName] = ip
		subnet.IPToPod[ip] = podName
		return ip, mac, nil
	}

	if split, newFreeList := splitIPRangeList(subnet.FreeIPList, ip); split {
		subnet.FreeIPList = newFreeList
		subnet.PodToIP[podName] = ip
		subnet.IPToPod[ip] = podName
		return ip, mac, nil
	} else {
		if split, newReleasedList := splitIPRangeList(subnet.ReleasedIPList, ip); split {
			subnet.ReleasedIPList = newReleasedList
			subnet.PodToIP[podName] = ip
			subnet.IPToPod[ip] = podName
			return ip, mac, nil
		}
		return ip, mac, NoAvailableError
	}
}

func (subnet *Subnet) ReleaseAddress(podName string) (IP, string) {
	subnet.mutex.Lock()
	defer subnet.mutex.Unlock()
	ip, mac := IP(""), ""
	var ok bool
	if ip, ok = subnet.PodToIP[podName]; ok {
		delete(subnet.PodToIP, podName)
		delete(subnet.IPToPod, ip)
		if mac, ok = subnet.PodToMac[podName]; ok {
			delete(subnet.PodToMac, podName)
			delete(subnet.MacToPod, mac)
		}

		if !subnet.CIDR.Contains(net.ParseIP(string(ip))) {
			return ip, mac
		}

		if subnet.ReservedIPList.Contains(ip) {
			return ip, mac
		}

		if merged, newReleasedList := mergeIPRangeList(subnet.ReleasedIPList, ip); merged {
			subnet.ReleasedIPList = newReleasedList
			return ip, mac
		}
	}
	return ip, mac
}

func (subnet *Subnet) ContainAddress(address IP) bool {
	subnet.mutex.RLock()
	defer subnet.mutex.RUnlock()
	if _, ok := subnet.IPToPod[address]; ok {
		return true
	}
	return false
}

func (subnet *Subnet) joinFreeWithReserve() {
	for _, reserveIpr := range subnet.ReservedIPList {
		newFreeList := IPRangeList{}
		for _, freeIpr := range subnet.FreeIPList {
			if iprl := splitRange(freeIpr, reserveIpr); iprl != nil {
				newFreeList = append(newFreeList, iprl...)
			}
		}
		subnet.FreeIPList = newFreeList
	}
}

func (subnet *Subnet) GetPodAddress(podName string) (IP, string, bool) {
	subnet.mutex.RLock()
	defer subnet.mutex.RUnlock()
	ip, mac := subnet.PodToIP[podName], subnet.PodToMac[podName]
	return ip, mac, (ip != "" && mac != "")
}
