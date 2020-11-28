package ipam

import (
	"net"
	"sync"
	"strconv"
	"strings"

	"github.com/alauda/kube-ovn/pkg/util"
	v1 "github.com/alauda/kube-ovn/pkg/apis/kubeovn/v1"
)

type Subnet struct {
	Name           string
	mutex          sync.RWMutex
	CIDR           map[v1.Protocol]*net.IPNet
	FreeIPList     map[v1.Protocol]IPRangeList
	ReservedIPList map[v1.Protocol]IPRangeList
    ReleasedIPList map[v1.Protocol]IPRangeList
	PodToIP        map[string]v1.DualStack
	IPToPod        map[IP]string
	PodToMac       map[string]string
	MacToPod       map[string]string
}

func NewSubnet(name string, cidrBlock v1.DualStack, excludeIps v1.DualStackList) (*Subnet, error) {
	var (
		firstIP, lastIP v1.DualStack
		cidrMap         = make(map[v1.Protocol]*net.IPNet)
		freeIPList      = make(map[v1.Protocol]IPRangeList)
		reservedIPList  = make(map[v1.Protocol]IPRangeList)
		err             error
	)

	for proto, cidrStr := range cidrBlock {
		_, cidr, err := net.ParseCIDR(cidrStr)
		if err != nil {
			return nil, InvalidCIDRError
		}
		cidrMap[proto] = cidr
	}

	firstIP, err = util.FirstSubnetIP(cidrBlock)
	lastIP, err = util.LastSubnetIP(cidrBlock)
	if err != nil {
		return nil, err
	}


	for proto := range cidrBlock {
		freeIPList[proto] = IPRangeList{&IPRange{Start: IP(firstIP[proto]), End: IP(lastIP[proto])}}
		reservedIPList[proto] = convertExcludeIps(excludeIps[proto])
	}

	subnet := Subnet{
		Name:           name,
		mutex:          sync.RWMutex{},
		CIDR:           cidrMap,
		FreeIPList:     freeIPList,
		ReservedIPList: reservedIPList,
		ReleasedIPList: IPRangeList{},
		PodToIP:        map[string]v1.DualStack{},
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

func (subnet *Subnet) GetRandomAddress(podName string) (v1.DualStack, string, error) {
	subnet.mutex.Lock()
	defer subnet.mutex.Unlock()
	var (
		ipDual = v1.DualStack{}
		mac    string
	)
	if ip, ok := subnet.PodToIP[podName]; ok {
		ipDual = ip
		mac = subnet.PodToMac[podName]
	}

	if mac != "" {
		return ipDual, mac, nil
	}

	var (
		ipv4 string
		ipv6 string
		err  error
	)

	if subnet.CIDR[v1.ProtocolIPv4] != nil {
		if ipv4, err = subnet.getRandomAddressWithProto(podName, v1.ProtocolIPv4); err != nil {
			return nil, "", err
		}
		ipDual[v1.ProtocolIPv4] = ipv4
	}

	if subnet.CIDR[v1.ProtocolIPv6] != nil {
        ipv6, err = subnet.getRandomAddressWithProto(podName, v1.ProtocolIPv6)
		if err != nil {
			delete(subnet.IPToPod, IP(ipv4))
			return nil, "", err
		}
		ipDual[v1.ProtocolIPv6] = ipv6
	}
	if len(subnet.FreeIPList) == 0 {
		if len(subnet.ReleasedIPList) != 0 {
			subnet.FreeIPList = subnet.ReleasedIPList
			subnet.ReleasedIPList = IPRangeList{}
		} else {
			return "", "", NoAvailableError
		}
	}
	subnet.PodToIP[podName] = ipDual
	return ipDual, subnet.GetRandomMac(podName), nil
}


func (subnet *Subnet) getRandomAddressWithProto(podName string, proto v1.Protocol) (string, error) {
	freeList := subnet.FreeIPList[proto]
	if len(freeList) == 0 {
		if len(subnet.ReleasedIPList[proto]) != 0 {
			subnet.FreeIPList[proto] = subnet.ReleasedIPList[proto]
			subnet.ReleasedIPList[proto] = IPRangeList{}
		} else {
			return "", "", NoAvailableError
		}
    }

	ipr := freeList[0]
	ip := ipr.Start
	newStart := ip.Add(1)
	if newStart.LessThan(ipr.End) || newStart.Equal(ipr.End) {
		ipr.Start = newStart
	} else {
		subnet.FreeIPList[proto] = subnet.FreeIPList[proto][1:]
   }
	subnet.IPToPod[ip] = podName
	return string(ip), nil
}

func (subnet *Subnet) getStaticAddressWithProto(podName, ip string, proto v1.Protocol) (string, error) {
	if subnet.ReservedIPList[proto].Contains(IP(ip)) {
		subnet.IPToPod[IP(ip)] = podName
		return ip, nil
	} else if split, newFreeList := splitIPRangeList(subnet.FreeIPList[proto], IP(ip)); split {
		subnet.FreeIPList[proto] = newFreeList
		subnet.IPToPod[IP(ip)] = podName
		return ip, nil
	} else {
		return "", NoAvailableError
	}
}

func (subnet *Subnet) GetStaticAddress(podName string, ipDual v1.DualStack, mac string, force) (v1.DualStack, string, error) {
	subnet.mutex.Lock()
	defer subnet.mutex.Unlock()

	if len(ipDual) != len(subnet.CIDR) {
		return nil, "", NoAvailableError
	}

	for proto, ip := range ipDual {
		if !subnet.CIDR[proto].Contains(net.ParseIP(ip)) {
			return ipDual, mac, OutOfRangeError
		}

		if existPod, ok := subnet.IPToPod[IP(ip)]; ok {
			if existPod != podName {
				return ipDual, mac, ConflictError
			}
			if !force {
				return ipDual, mac, nil
			}
		}
	}

	if mac == "" {
		if m, ok := subnet.PodToMac[podName]; ok {
			mac = m
		} else {
			mac = subnet.GetRandomMac(podName)
		}
	} else {
		if err := subnet.GetStaticMac(podName, mac); err != nil {
        return ipDual, mac, err
		}
	}

	for proto, ip := range ipDual {
		if subnet.ReservedIPList[proto].Contains(IP(ip)) {
			subnet.IPToPod[IP(ip)] = podName
		} else if split, newFreeList := splitIPRangeList(subnet.FreeIPList[proto], IP(ip)); split {
			subnet.FreeIPList[proto] = newFreeList
			subnet.IPToPod[IP(ip)] = podName
		} else {
            if split, newReleasedList := splitIPRangeList(subnet.ReleasedIPList[proto], IP(ip)); split {
                subnet.ReleasedIPList[proto] = newReleasedList[proto]
                subnet.IPToPod[ip] = podName
            } else {
			    return ipDual, mac, NoAvailableError
            }
		}
	}

	subnet.PodToIP[podName] = ipDual
	return ipDual, mac, nil
}

func (subnet *Subnet) ReleaseAddress(podName string) (v1.DualStack, string) {
	subnet.mutex.Lock()
	defer subnet.mutex.Unlock()
	ipDual, mac, ok := v1.DualStack{}, "", false

	if ipDual, ok = subnet.PodToIP[podName]; ok {
		delete(subnet.PodToIP, podName)
		if mac, ok = subnet.PodToMac[podName]; ok {
			delete(subnet.PodToMac, podName)
			delete(subnet.MacToPod, mac)
		}
		for proto, ip := range ipDual {
			delete(subnet.IPToPod, IP(ip))
			if !subnet.CIDR[proto].Contains(net.ParseIP(string(ip))) {
				continue
			}

			if subnet.ReservedIPList[proto].Contains(IP(ip)) {
				continue
			}

			if merged, newFreeList := mergeIPRangeList(subnet.FreeIPList[proto], IP(ip)); merged {
				subnet.FreeIPList[proto] = newFreeList
			}
		}
	}
	return ipDual, mac
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
	for proto, reservedIPList := range subnet.ReservedIPList {
		for _, reserveIpr := range reservedIPList {
			newFreeList := IPRangeList{}
			for _, freeIpr := range subnet.FreeIPList[proto] {
				if iprl := splitRange(freeIpr, reserveIpr); iprl != nil {
					newFreeList = append(newFreeList, iprl...)
				}
			}
			subnet.FreeIPList[proto] = newFreeList
		}
	}
}

func (subnet *Subnet) GetPodAddress(podName string) (v1.DualStack, string, bool) {
	subnet.mutex.RLock()
	defer subnet.mutex.RUnlock()
	mac := subnet.PodToMac[podName]
	ipDual := subnet.PodToIP[podName]

	return ipDual, mac, ipDual != nil && mac != ""
}

func (subnet *Subnet) transAddressV4ToV6(ipv4 string) string {
	if subnet.CIDR[v1.ProtocolIPv6] == nil {
		return ""
	}

	// ipv6 prefix length default to 80
	if strings.Split(subnet.CIDR[v1.ProtocolIPv6].String(), "/")[1] != "80" {
		return ""
	}

	v4 := net.ParseIP(ipv4).To4()[1:4]
	v6 := subnet.CIDR[v1.ProtocolIPv6].IP
	r := append(v6[0:10], byte(0), v4[0], byte(0), v4[1], byte(0), v4[2])

	covert := func(ip net.IP) string {
		l := strings.Split(ip.String(), ":")
		for i := len(l) - 3; i < len(l); i++ {
			j, _ := strconv.ParseInt(l[i], 16, 64)
			l[i] = strconv.FormatInt(j, 10)
		}
		return strings.Join(l, ":")
	}

	return covert(r)
}

func (subnet *Subnet) consistentIP(ipDual v1.DualStack) error {
	if len(subnet.CIDR) == 1 {
		return nil
	}

	v4 := ipDual[v1.ProtocolIPv4]
	v6 := ipDual[v1.ProtocolIPv6]

	if v4 == "" {
		return NoAvailableError
	}

	if v6 == "" {
		ipDual[v1.ProtocolIPv6] = subnet.transAddressV4ToV6(v4)
	} else if v6 != subnet.transAddressV4ToV6(v4) {
		return NoAvailableError
	}
	return nil
}
