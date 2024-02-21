package ipam_bench

import (
	"crypto/rand"
	"flag"
	"fmt"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/scylladb/go-set/strset"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ipam"
)

func init() {
	testing.Init()
	klog.InitFlags(nil)
	flag.Parse()
}

func BenchmarkIPAMSerialIPv4AddSubnet(b *testing.B) {
	im := ipam.NewIPAM()
	addSubnetCapacity(b, im, kubeovnv1.ProtocolIPv4)
}

func BenchmarkIPAMSerialIPv4DelSubnet(b *testing.B) {
	im := ipam.NewIPAM()
	addSubnetCapacity(b, im, kubeovnv1.ProtocolIPv4)
	b.ResetTimer()
	delSubnetCapacity(b, im)
}

func BenchmarkIPAMSerialIPv4AllocAddr(b *testing.B) {
	im := ipam.NewIPAM()
	addSerailAddrCapacity(b, im, kubeovnv1.ProtocolIPv4)
}

func BenchmarkIPAMSerialIPv4FreeAddr(b *testing.B) {
	im := ipam.NewIPAM()
	addSerailAddrCapacity(b, im, kubeovnv1.ProtocolIPv4)
	b.ResetTimer()
	delPodAddressCapacity(b, im, false)
}

func BenchmarkIPAMSerialIPv6AddSubnet(b *testing.B) {
	im := ipam.NewIPAM()
	addSubnetCapacity(b, im, kubeovnv1.ProtocolIPv6)
}

func BenchmarkIPAMSerialIPv6DelSubnet(b *testing.B) {
	im := ipam.NewIPAM()
	addSubnetCapacity(b, im, kubeovnv1.ProtocolIPv6)
	b.ResetTimer()
	delSubnetCapacity(b, im)
}

func BenchmarkIPAMSerialIPv6AllocAddr(b *testing.B) {
	im := ipam.NewIPAM()
	addSerailAddrCapacity(b, im, kubeovnv1.ProtocolIPv6)
}

func BenchmarkIPAMSerialIPv6FreeAddr(b *testing.B) {
	im := ipam.NewIPAM()
	addSerailAddrCapacity(b, im, kubeovnv1.ProtocolIPv6)
	b.ResetTimer()
	delPodAddressCapacity(b, im, false)
}

func BenchmarkIPAMSerialDualAddSubnet(b *testing.B) {
	im := ipam.NewIPAM()
	addSubnetCapacity(b, im, kubeovnv1.ProtocolDual)
}

func BenchmarkIPAMSerialDualDelSubnet(b *testing.B) {
	im := ipam.NewIPAM()
	addSubnetCapacity(b, im, kubeovnv1.ProtocolDual)
	b.ResetTimer()
	delSubnetCapacity(b, im)
}

func BenchmarkIPAMSerialDualAllocAddr(b *testing.B) {
	im := ipam.NewIPAM()
	addSerailAddrCapacity(b, im, kubeovnv1.ProtocolDual)
}

func BenchmarkIPAMSerialDualFreeAddr(b *testing.B) {
	im := ipam.NewIPAM()
	addSerailAddrCapacity(b, im, kubeovnv1.ProtocolDual)
	b.ResetTimer()
	delPodAddressCapacity(b, im, false)
}

func BenchmarkIPAMRandomIPv4AllocAddr(b *testing.B) {
	im := ipam.NewIPAM()
	addRandomAddrCapacity(b, im, kubeovnv1.ProtocolIPv4, false)
}

func BenchmarkIPAMRandomIPv4FreeAddr(b *testing.B) {
	im := ipam.NewIPAM()

	addRandomAddrCapacity(b, im, kubeovnv1.ProtocolIPv4, false)
	b.ResetTimer()
	delPodAddressCapacity(b, im, false)
}

func BenchmarkIPAMRandomIPv6AllocAddr(b *testing.B) {
	im := ipam.NewIPAM()
	addRandomAddrCapacity(b, im, kubeovnv1.ProtocolIPv6, false)
}

func BenchmarkIPAMRandomIPv6FreeAddr(b *testing.B) {
	im := ipam.NewIPAM()
	addRandomAddrCapacity(b, im, kubeovnv1.ProtocolIPv6, false)
	b.ResetTimer()
	delPodAddressCapacity(b, im, false)
}

