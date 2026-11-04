package ipam

import (
	"context"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ipam"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

func (t *ipamTest) resizeCountPool() {
	t.f.SkipVersionPriorTo(1, 17, "This feature was introduced in v1.17")

	ginkgo.By("Creating ippool " + t.ippoolName + " with ipCount 3")
	ippool := framework.MakeIPPool(t.ippoolName, t.subnetName, nil, nil)
	ippool.Spec.IPCount = 3
	ippool = t.ippoolClient.CreateSync(ippool)
	families := len(strings.Split(t.cidr, ","))
	framework.ExpectHaveLen(ippool.Status.IPs, families*3)
	framework.ExpectTrue(ippool.Status.IsReady(), "ippool should be ready")
	unique := make(map[string]struct{}, len(ippool.Status.IPs))
	for _, ip := range ippool.Status.IPs {
		_, duplicated := unique[ip]
		framework.ExpectFalse(duplicated, "ippool recorded %s twice", ip)
		unique[ip] = struct{}{}
		framework.ExpectTrue(util.CIDRContainIP(t.cidr, ip), "recorded address %s is outside the subnet", ip)
	}

	ginkgo.By("Growing the ippool to 4 IPs")
	updated := t.ippoolClient.Get(t.ippoolName)
	updated.Spec.IPCount = 4
	_ = t.ippoolClient.UpdateSync(updated, metav1.UpdateOptions{}, ippoolUpdateTimeout)
	grown := t.ippoolClient.WaitUntil(t.ippoolName, func(p *apiv1.IPPool) (bool, error) {
		return len(p.Status.IPs) == families*4, nil
	}, "status.ips grown", 2*time.Second, 30*time.Second)
	// growing keeps the recorded addresses untouched and appends new picks last
	grownV4, grownV6 := util.SplitIpsByProtocol(grown.Status.IPs)
	originalV4, originalV6 := util.SplitIpsByProtocol(ippool.Status.IPs)
	if len(originalV4) > 0 {
		framework.ExpectEqual(grownV4[:len(originalV4)], originalV4)
	}
	if len(originalV6) > 0 {
		framework.ExpectEqual(grownV6[:len(originalV6)], originalV6)
	}

	ginkgo.By("Shrinking the ippool back to 2 IPs releasing the most recently added ones")
	updated = t.ippoolClient.Get(t.ippoolName)
	updated.Spec.IPCount = 2
	_ = t.ippoolClient.UpdateSync(updated, metav1.UpdateOptions{}, ippoolUpdateTimeout)
	shrunk := t.ippoolClient.WaitUntil(t.ippoolName, func(p *apiv1.IPPool) (bool, error) {
		return len(p.Status.IPs) == families*2, nil
	}, "status.ips shrunk", 2*time.Second, 30*time.Second)
	// the most recently added addresses of each IP family are released first, so
	// what is left per family is a prefix of the grown selection; the controller
	// lists IPv4 addresses before IPv6 ones
	shrunkV4, shrunkV6 := util.SplitIpsByProtocol(shrunk.Status.IPs)
	if len(grownV4) > 0 {
		framework.ExpectEqual(shrunkV4, grownV4[:2])
	}
	if len(grownV6) > 0 {
		framework.ExpectEqual(shrunkV6, grownV6[:2])
	}

	ginkgo.By("Rejecting an ippool with both ips and ipCount")
	invalid := framework.MakeIPPool(t.ippoolName2, t.subnetName, []string{shrunk.Status.IPs[0]}, nil)
	invalid.Spec.IPCount = 1
	_, err := t.ippoolClient.IPPoolInterface.Create(context.Background(), invalid, metav1.CreateOptions{})
	framework.ExpectError(err, "ippool with both ips and ipCount should be rejected")

	ginkgo.By("Rejecting an ippool with neither ips nor ipCount")
	invalid = framework.MakeIPPool(t.ippoolName2, t.subnetName, nil, nil)
	_, err = t.ippoolClient.IPPoolInterface.Create(context.Background(), invalid, metav1.CreateOptions{})
	framework.ExpectError(err, "ippool with neither ips nor ipCount should be rejected")

	ginkgo.By("Rejecting an ippool asking for too many addresses")
	invalid = framework.MakeIPPool(t.ippoolName2, t.subnetName, nil, nil)
	invalid.Spec.IPCount = 33
	_, err = t.ippoolClient.IPPoolInterface.Create(context.Background(), invalid, metav1.CreateOptions{})
	framework.ExpectError(err, "ippool asking for more than 32 addresses per IP family should be rejected")

	ginkgo.By("Growing the ippool back to 3 IPs appending to the kept prefix")
	updated = t.ippoolClient.Get(t.ippoolName)
	updated.Spec.IPCount = 3
	_ = t.ippoolClient.UpdateSync(updated, metav1.UpdateOptions{}, ippoolUpdateTimeout)
	regrown := t.ippoolClient.WaitUntil(t.ippoolName, func(p *apiv1.IPPool) (bool, error) {
		return len(p.Status.IPs) == families*3, nil
	}, "status.ips regrown", 2*time.Second, 30*time.Second)
	// the addresses kept during shrinking stay untouched, new picks are appended
	regrownV4, regrownV6 := util.SplitIpsByProtocol(regrown.Status.IPs)
	if len(shrunkV4) > 0 {
		framework.ExpectEqual(regrownV4[:2], shrunkV4)
	}
	if len(shrunkV6) > 0 {
		framework.ExpectEqual(regrownV6[:2], shrunkV6)
	}

	ginkgo.By("Creating pod " + t.podName + " which should get an IP from the ippool")
	annotations := map[string]string{util.IPPoolAnnotation: t.ippoolName}
	pod := framework.MakePod(t.namespaceName, t.podName, nil, annotations, "", nil, nil)
	pod = t.podClient.CreateSync(pod)
	for ip := range strings.SplitSeq(pod.Annotations[util.IPAddressAnnotation], ",") {
		framework.ExpectContainElement(shrunk.Status.IPs, ip)
	}
}

func (t *ipamTest) switchCountPoolMode() {
	t.f.SkipVersionPriorTo(1, 17, "This feature was introduced in v1.17")
	families := len(strings.Split(t.cidr, ","))

	ginkgo.By("Creating ippool " + t.ippoolName + " with ipCount 2")
	ippool := framework.MakeIPPool(t.ippoolName, t.subnetName, nil, nil)
	ippool.Spec.IPCount = 2
	ippool = t.ippoolClient.CreateSync(ippool)
	framework.ExpectHaveLen(ippool.Status.IPs, families*2)
	// taking the first recorded address of each available IP family, releasing the rest
	var keep, former []string
	v4, v6 := util.SplitIpsByProtocol(ippool.Status.IPs)
	for _, familyIPs := range [][]string{v4, v6} {
		if len(familyIPs) > 0 {
			keep = append(keep, familyIPs[:1]...)
			former = append(former, familyIPs[1:2]...)
		}
	}

	ginkgo.By("Switching to spec.ips, keeping the first address of each family")
	updated := t.ippoolClient.Get(t.ippoolName)
	updated.Spec.IPCount = 0
	updated.Spec.IPs = keep
	_ = t.ippoolClient.UpdateSync(updated, metav1.UpdateOptions{}, ippoolUpdateTimeout)

	ginkgo.By("Checking the recorded addresses are cleared once the pool left count mode")
	cleared := t.ippoolClient.WaitUntil(t.ippoolName, func(p *apiv1.IPPool) (bool, error) {
		return len(p.Status.IPs) == 0, nil
	}, "status.ips cleared", 2*time.Second, 30*time.Second)
	framework.ExpectTrue(cleared.Status.IsReady(), "ippool should stay ready during the transition")

	ginkgo.By("Claiming one former address of each family with another ippool")
	other := framework.MakeIPPool(t.ippoolName2, t.subnetName, former, nil)
	_ = t.ippoolClient.CreateSync(other)

	ginkgo.By("Switching back to spec.ipCount")
	updated = t.ippoolClient.Get(t.ippoolName)
	updated.Spec.IPCount = 1
	updated.Spec.IPs = nil
	_ = t.ippoolClient.UpdateSync(updated, metav1.UpdateOptions{}, ippoolUpdateTimeout)
	stable := t.ippoolClient.WaitUntil(t.ippoolName, func(p *apiv1.IPPool) (bool, error) {
		return len(p.Status.IPs) == families && p.Status.IsReady(), nil
	}, "status.ips restored ready", 2*time.Second, 30*time.Second)
	// the pool keeps its own current addresses and must not resurrect the address
	// claimed by the other pool
	for _, ip := range keep {
		framework.ExpectContainElement(stable.Status.IPs, ip)
	}
	for _, ip := range former {
		framework.ExpectNotContainElement(stable.Status.IPs, ip)
	}
}

func (t *ipamTest) restartCountPool() {
	t.f.SkipVersionPriorTo(1, 17, "This feature was introduced in v1.17")
	families := len(strings.Split(t.cidr, ","))

	ginkgo.By("Creating ippool " + t.ippoolName + " with ipCount 2")
	ippool := framework.MakeIPPool(t.ippoolName, t.subnetName, nil, nil)
	ippool.Spec.IPCount = 2
	ippool = t.ippoolClient.CreateSync(ippool)
	framework.ExpectHaveLen(ippool.Status.IPs, families*2)
	recorded := slices.Clone(ippool.Status.IPs)

	ginkgo.By("Allocating a pool address before restarting the controller")
	annotations := map[string]string{util.IPPoolAnnotation: t.ippoolName}
	deploy := framework.MakeDeployment(t.deployName, 1, map[string]string{"app": t.deployName}, annotations, "pause", framework.PauseImage, "")
	deploy = t.deployClient.CreateSync(deploy)
	pods, err := t.deployClient.GetPods(deploy)
	framework.ExpectNoError(err)
	framework.ExpectHaveLen(pods.Items, 1)
	existingIP := pods.Items[0].Annotations[util.IPAddressAnnotation]
	_ = waitForIPPoolCounts(t.f, t.ippoolClient, t.ippoolName, 1, 1)

	ginkgo.By("Restarting the kube-ovn-controller deployment")
	deployClient := t.f.DeploymentClientNS("kube-system")
	_ = deployClient.RestartSync(deployClient.Get("kube-ovn-controller"))

	ginkgo.By("Checking the pool keeps its recorded addresses and stays ready")
	stable := t.ippoolClient.WaitUntil(t.ippoolName, func(p *apiv1.IPPool) (bool, error) {
		return p.Status.IsReady(), nil
	}, "ippool ready", 2*time.Second, 30*time.Second)
	framework.ExpectEqual(stable.Status.IPs, recorded)

	ginkgo.By("Creating pod " + t.podName + " which gets an address recorded for the ippool")
	pod := framework.MakePod(t.namespaceName, t.podName, nil, annotations, "", nil, nil)
	pod = t.podClient.CreateSync(pod)
	framework.ExpectNotEqual(existingIP, pod.Annotations[util.IPAddressAnnotation])
	for ip := range strings.SplitSeq(pod.Annotations[util.IPAddressAnnotation], ",") {
		framework.ExpectContainElement(recorded, ip)
	}
	_ = waitForIPPoolCounts(t.f, t.ippoolClient, t.ippoolName, 0, 2)
	pods, err = t.deployClient.GetPods(deploy)
	framework.ExpectNoError(err)
	framework.ExpectHaveLen(pods.Items, 1)
	framework.ExpectEqual(pods.Items[0].Annotations[util.IPAddressAnnotation], existingIP)
}

func (t *ipamTest) syncCountPoolAddressSet() {
	t.f.SkipVersionPriorTo(1, 17, "This feature was introduced in v1.17")
	if t.f.IsDual() {
		ginkgo.Skip("the address set backing an ippool does not support mixed IP families")
	}
	families := len(strings.Split(t.cidr, ","))

	ginkgo.By("Creating ippool " + t.ippoolName + " with ipCount 2 and EnableAddressSet")
	ippool := framework.MakeIPPool(t.ippoolName, t.subnetName, nil, nil)
	ippool.Spec.IPCount = 2
	ippool.Spec.EnableAddressSet = true
	ippool = t.ippoolClient.CreateSync(ippool)
	framework.ExpectHaveLen(ippool.Status.IPs, families*2)

	ginkgo.By("Checking the address set holds the picked addresses")
	framework.WaitForAddressSetIPs(t.ippoolName, ippool.Status.IPs)

	ginkgo.By("Growing the ippool to 3 IPs syncs the new addresses into the address set")
	updated := t.ippoolClient.Get(t.ippoolName)
	updated.Spec.IPCount = 3
	_ = t.ippoolClient.UpdateSync(updated, metav1.UpdateOptions{}, ippoolUpdateTimeout)
	grown := t.ippoolClient.WaitUntil(t.ippoolName, func(p *apiv1.IPPool) (bool, error) {
		return len(p.Status.IPs) == families*3, nil
	}, "status.ips grown", 2*time.Second, 30*time.Second)
	framework.WaitForAddressSetIPs(t.ippoolName, grown.Status.IPs)

	ginkgo.By("Shrinking the ippool back to 1 IP removes the released ones from the address set")
	updated = t.ippoolClient.Get(t.ippoolName)
	updated.Spec.IPCount = 1
	_ = t.ippoolClient.UpdateSync(updated, metav1.UpdateOptions{}, ippoolUpdateTimeout)
	shrunk := t.ippoolClient.WaitUntil(t.ippoolName, func(p *apiv1.IPPool) (bool, error) {
		return len(p.Status.IPs) == families, nil
	}, "status.ips shrunk", 2*time.Second, 30*time.Second)
	framework.WaitForAddressSetIPs(t.ippoolName, shrunk.Status.IPs)
}

func (t *ipamTest) isolateCountPools() {
	t.f.SkipVersionPriorTo(1, 17, "This feature was introduced in v1.17")
	families := len(strings.Split(t.cidr, ","))

	ginkgo.By("Creating two ippools with ipCount 2 each")
	ippool := framework.MakeIPPool(t.ippoolName, t.subnetName, nil, nil)
	ippool.Spec.IPCount = 2
	ippool = t.ippoolClient.CreateSync(ippool)
	ippool2 := framework.MakeIPPool(t.ippoolName2, t.subnetName, nil, nil)
	ippool2.Spec.IPCount = 2
	ippool2 = t.ippoolClient.CreateSync(ippool2)
	framework.ExpectHaveLen(ippool.Status.IPs, families*2)
	framework.ExpectHaveLen(ippool2.Status.IPs, families*2)
	ginkgo.By("Checking the pools do not share any address")
	for _, ip := range ippool2.Status.IPs {
		framework.ExpectNotContainElement(ippool.Status.IPs, ip)
	}

	ginkgo.By("Creating pod " + t.podName + " without the ippool annotation")
	annotations := map[string]string{util.LogicalSwitchAnnotation: t.subnetName}
	pod := framework.MakePod(t.namespaceName, t.podName, nil, annotations, "", nil, nil)
	pod = t.podClient.CreateSync(pod)
	for ip := range strings.SplitSeq(pod.Annotations[util.IPAddressAnnotation], ",") {
		framework.ExpectNotContainElement(ippool.Status.IPs, ip)
		framework.ExpectNotContainElement(ippool2.Status.IPs, ip)
	}

	ginkgo.By("Deleting the first ippool, its addresses return to the default pool")
	releasedV4, releasedV6 := util.SplitIpsByProtocol(ippool.Status.IPs)
	t.ippoolClient.DeleteSync(t.ippoolName)

	ginkgo.By("Checking the released addresses are available in the default pool again")
	allAvailable := func(rangeStr string, ips []string) (bool, error) {
		for _, ipStr := range ips {
			ip, err := ipam.NewIP(ipStr)
			if err != nil {
				return false, err
			}
			contained := false
			for part := range strings.SplitSeq(rangeStr, ",") {
				startStr, endStr, _ := strings.Cut(part, "-")
				if endStr == "" {
					endStr = startStr
				}
				start, err1 := ipam.NewIP(startStr)
				end, err2 := ipam.NewIP(endStr)
				if err1 != nil || err2 != nil {
					return false, fmt.Errorf("failed to parse range %q of available IPs", rangeStr)
				}
				if ipam.NewIPRange(start, end).Contains(ip) {
					contained = true
					break
				}
			}
			if !contained {
				return false, nil
			}
		}
		return true, nil
	}
	_ = t.subnetClient.WaitUntil(t.subnetName, func(s *apiv1.Subnet) (bool, error) {
		ok, err := allAvailable(s.Status.V4AvailableIPRange, releasedV4)
		if err != nil || !ok {
			return ok, err
		}
		if len(releasedV6) == 0 {
			return true, nil
		}
		return allAvailable(s.Status.V6AvailableIPRange, releasedV6)
	}, "released addresses available in the default pool again", 2*time.Second, 30*time.Second)
}

func (t *ipamTest) bindCountPoolNamespace() {
	t.f.SkipVersionPriorTo(1, 17, "This feature was introduced in v1.17")
	families := len(strings.Split(t.cidr, ","))

	ginkgo.By("Creating ippool " + t.ippoolName + " with ipCount 2 bound to namespace " + t.namespaceName)
	ippool := framework.MakeIPPool(t.ippoolName, t.subnetName, nil, []string{t.namespaceName})
	ippool.Spec.IPCount = 2
	ippool = t.ippoolClient.CreateSync(ippool)
	framework.ExpectHaveLen(ippool.Status.IPs, families*2)

	ginkgo.By("Checking the namespace refers to the ippool")
	framework.WaitUntil(time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
		ns := t.nsClient.Get(t.namespaceName)
		return len(ns.Annotations) != 0 && ns.Annotations[util.IPPoolAnnotation] == t.ippoolName, nil
	}, "")

	ginkgo.By("Creating pod " + t.podName + " without the ippool annotation")
	pod := framework.MakePod(t.namespaceName, t.podName, nil, nil, "", nil, nil)
	pod = t.podClient.CreateSync(pod)
	for ip := range strings.SplitSeq(pod.Annotations[util.IPAddressAnnotation], ",") {
		framework.ExpectContainElement(ippool.Status.IPs, ip)
	}

	ginkgo.By("Checking the pool reports the addresses as used")
	_ = t.ippoolClient.WaitUntil(t.ippoolName, func(p *apiv1.IPPool) (bool, error) {
		used := p.Status.V4UsingIPs.Int64() + p.Status.V6UsingIPs.Int64()
		return used == int64(families), nil
	}, "one address of each family used", 2*time.Second, 30*time.Second)
}

