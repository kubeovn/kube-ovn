package ipam

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ipam"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

func (t *ipamTest) inferSubnet() {
	t.f.SkipVersionPriorTo(1, 15, "Default subnet inference from a named IPPool was introduced in v1.15")

	poolCIDR := framework.RandomCIDR(t.f.ClusterIPFamily)
	ginkgo.By("Creating subnet " + t.subnetName2 + " without binding it to the namespace")
	poolSubnet := framework.MakeSubnet(t.subnetName2, "", poolCIDR, "", "", "", nil, nil, nil)
	poolSubnet = t.subnetClient.CreateSync(poolSubnet)

	poolIPs := framework.RandomIPPool(poolCIDR, 3)
	poolV4, poolV6 := util.SplitIpsByProtocol(poolIPs)
	v4Range, err := ipam.NewIPRangeListFrom(poolV4...)
	framework.ExpectNoError(err)
	v6Range, err := ipam.NewIPRangeListFrom(poolV6...)
	framework.ExpectNoError(err)
	ginkgo.By("Creating ippool " + t.ippoolName2 + " in subnet " + t.subnetName2)
	ippool := framework.MakeIPPool(t.ippoolName2, t.subnetName2, poolIPs, nil)
	_ = t.ippoolClient.CreateSync(ippool)

	ginkgo.By("Creating deployment " + t.deployName + " with only the ippool annotation")
	annotations := map[string]string{util.IPPoolAnnotation: t.ippoolName2}
	deploy := framework.MakeDeployment(t.deployName, 1, map[string]string{"app": t.deployName}, annotations, "pause", framework.PauseImage, "")
	deploy = t.deployClient.CreateSync(deploy)

	pods, err := t.deployClient.GetPods(deploy)
	framework.ExpectNoError(err)
	framework.ExpectHaveLen(pods.Items, 1)
	pod := pods.Items[0]
	framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
	framework.ExpectHaveKeyWithValue(pod.Annotations, util.LogicalSwitchAnnotation, poolSubnet.Name)
	framework.ExpectHaveKeyWithValue(pod.Annotations, util.CidrAnnotation, poolSubnet.Spec.CIDRBlock)
	framework.ExpectHaveKeyWithValue(pod.Annotations, util.GatewayAnnotation, poolSubnet.Spec.Gateway)
	for _, podIP := range util.PodIPs(pod) {
		ip, err := ipam.NewIP(podIP)
		framework.ExpectNoError(err)
		poolRange := v4Range
		if strings.ContainsRune(podIP, ':') {
			poolRange = v6Range
		}
		framework.ExpectTrue(poolRange.Contains(ip), "Pod IP %s should be contained by IPPool %v", podIP, poolIPs)
	}

	ginkgo.By("Waiting for ippool usage to reflect the pod allocation")
	framework.WaitUntil(time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
		pool := t.ippoolClient.Get(t.ippoolName2)
		v4Allocated := !t.f.HasIPv4() || !pool.Status.V4UsingIPs.EqualInt64(0)
		v6Allocated := !t.f.HasIPv6() || !pool.Status.V6UsingIPs.EqualInt64(0)
		return v4Allocated && v6Allocated, nil
	}, "")

	ginkgo.By("Deleting deployment " + t.deployName + " and waiting for ippool addresses to be released")
	t.deployClient.DeleteSync(t.deployName)
	framework.ExpectNoError(t.ipClient.WaitToDisappear(fmt.Sprintf("%s.%s", pod.Name, pod.Namespace), 0, 30*time.Second))
	framework.WaitUntil(time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
		pool := t.ippoolClient.Get(t.ippoolName2)
		return pool.Status.V4UsingIPs.EqualInt64(0) && pool.Status.V6UsingIPs.EqualInt64(0), nil
	}, "")
}

