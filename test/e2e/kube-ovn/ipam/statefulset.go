package ipam

import (
	"slices"
	"strconv"
	"strings"

	"github.com/onsi/ginkgo/v2"
	appsv1 "k8s.io/api/apps/v1"

	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

func (t *ipamTest) allocateStatefulSet() {
	replicas := 3
	labels := map[string]string{"app": t.stsName}

	ginkgo.By("Creating statefulset " + t.stsName)
	sts := framework.MakeStatefulSet(t.stsName, t.stsName, int32(replicas), labels, framework.PauseImage)
	sts = t.stsClient.CreateSync(sts)

	ginkgo.By("Getting pods for statefulset " + t.stsName)
	pods := t.stsClient.GetPods(sts)
	framework.ExpectHaveLen(pods.Items, replicas)

	ips := make([]string, 0, replicas)
	for _, pod := range pods.Items {
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.CidrAnnotation, t.subnet.Spec.CIDRBlock)
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.GatewayAnnotation, t.subnet.Spec.Gateway)
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.LogicalSwitchAnnotation, t.subnet.Name)
		framework.ExpectMAC(pod.Annotations[util.MacAddressAnnotation])
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")
		framework.ExpectConsistOf(util.PodIPs(pod), strings.Split(pod.Annotations[util.IPAddressAnnotation], ","))
		ips = append(ips, pod.Annotations[util.IPAddressAnnotation])
	}

	ginkgo.By("Deleting pods for statefulset " + t.stsName)
	for _, pod := range pods.Items {
		err := t.podClient.Delete(pod.Name)
		framework.ExpectNoError(err, "failed to delete pod "+pod.Name)
	}
	t.stsClient.WaitForRunningAndReady(sts)

	ginkgo.By("Getting pods for statefulset " + t.stsName)
	pods = t.stsClient.GetPods(sts)
	framework.ExpectHaveLen(pods.Items, replicas)

	for i, pod := range pods.Items {
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.CidrAnnotation, t.subnet.Spec.CIDRBlock)
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.GatewayAnnotation, t.subnet.Spec.Gateway)
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.IPAddressAnnotation, ips[i])
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.LogicalSwitchAnnotation, t.subnet.Name)
		framework.ExpectMAC(pod.Annotations[util.MacAddressAnnotation])
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")
	}
}

func (t *ipamTest) allocateStatefulSetPool() {
	ippoolSep := ";"
	if t.f.VersionPriorTo(1, 11) {
		if t.f.IsDual() {
			ginkgo.Skip("Support for dual stack ippool was introduced in v1.11")
		}
		ippoolSep = ","
	}

	for replicas := 3; replicas <= 3; replicas++ {
		t.stsName = "sts-" + framework.RandomSuffix()
		ippool := framework.RandomIPs(t.cidr, ippoolSep, replicas)
		labels := map[string]string{"app": t.stsName}

		ginkgo.By("Creating statefulset " + t.stsName + " with ippool " + ippool)
		sts := framework.MakeStatefulSet(t.stsName, t.stsName, int32(replicas), labels, framework.PauseImage)
		sts.Spec.Template.Annotations = map[string]string{util.IPPoolAnnotation: ippool}
		sts = t.stsClient.CreateSync(sts)

		ginkgo.By("Getting pods for statefulset " + t.stsName)
		pods := t.stsClient.GetPods(sts)
		framework.ExpectHaveLen(pods.Items, replicas)

		ips := make([]string, 0, replicas)
		for _, pod := range pods.Items {
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.CidrAnnotation, t.subnet.Spec.CIDRBlock)
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.GatewayAnnotation, t.subnet.Spec.Gateway)
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.IPPoolAnnotation, ippool)
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.LogicalSwitchAnnotation, t.subnet.Name)
			framework.ExpectMAC(pod.Annotations[util.MacAddressAnnotation])
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")
			framework.ExpectConsistOf(util.PodIPs(pod), strings.Split(pod.Annotations[util.IPAddressAnnotation], ","))
			ips = append(ips, pod.Annotations[util.IPAddressAnnotation])
		}
		framework.ExpectConsistOf(ips, strings.Split(ippool, ippoolSep))

		ginkgo.By("Deleting pods for statefulset " + t.stsName)
		for _, pod := range pods.Items {
			err := t.podClient.Delete(pod.Name)
			framework.ExpectNoError(err, "failed to delete pod "+pod.Name)
		}
		t.stsClient.WaitForRunningAndReady(sts)

		ginkgo.By("Getting pods for statefulset " + t.stsName)
		pods = t.stsClient.GetPods(sts)
		framework.ExpectHaveLen(pods.Items, replicas)

		for i, pod := range pods.Items {
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.CidrAnnotation, t.subnet.Spec.CIDRBlock)
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.GatewayAnnotation, t.subnet.Spec.Gateway)
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.IPPoolAnnotation, ippool)
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.IPAddressAnnotation, ips[i])
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.LogicalSwitchAnnotation, t.subnet.Name)
			framework.ExpectMAC(pod.Annotations[util.MacAddressAnnotation])
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")
			framework.ExpectConsistOf(util.PodIPs(pod), strings.Split(pod.Annotations[util.IPAddressAnnotation], ","))
		}

		ginkgo.By("Deleting statefulset " + t.stsName)
		t.stsClient.DeleteSync(t.stsName)
	}
}