func (t *ipamTest) recycleCountPool() {
	t.f.SkipVersionPriorTo(1, 17, "This feature was introduced in v1.17")
	families := len(strings.Split(t.cidr, ","))

	ginkgo.By("Creating ippool " + t.ippoolName + " with ipCount 2")
	ippool := framework.MakeIPPool(t.ippoolName, t.subnetName, nil, nil)
	ippool.Spec.IPCount = 2
	ippool = t.ippoolClient.CreateSync(ippool)
	framework.ExpectHaveLen(ippool.Status.IPs, families*2)
	recorded := slices.Clone(ippool.Status.IPs)
	baseline := t.subnetClient.Get(t.subnetName)

	ginkgo.By("Creating pod " + t.podName + " using the pool and deleting it")
	annotations := map[string]string{util.IPPoolAnnotation: t.ippoolName}
	pod := framework.MakePod(t.namespaceName, t.podName, nil, annotations, "", nil, nil)
	pod = t.podClient.CreateSync(pod)
	for ip := range strings.SplitSeq(pod.Annotations[util.IPAddressAnnotation], ",") {
		framework.ExpectContainElement(recorded, ip)
	}
	t.podClient.DeleteGracefully(t.podName)
	t.podClient.WaitForNotFound(t.podName)
	ipName := ovs.PodNameToPortName(t.podName, t.namespaceName, util.OvnProvider)
	framework.ExpectNoError(t.ipClient.WaitToDisappear(ipName, time.Second, 60*time.Second))

	ginkgo.By("Checking the released addresses return to the pool, not to the default pool")
	// the pool's membership list does not change, and its free addresses recover
	_ = t.ippoolClient.WaitUntil(t.ippoolName, func(p *apiv1.IPPool) (bool, error) {
		return slices.Equal(p.Status.IPs, recorded) &&
			int64(families*2) == p.Status.V4AvailableIPs.Int64()+p.Status.V6AvailableIPs.Int64(), nil
	}, "pool addresses recovered", 2*time.Second, 60*time.Second)
	// IP CR deletion and subnet statistics are reconciled by separate workers.
	t.subnetClient.WaitUntil(t.subnetName, func(subnet *apiv1.Subnet) (bool, error) {
		return baseline.Status.V4AvailableIPs.Equal(subnet.Status.V4AvailableIPs) &&
			baseline.Status.V6AvailableIPs.Equal(subnet.Status.V6AvailableIPs), nil
	}, "subnet available counts recover after IP cleanup", time.Second, 60*time.Second)

	ginkgo.By("Recreating pod " + t.podName + " using the pool again")
	pod = framework.MakePod(t.namespaceName, t.podName, nil, annotations, "", nil, nil)
	pod = t.podClient.CreateSync(pod)
	for ip := range strings.SplitSeq(pod.Annotations[util.IPAddressAnnotation], ",") {
		framework.ExpectContainElement(recorded, ip)
	}
}