func (t *ipamTest) manageIPPool() {
	t.f.SkipVersionPriorTo(1, 12, "Support for IPPool feature was introduced in v1.12")

	ipsCount := 12
	ips := framework.RandomIPPool(t.cidr, ipsCount)
	ipv4, ipv6 := util.SplitIpsByProtocol(ips)
	if t.f.HasIPv4() {
		framework.ExpectHaveLen(ipv4, ipsCount)
	}
	if t.f.HasIPv6() {
		framework.ExpectHaveLen(ipv6, ipsCount)
	}

	ipv4Range, err := ipam.NewIPRangeListFrom(ipv4...)
	framework.ExpectNoError(err)
	ipv6Range, err := ipam.NewIPRangeListFrom(ipv6...)
	framework.ExpectNoError(err)

	excludeV4, excludeV6 := util.SplitIpsByProtocol(t.subnet.Spec.ExcludeIps)
	excludeV4Range, err := ipam.NewIPRangeListFrom(excludeV4...)
	framework.ExpectNoError(err)
	excludeV6Range, err := ipam.NewIPRangeListFrom(excludeV6...)
	framework.ExpectNoError(err)

	ipv4Range = ipv4Range.Separate(excludeV4Range)
	ipv6Range = ipv6Range.Separate(excludeV6Range)

	ginkgo.By(fmt.Sprintf("Creating ippool %s with ips %v", t.ippoolName, ips))
	ippool := framework.MakeIPPool(t.ippoolName, t.subnetName, ips, nil)
	ippool = t.ippoolClient.CreateSync(ippool)

	ginkgo.By("Validating ippool status")
	t.waitForPoolRanges(ipv4Range, ipv6Range, ipam.NewEmptyIPRangeList(), ipam.NewEmptyIPRangeList())

	ginkgo.By("Creating deployment " + t.deployName + " within ippool " + t.ippoolName)
	replicas := 3
	labels := map[string]string{"app": t.deployName}
	annotations := map[string]string{util.IPPoolAnnotation: t.ippoolName}
	deploy := framework.MakeDeployment(t.deployName, int32(replicas), labels, annotations, "pause", framework.PauseImage, "")
	deploy = t.deployClient.CreateSync(deploy)

	checkFn := func() {
		ginkgo.GinkgoHelper()

		ginkgo.By("Getting pods for deployment " + t.deployName)
		pods, err := t.deployClient.GetPods(deploy)
		framework.ExpectNoError(err, "failed to get pods for deployment "+t.deployName)
		framework.ExpectHaveLen(pods.Items, replicas)

		v4Using, v6Using := ipam.NewEmptyIPRangeList(), ipam.NewEmptyIPRangeList()
		for _, pod := range pods.Items {
			for _, podIP := range pod.Status.PodIPs {
				ip, err := ipam.NewIP(podIP.IP)
				framework.ExpectNoError(err)
				if strings.ContainsRune(podIP.IP, ':') {
					framework.ExpectTrue(ipv6Range.Contains(ip), "Pod IP %s should be contained by %v", ip.String(), ipv6Range.String())
					v6Using.Add(ip)
				} else {
					framework.ExpectTrue(ipv4Range.Contains(ip), "Pod IP %s should be contained by %v", ip.String(), ipv4Range.String())
					v4Using.Add(ip)
				}
			}
		}

		ginkgo.By("Validating ippool status")
		t.waitForPoolRanges(ipv4Range, ipv6Range, v4Using, v6Using)
	}
	checkFn()

	ginkgo.By("Restarting deployment " + t.deployName)
	deploy = t.deployClient.RestartSync(deploy)
	checkFn()

	ginkgo.By("Adding namespace " + t.namespaceName + " to ippool " + t.ippoolName)
	patchedIPPool := ippool.DeepCopy()
	patchedIPPool.Spec.Namespaces = []string{t.namespaceName}
	_ = t.ippoolClient.Patch(ippool, patchedIPPool, 10*time.Second)

	ginkgo.By("Validating namespace annotations")
	framework.WaitUntil(time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
		ns := t.nsClient.Get(t.namespaceName)
		return len(ns.Annotations) != 0 && ns.Annotations[util.IPPoolAnnotation] == t.ippoolName, nil
	}, "")

	ginkgo.By("Patching deployment " + t.deployName)
	deploy = t.deployClient.RestartSync(deploy)
	patchedDeploy := deploy.DeepCopy()
	patchedDeploy.Spec.Template.Annotations = nil
	deploy = t.deployClient.PatchSync(deploy, patchedDeploy)
	checkFn()
}