func (t *ipamTest) allocateStatefulSetCommaPool() {
	if t.f.IsDual() {
		ginkgo.Skip("Comma separated ippool is not supported for dual stack")
	}

	ippoolSep := ","
	replicas := 3
	ippool := framework.RandomIPs(t.cidr, ippoolSep, replicas)
	labels := map[string]string{"app": t.stsName}

	ginkgo.By("Creating statefulset " + t.stsName + " with ippool " + ippool)
	sts := framework.MakeStatefulSet(t.stsName, t.stsName, int32(replicas), labels, framework.PauseImage)
	sts.Spec.Template.Annotations = map[string]string{util.IPPoolAnnotation: ippool}
	sts = t.stsClient.CreateSync(sts)

	ginkgo.By("Getting pods for statefulset " + t.stsName)
	pods := t.stsClient.GetPods(sts)
	framework.ExpectHaveLen(pods.Items, replicas)

	ips := make([]string, 0, replicas)
	for _, pod := range pods.Items {
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.CidrAnnotation, t.subnet.Spec.CIDRBlock)
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.GatewayAnnotation, t.subnet.Spec.Gateway)
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.IPPoolAnnotation, ippool)
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.LogicalSwitchAnnotation, t.subnet.Name)
		framework.ExpectMAC(pod.Annotations[util.MacAddressAnnotation])
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")
		framework.ExpectConsistOf(util.PodIPs(pod), strings.Split(pod.Annotations[util.IPAddressAnnotation], ","))
		ips = append(ips, pod.Annotations[util.IPAddressAnnotation])
	}
	framework.ExpectConsistOf(ips, strings.Split(ippool, ippoolSep))

	ginkgo.By("Deleting pods for statefulset " + t.stsName)
	for _, pod := range pods.Items {
		err := t.podClient.Delete(pod.Name)
		framework.ExpectNoError(err, "failed to delete pod "+pod.Name)
	}
	t.stsClient.WaitForRunningAndReady(sts)

	ginkgo.By("Getting pods for statefulset " + t.stsName)
	pods = t.stsClient.GetPods(sts)
	framework.ExpectHaveLen(pods.Items, replicas)

	for i, pod := range pods.Items {
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.CidrAnnotation, t.subnet.Spec.CIDRBlock)
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.GatewayAnnotation, t.subnet.Spec.Gateway)
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.IPPoolAnnotation, ippool)
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.IPAddressAnnotation, ips[i])
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.LogicalSwitchAnnotation, t.subnet.Name)
		framework.ExpectMAC(pod.Annotations[util.MacAddressAnnotation])
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")
		framework.ExpectConsistOf(util.PodIPs(pod), strings.Split(pod.Annotations[util.IPAddressAnnotation], ","))
	}
}

