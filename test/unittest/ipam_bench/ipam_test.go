package ipam

import (
	"fmt"
	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ipam"

	"k8s.io/klog/v2"
	"math/rand"
	"testing"
)

func BenchmarkIPAMIPv4AddSubnet(b *testing.B) {
	im := ipam.NewIPAM()
	for n := 0; n < b.N; n++ {
		if ok := addIPAMSubnet(im, n, kubeovnv1.ProtocolIPv4); !ok {
			return
		}
	}
}

func BenchmarkIPAMIPv4DelSubnet(b *testing.B) {
	im := ipam.NewIPAM()
	for n := 0; n < b.N; n++ {
		if ok := addIPAMSubnet(im, n, kubeovnv1.ProtocolIPv4); !ok {
			return
		}
	}

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		delIPAMSubnet(im, n)
	}
}

func BenchmarkIPAMIPv4AllocAddr(b *testing.B) {
	im := ipam.NewIPAM()

	subnetName, ipv4CIDR, v4Gw, ipv4ExcludeIPs := getDefaultSubnetParam(kubeovnv1.ProtocolIPv4)
	if err := im.AddOrUpdateSubnet(subnetName, ipv4CIDR, v4Gw, ipv4ExcludeIPs); err != nil {
		klog.Fatalf("ERROR: add subnet with ipv4 cidr %s ", ipv4CIDR)
	}

	for n := 0; n < b.N; n++ {
		podName := fmt.Sprintf("pod%d", n)
		nicName := fmt.Sprintf("nic%d", n)
		if _, _, _, err := im.GetRandomAddress(podName, nicName, "", subnetName, nil, true); err != nil {
			klog.Fatalf("ERROR: allocate ipv4 address failed with index %d ", n)
		}
	}
}

func BenchmarkIPAMIPv4FreeAddr(b *testing.B) {
	im := ipam.NewIPAM()

	subnetName, ipv4CIDR, v4Gw, ipv4ExcludeIPs := getDefaultSubnetParam(kubeovnv1.ProtocolIPv4)
	if err := im.AddOrUpdateSubnet(subnetName, ipv4CIDR, v4Gw, ipv4ExcludeIPs); err != nil {
		klog.Fatalf("ERROR: add subnet with ipv4 cidr %s ", ipv4CIDR)
	}

	for n := 0; n < b.N; n++ {
		podName := fmt.Sprintf("pod%d", n)
		nicName := fmt.Sprintf("nic%d", n)
		if _, _, _, err := im.GetRandomAddress(podName, nicName, "", subnetName, nil, true); err != nil {
			klog.Fatalf("ERROR: allocate ipv4 address failed with index %d ", n)
		}
	}

	b.ResetTimer()

	for n := 0; n < b.N; n++ {
		podName := fmt.Sprintf("pod%d", n)
		im.ReleaseAddressByPod(podName)
	}
}

func BenchmarkIPAMIPv6AddSubnet(b *testing.B) {
	im := ipam.NewIPAM()

	for n := 0; n < b.N; n++ {
		if ok := addIPAMSubnet(im, n, kubeovnv1.ProtocolIPv6); !ok {
			return
		}
	}
}

func BenchmarkIPAMIPv6DelSubnet(b *testing.B) {
	im := ipam.NewIPAM()
	for n := 0; n < b.N; n++ {
		if ok := addIPAMSubnet(im, n, kubeovnv1.ProtocolIPv6); !ok {
			return
		}
	}

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		delIPAMSubnet(im, n)
	}
}

func BenchmarkIPAMIPv6AllocAddr(b *testing.B) {
	im := ipam.NewIPAM()

	subnetName, ipv6CIDR, v6Gw, ipv6ExcludeIPs := getDefaultSubnetParam(kubeovnv1.ProtocolIPv6)
	if err := im.AddOrUpdateSubnet(subnetName, ipv6CIDR, v6Gw, ipv6ExcludeIPs); err != nil {
		klog.Fatalf("ERROR: add subnet with ipv6 cidr %s ", ipv6CIDR)
	}

	for n := 0; n < b.N; n++ {
		podName := fmt.Sprintf("pod%d", n)
		nicName := fmt.Sprintf("nic%d", n)
		if _, _, _, err := im.GetRandomAddress(podName, nicName, "", subnetName, nil, true); err != nil {
			klog.Fatalf("ERROR: allocate ipv6 address failed with index %d ", n)
		}
	}
}

