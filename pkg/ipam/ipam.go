package ipam

import (
	"errors"
	"net"
	"strings"
	"sync"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"k8s.io/klog"
)

var (
	OutOfRangeError  = errors.New("AddressOutOfRange")
	ConflictError    = errors.New("AddressConflict")
	NoAvailableError = errors.New("NoAvailableAddress")
	InvalidCIDRError = errors.New("CIDRInvalid")
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

func (ipam *IPAM) GetRandomAddress(podName, subnetName string, skippedAddrs []string) (string, string, string, error) {
	ipam.mutex.RLock()
	defer ipam.mutex.RUnlock()

	subnet, ok := ipam.Subnets[subnetName]
	if !ok {
		return "", "", "", NoAvailableError
	}

	v4IP, v6IP, mac, err := subnet.GetRandomAddress(podName, skippedAddrs)
	klog.Infof("allocate v4 %s v6 %s mac %s for %s", v4IP, v6IP, mac, podName)
	return string(v4IP), string(v6IP), mac, err
}

func (ipam *IPAM) GetStaticAddress(podName, ip, mac, subnetName string) (string, string, string, error) {
	ipam.mutex.RLock()
	defer ipam.mutex.RUnlock()
	if subnet, ok := ipam.Subnets[subnetName]; !ok {
		return "", "", "", NoAvailableError
	} else {
		var ips []IP
		var err error
		var ipAddr IP
		for _, ipStr := range strings.Split(ip, ",") {
			ipAddr, mac, err = subnet.GetStaticAddress(podName, IP(ipStr), mac, false)
			if err != nil {
				return "", "", "", err
			}
			ips = append(ips, ipAddr)
		}
		ips, err = checkAndAppendIpsForDual(ips, podName, subnet)
		if err != nil {
			klog.Errorf("failed to append allocate ip %v mac %s for %s", ips, mac, podName)
			return "", "", "", err
		}

		switch subnet.Protocol {
		case kubeovnv1.ProtocolIPv4:
			klog.Infof("allocate v4 %s mac %s for %s", ip, mac, podName)
			return ip, "", mac, err
		case kubeovnv1.ProtocolIPv6:
			klog.Infof("allocate v6 %s mac %s for %s", ip, mac, podName)
			return "", ip, mac, err
		case kubeovnv1.ProtocolDual:
			klog.Infof("allocate v4 %s v6 %s mac %s for %s", string(ips[0]), string(ips[1]), mac, podName)
			return string(ips[0]), string(ips[1]), mac, err
		}
	}
	return "", "", "", NoAvailableError
}

func checkAndAppendIpsForDual(ips []IP, podName string, subnet *Subnet) ([]IP, error) {
	// IP Address for dual-stack should be format of 'IPv4,IPv6'
	if subnet.Protocol != kubeovnv1.ProtocolDual || len(ips) == 2 {
		return ips, nil
	}

	var newIps []IP
	var ipAddr IP
	var err error
	if util.CheckProtocol(string(ips[0])) == kubeovnv1.ProtocolIPv4 {
		newIps = ips
		_, ipAddr, _, err = subnet.getV6RandomAddress(podName, nil)
		newIps = append(newIps, ipAddr)
	} else if util.CheckProtocol(string(ips[0])) == kubeovnv1.ProtocolIPv6 {
		ipAddr, _, _, err = subnet.getV4RandomAddress(podName, nil)
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

func (ipam *IPAM) AddOrUpdateSubnet(name, cidrStr string, excludeIps []string) error {
	excludeIps = util.ExpandExcludeIPs(excludeIps, cidrStr)

	ipam.mutex.Lock()
	defer ipam.mutex.Unlock()

	var v4cidrStr, v6cidrStr string
	var cidrs []*net.IPNet
	for _, cidrBlock := range strings.Split(cidrStr, ",") {
		if _, cidr, err := net.ParseCIDR(cidrBlock); err != nil {
			return InvalidCIDRError
		} else {
			cidrs = append(cidrs, cidr)
		}
	}
	protocol := util.CheckProtocol(cidrStr)
	switch protocol {
	case kubeovnv1.ProtocolDual:
		v4cidrStr = cidrs[0].String()
		v6cidrStr = cidrs[1].String()
	case kubeovnv1.ProtocolIPv4:
		v4cidrStr = cidrs[0].String()
	case kubeovnv1.ProtocolIPv6:
		v6cidrStr = cidrs[0].String()
	}

	// subnet.Spec.ExcludeIps contains both v4 and v6 addresses
	v4ExcludeIps, v6ExcludeIps := util.SplitIpsByProtocol(excludeIps)

	if subnet, ok := ipam.Subnets[name]; ok {
		subnet.Protocol = protocol
		if protocol == kubeovnv1.ProtocolDual || protocol == kubeovnv1.ProtocolIPv4 {
			_, cidr, _ := net.ParseCIDR(v4cidrStr)
			subnet.V4CIDR = cidr
			subnet.V4ReservedIPList = convertExcludeIps(v4ExcludeIps)
			firstIP, _ := util.FirstIP(v4cidrStr)
			lastIP, _ := util.LastIP(v4cidrStr)
			subnet.V4FreeIPList = IPRangeList{&IPRange{Start: IP(firstIP), End: IP(lastIP)}}
			subnet.joinFreeWithReserve()
			for podName, ip := range subnet.V4PodToIP {
				mac := subnet.PodToMac[podName]
				if _, _, err := subnet.GetStaticAddress(podName, ip, mac, true); err != nil {
					klog.Errorf("%s address not in subnet %s new cidr %s", podName, name, cidrStr)
				}
			}
		}
		if protocol == kubeovnv1.ProtocolDual || protocol == kubeovnv1.ProtocolIPv6 {
			_, cidr, _ := net.ParseCIDR(v6cidrStr)
			subnet.V6CIDR = cidr
			subnet.V6ReservedIPList = convertExcludeIps(v6ExcludeIps)
			firstIP, _ := util.FirstIP(v6cidrStr)
			lastIP, _ := util.LastIP(v6cidrStr)
			subnet.V6FreeIPList = IPRangeList{&IPRange{Start: IP(firstIP), End: IP(lastIP)}}
			subnet.joinFreeWithReserve()
			for podName, ip := range subnet.V6PodToIP {
				mac := subnet.PodToMac[podName]
				if _, _, err := subnet.GetStaticAddress(podName, ip, mac, true); err != nil {
					klog.Errorf("%s address not in subnet %s new cidr %s", podName, name, cidrStr)
				}
			}
		}
		return nil
	}

	subnet, err := NewSubnet(name, cidrStr, excludeIps)
	if err != nil {
		return err
	}
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
		v4IP, v6IP, mac, protocol := subnet.GetPodAddress(podName)
		switch protocol {
		case kubeovnv1.ProtocolIPv4:
			addresses = append(addresses, &SubnetAddress{Subnet: subnet, Ip: string(v4IP), Mac: mac})
		case kubeovnv1.ProtocolIPv6:
			addresses = append(addresses, &SubnetAddress{Subnet: subnet, Ip: string(v6IP), Mac: mac})
		case kubeovnv1.ProtocolDual:
			addresses = append(addresses, &SubnetAddress{Subnet: subnet, Ip: string(v4IP), Mac: mac})
			addresses = append(addresses, &SubnetAddress{Subnet: subnet, Ip: string(v6IP), Mac: mac})
		}
	}
	return addresses
}

func (ipam *IPAM) ContainAddress(address string) bool {
	ipam.mutex.RLock()
	defer ipam.mutex.RUnlock()
	for _, subnet := range ipam.Subnets {
		if subnet.ContainAddress(IP(address)) {
			return true
		}
	}
	return false
}

func (ipam *IPAM) IsIPAssignedToPod(ip, subnetName string) bool {
	ipam.mutex.RLock()
	defer ipam.mutex.RUnlock()

	if subnet, ok := ipam.Subnets[subnetName]; !ok {
		return false
	} else {
		return subnet.isIPAssignedToPod(ip)
	}
}