func BenchmarkIPAMRandomDualAllocAddr(b *testing.B) {
	im := ipam.NewIPAM()
	addRandomAddrCapacity(b, im, kubeovnv1.ProtocolDual, false)
}

func BenchmarkIPAMRandomDualFreeAddr(b *testing.B) {
	im := ipam.NewIPAM()
	addRandomAddrCapacity(b, im, kubeovnv1.ProtocolDual, false)
	b.ResetTimer()
	delPodAddressCapacity(b, im, false)
}

func BenchmarkParallelIPAMIPv4AddDel1000Subnet(b *testing.B) {
	benchmarkAddDelSubnetParallel(b, 1000, kubeovnv1.ProtocolIPv4)
}

func BenchmarkParallelIPAMIPv4AllocFree3000Addr(b *testing.B) {
	benchmarkAllocFreeAddrParallel(b, 3000, kubeovnv1.ProtocolIPv4)
}

func BenchmarkParallelIPAMIPv6AddDel1000Subnet(b *testing.B) {
	benchmarkAddDelSubnetParallel(b, 1000, kubeovnv1.ProtocolIPv6)
}

func BenchmarkParallelIPAMIPv6AllocFree3000Addr(b *testing.B) {
	benchmarkAllocFreeAddrParallel(b, 3000, kubeovnv1.ProtocolIPv6)
}

func BenchmarkParallelIPAMDualAddDel1000Subnet(b *testing.B) {
	benchmarkAddDelSubnetParallel(b, 1000, kubeovnv1.ProtocolDual)
}

func BenchmarkParallelIPAMDualAllocFree3000Addr(b *testing.B) {
	benchmarkAllocFreeAddrParallel(b, 3000, kubeovnv1.ProtocolDual)
}

func BenchmarkPerformanceIPAMRandomIPv4AllocAddr(b *testing.B) {
	im := ipam.NewIPAM()
	addRandomAddrCapacity(b, im, kubeovnv1.ProtocolIPv4, true)
	delPodAddressCapacity(b, im, true)
}

func addSubnetCapacity(b *testing.B, im *ipam.IPAM, protocol string) {
	for n := 0; n < b.N; n++ {
		if !addIPAMSubnet(b, im, n, protocol) {
			b.Errorf("ERROR: add %s subnet with index %d", protocol, n)
			return
		}
	}
}

func delSubnetCapacity(b *testing.B, im *ipam.IPAM) {
	for n := 0; n < b.N; n++ {
		delIPAMSubnet(im, n)
	}
}

func addSerailAddrCapacity(b *testing.B, im *ipam.IPAM, protocol string) {
	subnetName, cidr, gw, excludeIPs := getDefaultSubnetParam(protocol)
	if err := im.AddOrUpdateSubnet(subnetName, cidr, gw, excludeIPs); err != nil {
		b.Errorf("ERROR: add subnet with %s cidr %s err %v", protocol, cidr, err)
		return
	}

	for n := 0; n < b.N; n++ {
		podName := fmt.Sprintf("pod%d", n)
		nicName := fmt.Sprintf("nic%d", n)
		if _, _, _, err := im.GetRandomAddress(podName, nicName, nil, subnetName, "", nil, true); err != nil {
			b.Errorf("ERROR: allocate %s address failed with index %d with err %v", protocol, n, err)
			return
		}
	}
}

func addRandomAddrCapacity(b *testing.B, im *ipam.IPAM, protocol string, isTimeTrace bool) {
	subnetName, cidr, gw, excludeIPs := getDefaultSubnetParam(protocol)
	if err := im.AddOrUpdateSubnet(subnetName, cidr, gw, excludeIPs); err != nil {
		b.Errorf("ERROR: add subnet with %s cidr %s err %v", protocol, cidr, err)
		return
	}

	step := 10000
	ipSet := getDefaultSubnetRandomIps(b, protocol, b.N)
	startTime := time.Now().Unix()
	for n := 0; n < b.N; n++ {
		podName := fmt.Sprintf("pod%d", n)
		nicName := fmt.Sprintf("nic%d", n)
		ip, ok := ipSet.Pop()
		if !ok {
			b.Errorf("pop item failed")
			return
		}
		if isTimeTrace && (n+1)%step == 0 {
			currentTime := time.Now().Unix()
			fmt.Printf("%d to %d pods spent time %ds \n", n+1-step, n, currentTime-startTime)
			startTime = currentTime
		}

		if _, _, _, err := im.GetStaticAddress(podName, nicName, ip, nil, subnetName, true); err != nil {
			b.Errorf("ERROR: allocate %s address failed with index %d with err %v", protocol, n, err)
			return
		}
	}
}

