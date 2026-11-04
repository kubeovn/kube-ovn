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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	e2epod "k8s.io/kubernetes/test/e2e/framework/pod"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ipam"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/iproute"
)

const ippoolUpdateTimeout = 2 * time.Minute

func expectPodIPFamily(pod *corev1.Pod, family string) {
	ginkgo.GinkgoHelper()

	ipv4, ipv6 := util.SplitStringIP(pod.Annotations[util.IPAddressAnnotation])
	switch family {
	case apiv1.ProtocolIPv4:
		framework.ExpectNotEmpty(ipv4)
		framework.ExpectEmpty(ipv6)
		framework.ExpectConsistOf(util.PodIPs(*pod), []string{ipv4})
	case apiv1.ProtocolIPv6:
		framework.ExpectEmpty(ipv4)
		framework.ExpectNotEmpty(ipv6)
		framework.ExpectConsistOf(util.PodIPs(*pod), []string{ipv6})
	case apiv1.ProtocolDual:
		framework.ExpectNotEmpty(ipv4)
		framework.ExpectNotEmpty(ipv6)
		framework.ExpectConsistOf(util.PodIPs(*pod), []string{ipv4, ipv6})
	default:
		framework.Failf("unexpected IP family %q", family)
	}
}

func expectDefaultIPCRFamily(f *framework.Framework, pod *corev1.Pod, subnetName, family string) {
	ginkgo.GinkgoHelper()

	ipName := ovs.PodNameToPortName(pod.Name, pod.Namespace, util.OvnProvider)
	ipCR := f.IPClient().Get(ipName)
	framework.ExpectEqual(ipCR.Spec.Subnet, subnetName)
	framework.ExpectEqual(ipCR.Spec.PodName, pod.Name)
	framework.ExpectEqual(ipCR.Spec.Namespace, pod.Namespace)
	framework.ExpectEqual(ipCR.Spec.NodeName, pod.Spec.NodeName)
	framework.ExpectEqual(ipCR.Spec.IPAddress, pod.Annotations[util.IPAddressAnnotation])
	ipv4, ipv6 := util.SplitStringIP(pod.Annotations[util.IPAddressAnnotation])
	framework.ExpectEqual(ipCR.Spec.V4IPAddress, ipv4)
	framework.ExpectEqual(ipCR.Spec.V6IPAddress, ipv6)

	switch family {
	case apiv1.ProtocolIPv4:
		framework.ExpectNotEmpty(ipCR.Spec.V4IPAddress)
		framework.ExpectEmpty(ipCR.Spec.V6IPAddress)
	case apiv1.ProtocolIPv6:
		framework.ExpectEmpty(ipCR.Spec.V4IPAddress)
		framework.ExpectNotEmpty(ipCR.Spec.V6IPAddress)
	case apiv1.ProtocolDual:
		framework.ExpectNotEmpty(ipCR.Spec.V4IPAddress)
		framework.ExpectNotEmpty(ipCR.Spec.V6IPAddress)
	}
}

func expectEth0IPs(pod *corev1.Pod, expectedIPs []string) {
	ginkgo.GinkgoHelper()

	links, err := iproute.AddressShow("eth0", func(cmd ...string) ([]byte, []byte, error) {
		return framework.KubectlExec(pod.Namespace, pod.Name, cmd...)
	})
	framework.ExpectNoError(err)
	framework.ExpectHaveLen(links, 1)
	framework.ExpectConsistOf(links[0].NonLinkLocalIPs(), expectedIPs)
}

func waitForIPPoolCounts(f *framework.Framework, client *framework.IPPoolClient, name string, available, using int64) *apiv1.IPPool {
	ginkgo.GinkgoHelper()

	return client.WaitUntil(name, func(pool *apiv1.IPPool) (bool, error) {
		v4Ready := !f.HasIPv4() || (pool.Status.V4AvailableIPs.EqualInt64(available) && pool.Status.V4UsingIPs.EqualInt64(using))
		v6Ready := !f.HasIPv6() || (pool.Status.V6AvailableIPs.EqualInt64(available) && pool.Status.V6UsingIPs.EqualInt64(using))
		return v4Ready && v6Ready, nil
	}, fmt.Sprintf("available=%d and using=%d for each supported IP family", available, using), time.Second, 30*time.Second)
}

