package ipam

import (
	"errors"
	"fmt"
	v1 "github.com/alauda/kube-ovn/pkg/apis/kubeovn/v1"
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
	mutex      sync.RWMutex
	Subnets    map[string]*Subnet
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

func (ipam *IPAM) GetRandomAddress(podName string, subnetName string) (v1.DualStack, string, error) {
	ipam.mutex.RLock()
	defer ipam.mutex.RUnlock()
	if subnet, ok := ipam.Subnets[subnetName]; !ok {
		return nil, "", NoAvailableError
	} else {
		ip, mac, err := subnet.GetRandomAddress(podName)
		klog.Infof("allocate %s %s for %s", ip, mac, podName)
		return ip, mac, err
	}
}

func (ipam *IPAM) GetStaticAddress(podName, ip, mac, subnetName string) (v1.DualStack, string, error) {
	ipam.mutex.RLock()
	defer ipam.mutex.RUnlock()
	if subnet, ok := ipam.Subnets[subnetName]; !ok  {
		return nil, "", NoAvailableError
	} else {
		ipDual, _ := util.StringToDualStack(ip)
		ip, mac, err := subnet.GetStaticAddress(podName, ipDual, mac, false)
		if err != nil {
			return nil, "", err
		}
		klog.Infof("allocate %s %s for %s", ip, mac, podName)
		return ip, mac, err
	}
}

func (ipam *IPAM) ReleaseAddressByPod(podName string) {
	ipam.mutex.RLock()
	defer ipam.mutex.RUnlock()
	for _, subnet := range ipam.Subnets {
		ip, mac := subnet.ReleaseAddress(podName)
		if ip != nil {
			klog.Infof("release %s %s for %s", ip, mac, podName)
		}
	}
	return
}

func (ipam *IPAM) AddOrUpdateSubnet(name string, cidrBlock v1.DualStack, excludeIps v1.DualStackList) error {
	ipam.mutex.Lock()
	defer ipam.mutex.Unlock()

	subnetNew, err := NewSubnet(name, cidrBlock, excludeIps)
	if err != nil {
		return err
	}

	if subnet, ok := ipam.Subnets[name]; ok {
		// subnet update, populate pod ip&mac
		for podName, ip := range subnet.PodToIP {
			mac := subnet.PodToMac[podName]
			if _, _, err := subnetNew.GetStaticAddress(podName, ip, mac, true); err != nil {
				klog.Errorf("%s address not in subnet %s new cidr %s", podName, name, cidrBlock)
				return fmt.Errorf("update subnet fail, ip %s not in %s new cidr %s", podName, name, cidrBlock)
			}
		}
	}

	klog.Infof("adding/updating subnet %s", name)
	ipam.Subnets[name] = subnetNew
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
		if ipDual, mac, exist := subnet.GetPodAddress(podName); exist {
			for _, ip := range ipDual {
			    addresses = append(addresses, &SubnetAddress{Subnet: subnet, Ip: string(ip), Mac: mac})
			}
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