func (t *ipamTest) allocateStatefulSetOrdinal() {
	t.f.SkipVersionPriorTo(1, 11, "Support for start ordinal was introduced in v1.11")

	replicas, startOrdinal := int32(3), int32(10)
	labels := map[string]string{"app": t.stsName}

	ginkgo.By("Creating statefulset " + t.stsName + " with start ordinal " + strconv.Itoa(int(startOrdinal)))
	sts := framework.MakeStatefulSet(t.stsName, t.stsName, replicas, labels, framework.PauseImage)
	sts.Spec.Ordinals = &appsv1.StatefulSetOrdinals{Start: startOrdinal}
	sts = t.stsClient.CreateSync(sts)

	ginkgo.By("Getting pods for statefulset " + t.stsName)
	pods := t.stsClient.GetPods(sts)
	framework.ExpectHaveLen(pods.Items, int(replicas))

	ips := make([]string, 0, int(replicas))
	for _, pod := range pods.Items {
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
		framework.ExpectMAC(pod.Annotations[util.MacAddressAnnotation])
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")
		framework.ExpectConsistOf(util.PodIPs(pod), strings.Split(pod.Annotations[util.IPAddressAnnotation], ","))
		ips = append(ips, pod.Annotations[util.IPAddressAnnotation])
	}

	ginkgo.By("Deleting pods for statefulset " + t.stsName)
	for _, pod := range pods.Items {
		err := t.podClient.Delete(pod.Name)
		framework.ExpectNoError(err, "failed to delete pod "+pod.Name)
	}
	t.stsClient.WaitForRunningAndReady(sts)

	ginkgo.By("Getting pods for statefulset " + t.stsName)
	pods = t.stsClient.GetPods(sts)
	framework.ExpectHaveLen(pods.Items, int(replicas))

	for i, pod := range pods.Items {
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.IPAddressAnnotation, ips[i])
		framework.ExpectMAC(pod.Annotations[util.MacAddressAnnotation])
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")
	}
}

func (t *ipamTest) allocateMultipleStatefulSetPools() {
	t.f.SkipVersionPriorTo(1, 14, "Multiple IP Pools per namespace support was introduced in v1.14")
	replicas := 1
	ipsCount := 12

	ginkgo.By("Creating a new subnet " + t.subnetName2)
	testCidr := framework.RandomCIDR(t.f.ClusterIPFamily)
	testSubnet := framework.MakeSubnet(t.subnetName2, "", testCidr, "", "", "", nil, nil, []string{t.namespaceName})
	testSubnet = t.subnetClient.CreateSync(testSubnet)

	ginkgo.By("Creating IPPool resources")
	ipsRange1 := framework.RandomIPPool(t.cidr, ipsCount)
	ipsRange2 := framework.RandomIPPool(testCidr, ipsCount)
	ippool1 := framework.MakeIPPool(t.ippoolName, t.subnetName, ipsRange1, []string{t.namespaceName})
	ippool2 := framework.MakeIPPool(t.ippoolName2, t.subnetName2, ipsRange2, []string{t.namespaceName})
	t.ippoolClient.CreateSync(ippool1)
	t.ippoolClient.CreateSync(ippool2)

	ginkgo.By("Creating statefulset " + t.stsName + " with logical switch annotation and no ippool annotation")
	labels := map[string]string{"app": t.stsName}
	sts := framework.MakeStatefulSet(t.stsName, t.stsName, int32(replicas), labels, framework.PauseImage)
	sts.Spec.Template.Annotations = map[string]string{util.LogicalSwitchAnnotation: t.subnetName2}
	sts = t.stsClient.CreateSync(sts)

	ginkgo.By("Getting pods for statefulset " + t.stsName)
	pods := t.stsClient.GetPods(sts)
	framework.ExpectHaveLen(pods.Items, replicas)

	for _, pod := range pods.Items {
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.CidrAnnotation, testSubnet.Spec.CIDRBlock)
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.GatewayAnnotation, testSubnet.Spec.Gateway)
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.LogicalSwitchAnnotation, t.subnetName2)
		framework.ExpectIPInCIDR(pod.Annotations[util.IPAddressAnnotation], testSubnet.Spec.CIDRBlock)
		framework.ExpectMAC(pod.Annotations[util.MacAddressAnnotation])
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")
	}
}