var _ = framework.Describe("[group:ipam]", func() {
	t := &ipamTest{f: framework.NewDefaultFramework("ipam")}
	ginkgo.BeforeEach(t.setup)
	ginkgo.AfterEach(t.cleanup)
	framework.ConformanceIt("should allocate static ipv4 and mac for pod", t.allocateStaticPod)
	framework.ConformanceIt("should allocate requested IP family for pod on dual-stack subnet", t.allocatePodFamily)
	framework.ConformanceIt("should allocate requested IP family from named dual-stack IPPool", t.allocatePoolFamily)
	framework.ConformanceIt("should reject requested IP family that does not match single-stack subnet", t.rejectPodFamily)
	framework.ConformanceIt("should allocate static ip for pod with comma separated ippool", t.allocateCommaPool)
	framework.ConformanceIt("should allocate static ip for deployment with ippool", t.allocateDeployment)
	framework.ConformanceIt("should allocate static ip for statefulset", t.allocateStatefulSet)
	framework.ConformanceIt("should allocate static ip for statefulset with ippool", t.allocateStatefulSetPool)
	framework.ConformanceIt("should allocate static ip for statefulset with ippool separated by comma", t.allocateStatefulSetCommaPool)
	framework.ConformanceIt("should consider statefulset's start ordinal", t.allocateStatefulSetOrdinal)
	framework.ConformanceIt("should infer the default subnet from a named ippool", t.inferSubnet)
	framework.ConformanceIt("should support IPPool feature", t.manageIPPool)
	framework.ConformanceIt("should allocate right IPs for the statefulset when there are multiple IP Pools added to its namespace", t.allocateMultipleStatefulSetPools)
	framework.ConformanceIt("should allocate right IPs for the statefulset when there are multiple ippools added in the namespace and there are no available ips in the first ippool", t.allocateExhaustedStatefulSetPools)
	framework.ConformanceIt("should block IP allocation if the ippool bound by namespace annotation has no available IPs", t.blockExhaustedPool)
	framework.ConformanceIt("should not allocate newly excluded addresses from a named IPPool", t.excludePoolAddress)
	framework.ConformanceIt("should not make an excluded IPPool address available after release", t.releaseExcludedPoolAddress)
	framework.ConformanceIt("should preserve named IPPool ownership after subnet reconciliation", t.preservePoolOwnership)
	framework.ConformanceIt("should return deleted IPPool addresses to the default pool", t.deletePool)
	framework.ConformanceIt("should be able to allocate IP from IPPools in different subnets", t.allocateMultipleSubnets)
	framework.ConformanceIt("should pick IPs from the subnet when spec.ipCount is set", t.resizeCountPool)
	framework.ConformanceIt("should transition between spec.ips and spec.ipCount cleanly", t.switchCountPoolMode)
	framework.ConformanceIt("should keep ipCount addresses stable across controller restarts", t.restartCountPool)
	framework.ConformanceIt("should sync the address set of an ipCount ippool", t.syncCountPoolAddressSet)
	framework.ConformanceIt("should isolate ipCount pool addresses from other pools and the default pool", t.isolateCountPools)
	framework.ConformanceIt("should bind an ipCount ippool to the namespace", t.bindCountPoolNamespace)
	framework.ConformanceIt("should recycle released pod addresses within the ippool", t.recycleCountPool)
	framework.ConformanceIt("should allocate addresses for a deployment from an ipCount ippool", t.scaleCountPoolDeployment)
	framework.ConformanceIt("should reconcile an ipCount ippool when the subnet CIDR shrinks", t.shrinkCountPoolSubnet)
	framework.ConformanceIt("should manage address set when EnableAddressSet is true", t.manageAddressSet)
})

type ipamTest struct {
	f                                                                                                       *framework.Framework
	cs                                                                                                      clientset.Interface
	nsClient                                                                                                *framework.NamespaceClient
	podClient                                                                                               *framework.PodClient
	deployClient                                                                                            *framework.DeploymentClient
	stsClient                                                                                               *framework.StatefulSetClient
	subnetClient                                                                                            *framework.SubnetClient
	ippoolClient                                                                                            *framework.IPPoolClient
	ipClient                                                                                                *framework.IPClient
	namespaceName, subnetName, subnetName2, ippoolName, ippoolName2, podName, deployName, stsName, stsName2 string
	subnet                                                                                                  *apiv1.Subnet
	cidr                                                                                                    string
}

