package ipam

import (
	"net"
	"strings"
	"sync"

	kubeovnv1 "github.com/alauda/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/alauda/kube-ovn/pkg/util"
)

type Subnet struct {
	Name             string
	mutex            sync.RWMutex
	Protocol         string
	V4CIDR           *net.IPNet
	V4FreeIPList     IPRangeList
	V4ReleasedIPList IPRangeList
	V4ReservedIPList IPRangeList
	V4PodToIP        map[string]IP
	V4IPToPod        map[IP]string
	V6CIDR           *net.IPNet
	V6FreeIPList     IPRangeList
	V6ReleasedIPList IPRangeList
	V6ReservedIPList IPRangeList
	V6PodToIP        map[string]IP
	V6IPToPod        map[IP]string
	PodToMac         map[string]string
	MacToPod         map[string]string
}

func NewSubnet(name, cidrStr string, excludeIps []string) (*Subnet, error) {
	var v4CIDR, v6CIDR *net.IPNet
	var err error
	v4CidrStr := cidrStr
	v6CidrStr := cidrStr
	protocol := util.CheckProtocol(cidrStr)
	if protocol == kubeovnv1.ProtocolDual {
		cidrBlocks := strings.Split(cidrStr, ",")
		_, v4CIDR, err = net.ParseCIDR(cidrBlocks[0])
		if err != nil {
			return nil, InvalidCIDRError
		}
		_, v6CIDR, err = net.ParseCIDR(cidrBlocks[1])
		if err != nil {
			return nil, InvalidCIDRError
		}
		v4CidrStr = cidrBlocks[0]
		v6CidrStr = cidrBlocks[1]
	} else if protocol == kubeovnv1.ProtocolIPv4 {
		_, v4CIDR, err = net.ParseCIDR(cidrStr)
		if err != nil {
			return nil, InvalidCIDRError
		}
	} else {
		_, v6CIDR, err = net.ParseCIDR(cidrStr)
		if err != nil {
			return nil, InvalidCIDRError
		}
	}

	// subnet.Spec.ExcludeIps contains both v4 and v6 addresses
	var v4ExcludeIps, v6ExcludeIps []string
	for _, ip := range excludeIps {
		if net.ParseIP(ip).To4() != nil {
			v4ExcludeIps = append(v4ExcludeIps, ip)
		} else {
			v6ExcludeIps = append(v6ExcludeIps, ip)
		}
	}

	subnet := Subnet{}
	if protocol == kubeovnv1.ProtocolDual || protocol == kubeovnv1.ProtocolIPv4 {
		firstIP, _ := util.FirstSubnetIP(v4CidrStr)
		lastIP, _ := util.LastIP(v4CidrStr)

		subnet = Subnet{
			Name:             name,
			mutex:            sync.RWMutex{},
			V4CIDR:           v4CIDR,
			V4FreeIPList:     IPRangeList{&IPRange{Start: IP(firstIP), End: IP(lastIP)}},
			V4ReleasedIPList: IPRangeList{},
			V4ReservedIPList: convertExcludeIps(v4ExcludeIps),
			V4PodToIP:        map[string]IP{},
			V4IPToPod:        map[IP]string{},
			MacToPod:         map[string]string{},
			PodToMac:         map[string]string{},
		}
		subnet.joinFreeWithReserve()
	}

	if protocol == kubeovnv1.ProtocolDual || protocol == kubeovnv1.ProtocolIPv6 {
		firstIP, _ := util.FirstSubnetIP(v6CidrStr)
		lastIP, _ := util.LastIP(v6CidrStr)

		subnet = Subnet{
			Name:             name,
			mutex:            sync.RWMutex{},
			V6CIDR:           v6CIDR,
			V6FreeIPList:     IPRangeList{&IPRange{Start: IP(firstIP), End: IP(lastIP)}},
			V6ReleasedIPList: IPRangeList{},
			V6ReservedIPList: convertExcludeIps(v6ExcludeIps),
			V6PodToIP:        map[string]IP{},
			V6IPToPod:        map[IP]string{},
			MacToPod:         map[string]string{},
			PodToMac:         map[string]string{},
		}
		subnet.joinFreeWithReserve()
	}
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

func (subnet *Subnet) GetRandomAddress(podName string) (IP, IP, string, error) {
	subnet.mutex.Lock()
	defer subnet.mutex.Unlock()
	protocol := util.CheckProtocol(subnet.Protocol)
	if protocol == kubeovnv1.ProtocolDual {
		return subnet.getDualRandomAddress(podName)
	} else if protocol == kubeovnv1.ProtocolIPv4 {
		return subnet.getV4RandomAddress(podName)
	} else {
		return subnet.getV6RandomAddress(podName)
	}
}

func (subnet *Subnet) getDualRandomAddress(podName string) (IP, IP, string, error) {
	var v4IP, v6IP IP
	var ok bool
	v4IPExist := false
	v6IPExist := false
	if v4IP, ok = subnet.V4PodToIP[podName]; ok {
		v4IPExist = true
	}
	if v6IP, ok = subnet.V6PodToIP[podName]; ok {
		v6IPExist = true
	}
	if v4IPExist && v6IPExist {
		return v4IP, v6IP, subnet.PodToMac[podName], nil
	}

	if len(subnet.V4FreeIPList) == 0 {
		if len(subnet.V4ReleasedIPList) != 0 {
			subnet.V4FreeIPList = subnet.V4ReleasedIPList
			subnet.V4ReleasedIPList = IPRangeList{}
		} else {
			return "", "", "", NoAvailableError
		}
	}
	if len(subnet.V6FreeIPList) == 0 {
		if len(subnet.V6ReleasedIPList) != 0 {
			subnet.V6FreeIPList = subnet.V6ReleasedIPList
			subnet.V6ReleasedIPList = IPRangeList{}
		} else {
			return "", "", "", NoAvailableError
		}
	}

	freeList := subnet.V4FreeIPList
	ipr := freeList[0]
	v4IP = ipr.Start
	newStart := v4IP.Add(1)
	if newStart.LessThan(ipr.End) || newStart.Equal(ipr.End) {
		ipr.Start = newStart
	} else {
		subnet.V4FreeIPList = subnet.V4FreeIPList[1:]
	}
	subnet.V4PodToIP[podName] = v4IP
	subnet.V4IPToPod[v4IP] = podName

	freeList = subnet.V6FreeIPList
	ipr = freeList[0]
	v6IP = ipr.Start
	newStart = v6IP.Add(1)
	if newStart.LessThan(ipr.End) || newStart.Equal(ipr.End) {
		ipr.Start = newStart
	} else {
		subnet.V6FreeIPList = subnet.V6FreeIPList[1:]
	}
	subnet.V6PodToIP[podName] = v6IP
	subnet.V6IPToPod[v6IP] = podName

	return v4IP, v6IP, subnet.GetRandomMac(podName), nil
}

func (subnet *Subnet) getV4RandomAddress(podName string) (IP, IP, string, error) {
	if ip, ok := subnet.V4PodToIP[podName]; ok {
		return ip, "", subnet.PodToMac[podName], nil
	}
	if len(subnet.V4FreeIPList) == 0 {
		if len(subnet.V4ReleasedIPList) != 0 {
			subnet.V4FreeIPList = subnet.V4ReleasedIPList
			subnet.V4ReleasedIPList = IPRangeList{}
		} else {
			return "", "", "", NoAvailableError
		}
	}
	freeList := subnet.V4FreeIPList
	ipr := freeList[0]
	ip := ipr.Start
	newStart := ip.Add(1)
	if newStart.LessThan(ipr.End) || newStart.Equal(ipr.End) {
		ipr.Start = newStart
	} else {
		subnet.V4FreeIPList = subnet.V4FreeIPList[1:]
	}
	subnet.V4PodToIP[podName] = ip
	subnet.V4IPToPod[ip] = podName

	return ip, "", subnet.GetRandomMac(podName), nil
}

func (subnet *Subnet) getV6RandomAddress(podName string) (IP, IP, string, error) {
	if ip, ok := subnet.V6PodToIP[podName]; ok {
		return "", ip, subnet.PodToMac[podName], nil
	}
	if len(subnet.V6FreeIPList) == 0 {
		if len(subnet.V6ReleasedIPList) != 0 {
			subnet.V6FreeIPList = subnet.V6ReleasedIPList
			subnet.V6ReleasedIPList = IPRangeList{}
		} else {
			return "", "", "", NoAvailableError
		}
	}
	freeList := subnet.V6FreeIPList
	ipr := freeList[0]
	ip := ipr.Start
	newStart := ip.Add(1)
	if newStart.LessThan(ipr.End) || newStart.Equal(ipr.End) {
		ipr.Start = newStart
	} else {
		subnet.V6FreeIPList = subnet.V6FreeIPList[1:]
	}
	subnet.V6PodToIP[podName] = ip
	subnet.V6IPToPod[ip] = podName

	return "", ip, subnet.GetRandomMac(podName), nil
}

func (subnet *Subnet) GetStaticAddress(podName string, ip IP, mac string, force bool) (IP, string, error) {
	subnet.mutex.Lock()
	defer subnet.mutex.Unlock()
	var v4, v6 bool
	if net.ParseIP(string(ip)).To4() != nil {
		v4 = true
	} else {
		v6 = true
	}
	if v4 == true && !subnet.V4CIDR.Contains(net.ParseIP(string(ip))) {
		return ip, mac, OutOfRangeError
	}
	if v6 == true && !subnet.V6CIDR.Contains(net.ParseIP(string(ip))) {
		return ip, mac, OutOfRangeError
	}

	if mac == "" {
		if m, ok := subnet.PodToMac[podName]; ok {
			mac = m
		} else {
			mac = subnet.GetRandomMac(podName)
		}
	} else {
		if err := subnet.GetStaticMac(podName, mac); err != nil {
			return ip, mac, err
		}
	}

	if v4 == true {
		if existPod, ok := subnet.V4IPToPod[ip]; ok {
			if existPod != podName {
				return ip, mac, ConflictError
			}
			if !force {
				return ip, mac, nil
			}
		}

		if subnet.V4ReservedIPList.Contains(ip) {
			subnet.V4PodToIP[podName] = ip
			subnet.V4IPToPod[ip] = podName
			return ip, mac, nil
		}

		if split, newFreeList := splitIPRangeList(subnet.V4FreeIPList, ip); split {
			subnet.V4FreeIPList = newFreeList
			subnet.V4PodToIP[podName] = ip
			subnet.V4IPToPod[ip] = podName
			return ip, mac, nil
		} else {
			if split, newReleasedList := splitIPRangeList(subnet.V4ReleasedIPList, ip); split {
				subnet.V4ReleasedIPList = newReleasedList
				subnet.V4PodToIP[podName] = ip
				subnet.V4IPToPod[ip] = podName
				return ip, mac, nil
			}
			return ip, mac, NoAvailableError
		}
	} else if v6 == true {
		if existPod, ok := subnet.V6IPToPod[ip]; ok {
			if existPod != podName {
				return ip, mac, ConflictError
			}
			if !force {
				return ip, mac, nil
			}
		}

		if subnet.V6ReservedIPList.Contains(ip) {
			subnet.V6PodToIP[podName] = ip
			subnet.V6IPToPod[ip] = podName
			return ip, mac, nil
		}

		if split, newFreeList := splitIPRangeList(subnet.V6FreeIPList, ip); split {
			subnet.V6FreeIPList = newFreeList
			subnet.V6PodToIP[podName] = ip
			subnet.V6IPToPod[ip] = podName
			return ip, mac, nil
		} else {
			if split, newReleasedList := splitIPRangeList(subnet.V6ReleasedIPList, ip); split {
				subnet.V6ReleasedIPList = newReleasedList
				subnet.V6PodToIP[podName] = ip
				subnet.V6IPToPod[ip] = podName
				return ip, mac, nil
			}
			return ip, mac, NoAvailableError
		}
	}
	return ip, mac, nil
}

func (subnet *Subnet) ReleaseAddress(podName string) (IP, string) {
	subnet.mutex.Lock()
	defer subnet.mutex.Unlock()
	ip, mac := IP(""), ""
	var ok bool
	if ip, ok = subnet.V4PodToIP[podName]; ok {
		delete(subnet.V4PodToIP, podName)
		delete(subnet.V4IPToPod, ip)
		if mac, ok = subnet.PodToMac[podName]; ok {
			delete(subnet.PodToMac, podName)
			delete(subnet.MacToPod, mac)
		}

		// When CIDR changed, do not relocate ip to CIDR list
		if !subnet.V4CIDR.Contains(net.ParseIP(string(ip))) {
			return ip, mac
		}

		if subnet.V4ReservedIPList.Contains(ip) {
			return ip, mac
		}

		if merged, newReleasedList := mergeIPRangeList(subnet.V4ReleasedIPList, ip); merged {
			subnet.V4ReleasedIPList = newReleasedList
			return ip, mac
		}
	}
	if ip, ok = subnet.V6PodToIP[podName]; ok {
		delete(subnet.V6PodToIP, podName)
		delete(subnet.V6IPToPod, ip)
		if mac, ok = subnet.PodToMac[podName]; ok {
			delete(subnet.PodToMac, podName)
			delete(subnet.MacToPod, mac)
		}
		// When CIDR changed, do not relocate ip to CIDR list
		if !subnet.V6CIDR.Contains(net.ParseIP(string(ip))) {
			return ip, mac
		}

		if subnet.V6ReservedIPList.Contains(ip) {
			return ip, mac
		}

		if merged, newReleasedList := mergeIPRangeList(subnet.V6ReleasedIPList, ip); merged {
			subnet.V6ReleasedIPList = newReleasedList
			return ip, mac
		}
	}
	return ip, mac
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
		}
	}
}

func (subnet *Subnet) GetPodAddress(podName string) (IP, IP, string, string) {
	subnet.mutex.RLock()
	defer subnet.mutex.RUnlock()

	if subnet.Protocol == kubeovnv1.ProtocolIPv4 {
		ip, mac := subnet.V4PodToIP[podName], subnet.PodToMac[podName]
		return ip, "", mac, kubeovnv1.ProtocolIPv4
	} else if subnet.Protocol == kubeovnv1.ProtocolIPv6 {
		ip, mac := subnet.V6PodToIP[podName], subnet.PodToMac[podName]
		return "", ip, mac, kubeovnv1.ProtocolIPv6
	} else {
		v4IP, v6IP, mac := subnet.V4PodToIP[podName], subnet.V6PodToIP[podName], subnet.PodToMac[podName]
		return v4IP, v6IP, mac, kubeovnv1.ProtocolDual
	}
}