func (t *ipamTest) allocateExhaustedStatefulSetPools() {
	t.f.SkipVersionPriorTo(1, 14, "Multiple IP Pools per namespace support was introduced in v1.14")
	replicas := 1
	ipsCount := 1

	ginkgo.By("Creating IPPool resources")
	ipsRange := framework.RandomIPPool(t.cidr, ipsCount*2)
	ipv4Range, ipv6Range := util.SplitIpsByProtocol(ipsRange)
	var ipsRange1, ipsRange2 []string
	if t.f.HasIPv4() {
		ipsRange1, ipsRange2 = slices.Clone(ipv4Range[:ipsCount]), slices.Clone(ipv4Range[ipsCount:])
	}
	if t.f.HasIPv6() {
		ipsRange1 = append(ipsRange1, ipv6Range[:ipsCount]...)
		ipsRange2 = append(ipsRange2, ipv6Range[ipsCount:]...)
	}
	ippool1 := framework.MakeIPPool(t.ippoolName, t.subnetName, ipsRange1, []string{t.namespaceName})
	ippool2 := framework.MakeIPPool(t.ippoolName2, t.subnetName, ipsRange2, []string{t.namespaceName})
	t.ippoolClient.CreateSync(ippool1)
	t.ippoolClient.CreateSync(ippool2)

	ginkgo.By("Creating first statefulset " + t.stsName + " with logical switch annotation and no ippool annotation")
	sts := framework.MakeStatefulSet(t.stsName, t.stsName, int32(replicas), map[string]string{"app": t.stsName}, framework.PauseImage)
	sts.Spec.Template.Annotations = map[string]string{util.LogicalSwitchAnnotation: t.subnetName}
	sts = t.stsClient.CreateSync(sts)

	ginkgo.By("Getting pods for the first statefulset " + t.stsName)
	pods := t.stsClient.GetPods(sts)
	framework.ExpectHaveLen(pods.Items, replicas)

	for _, pod := range pods.Items {
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.CidrAnnotation, t.subnet.Spec.CIDRBlock)
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.GatewayAnnotation, t.subnet.Spec.Gateway)
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.LogicalSwitchAnnotation, t.subnetName)
		framework.ExpectIPInCIDR(pod.Annotations[util.IPAddressAnnotation], t.subnet.Spec.CIDRBlock)
		framework.ExpectMAC(pod.Annotations[util.MacAddressAnnotation])
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")
		for _, ip := range util.PodIPs(pod) {
			framework.ExpectContainElement(append(ipsRange1, ipsRange2...), ip)
		}
	}

	ginkgo.By("Creating second statefulset " + t.stsName2 + " with logical switch annotation and no ippool annotation")
	sts2 := framework.MakeStatefulSet(t.stsName2, t.stsName2, int32(replicas), map[string]string{"app": t.stsName2}, framework.PauseImage)
	sts2.Spec.Template.Annotations = map[string]string{util.LogicalSwitchAnnotation: t.subnetName}
	sts2 = t.stsClient.CreateSync(sts2)

	ginkgo.By("Getting pods for the second statefulset " + t.stsName2)
	pods2 := t.stsClient.GetPods(sts2)
	framework.ExpectHaveLen(pods2.Items, replicas)

	for _, pod := range pods2.Items {
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.CidrAnnotation, t.subnet.Spec.CIDRBlock)
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.GatewayAnnotation, t.subnet.Spec.Gateway)
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.LogicalSwitchAnnotation, t.subnetName)
		framework.ExpectIPInCIDR(pod.Annotations[util.IPAddressAnnotation], t.subnet.Spec.CIDRBlock)
		framework.ExpectMAC(pod.Annotations[util.MacAddressAnnotation])
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")
		for _, ip := range util.PodIPs(pod) {
			framework.ExpectContainElement(append(ipsRange1, ipsRange2...), ip)
		}
	}
}
