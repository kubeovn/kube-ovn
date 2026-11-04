package ipam

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	e2epod "k8s.io/kubernetes/test/e2e/framework/pod"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
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