func (t *ipamTest) blockExhaustedPool() {
	t.f.SkipVersionPriorTo(1, 14, "This feature was introduced in v1.14")

	ginkgo.By("Creating IPPool " + t.ippoolName)
	ipsCount := 1
	ips := framework.RandomIPPool(t.cidr, ipsCount)
	ippool := framework.MakeIPPool(t.ippoolName, t.subnetName, ips, []string{t.namespaceName})
	_ = t.ippoolClient.CreateSync(ippool)

	ginkgo.By("Waiting for namespace " + t.namespaceName + " to have IPPool annotation")
	t.nsClient.WaitUntil(t.namespaceName, func(ns *corev1.Namespace) (bool, error) {
		return strings.Contains(ns.Annotations[util.IPPoolAnnotation], t.ippoolName), nil
	}, "IPPool annotation contains "+t.ippoolName, 2*time.Second, 30*time.Second)

	ginkgo.By("Creating deployment " + t.deployName + " with replicas equal to the number of IPs in the ippool")
	labels := map[string]string{"app": t.deployName}
	deploy := framework.MakeDeployment(t.deployName, int32(ipsCount), labels, nil, "pause", framework.PauseImage, "")
	_ = t.deployClient.CreateSync(deploy)

	ginkgo.By("Creating pod " + t.podName + " which should be blocked for IP allocation")
	pod := framework.MakePod(t.namespaceName, t.podName, nil, nil, "", nil, nil)
	_ = t.podClient.Create(pod)

	ginkgo.By("Waiting for pod " + t.podName + " to have event indicating IP allocation failure")
	eventClient := t.f.EventClient()
	_ = eventClient.WaitToHaveEvent(util.KindPod, t.podName, corev1.EventTypeWarning, "AcquireAddressFailed", "kube-ovn-controller", "")
}

func (t *ipamTest) excludePoolAddress() {
	t.f.SkipVersionPriorTo(1, 15, "Preserving named IPPool state during subnet reconciliation was introduced in v1.15")

	staticIP := framework.RandomIPs(t.cidr, ",", 1)
	poolIPs := strings.Split(staticIP, ",")
	ginkgo.By("Creating IPPool " + t.ippoolName + " with addresses " + staticIP)
	ippool := framework.MakeIPPool(t.ippoolName, t.subnetName, poolIPs, nil)
	_ = t.ippoolClient.CreateSync(ippool)

	ginkgo.By("Adding the IPPool addresses to subnet excludeIps")
	modifiedSubnet := t.subnet.DeepCopy()
	modifiedSubnet.Spec.ExcludeIps = append(slices.Clone(t.subnet.Spec.ExcludeIps), poolIPs...)
	t.subnet = t.subnetClient.PatchSync(t.subnet, modifiedSubnet)

	ginkgo.By("Waiting for the IPPool to become exhausted")
	_ = waitForIPPoolCounts(t.f, t.ippoolClient, t.ippoolName, 0, 0)

	ginkgo.By("Creating pod " + t.podName + " from the exhausted IPPool")
	annotations := map[string]string{util.IPPoolAnnotation: t.ippoolName}
	pod := framework.MakePod(t.namespaceName, t.podName, nil, annotations, "", nil, nil)
	_ = t.podClient.Create(pod)

	ginkgo.By("Waiting for pod " + t.podName + " to report IP allocation failure")
	_ = t.f.EventClient().WaitToHaveEvent(util.KindPod, t.podName, corev1.EventTypeWarning, "AcquireAddressFailed", "kube-ovn-controller", "")
	pod = t.podClient.GetPod(t.podName)
	framework.ExpectNotHaveKey(pod.Annotations, util.AllocatedAnnotation)

	ippool = waitForIPPoolCounts(t.f, t.ippoolClient, t.ippoolName, 0, 0)
	framework.ExpectEmpty(ippool.Status.V4AvailableIPRange)
	framework.ExpectEmpty(ippool.Status.V6AvailableIPRange)
}