func (t *ipamTest) setup() {
	t.cs = t.f.ClientSet
	t.nsClient = t.f.NamespaceClient()
	t.podClient = t.f.PodClient()
	t.deployClient = t.f.DeploymentClient()
	t.stsClient = t.f.StatefulSetClient()
	t.subnetClient = t.f.SubnetClient()
	t.ippoolClient = t.f.IPPoolClient()
	t.ipClient = t.f.IPClient()
	t.namespaceName = t.f.Namespace.Name
	t.subnetName = "subnet-" + framework.RandomSuffix()
	t.subnetName2 = "subnet2-" + framework.RandomSuffix()
	t.ippoolName = "ippool-" + framework.RandomSuffix()
	t.ippoolName2 = "ippool2-" + framework.RandomSuffix()
	t.podName = "pod-" + framework.RandomSuffix()
	t.deployName = "deploy-" + framework.RandomSuffix()
	t.stsName = "sts-" + framework.RandomSuffix()
	t.stsName2 = "sts2-" + framework.RandomSuffix()

	t.cidr = framework.RandomCIDR(t.f.ClusterIPFamily)

	ginkgo.By("Creating subnet " + t.subnetName)
	t.subnet = framework.MakeSubnet(t.subnetName, "", t.cidr, "", "", "", nil, nil, []string{t.namespaceName})
	t.subnet = t.subnetClient.CreateSync(t.subnet)
}

func (t *ipamTest) cleanup() {
	// Level 1: Delete all workloads in parallel
	ginkgo.By("Deleting pod " + t.podName + ", deployment " + t.deployName + ", statefulsets " + t.stsName + " and " + t.stsName2)
	t.podClient.DeleteGracefully(t.podName)
	t.deployClient.Delete(t.deployName)
	t.stsClient.Delete(t.stsName)
	t.stsClient.Delete(t.stsName2)

	t.podClient.WaitForNotFound(t.podName)
	framework.ExpectNoError(t.deployClient.WaitToDisappear(t.deployName, 0, 2*time.Minute))
	framework.ExpectNoError(t.stsClient.WaitToDisappear(t.stsName, 0, 2*time.Minute))
	framework.ExpectNoError(t.stsClient.WaitToDisappear(t.stsName2, 0, 2*time.Minute))
	framework.WaitUntil(time.Second, 2*time.Minute, func(context.Context) (bool, error) {
		ips, err := t.ipClient.List(context.Background(), metav1.ListOptions{})
		if err != nil {
			return false, err
		}
		for _, ip := range ips.Items {
			if ip.Spec.Namespace == t.namespaceName {
				return false, nil
			}
		}
		return true, nil
	}, "all suite IP resources are released")

	// Level 2: Delete ippools in parallel (needs workloads gone)
	ginkgo.By("Deleting ippool " + t.ippoolName + " and " + t.ippoolName2)
	t.ippoolClient.Delete(t.ippoolName)
	t.ippoolClient.Delete(t.ippoolName2)
	framework.ExpectNoError(t.ippoolClient.WaitToDisappear(t.ippoolName, 0, 2*time.Minute))
	framework.ExpectNoError(t.ippoolClient.WaitToDisappear(t.ippoolName2, 0, 2*time.Minute))

	// Level 3: Delete subnets in parallel (needs ippools gone)
	ginkgo.By("Deleting subnet " + t.subnetName + " and " + t.subnetName2)
	t.subnetClient.Delete(t.subnetName)
	t.subnetClient.Delete(t.subnetName2)
	framework.ExpectNoError(t.subnetClient.WaitToDisappear(t.subnetName, 0, 2*time.Minute))
	framework.ExpectNoError(t.subnetClient.WaitToDisappear(t.subnetName2, 0, 2*time.Minute))
}