func (t *ipamTest) scaleCountPoolDeployment() {
	t.f.SkipVersionPriorTo(1, 17, "This feature was introduced in v1.17")
	families := len(strings.Split(t.cidr, ","))
	replicas := 2

	ginkgo.By("Creating ippool " + t.ippoolName + " with ipCount 2")
	ippool := framework.MakeIPPool(t.ippoolName, t.subnetName, nil, nil)
	ippool.Spec.IPCount = 2
	ippool = t.ippoolClient.CreateSync(ippool)
	framework.ExpectHaveLen(ippool.Status.IPs, families*2)
	recorded := slices.Clone(ippool.Status.IPs)

	ginkgo.By("Creating deployment " + t.deployName + " with " + strconv.Itoa(replicas) + " replicas using the pool")
	labels := map[string]string{"app": t.deployName}
	annotations := map[string]string{util.IPPoolAnnotation: t.ippoolName}
	deploy := framework.MakeDeployment(t.deployName, int32(replicas), labels, annotations, "pause", framework.PauseImage, "")
	deploy = t.deployClient.CreateSync(deploy)

	pods, err := t.deployClient.GetPods(deploy)
	framework.ExpectNoError(err, "failed to get pods for deployment "+t.deployName)
	framework.ExpectHaveLen(pods.Items, replicas)
	for _, pod := range pods.Items {
		for ip := range strings.SplitSeq(pod.Annotations[util.IPAddressAnnotation], ",") {
			framework.ExpectContainElement(recorded, ip)
		}
	}
	_ = waitForIPPoolCounts(t.f, t.ippoolClient, t.ippoolName, 0, int64(replicas))

	ginkgo.By("Scaling down to 1 replica releases one address per family back to the pool")
	t.deployClient.SetScale(t.deployName, 1)
	_ = waitForIPPoolCounts(t.f, t.ippoolClient, t.ippoolName, 1, 1)

	ginkgo.By("Scaling to zero returns all addresses to the pool without changing its membership")
	t.deployClient.SetScale(t.deployName, 0)
	_ = waitForIPPoolCounts(t.f, t.ippoolClient, t.ippoolName, int64(replicas), 0)
	framework.ExpectEqual(slices.Clone(t.ippoolClient.Get(t.ippoolName).Status.IPs), recorded)
}