func (t *ipamTest) releaseExcludedPoolAddress() {
	t.f.SkipVersionPriorTo(1, 15, "Preserving named IPPool state during subnet reconciliation was introduced in v1.15")

	staticIP := framework.RandomIPs(t.cidr, ",", 1)
	poolIPs := strings.Split(staticIP, ",")
	ginkgo.By("Creating IPPool " + t.ippoolName + " with addresses " + staticIP)
	ippool := framework.MakeIPPool(t.ippoolName, t.subnetName, poolIPs, nil)
	_ = t.ippoolClient.CreateSync(ippool)

	ginkgo.By("Creating pod " + t.podName + " from IPPool " + t.ippoolName)
	annotations := map[string]string{util.IPPoolAnnotation: t.ippoolName}
	pod := framework.MakePod(t.namespaceName, t.podName, nil, annotations, "", nil, nil)
	pod = t.podClient.CreateSync(pod)
	framework.ExpectHaveKeyWithValue(pod.Annotations, util.IPAddressAnnotation, staticIP)

	ginkgo.By("Adding the allocated addresses to subnet excludeIps")
	modifiedSubnet := t.subnet.DeepCopy()
	modifiedSubnet.Spec.ExcludeIps = append(slices.Clone(t.subnet.Spec.ExcludeIps), poolIPs...)
	t.subnet = t.subnetClient.PatchSync(t.subnet, modifiedSubnet)
	_ = waitForIPPoolCounts(t.f, t.ippoolClient, t.ippoolName, 0, 1)

	ginkgo.By("Deleting pod " + t.podName + " and waiting for its addresses to be released")
	t.podClient.DeleteSync(t.podName)
	framework.ExpectNoError(t.ipClient.WaitToDisappear(ovs.PodNameToPortName(t.podName, t.namespaceName, util.OvnProvider), 0, 30*time.Second))

	ippool = waitForIPPoolCounts(t.f, t.ippoolClient, t.ippoolName, 0, 0)
	framework.ExpectEmpty(ippool.Status.V4AvailableIPRange)
	framework.ExpectEmpty(ippool.Status.V6AvailableIPRange)
}

func (t *ipamTest) preservePoolOwnership() {
	t.f.SkipVersionPriorTo(1, 15, "Preserving named IPPool state during subnet reconciliation was introduced in v1.15")

	replicas := 5
	candidates := strings.Split(framework.RandomIPs(t.cidr, ";", replicas+1), ";")
	staticIPs := candidates[:replicas]
	poolIPs := make([]string, 0, replicas*2)
	for _, staticIP := range staticIPs {
		poolIPs = append(poolIPs, strings.Split(staticIP, ",")...)
	}

	ginkgo.By("Creating IPPool " + t.ippoolName + " with addresses " + strings.Join(staticIPs, ";"))
	ippool := framework.MakeIPPool(t.ippoolName, t.subnetName, poolIPs, nil)
	_ = t.ippoolClient.CreateSync(ippool)

	ginkgo.By("Updating subnet excludeIps with addresses outside the named IPPool")
	modifiedSubnet := t.subnet.DeepCopy()
	modifiedSubnet.Spec.ExcludeIps = append(slices.Clone(t.subnet.Spec.ExcludeIps), strings.Split(candidates[replicas], ",")...)
	t.subnet = t.subnetClient.PatchSync(t.subnet, modifiedSubnet)

	ginkgo.By("Creating deployment " + t.deployName + " with static addresses owned by IPPool " + t.ippoolName)
	labels := map[string]string{"app": t.deployName}
	annotations := map[string]string{util.IPPoolAnnotation: strings.Join(staticIPs, ";")}
	deploy := framework.MakeDeployment(t.deployName, int32(replicas), labels, annotations, "pause", framework.PauseImage, "")
	deploy = t.deployClient.CreateSync(deploy)

	pods, err := t.deployClient.GetPods(deploy)
	framework.ExpectNoError(err, "failed to get pods for deployment "+t.deployName)
	framework.ExpectHaveLen(pods.Items, replicas)
	allocatedIPs := make([]string, 0, replicas)
	for _, pod := range pods.Items {
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
		framework.ExpectContainElement(staticIPs, pod.Annotations[util.IPAddressAnnotation])
		allocatedIPs = append(allocatedIPs, pod.Annotations[util.IPAddressAnnotation])
	}
	framework.ExpectConsistOf(allocatedIPs, staticIPs)
	_ = waitForIPPoolCounts(t.f, t.ippoolClient, t.ippoolName, 0, int64(replicas))
}

