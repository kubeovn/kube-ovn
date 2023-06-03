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
	Ip     string
	Mac    string
}

func NewIPAM() *IPAM {
	return &IPAM{
		mutex:   sync.RWMutex{},
		Subnets: map[string]*Subnet{},
	}
}

func (ipam *IPAM) GetRandomAddress(podName, nicName string, mac *string, subnetName string, skippedAddrs []string, checkConflict bool) (string, string, string, error) {
	ipam.mutex.RLock()
	defer ipam.mutex.RUnlock()

	subnet, ok := ipam.Subnets[subnetName]
	if !ok {
		return "", "", "", ErrNoAvailable
	}

	v4IP, v6IP, macStr, err := subnet.GetRandomAddress(podName, nicName, mac, skippedAddrs, checkConflict)
	klog.Infof("allocate v4 %s v6 %s mac %s for %s from subnet %s", v4IP, v6IP, macStr, podName, subnetName)
	return v4IP.String(), v6IP.String(), macStr, err
}

func (ipam *IPAM) GetStaticAddress(podName, nicName, ip string, mac *string, subnetName string, checkConflict bool) (string, string, string, error) {
	ipam.mutex.RLock()
	defer ipam.mutex.RUnlock()
	if subnet, ok := ipam.Subnets[subnetName]; !ok {
		return "", "", "", ErrNoAvailable
	} else {
		var ips []IP
		var err error
		var ipAddr IP
		var macStr string
		for _, ipStr := range strings.Split(ip, ",") {
			ipAddr, macStr, err = subnet.GetStaticAddress(podName, nicName, NewIP(ipStr), mac, false, checkConflict)
			if err != nil {
				return "", "", "", err
			}
			ips = append(ips, ipAddr)
		}
		ips, err = checkAndAppendIpsForDual(ips, macStr, podName, nicName, subnet, checkConflict)
		if err != nil {
			klog.Errorf("failed to append allocate ip %v mac %s for %s", ips, mac, podName)
			return "", "", "", err
		}

		switch subnet.Protocol {
		case kubeovnv1.ProtocolIPv4:
			klog.Infof("allocate v4 %s mac %s for %s", ip, macStr, podName)
			return ip, "", macStr, err
		case kubeovnv1.ProtocolIPv6:
			klog.Infof("allocate v6 %s mac %s for %s", ip, macStr, podName)
			return "", ip, macStr, err
		case kubeovnv1.ProtocolDual:
			klog.Infof("allocate v4 %s v6 %s mac %s for %s", ips[0].String(), ips[1].String(), macStr, podName)
			return ips[0].String(), ips[1].String(), macStr, err
		}
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
		_, ipAddr, _, err = subnet.getV6RandomAddress(podName, nicName, &mac, nil, checkConflict)
		newIps = append(newIps, ipAddr)
	} else if util.CheckProtocol(ips[0].String()) == kubeovnv1.ProtocolIPv6 {
		ipAddr, _, _, err = subnet.getV4RandomAddress(podName, nicName, &mac, nil, checkConflict)
		newIps = append(newIps, ipAddr)
		newIps = append(newIps, ips...)
	}

	return newIps, err
}

func (ipam *IPAM) ReleaseAddressByPod(podName string) {
	ipam.mutex.RLock()
	defer ipam.mutex.RUnlock()
	for _, subnet := range ipam.Subnets {
		subnet.ReleaseAddress(podName)
	}
}