func BenchmarkIPAMIPv6FreeAddr(b *testing.B) {
	im := ipam.NewIPAM()

	subnetName, ipv6CIDR, v6Gw, ipv6ExcludeIPs := getDefaultSubnetParam(kubeovnv1.ProtocolIPv6)
	if err := im.AddOrUpdateSubnet(subnetName, ipv6CIDR, v6Gw, ipv6ExcludeIPs); err != nil {
		klog.Fatalf("ERROR: add subnet with ipv6 cidr %s ", ipv6CIDR)
	}

	for n := 0; n < b.N; n++ {
		podName := fmt.Sprintf("pod%d", n)
		nicName := fmt.Sprintf("nic%d", n)
		if _, _, _, err := im.GetRandomAddress(podName, nicName, "", subnetName, nil, true); err != nil {
			klog.Fatalf("ERROR: allocate ipv6 address failed with index %d ", n)
		}
	}

	b.ResetTimer()

	for n := 0; n < b.N; n++ {
		podName := fmt.Sprintf("pod%d", n)
		im.ReleaseAddressByPod(podName)
	}
}

func BenchmarkIPAMDualAddSubnet(b *testing.B) {
	im := ipam.NewIPAM()

	for n := 0; n < b.N; n++ {
		if ok := addIPAMSubnet(im, n, kubeovnv1.ProtocolDual); !ok {
			return
		}
	}
}

func BenchmarkIPAMDualDelSubnet(b *testing.B) {
	im := ipam.NewIPAM()
	for n := 0; n < b.N; n++ {
		if ok := addIPAMSubnet(im, n, kubeovnv1.ProtocolDual); !ok {
			return
		}
	}

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		delIPAMSubnet(im, n)
	}
}

func BenchmarkIPAMDualAllocAddr(b *testing.B) {
	im := ipam.NewIPAM()
	subnetName, dualCIDR, dualGw, dualExcludeIPs := getDefaultSubnetParam(kubeovnv1.ProtocolDual)

	if err := im.AddOrUpdateSubnet(subnetName, dualCIDR, dualGw, dualExcludeIPs); err != nil {
		klog.Fatalf("ERROR: add subnet with dual cidr %s ", dualCIDR)
	}

	for n := 0; n < b.N; n++ {
		podName := fmt.Sprintf("pod%d", n)
		nicName := fmt.Sprintf("nic%d", n)
		if _, _, _, err := im.GetRandomAddress(podName, nicName, "", subnetName, nil, true); err != nil {
			klog.Fatalf("ERROR: allocate dual address failed with index %d ", n)
		}
	}
}

func BenchmarkIPAMDualFreeAddr(b *testing.B) {
	im := ipam.NewIPAM()

	subnetName, dualCIDR, dualGw, dualExcludeIPs := getDefaultSubnetParam(kubeovnv1.ProtocolDual)
	if err := im.AddOrUpdateSubnet(subnetName, dualCIDR, dualGw, dualExcludeIPs); err != nil {
		klog.Fatalf("ERROR: add subnet with ipv4 cidr %s ", dualCIDR)
	}

	for n := 0; n < b.N; n++ {
		podName := fmt.Sprintf("pod%d", n)
		nicName := fmt.Sprintf("nic%d", n)
		if _, _, _, err := im.GetRandomAddress(podName, nicName, "", subnetName, nil, true); err != nil {
			klog.Fatalf("ERROR: allocate ipv4 address failed with index %d ", n)
		}
	}

	b.ResetTimer()

	for n := 0; n < b.N; n++ {
		podName := fmt.Sprintf("pod%d", n)
		im.ReleaseAddressByPod(podName)
	}
}

func BenchmarkParallelIPAMIPv4AddDel1000Subnet(b *testing.B) {
	benchmarkIPv4AddDelSubnetParallel(b, 1000)
}