func (t *ipamTest) deletePool() {
	t.f.SkipVersionPriorTo(1, 15, "Returning deleted IPPool addresses to the default pool was introduced in v1.15")

	staticIP := framework.RandomIPs(t.cidr, ",", 1)
	poolIPs := strings.Split(staticIP, ",")
	ginkgo.By("Creating IPPool " + t.ippoolName + " with addresses " + staticIP)
	ippool := framework.MakeIPPool(t.ippoolName, t.subnetName, poolIPs, nil)
	_ = t.ippoolClient.CreateSync(ippool)

	ginkgo.By("Deleting IPPool " + t.ippoolName)
	t.ippoolClient.DeleteSync(t.ippoolName)

	ginkgo.By("Creating pod " + t.podName + " with the former IPPool addresses from the default pool")
	annotations := map[string]string{
		util.IPAddressAnnotation:     staticIP,
		util.LogicalSwitchAnnotation: t.subnetName,
	}
	pod := framework.MakePod(t.namespaceName, t.podName, nil, annotations, "", nil, nil)
	pod = t.podClient.CreateSync(pod)
	framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
	framework.ExpectHaveKeyWithValue(pod.Annotations, util.IPAddressAnnotation, staticIP)
	framework.ExpectConsistOf(util.PodIPs(*pod), poolIPs)
}

func (t *ipamTest) allocateMultipleSubnets() {
	t.f.SkipVersionPriorTo(1, 14, "This feature was introduced in v1.14")
	ipsCount := 1

	ginkgo.By("Creating subnet " + t.subnetName2)
	cidr2 := framework.RandomCIDR(t.f.ClusterIPFamily)
	subnet2 := framework.MakeSubnet(t.subnetName2, "", cidr2, "", "", "", nil, nil, []string{t.namespaceName})
	_ = t.subnetClient.CreateSync(subnet2)

	ginkgo.By("Creating IPPool " + t.ippoolName)
	ips := framework.RandomIPPool(t.cidr, ipsCount)
	ippool := framework.MakeIPPool(t.ippoolName, t.subnetName, ips, []string{t.namespaceName})
	_ = t.ippoolClient.CreateSync(ippool)

	ginkgo.By("Creating IPPool " + t.ippoolName2)
	ips2 := framework.RandomIPPool(cidr2, ipsCount)
	ippool2 := framework.MakeIPPool(t.ippoolName2, t.subnetName2, ips2, []string{t.namespaceName})
	_ = t.ippoolClient.CreateSync(ippool2)

	ginkgo.By("Waiting for namespace " + t.namespaceName + " to have both IPPool annotations")
	t.nsClient.WaitUntil(t.namespaceName, func(ns *corev1.Namespace) (bool, error) {
		ann := ns.Annotations[util.IPPoolAnnotation]
		return strings.Contains(ann, t.ippoolName) && strings.Contains(ann, t.ippoolName2), nil
	}, "IPPool annotation contains both "+t.ippoolName+" and "+t.ippoolName2, 2*time.Second, 30*time.Second)

	ginkgo.By("Creating deployment " + t.deployName + " with replicas equal to the number of IPs in the ippool " + t.ippoolName)
	labels := map[string]string{"app": t.deployName}
	deploy := framework.MakeDeployment(t.deployName, int32(ipsCount), labels, nil, "pause", framework.PauseImage, "")
	_ = t.deployClient.CreateSync(deploy)

	ginkgo.By("Creating pod " + t.podName + " which should have IP allocated from ippool " + t.ippoolName2)
	pod := framework.MakePod(t.namespaceName, t.podName, nil, nil, "", nil, nil)
	_ = t.podClient.CreateSync(pod)
}