func (t *ipamTest) allocateStaticPod() {
	mac := util.GenerateMac()
	ip := framework.RandomIPs(t.cidr, ";", 1)

	ginkgo.By("Creating pod " + t.podName + " with ip " + ip + " and mac " + mac)
	annotations := map[string]string{
		util.IPAddressAnnotation:  ip,
		util.MacAddressAnnotation: mac,
	}
	pod := framework.MakePod(t.namespaceName, t.podName, nil, annotations, "", nil, nil)
	pod = t.podClient.CreateSync(pod)

	framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
	framework.ExpectHaveKeyWithValue(pod.Annotations, util.CidrAnnotation, t.subnet.Spec.CIDRBlock)
	framework.ExpectHaveKeyWithValue(pod.Annotations, util.GatewayAnnotation, t.subnet.Spec.Gateway)
	framework.ExpectHaveKeyWithValue(pod.Annotations, util.IPAddressAnnotation, ip)
	framework.ExpectHaveKeyWithValue(pod.Annotations, util.LogicalSwitchAnnotation, t.subnet.Name)
	framework.ExpectHaveKeyWithValue(pod.Annotations, util.MacAddressAnnotation, mac)
	framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")

	framework.ExpectConsistOf(util.PodIPs(*pod), strings.Split(ip, ","))
}

func (t *ipamTest) allocatePodFamily() {
	t.f.SkipVersionPriorTo(1, 17, "Per-pod IP family selection was introduced in v1.17")
	if !t.f.IsDual() {
		ginkgo.Skip("This test requires a dual-stack cluster")
	}

	cmd := []string{"sleep", "infinity"}
	cases := []struct {
		name        string
		annotations map[string]string
		family      string
	}{
		{
			name:   "default dual-stack allocation",
			family: apiv1.ProtocolDual,
		},
		{
			name:        "IPv4-only allocation",
			annotations: map[string]string{util.IPFamilyAnnotation: strings.ToLower(apiv1.ProtocolIPv4)},
			family:      apiv1.ProtocolIPv4,
		},
		{
			name:        "IPv6-only allocation",
			annotations: map[string]string{util.IPFamilyAnnotation: strings.ToLower(apiv1.ProtocolIPv6)},
			family:      apiv1.ProtocolIPv6,
		},
	}

	for _, tt := range cases {
		ginkgo.By("Creating pod " + t.podName + " with " + tt.name)
		pod := framework.MakePrivilegedPod(t.namespaceName, t.podName, nil, tt.annotations, t.f.KubeOVNImage, cmd, nil)
		pod = t.podClient.CreateSync(pod)

		ginkgo.By("Validating pod annotations")
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.CidrAnnotation, t.subnet.Spec.CIDRBlock)
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.GatewayAnnotation, t.subnet.Spec.Gateway)
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.LogicalSwitchAnnotation, t.subnet.Name)
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")
		framework.ExpectMAC(pod.Annotations[util.MacAddressAnnotation])
		expectPodIPFamily(pod, tt.family)
		expectDefaultIPCRFamily(t.f, pod, t.subnetName, tt.family)
		expectEth0IPs(pod, util.PodIPs(*pod))

		ginkgo.By("Deleting pod " + t.podName)
		t.podClient.DeleteSync(t.podName)
		t.f.IPClient().DeleteSync(ovs.PodNameToPortName(t.podName, t.namespaceName, util.OvnProvider))
	}
}

func (t *ipamTest) allocatePoolFamily() {
	t.f.SkipVersionPriorTo(1, 17, "Per-pod IP family selection was introduced in v1.17")
	if !t.f.IsDual() {
		ginkgo.Skip("This test requires a dual-stack cluster")
	}

	ginkgo.By("Creating IPPool " + t.ippoolName)
	poolIPs := strings.Split(framework.RandomIPs(t.cidr, ",", 2), ",")
	ippool := framework.MakeIPPool(t.ippoolName, t.subnetName, poolIPs, []string{t.namespaceName})
	ippool = t.ippoolClient.CreateSync(ippool)
	framework.ExpectNotEmpty(ippool.Status.V4AvailableIPRange)
	framework.ExpectNotEmpty(ippool.Status.V6AvailableIPRange)

	cmd := []string{"sleep", "infinity"}
	for _, family := range []string{apiv1.ProtocolIPv6, apiv1.ProtocolIPv4} {
		ginkgo.By("Creating pod " + t.podName + " with " + family + " IP family from IPPool " + t.ippoolName)
		annotations := map[string]string{
			util.IPFamilyAnnotation: strings.ToLower(family),
			util.IPPoolAnnotation:   t.ippoolName,
		}
		pod := framework.MakePrivilegedPod(t.namespaceName, t.podName, nil, annotations, t.f.KubeOVNImage, cmd, nil)
		pod = t.podClient.CreateSync(pod)

		ginkgo.By("Validating pod allocation")
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.IPPoolAnnotation, t.ippoolName)
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.CidrAnnotation, t.subnet.Spec.CIDRBlock)
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.LogicalSwitchAnnotation, t.subnet.Name)
		expectPodIPFamily(pod, family)
		expectDefaultIPCRFamily(t.f, pod, t.subnetName, family)
		framework.ExpectContainElement(poolIPs, pod.Annotations[util.IPAddressAnnotation])
		expectEth0IPs(pod, util.PodIPs(*pod))

		ginkgo.By("Deleting pod " + t.podName)
		t.podClient.DeleteSync(t.podName)
		t.f.IPClient().DeleteSync(ovs.PodNameToPortName(t.podName, t.namespaceName, util.OvnProvider))
	}
}