func BenchmarkParallelIPAMIPv4AllocFree10000Addr(b *testing.B) {
	benchmarkIPv4AllocFreeAddrParallel(b, 10000)
}

func BenchmarkParallelIPAMIPv6AddDel1000Subnet(b *testing.B) {
	benchmarkIPv6AddDelSubnetParallel(b, 1000)
}

func BenchmarkParallelIPAMIPv6AllocFree10000Addr(b *testing.B) {
	benchmarkIPv6AllocFreeAddrParallel(b, 10000)
}

func BenchmarkParallelIPAMDualAddDel1000Subnet(b *testing.B) {
	benchmarkDualAddDelSubnetParallel(b, 1000)
}

func BenchmarkParallelIPAMDualAllocFree10000Addr(b *testing.B) {
	benchmarkDualAllocFreeAddrParallel(b, 10000)
}

func addIPAMSubnet(im *ipam.IPAM, index int, protocol string) bool {
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

	if protocol == kubeovnv1.ProtocolIPv4 {
		if err := im.AddOrUpdateSubnet(subnetName, ipv4CIDR, v4Gw, ipv4ExcludeIPs); err != nil {
			klog.Fatalf("ERROR: add subnet with ipv4 cidr %s, with index %d ", ipv4CIDR, index)
		}
	} else if protocol == kubeovnv1.ProtocolIPv6 {
		if err := im.AddOrUpdateSubnet(subnetName, ipv6CIDR, v6Gw, ipv6ExcludeIPs); err != nil {
			klog.Fatalf("ERROR: add subnet with ipv6 cidr %s, with index %d ", ipv6CIDR, index)
		}
	} else if protocol == kubeovnv1.ProtocolDual {
		if err := im.AddOrUpdateSubnet(subnetName, dualCIDR, dualGw, dualExcludeIPs); err != nil {
			klog.Fatalf("ERROR: add subnet with dual cidr %s, with index %d ", dualCIDR, index)
		}
	}

	return true
}

func delIPAMSubnet(im *ipam.IPAM, index int) {
	subnetName := fmt.Sprintf("test%d", index)
	im.DeleteSubnet(subnetName)
}

func benchmarkIPv4AddDelSubnetParallel(b *testing.B, subnetNumber int) {
	im := ipam.NewIPAM()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			for n := 0; n < subnetNumber; n++ {
				if ok := addIPAMSubnet(im, n, kubeovnv1.ProtocolIPv4); !ok {
					return
				}
			}
			for n := 0; n < subnetNumber; n++ {
				delIPAMSubnet(im, n)
			}
		}
	})
}

func benchmarkIPv4AllocFreeAddrParallel(b *testing.B, podNumber int) {
	im := ipam.NewIPAM()
	subnetName, ipv4CIDR, v4Gw, ipv4ExcludeIPs := getDefaultSubnetParam(kubeovnv1.ProtocolIPv4)
	if err := im.AddOrUpdateSubnet(subnetName, ipv4CIDR, v4Gw, ipv4ExcludeIPs); err != nil {
		klog.Fatalf("ERROR: add subnet with ipv4 cidr %s ", ipv4CIDR)
	}

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			key := getRandomInt()
			for n := 0; n < podNumber; n++ {
				podName := fmt.Sprintf("pod%d_%d", key, n)
				nicName := fmt.Sprintf("nic%d_%d", key, n)
				if _, _, _, err := im.GetRandomAddress(podName, nicName, "", subnetName, nil, true); err != nil {
					klog.Fatalf("ERROR: allocate ipv4 address failed with index %d ", n)
				}
			}
			for n := 0; n < podNumber; n++ {
				podName := fmt.Sprintf("pod%d_%d", key, n)
				im.ReleaseAddressByPod(podName)
			}
		}
	})
}

func benchmarkIPv6AddDelSubnetParallel(b *testing.B, subnetNumber int) {
	im := ipam.NewIPAM()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			for n := 0; n < subnetNumber; n++ {
				if ok := addIPAMSubnet(im, n, kubeovnv1.ProtocolIPv6); !ok {
					return
				}
			}
			for n := 0; n < subnetNumber; n++ {
				delIPAMSubnet(im, n)
			}
		}
	})
}