func delPodAddressCapacity(b *testing.B, im *ipam.IPAM, isTimeTrace bool) {
	step := 10000
	startTime := time.Now().Unix()
	for n := 0; n < b.N; n++ {
		podName := fmt.Sprintf("pod%d", n)
		if isTimeTrace && (n+1)%step == 0 {
			currentTime := time.Now().Unix()
			fmt.Printf("delete %d to %d pods spent time %ds \n", n+1-step, n, currentTime-startTime)
			startTime = currentTime
		}
		im.ReleaseAddressByPod(podName, "")
	}
}

func addIPAMSubnet(b *testing.B, im *ipam.IPAM, index int, protocol string) bool {
	subnetName := fmt.Sprintf("subnet%d", index)
	key1 := index / 65536
	key2 := (index / 256) % 256
	key3 := index % 256
	ipv4CIDR := fmt.Sprintf("%d.%d.%d.0/24", key1, key2, key3)
	v4Gw := fmt.Sprintf("%d.%d.%d.1", key1, key2, key3)
	ipv4ExcludeIPs := []string{v4Gw}

	ipv6CIDR := fmt.Sprintf("fd00::%02X:%02X:%02X/120", key1, key2, key3)
	v6Gw := fmt.Sprintf("fd00::%02X:%02X:%02X:01", key1, key2, key3)
	ipv6ExcludeIPs := []string{v6Gw}

	dualCIDR := fmt.Sprintf("%s,%s", ipv4CIDR, ipv6CIDR)
	dualGw := fmt.Sprintf("%s,%s", v4Gw, v6Gw)
	dualExcludeIPs := append(ipv4ExcludeIPs, ipv6ExcludeIPs...)

	switch protocol {
	case kubeovnv1.ProtocolIPv4:
		if err := im.AddOrUpdateSubnet(subnetName, ipv4CIDR, v4Gw, ipv4ExcludeIPs); err != nil {
			b.Errorf("ERROR: add subnet with ipv4 cidr %s, with index %d err %v", ipv4CIDR, index, err)
			return false
		}
	case kubeovnv1.ProtocolIPv6:
		if err := im.AddOrUpdateSubnet(subnetName, ipv6CIDR, v6Gw, ipv6ExcludeIPs); err != nil {
			b.Errorf("ERROR: add subnet with ipv6 cidr %s, with index %d err %v", ipv6CIDR, index, err)
			return false
		}
	case kubeovnv1.ProtocolDual:
		if err := im.AddOrUpdateSubnet(subnetName, dualCIDR, dualGw, dualExcludeIPs); err != nil {
			b.Errorf("ERROR: add subnet with dual cidr %s, with index %d err %v", dualCIDR, index, err)
			return false
		}
	}
	return true
}

func delIPAMSubnet(im *ipam.IPAM, index int) {
	subnetName := fmt.Sprintf("test%d", index)
	im.DeleteSubnet(subnetName)
}

func benchmarkAddDelSubnetParallel(b *testing.B, subnetNumber int, protocol string) {
	im := ipam.NewIPAM()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			for n := 0; n < subnetNumber; n++ {
				if !addIPAMSubnet(b, im, n, protocol) {
					return
				}
			}
			for n := 0; n < subnetNumber; n++ {
				delIPAMSubnet(im, n)
			}
		}
	})
}