func (t *ipamTest) shrinkCountPoolSubnet() {
	t.f.SkipVersionPriorTo(1, 17, "This feature was introduced in v1.17")
	if t.f.IsDual() {
		ginkgo.Skip("shrinking both families of a dual-stack subnet at once is not deterministic")
	}

	ginkgo.By("Creating subnet " + t.subnetName2 + " with the gateway excluded")
	cidr2 := framework.RandomCIDR(t.f.ClusterIPFamily)
	gateway, err := util.FirstIP(cidr2)
	framework.ExpectNoError(err)
	subnet2 := framework.MakeSubnet(t.subnetName2, "", cidr2, gateway, "", "", []string{gateway}, nil, []string{t.namespaceName})
	_ = t.subnetClient.CreateSync(subnet2)

	ginkgo.By("Creating ippool " + t.ippoolName + " with ipCount 2")
	ippool := framework.MakeIPPool(t.ippoolName, t.subnetName2, nil, nil)
	ippool.Spec.IPCount = 2
	ippool = t.ippoolClient.CreateSync(ippool)
	framework.ExpectHaveLen(ippool.Status.IPs, 2)
	recorded := slices.Clone(ippool.Status.IPs)

	ginkgo.By("Shrinking the subnet so the top address becomes the broadcast address")
	_, ipNet, err := net.ParseCIDR(cidr2)
	framework.ExpectNoError(err)
	_, bits := ipNet.Mask.Size()
	shrunkCIDR := fmt.Sprintf("%s/%d", ipNet.IP.String(), bits-2)
	updated := t.subnetClient.Get(t.subnetName2)
	updated.Spec.CIDRBlock = shrunkCIDR
	_ = t.subnetClient.UpdateSync(updated, metav1.UpdateOptions{}, 30*time.Second)

	ginkgo.By("Checking the pool reports the shortage without dropping the recorded addresses")
	short := t.ippoolClient.WaitUntil(t.ippoolName, func(p *apiv1.IPPool) (bool, error) {
		return !p.Status.IsReady() && slices.Equal(p.Status.IPs, recorded), nil
	}, "ippool not ready", 2*time.Second, 30*time.Second)
	framework.ExpectEqual(short.Status.IPs, recorded)

	ginkgo.By("Restoring the original CIDR recovers the pool")
	updated = t.subnetClient.Get(t.subnetName2)
	updated.Spec.CIDRBlock = cidr2
	_ = t.subnetClient.UpdateSync(updated, metav1.UpdateOptions{}, 30*time.Second)
	stable := t.ippoolClient.WaitUntil(t.ippoolName, func(p *apiv1.IPPool) (bool, error) {
		return p.Status.IsReady(), nil
	}, "ippool ready", 2*time.Second, 30*time.Second)
	// the recorded addresses are kept as long as they stay usable
	framework.ExpectEqual(stable.Status.IPs, recorded)
}
