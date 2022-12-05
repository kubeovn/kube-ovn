package ipam_bench

import (
	"flag"
	"fmt"
	"math/rand"
	"testing"

	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ipam"
)

/*
[root@localhost kube-ovn]# make ipam-bench
go test -bench='^BenchmarkIPAM' -benchtime=10000x test/unittest/ipam_bench/ipam_test.go -args -logtostderr=false
goos: linux
goarch: amd64
cpu: AMD Ryzen 7 5800H with Radeon Graphics
BenchmarkIPAMIPv4AddSubnet-8       10000             25678 ns/op
BenchmarkIPAMIPv4DelSubnet-8       10000             11381 ns/op
BenchmarkIPAMIPv4AllocAddr-8       10000             19334 ns/op
BenchmarkIPAMIPv4FreeAddr-8        10000             17432 ns/op
BenchmarkIPAMIPv6AddSubnet-8       10000             19285 ns/op
BenchmarkIPAMIPv6DelSubnet-8       10000             12678 ns/op
BenchmarkIPAMIPv6AllocAddr-8       10000             19778 ns/op
BenchmarkIPAMIPv6FreeAddr-8        10000             19717 ns/op
BenchmarkIPAMDualAddSubnet-8       10000             31187 ns/op
BenchmarkIPAMDualDelSubnet-8       10000             10764 ns/op
BenchmarkIPAMDualAllocAddr-8       10000             23416 ns/op
BenchmarkIPAMDualFreeAddr-8        10000             35587 ns/op
PASS
ok      command-line-arguments  3.977s
go test -bench='^BenchmarkParallelIPAM' -benchtime=10x test/unittest/ipam_bench/ipam_test.go -args -logtostderr=false
goos: linux
goarch: amd64
cpu: AMD Ryzen 7 5800H with Radeon Graphics
BenchmarkParallelIPAMIPv4AddDel1000Subnet-8                   10          25722807 ns/op
BenchmarkParallelIPAMIPv4AllocFree10000Addr-8                 10        13791297785 ns/op
BenchmarkParallelIPAMIPv6AddDel1000Subnet-8                   10          20382767 ns/op
BenchmarkParallelIPAMIPv6AllocFree10000Addr-8                 10        12932703206 ns/op
BenchmarkParallelIPAMDualAddDel1000Subnet-8                   10          31276342 ns/op
BenchmarkParallelIPAMDualAllocFree10000Addr-8                 10        7242771662 ns/op
PASS
ok      command-line-arguments  342.035s
*/

func init() {
	testing.Init()
	klog.InitFlags(nil)
	flag.Parse()
}

func BenchmarkIPAMIPv4AddSubnet(b *testing.B) {
	im := ipam.NewIPAM()
	for n := 0; n < b.N; n++ {
		if ok := addIPAMSubnet(im, n, kubeovnv1.ProtocolIPv4); !ok {
			klog.Fatalf("ERROR: add v4 subnet with index %d ", n)
		}
	}
}

func BenchmarkIPAMIPv4DelSubnet(b *testing.B) {
	im := ipam.NewIPAM()
	for n := 0; n < b.N; n++ {
		if ok := addIPAMSubnet(im, n, kubeovnv1.ProtocolIPv4); !ok {
			klog.Fatalf("ERROR: add v4 subnet with index %d ", n)
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
			klog.Fatalf("ERROR: add subnet with dual cidr with index %d ", n)
		}
	}
}

func BenchmarkIPAMIPv6DelSubnet(b *testing.B) {
	im := ipam.NewIPAM()
	for n := 0; n < b.N; n++ {
		if ok := addIPAMSubnet(im, n, kubeovnv1.ProtocolIPv6); !ok {
			klog.Fatalf("ERROR: add subnet with dual cidr with index %d ", n)
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
			klog.Fatalf("ERROR: add dual subnet with index %d ", n)
		}
	}
}

func BenchmarkIPAMDualDelSubnet(b *testing.B) {
	im := ipam.NewIPAM()
	for n := 0; n < b.N; n++ {
		if ok := addIPAMSubnet(im, n, kubeovnv1.ProtocolDual); !ok {
			klog.Fatalf("ERROR: add dual subnet with index %d ", n)
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