func (t *ipamTest) rejectPodFamily() {
	t.f.SkipVersionPriorTo(1, 17, "Per-pod IP family selection was introduced in v1.17")
	if t.f.IsDual() {
		ginkgo.Skip("This test requires a single-stack cluster")
	}

	requestedFamily := apiv1.ProtocolIPv4
	if t.f.HasIPv4() {
		requestedFamily = apiv1.ProtocolIPv6
	}

	ginkgo.By("Creating pod " + t.podName + " with mismatched " + requestedFamily + " IP family on " + t.subnet.Spec.Protocol + " subnet")
	annotations := map[string]string{util.IPFamilyAnnotation: strings.ToLower(requestedFamily)}
	pod := framework.MakePod(t.namespaceName, t.podName, nil, annotations, "", nil, nil)
	_ = t.podClient.Create(pod)

	ginkgo.By("Waiting for pod " + t.podName + " to have event indicating IP family mismatch")
	events := t.f.EventClient().WaitToHaveEvent(util.KindPod, t.podName, corev1.EventTypeWarning, "AcquireAddressFailed", "kube-ovn-controller", "")
	framework.ExpectContainSubstring(events[0].Message, fmt.Sprintf("requested ip family %s does not match subnet %s protocol %s", requestedFamily, t.subnetName, t.subnet.Spec.Protocol))

	ginkgo.By("Validating pod " + t.podName + " is not allocated")
	pod = t.podClient.GetPod(t.podName)
	framework.ExpectNotHaveKey(pod.Annotations, util.AllocatedAnnotation)
}

func (t *ipamTest) allocateCommaPool() {
	if t.f.IsDual() {
		ginkgo.Skip("Comma separated ippool is not supported for dual stack")
	}

	pool := framework.RandomIPs(t.cidr, ",", 3)
	ginkgo.By("Creating pod " + t.podName + " with ippool " + pool)
	annotations := map[string]string{util.IPPoolAnnotation: pool}
	pod := framework.MakePod(t.namespaceName, t.podName, nil, annotations, "", nil, nil)
	pod = t.podClient.CreateSync(pod)

	framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
	framework.ExpectHaveKeyWithValue(pod.Annotations, util.CidrAnnotation, t.subnet.Spec.CIDRBlock)
	framework.ExpectHaveKeyWithValue(pod.Annotations, util.GatewayAnnotation, t.subnet.Spec.Gateway)
	framework.ExpectHaveKeyWithValue(pod.Annotations, util.IPPoolAnnotation, pool)
	framework.ExpectEqual(pod.Annotations[util.IPAddressAnnotation], pod.Status.PodIP)
	framework.ExpectHaveKeyWithValue(pod.Annotations, util.LogicalSwitchAnnotation, t.subnet.Name)
	framework.ExpectMAC(pod.Annotations[util.MacAddressAnnotation])
	framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")
	framework.ExpectContainElement(strings.Split(pool, ","), pod.Status.PodIP)
}

