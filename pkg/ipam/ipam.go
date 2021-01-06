package ipam

import (
	"errors"
	"net"
	"sync"

	"github.com/alauda/kube-ovn/pkg/util"
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

func (ipam *IPAM) GetRandomAddress(podName string, subnetName string) (string, string, error) {
	ipam.mutex.RLock()
	defer ipam.mutex.RUnlock()
	if subnet, ok := ipam.Subnets[subnetName]; !ok {
		return "", "", NoAvailableError
	} else {
		ip, mac, err := subnet.GetRandomAddress(podName)
		klog.Infof("allocate %s %s for %s", ip, mac, podName)
		return string(ip), mac, err
	}
}

func (ipam *IPAM) GetStaticAddress(podName string, ip, mac string, subnetName string) (string, string, error) {
	ipam.mutex.RLock()
	defer ipam.mutex.RUnlock()
	if subnet, ok := ipam.Subnets[subnetName]; !ok {
		return "", "", NoAvailableError
	} else {
		ip, mac, err := subnet.GetStaticAddress(podName, IP(ip), mac, false)
		klog.Infof("allocate %s %s for %s", ip, mac, podName)
		return string(ip), mac, err
	}
}

func (ipam *IPAM) ReleaseAddressByPod(podName string) {
	ipam.mutex.RLock()
	defer ipam.mutex.RUnlock()
	for _, subnet := range ipam.Subnets {
		ip, mac := subnet.ReleaseAddress(podName)
		if ip != "" {
			klog.Infof("release %s %s for %s", ip, mac, podName)
		}
	}
	return
}

func (ipam *IPAM) AddOrUpdateSubnet(name, cidrStr string, excludeIps []string) error {
	ipam.mutex.Lock()
	defer ipam.mutex.Unlock()

	_, cidr, err := net.ParseCIDR(cidrStr)
	if err != nil {
		return InvalidCIDRError
	}

	if subnet, ok := ipam.Subnets[name]; ok {
		subnet.CIDR = cidr
		subnet.ReservedIPList = convertExcludeIps(excludeIps)
		firstIP, _ := util.FirstSubnetIP(cidrStr)
		lastIP, _ := util.LastIP(cidrStr)
		subnet.FreeIPList = IPRangeList{&IPRange{Start: IP(firstIP), End: IP(lastIP)}}
		subnet.joinFreeWithReserve()
		for podName, ip := range subnet.PodToIP {
			mac := subnet.PodToMac[podName]
			if _, _, err := subnet.GetStaticAddress(podName, ip, mac, true); err != nil {
				klog.Errorf("%s address not in subnet %s new cidr %s", podName, name, cidrStr)
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
		if ip, mac, exist := subnet.GetPodAddress(podName); exist {
			addresses = append(addresses, &SubnetAddress{Subnet: subnet, Ip: string(ip), Mac: mac})
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