func benchmarkIPv6AllocFreeAddrParallel(b *testing.B, podNumber int) {
	im := ipam.NewIPAM()

	subnetName, ipv6CIDR, v6Gw, ipv6ExcludeIPs := getDefaultSubnetParam(kubeovnv1.ProtocolIPv6)

	if err := im.AddOrUpdateSubnet(subnetName, ipv6CIDR, v6Gw, ipv6ExcludeIPs); err != nil {
		klog.Fatalf("ERROR: add subnet with ipv6 cidr %s ", ipv6CIDR)
	}

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			key := getRandomInt()
			for n := 0; n < podNumber; n++ {
				podName := fmt.Sprintf("pod%d_%d", key, n)
				nicName := fmt.Sprintf("nic%d_%d", key, n)
				if _, _, _, err := im.GetRandomAddress(podName, nicName, "", subnetName, nil, true); err != nil {
					klog.Fatalf("ERROR: allocate ipv6 address failed with index %d ", n)
				}
			}
			for n := 0; n < podNumber; n++ {
				podName := fmt.Sprintf("pod%d_%d", key, n)
				im.ReleaseAddressByPod(podName)
			}
		}
	})
}

func benchmarkDualAddDelSubnetParallel(b *testing.B, subnetNumber int) {
	im := ipam.NewIPAM()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			for n := 0; n < subnetNumber; n++ {
				if ok := addIPAMSubnet(im, n, kubeovnv1.ProtocolDual); !ok {
					return
				}
			}
			for n := 0; n < subnetNumber; n++ {
				delIPAMSubnet(im, n)
			}
		}
	})
}

func benchmarkDualAllocFreeAddrParallel(b *testing.B, podNumber int) {
	im := ipam.NewIPAM()
	subnetName, dualCIDR, dualGw, dualExcludeIPs := getDefaultSubnetParam(kubeovnv1.ProtocolDual)
	if err := im.AddOrUpdateSubnet(subnetName, dualCIDR, dualGw, dualExcludeIPs); err != nil {
		klog.Fatalf("ERROR: add subnet with dual cidr %s ", dualCIDR)
	}

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			key := getRandomInt()
			for n := 0; n < podNumber; n++ {
				podName := fmt.Sprintf("pod%d_%d", key, n)
				nicName := fmt.Sprintf("nic%d_%d", key, n)
				if _, _, _, err := im.GetRandomAddress(podName, nicName, "", subnetName, nil, true); err != nil {
					klog.Fatalf("ERROR: allocate dual address failed with index %d ", n)
				}
			}
			for n := 0; n < podNumber; n++ {
				podName := fmt.Sprintf("pod%d_%d", key, n)
				im.ReleaseAddressByPod(podName)
			}
		}
	})
}

func getDefaultSubnetParam(protocol string) (string, string, string, []string) {
	subnetName := "subnetBench"
	ipv4CIDR := "10.0.0.0/8"
	v4Gw := "10.0.0.1"
	ipv4ExcludeIPs := []string{v4Gw}

	ipv6CIDR := "fd00::a0:00:00:00:00:00/104"
	v6Gw := "fd00::a0:00:00:00:00:01"
	ipv6ExcludeIPs := []string{v6Gw}

	dualCIDR := fmt.Sprintf("%s,%s", ipv4CIDR, ipv6CIDR)
	dualGw := fmt.Sprintf("%s,%s", v4Gw, v6Gw)
	dualExcludeIPs := append(ipv4ExcludeIPs, ipv6ExcludeIPs...)

	if protocol == kubeovnv1.ProtocolIPv4 {
		return subnetName, ipv4CIDR, v4Gw, ipv4ExcludeIPs
	} else if protocol == kubeovnv1.ProtocolIPv6 {
		return subnetName, ipv6CIDR, v6Gw, ipv6ExcludeIPs
	} else {
		return subnetName, dualCIDR, dualGw, dualExcludeIPs
	}
}

func getRandomInt() int {
	return rand.Intn(10000)
}