func (ipam *IPAM) AddOrUpdateSubnet(name, cidrStr, gw string, excludeIps []string) error {
	excludeIps = util.ExpandExcludeIPs(excludeIps, cidrStr)

	ipam.mutex.Lock()
	defer ipam.mutex.Unlock()

	var v4cidrStr, v6cidrStr, v4Gw, v6Gw string
	var cidrs []*net.IPNet
	for _, cidrBlock := range strings.Split(cidrStr, ",") {
		if _, cidr, err := net.ParseCIDR(cidrBlock); err != nil {
			return ErrInvalidCIDR
		} else {
			cidrs = append(cidrs, cidr)
		}
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
		v4Reserved := NewIPRangeListFrom(v4ExcludeIps...)
		v6Reserved := NewIPRangeListFrom(v6ExcludeIps...)
		if protocol == kubeovnv1.ProtocolDual || protocol == kubeovnv1.ProtocolIPv4 &&
			(subnet.V4CIDR.String() != v4cidrStr || subnet.V4Gw != v4Gw || !subnet.V4ReservedIPList.Equal(v4Reserved)) {
			_, cidr, _ := net.ParseCIDR(v4cidrStr)
			subnet.V4CIDR = cidr
			subnet.V4ReservedIPList = v4Reserved
			firstIP, _ := util.FirstIP(v4cidrStr)
			lastIP, _ := util.LastIP(v4cidrStr)
			subnet.V4FreeIPList = NewIPRangeListFrom(fmt.Sprintf("%s..%s", firstIP, lastIP)).Difference(subnet.V4ReservedIPList)
			subnet.V4AvailIPList = subnet.V4FreeIPList.Clone()
			subnet.V4ReleasedIPList = NewIPRangeList()
			subnet.V4Gw = v4Gw
			for nicName, ip := range subnet.V4NicToIP {
				mac := subnet.NicToMac[nicName]
				podName := subnet.V4IPToPod[ip.String()]
				if _, _, err := subnet.GetStaticAddress(podName, nicName, ip, &mac, true, true); err != nil {
					klog.Errorf("%s address not in subnet %s new cidr %s: %v", podName, name, cidrStr, err)
				}
			}
		}
		if protocol == kubeovnv1.ProtocolDual || protocol == kubeovnv1.ProtocolIPv6 &&
			(subnet.V6CIDR.String() != v6cidrStr || subnet.V6Gw != v6Gw || !subnet.V6ReservedIPList.Equal(v6Reserved)) {
			_, cidr, _ := net.ParseCIDR(v6cidrStr)
			subnet.V6CIDR = cidr
			subnet.V6ReservedIPList = v6Reserved
			firstIP, _ := util.FirstIP(v6cidrStr)
			lastIP, _ := util.LastIP(v6cidrStr)
			subnet.V6FreeIPList = NewIPRangeListFrom(fmt.Sprintf("%s..%s", firstIP, lastIP)).Difference(subnet.V6ReservedIPList)
			subnet.V6AvailIPList = subnet.V6FreeIPList.Clone()
			subnet.V6ReleasedIPList = NewIPRangeList()
			subnet.V6Gw = v6Gw
			for nicName, ip := range subnet.V6NicToIP {
				mac := subnet.NicToMac[nicName]
				podName := subnet.V6IPToPod[ip.String()]
				if _, _, err := subnet.GetStaticAddress(podName, nicName, ip, &mac, true, true); err != nil {
					klog.Errorf("%s address not in subnet %s new cidr %s: %v", podName, name, cidrStr, err)
				}
			}
		}
		return nil
	}

	subnet, err := NewSubnet(name, cidrStr, excludeIps)
	if err != nil {
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
				addresses = append(addresses, &SubnetAddress{Subnet: subnet, Ip: v4IP.String(), Mac: mac})
			case kubeovnv1.ProtocolIPv6:
				addresses = append(addresses, &SubnetAddress{Subnet: subnet, Ip: v6IP.String(), Mac: mac})
			case kubeovnv1.ProtocolDual:
				addresses = append(addresses, &SubnetAddress{Subnet: subnet, Ip: v4IP.String(), Mac: mac})
				addresses = append(addresses, &SubnetAddress{Subnet: subnet, Ip: v6IP.String(), Mac: mac})
			}
		}
		subnet.mutex.RUnlock()
	}
	return addresses
}

func (ipam *IPAM) ContainAddress(address string) bool {
	ipam.mutex.RLock()
	defer ipam.mutex.RUnlock()
	for _, subnet := range ipam.Subnets {
		if subnet.ContainAddress(NewIP(address)) {
			return true
		}
	}
	return false
}

func (ipam *IPAM) IsIPAssignedToOtherPod(ip, subnetName, podName string) (string, bool) {
	ipam.mutex.RLock()
	defer ipam.mutex.RUnlock()

	if subnet, ok := ipam.Subnets[subnetName]; !ok {
		return "", false
	} else {
		return subnet.isIPAssignedToOtherPod(ip, podName)
	}
}

func (ipam *IPAM) GetSubnetV4Mask(subnetName string) (string, error) {
	if subnet, ok := ipam.Subnets[subnetName]; ok {
		mask, _ := subnet.V4CIDR.Mask.Size()
		return strconv.Itoa(mask), nil
	} else {
		return "", ErrNoAvailable
	}
}

func (ipam *IPAM) GetSubnetIPRangeString(subnetName string) (string, string, string, string) {
	ipam.mutex.RLock()
	defer ipam.mutex.RUnlock()

	var v4UsingIPStr, v6UsingIPStr, v4AvailableIPStr, v6AvailableIPStr string
	if subnet, ok := ipam.Subnets[subnetName]; ok {
		v4UsingIPStr = subnet.V4UsingIPList.String()
		v6UsingIPStr = subnet.V6UsingIPList.String()
		v4AvailableIPStr = subnet.V4AvailIPList.String()
		v6AvailableIPStr = subnet.V6AvailIPList.String()
	}

	return v4UsingIPStr, v6UsingIPStr, v4AvailableIPStr, v6AvailableIPStr
}