func (t *ipamTest) manageAddressSet() {
	t.f.SkipVersionPriorTo(1, 15, "This feature was introduced in v1.15")

	ginkgo.By("Creating ippool " + t.ippoolName + " with EnableAddressSet enabled")
	// Use only IPv4 or IPv6 addresses to avoid mixed IP family issue in OVN address set
	cidrV4, cidrV6 := util.SplitStringIP(t.cidr)
	var poolCIDR string
	if cidrV4 != "" {
		poolCIDR = cidrV4
	} else {
		poolCIDR = cidrV6
	}
	poolIPs := framework.RandomIPPool(poolCIDR, 4)
	framework.ExpectTrue(len(poolIPs) >= 2, "expected at least two IPs in pool")
	ippool := framework.MakeIPPool(t.ippoolName, t.subnetName, poolIPs, nil)
	ippool.Spec.EnableAddressSet = true
	_ = t.ippoolClient.CreateSync(ippool)

	ginkgo.By("Verifying address set contains pool IPs")
	framework.WaitForAddressSetIPs(t.ippoolName, poolIPs)

	ginkgo.By("Updating ippool to remove one IP entry")
	// Get the latest version to avoid resourceVersion conflict
	updated := t.ippoolClient.Get(t.ippoolName)
	updated.Spec.IPs = updated.Spec.IPs[:len(updated.Spec.IPs)-1]
	updated = t.ippoolClient.UpdateSync(updated, metav1.UpdateOptions{}, ippoolUpdateTimeout)

	ginkgo.By("Checking address set reflects IP removal")
	framework.WaitForAddressSetIPs(t.ippoolName, updated.Spec.IPs)

	ginkgo.By("Disabling EnableAddressSet to trigger address set deletion")
	// Get the latest version to avoid resourceVersion conflict
	updated = t.ippoolClient.Get(t.ippoolName)
	updated.Spec.EnableAddressSet = false
	_ = t.ippoolClient.UpdateSync(updated, metav1.UpdateOptions{}, ippoolUpdateTimeout)
	framework.WaitForAddressSetDeletion(t.ippoolName)
}

func (t *ipamTest) waitForPoolRanges(v4, v6, v4Using, v6Using *ipam.IPRangeList) {
	ginkgo.GinkgoHelper()
	v4Available, v6Available := v4.Separate(v4Using), v6.Separate(v6Using)
	t.ippoolClient.WaitUntil(t.ippoolName, func(pool *apiv1.IPPool) (bool, error) {
		status := pool.Status
		return status.V4UsingIPs.Equal(v4Using.Count()) &&
			status.V6UsingIPs.Equal(v6Using.Count()) &&
			status.V4UsingIPRange == v4Using.String() &&
			status.V6UsingIPRange == v6Using.String() &&
			status.V4AvailableIPs.Equal(v4Available.Count()) &&
			status.V6AvailableIPs.Equal(v6Available.Count()) &&
			status.V4AvailableIPRange == v4Available.String() &&
			status.V6AvailableIPRange == v6Available.String(), nil
	}, "pool statistics match workload addresses", time.Second, 30*time.Second)
}