func benchmarkAllocFreeAddrParallel(b *testing.B, podNumber int, protocol string) {
	im := ipam.NewIPAM()
	subnetName, CIDR, Gw, ExcludeIPs := getDefaultSubnetParam(protocol)
	if err := im.AddOrUpdateSubnet(subnetName, CIDR, Gw, ExcludeIPs); err != nil {
		b.Errorf("ERROR: add subnet with %s cidr %s: %v", protocol, CIDR, err)
		return
	}

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			key := getRandomInt()
			ipSet := getDefaultSubnetRandomIps(b, protocol, podNumber)
			for n := 0; n < podNumber; n++ {
				podName := fmt.Sprintf("pod%d_%d", key, n)
				nicName := fmt.Sprintf("nic%d_%d", key, n)
				if key%2 == 1 {
					if _, _, _, err := im.GetRandomAddress(podName, nicName, nil, subnetName, "", nil, true); err != nil {
						b.Errorf("ERROR: allocate %s address failed with index %d err %v", protocol, n, err)
						return
					}
				} else {
					ip, ok := ipSet.Pop()
					if !ok {
						b.Errorf("pop item failed")
						return
					}

					if _, _, _, err := im.GetStaticAddress(podName, nicName, ip, nil, subnetName, false); err != nil {
						b.Errorf("ERROR: allocate %s address failed with index %d with err %v", protocol, n, err)
						return
					}
				}
			}
			for n := 0; n < podNumber; n++ {
				podName := fmt.Sprintf("pod%d_%d", key, n)
				im.ReleaseAddressByPod(podName, "")
			}
		}
	})
}

func getDefaultSubnetParam(protocol string) (string, string, string, []string) {
	subnetName := "subnetBench"
	ipv4CIDR := "10.0.0.0/8"
	v4Gw := "10.0.0.1"
	ipv4ExcludeIPs := []string{v4Gw}

	ipv6CIDR := "fd00::/104"
	v6Gw := "fd00::01"
	ipv6ExcludeIPs := []string{v6Gw}

	dualCIDR := fmt.Sprintf("%s,%s", ipv4CIDR, ipv6CIDR)
	dualGw := fmt.Sprintf("%s,%s", v4Gw, v6Gw)
	dualExcludeIPs := append(ipv4ExcludeIPs, ipv6ExcludeIPs...)

	switch protocol {
	case kubeovnv1.ProtocolIPv4:
		return subnetName, ipv4CIDR, v4Gw, ipv4ExcludeIPs
	case kubeovnv1.ProtocolIPv6:
		return subnetName, ipv6CIDR, v6Gw, ipv6ExcludeIPs
	case kubeovnv1.ProtocolDual:
		return subnetName, dualCIDR, dualGw, dualExcludeIPs
	}
	return "", "", "", nil
}

func getDefaultSubnetRandomIps(b *testing.B, protocol string, ipCount int) *stringSet {
	var newIP string
	ipSet := newStringSet()
	for n := 0; ipSet.Len() < ipCount; n++ {
		bytes := make([]byte, 3)
		if _, err := rand.Read(bytes); err != nil {
			b.Errorf("generate random error: %v", err)
		}
		switch protocol {
		case kubeovnv1.ProtocolIPv4:
			newIP = fmt.Sprintf("10.%d.%d.%d", bytes[0], bytes[1], bytes[2])
		case kubeovnv1.ProtocolIPv6:
			newIP = fmt.Sprintf("fd00::00%02x:%02x%02x", bytes[0], bytes[1], bytes[2])
		case kubeovnv1.ProtocolDual:
			newIP = fmt.Sprintf("10.%d.%d.%d,fd00::00%02x:%02x%02x",
				bytes[0], bytes[1], bytes[2], bytes[0], bytes[1], bytes[2])
		}
		ipSet.Add(newIP)
	}
	return ipSet
}

func getRandomInt() int {
	b := new(big.Int).SetInt64(int64(100000))
	randInt, _ := rand.Int(rand.Reader, b)
	return int(randInt.Int64())
}

type stringSet struct {
	*strset.Set
	mutex sync.RWMutex
}

func newStringSet() *stringSet {
	return &stringSet{
		Set: strset.New(),
	}
}

func (s *stringSet) Add(item string) {
	s.Set.Add(item)
}

func (s *stringSet) Pop() (string, bool) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.Pop2()
}

func (s *stringSet) Len() int {
	return s.Size()
}