func (t *ipamTest) allocateDeployment() {
	ippoolSep := ";"
	if t.f.VersionPriorTo(1, 11) {
		if t.f.IsDual() {
			ginkgo.Skip("Support for dual stack ippool was introduced in v1.11")
		}
		ippoolSep = ","
	}

	replicas := 3
	ippool := framework.RandomIPs(t.cidr, ippoolSep, replicas)

	ginkgo.By("Creating deployment " + t.deployName + " with ippool " + ippool)
	labels := map[string]string{"app": t.deployName}
	annotations := map[string]string{util.IPPoolAnnotation: ippool}
	deploy := framework.MakeDeployment(t.deployName, int32(replicas), labels, annotations, "pause", framework.PauseImage, "")
	deploy = t.deployClient.CreateSync(deploy)

	ginkgo.By("Getting pods for deployment " + t.deployName)
	pods, err := t.deployClient.GetPods(deploy)
	framework.ExpectNoError(err, "failed to get pods for deployment "+t.deployName)
	framework.ExpectHaveLen(pods.Items, replicas)

	ips := strings.Split(ippool, ippoolSep)
	for _, pod := range pods.Items {
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.CidrAnnotation, t.subnet.Spec.CIDRBlock)
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.GatewayAnnotation, t.subnet.Spec.Gateway)
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.IPPoolAnnotation, ippool)
		framework.ExpectContainElement(ips, pod.Annotations[util.IPAddressAnnotation])
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.LogicalSwitchAnnotation, t.subnet.Name)
		framework.ExpectMAC(pod.Annotations[util.MacAddressAnnotation])
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")

		framework.ExpectConsistOf(util.PodIPs(pod), strings.Split(pod.Annotations[util.IPAddressAnnotation], ","))
	}

	ginkgo.By("Deleting pods for deployment " + t.deployName)
	for _, pod := range pods.Items {
		t.podClient.DeleteGracefully(pod.Name)
	}
	for _, pod := range pods.Items {
		t.podClient.WaitForNotFound(pod.Name)
	}

	ginkgo.By("Waiting for new pods to be ready")
	err = t.deployClient.WaitToComplete(deploy)
	framework.ExpectNoError(err)
	err = e2epod.WaitForPodsRunningReady(context.Background(), t.cs, t.namespaceName, int(*deploy.Spec.Replicas), time.Minute)
	framework.ExpectNoError(err, "timed out waiting for pods to be ready")

	ginkgo.By("Getting pods for deployment " + t.deployName + " after deletion")
	pods, err = t.deployClient.GetPods(deploy)
	framework.ExpectNoError(err, "failed to get pods for deployment "+t.deployName)
	framework.ExpectHaveLen(pods.Items, replicas)
	for _, pod := range pods.Items {
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.CidrAnnotation, t.subnet.Spec.CIDRBlock)
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.GatewayAnnotation, t.subnet.Spec.Gateway)
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.IPPoolAnnotation, ippool)
		framework.ExpectContainElement(ips, pod.Annotations[util.IPAddressAnnotation])
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.LogicalSwitchAnnotation, t.subnet.Name)
		framework.ExpectMAC(pod.Annotations[util.MacAddressAnnotation])
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")
		framework.ExpectConsistOf(util.PodIPs(pod), strings.Split(pod.Annotations[util.IPAddressAnnotation], ","))
	}
}

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

	ginkgo.By("Restarting the kube-ovn-controller deployment")
	deployClient := t.f.DeploymentClientNS("kube-system")
	_ = deployClient.RestartSync(deployClient.Get("kube-ovn-controller"))

	ginkgo.By("Checking the pool keeps its recorded addresses and stays ready")
	stable := t.ippoolClient.WaitUntil(t.ippoolName, func(p *apiv1.IPPool) (bool, error) {
		return p.Status.IsReady(), nil
	}, "ippool ready", 2*time.Second, 30*time.Second)
	framework.ExpectEqual(stable.Status.IPs, recorded)

	ginkgo.By("Creating pod " + t.podName + " which gets an address recorded for the ippool")
	annotations := map[string]string{util.IPPoolAnnotation: t.ippoolName}
	pod := framework.MakePod(t.namespaceName, t.podName, nil, annotations, "", nil, nil)
	pod = t.podClient.CreateSync(pod)
	for ip := range strings.SplitSeq(pod.Annotations[util.IPAddressAnnotation], ",") {
		framework.ExpectContainElement(recorded, ip)
	}
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
