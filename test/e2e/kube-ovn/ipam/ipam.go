package ipam

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	e2epod "k8s.io/kubernetes/test/e2e/framework/pod"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ipam"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

const ippoolUpdateTimeout = 2 * time.Minute

var _ = framework.Describe("[group:ipam]", func() {
	f := framework.NewDefaultFramework("ipam")

	var cs clientset.Interface
	var nsClient *framework.NamespaceClient
	var podClient *framework.PodClient
	var deployClient *framework.DeploymentClient
	var stsClient *framework.StatefulSetClient
	var subnetClient *framework.SubnetClient
	var ippoolClient *framework.IPPoolClient
	var namespaceName, subnetName, subnetName2, ippoolName, ippoolName2, podName, deployName, stsName, stsName2 string
	var subnet *apiv1.Subnet
	var cidr string

	ginkgo.BeforeEach(func() {
		cs = f.ClientSet
		nsClient = f.NamespaceClient()
		podClient = f.PodClient()
		deployClient = f.DeploymentClient()
		stsClient = f.StatefulSetClient()
		subnetClient = f.SubnetClient()
		ippoolClient = f.IPPoolClient()
		namespaceName = f.Namespace.Name
		subnetName = "subnet-" + framework.RandomSuffix()
		subnetName2 = "subnet2-" + framework.RandomSuffix()
		ippoolName = "ippool-" + framework.RandomSuffix()
		ippoolName2 = "ippool2-" + framework.RandomSuffix()
		podName = "pod-" + framework.RandomSuffix()
		deployName = "deploy-" + framework.RandomSuffix()
		stsName = "sts-" + framework.RandomSuffix()
		stsName2 = "sts2-" + framework.RandomSuffix()

		cidr = framework.RandomCIDR(f.ClusterIPFamily)

		ginkgo.By("Creating subnet " + subnetName)
		subnet = framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, []string{namespaceName})
		subnet = subnetClient.CreateSync(subnet)
	})
	ginkgo.AfterEach(func() {
		ginkgo.By("Deleting pod " + podName)
		podClient.DeleteSync(podName)

		ginkgo.By("Deleting deployment " + deployName)
		deployClient.DeleteSync(deployName)

		ginkgo.By("Deleting statefulset " + stsName + " and " + stsName2)
		stsClient.DeleteSync(stsName)
		stsClient.DeleteSync(stsName2)

		ginkgo.By("Deleting ippool " + ippoolName + " and " + ippoolName2)
		ippoolClient.DeleteSync(ippoolName)
		ippoolClient.DeleteSync(ippoolName2)

		ginkgo.By("Deleting subnet " + subnetName + " and " + subnetName2)
		subnetClient.DeleteSync(subnetName)
		subnetClient.DeleteSync(subnetName2)
	})

	framework.ConformanceIt("should allocate static ipv4 and mac for pod", func() {
		mac := util.GenerateMac()
		ip := framework.RandomIPs(cidr, ";", 1)

		ginkgo.By("Creating pod " + podName + " with ip " + ip + " and mac " + mac)
		annotations := map[string]string{
			util.IPAddressAnnotation:  ip,
			util.MacAddressAnnotation: mac,
		}
		pod := framework.MakePod(namespaceName, podName, nil, annotations, "", nil, nil)
		pod = podClient.CreateSync(pod)

		framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.CidrAnnotation, subnet.Spec.CIDRBlock)
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.GatewayAnnotation, subnet.Spec.Gateway)
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.IPAddressAnnotation, ip)
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.LogicalSwitchAnnotation, subnet.Name)
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.MacAddressAnnotation, mac)
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")

		framework.ExpectConsistOf(util.PodIPs(*pod), strings.Split(ip, ","))
	})

	framework.ConformanceIt("should allocate static ip for pod with comma separated ippool", func() {
		if f.IsDual() {
			ginkgo.Skip("Comma separated ippool is not supported for dual stack")
		}

		pool := framework.RandomIPs(cidr, ",", 3)
		ginkgo.By("Creating pod " + podName + " with ippool " + pool)
		annotations := map[string]string{util.IPPoolAnnotation: pool}
		pod := framework.MakePod(namespaceName, podName, nil, annotations, "", nil, nil)
		pod = podClient.CreateSync(pod)

		framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.CidrAnnotation, subnet.Spec.CIDRBlock)
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.GatewayAnnotation, subnet.Spec.Gateway)
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.IPPoolAnnotation, pool)
		framework.ExpectEqual(pod.Annotations[util.IPAddressAnnotation], pod.Status.PodIP)
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.LogicalSwitchAnnotation, subnet.Name)
		framework.ExpectMAC(pod.Annotations[util.MacAddressAnnotation])
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")
		framework.ExpectContainElement(strings.Split(pool, ","), pod.Status.PodIP)
	})

	framework.ConformanceIt("should allocate static ip for deployment with ippool", func() {
		ippoolSep := ";"
		if f.VersionPriorTo(1, 11) {
			if f.IsDual() {
				ginkgo.Skip("Support for dual stack ippool was introduced in v1.11")
			}
			ippoolSep = ","
		}

		replicas := 3
		ippool := framework.RandomIPs(cidr, ippoolSep, replicas)

		ginkgo.By("Creating deployment " + deployName + " with ippool " + ippool)
		labels := map[string]string{"app": deployName}
		annotations := map[string]string{util.IPPoolAnnotation: ippool}
		deploy := framework.MakeDeployment(deployName, int32(replicas), labels, annotations, "pause", framework.PauseImage, "")
		deploy = deployClient.CreateSync(deploy)

		ginkgo.By("Getting pods for deployment " + deployName)
		pods, err := deployClient.GetPods(deploy)
		framework.ExpectNoError(err, "failed to get pods for deployment "+deployName)
		framework.ExpectHaveLen(pods.Items, replicas)

		ips := strings.Split(ippool, ippoolSep)
		for _, pod := range pods.Items {
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.CidrAnnotation, subnet.Spec.CIDRBlock)
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.GatewayAnnotation, subnet.Spec.Gateway)
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.IPPoolAnnotation, ippool)
			framework.ExpectContainElement(ips, pod.Annotations[util.IPAddressAnnotation])
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.LogicalSwitchAnnotation, subnet.Name)
			framework.ExpectMAC(pod.Annotations[util.MacAddressAnnotation])
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")

			framework.ExpectConsistOf(util.PodIPs(pod), strings.Split(pod.Annotations[util.IPAddressAnnotation], ","))
		}

		ginkgo.By("Deleting pods for deployment " + deployName)
		for _, pod := range pods.Items {
			err = podClient.Delete(pod.Name)
			framework.ExpectNoError(err, "failed to delete pod "+pod.Name)
		}
		err = deployClient.WaitToComplete(deploy)
		framework.ExpectNoError(err)

		ginkgo.By("Waiting for new pods to be ready")
		err = e2epod.WaitForPodsRunningReady(context.Background(), cs, namespaceName, int(*deploy.Spec.Replicas), time.Minute)
		framework.ExpectNoError(err, "timed out waiting for pods to be ready")

		ginkgo.By("Getting pods for deployment " + deployName + " after deletion")
		pods, err = deployClient.GetPods(deploy)
		framework.ExpectNoError(err, "failed to get pods for deployment "+deployName)
		framework.ExpectHaveLen(pods.Items, replicas)
		for _, pod := range pods.Items {
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.CidrAnnotation, subnet.Spec.CIDRBlock)
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.GatewayAnnotation, subnet.Spec.Gateway)
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.IPPoolAnnotation, ippool)
			framework.ExpectContainElement(ips, pod.Annotations[util.IPAddressAnnotation])
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.LogicalSwitchAnnotation, subnet.Name)
			framework.ExpectMAC(pod.Annotations[util.MacAddressAnnotation])
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")
			framework.ExpectConsistOf(util.PodIPs(pod), strings.Split(pod.Annotations[util.IPAddressAnnotation], ","))
		}
	})

	framework.ConformanceIt("should allocate static ip for statefulset", func() {
		replicas := 3
		labels := map[string]string{"app": stsName}

		ginkgo.By("Creating statefulset " + stsName)
		sts := framework.MakeStatefulSet(stsName, stsName, int32(replicas), labels, framework.PauseImage)
		sts = stsClient.CreateSync(sts)

		ginkgo.By("Getting pods for statefulset " + stsName)
		pods := stsClient.GetPods(sts)
		framework.ExpectHaveLen(pods.Items, replicas)

		ips := make([]string, 0, replicas)
		for _, pod := range pods.Items {
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.CidrAnnotation, subnet.Spec.CIDRBlock)
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.GatewayAnnotation, subnet.Spec.Gateway)
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.LogicalSwitchAnnotation, subnet.Name)
			framework.ExpectMAC(pod.Annotations[util.MacAddressAnnotation])
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")
			framework.ExpectConsistOf(util.PodIPs(pod), strings.Split(pod.Annotations[util.IPAddressAnnotation], ","))
			ips = append(ips, pod.Annotations[util.IPAddressAnnotation])
		}

		ginkgo.By("Deleting pods for statefulset " + stsName)
		for _, pod := range pods.Items {
			err := podClient.Delete(pod.Name)
			framework.ExpectNoError(err, "failed to delete pod "+pod.Name)
		}
		stsClient.WaitForRunningAndReady(sts)

		ginkgo.By("Getting pods for statefulset " + stsName)
		pods = stsClient.GetPods(sts)
		framework.ExpectHaveLen(pods.Items, replicas)

		for i, pod := range pods.Items {
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.CidrAnnotation, subnet.Spec.CIDRBlock)
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.GatewayAnnotation, subnet.Spec.Gateway)
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.IPAddressAnnotation, ips[i])
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.LogicalSwitchAnnotation, subnet.Name)
			framework.ExpectMAC(pod.Annotations[util.MacAddressAnnotation])
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")
		}
	})

	framework.ConformanceIt("should allocate static ip for statefulset with ippool", func() {
		ippoolSep := ";"
		if f.VersionPriorTo(1, 11) {
			if f.IsDual() {
				ginkgo.Skip("Support for dual stack ippool was introduced in v1.11")
			}
			ippoolSep = ","
		}

		for replicas := 1; replicas <= 3; replicas++ {
			stsName = "sts-" + framework.RandomSuffix()
			ippool := framework.RandomIPs(cidr, ippoolSep, replicas)
			labels := map[string]string{"app": stsName}

			ginkgo.By("Creating statefulset " + stsName + " with ippool " + ippool)
			sts := framework.MakeStatefulSet(stsName, stsName, int32(replicas), labels, framework.PauseImage)
			sts.Spec.Template.Annotations = map[string]string{util.IPPoolAnnotation: ippool}
			sts = stsClient.CreateSync(sts)

			ginkgo.By("Getting pods for statefulset " + stsName)
			pods := stsClient.GetPods(sts)
			framework.ExpectHaveLen(pods.Items, replicas)

			ips := make([]string, 0, replicas)
			for _, pod := range pods.Items {
				framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
				framework.ExpectHaveKeyWithValue(pod.Annotations, util.CidrAnnotation, subnet.Spec.CIDRBlock)
				framework.ExpectHaveKeyWithValue(pod.Annotations, util.GatewayAnnotation, subnet.Spec.Gateway)
				framework.ExpectHaveKeyWithValue(pod.Annotations, util.IPPoolAnnotation, ippool)
				framework.ExpectHaveKeyWithValue(pod.Annotations, util.LogicalSwitchAnnotation, subnet.Name)
				framework.ExpectMAC(pod.Annotations[util.MacAddressAnnotation])
				framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")
				framework.ExpectConsistOf(util.PodIPs(pod), strings.Split(pod.Annotations[util.IPAddressAnnotation], ","))
				ips = append(ips, pod.Annotations[util.IPAddressAnnotation])
			}
			framework.ExpectConsistOf(ips, strings.Split(ippool, ippoolSep))

			ginkgo.By("Deleting pods for statefulset " + stsName)
			for _, pod := range pods.Items {
				err := podClient.Delete(pod.Name)
				framework.ExpectNoError(err, "failed to delete pod "+pod.Name)
			}
			stsClient.WaitForRunningAndReady(sts)

			ginkgo.By("Getting pods for statefulset " + stsName)
			pods = stsClient.GetPods(sts)
			framework.ExpectHaveLen(pods.Items, replicas)

			for i, pod := range pods.Items {
				framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
				framework.ExpectHaveKeyWithValue(pod.Annotations, util.CidrAnnotation, subnet.Spec.CIDRBlock)
				framework.ExpectHaveKeyWithValue(pod.Annotations, util.GatewayAnnotation, subnet.Spec.Gateway)
				framework.ExpectHaveKeyWithValue(pod.Annotations, util.IPPoolAnnotation, ippool)
				framework.ExpectHaveKeyWithValue(pod.Annotations, util.IPAddressAnnotation, ips[i])
				framework.ExpectHaveKeyWithValue(pod.Annotations, util.LogicalSwitchAnnotation, subnet.Name)
				framework.ExpectMAC(pod.Annotations[util.MacAddressAnnotation])
				framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")
				framework.ExpectConsistOf(util.PodIPs(pod), strings.Split(pod.Annotations[util.IPAddressAnnotation], ","))
			}

			ginkgo.By("Deleting statefulset " + stsName)
			stsClient.DeleteSync(stsName)
		}
	})

	// separate ippool annotation by comma
	framework.ConformanceIt("should allocate static ip for statefulset with ippool separated by comma", func() {
		if f.IsDual() {
			ginkgo.Skip("Comma separated ippool is not supported for dual stack")
		}

		ippoolSep := ","
		replicas := 3
		ippool := framework.RandomIPs(cidr, ippoolSep, replicas)
		labels := map[string]string{"app": stsName}

		ginkgo.By("Creating statefulset " + stsName + " with ippool " + ippool)
		sts := framework.MakeStatefulSet(stsName, stsName, int32(replicas), labels, framework.PauseImage)
		sts.Spec.Template.Annotations = map[string]string{util.IPPoolAnnotation: ippool}
		sts = stsClient.CreateSync(sts)

		ginkgo.By("Getting pods for statefulset " + stsName)
		pods := stsClient.GetPods(sts)
		framework.ExpectHaveLen(pods.Items, replicas)

		ips := make([]string, 0, replicas)
		for _, pod := range pods.Items {
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.CidrAnnotation, subnet.Spec.CIDRBlock)
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.GatewayAnnotation, subnet.Spec.Gateway)
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.IPPoolAnnotation, ippool)
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.LogicalSwitchAnnotation, subnet.Name)
			framework.ExpectMAC(pod.Annotations[util.MacAddressAnnotation])
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")
			framework.ExpectConsistOf(util.PodIPs(pod), strings.Split(pod.Annotations[util.IPAddressAnnotation], ","))
			ips = append(ips, pod.Annotations[util.IPAddressAnnotation])
		}
		framework.ExpectConsistOf(ips, strings.Split(ippool, ippoolSep))

		ginkgo.By("Deleting pods for statefulset " + stsName)
		for _, pod := range pods.Items {
			err := podClient.Delete(pod.Name)
			framework.ExpectNoError(err, "failed to delete pod "+pod.Name)
		}
		stsClient.WaitForRunningAndReady(sts)

		ginkgo.By("Getting pods for statefulset " + stsName)
		pods = stsClient.GetPods(sts)
		framework.ExpectHaveLen(pods.Items, replicas)

		for i, pod := range pods.Items {
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.CidrAnnotation, subnet.Spec.CIDRBlock)
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.GatewayAnnotation, subnet.Spec.Gateway)
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.IPPoolAnnotation, ippool)
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.IPAddressAnnotation, ips[i])
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.LogicalSwitchAnnotation, subnet.Name)
			framework.ExpectMAC(pod.Annotations[util.MacAddressAnnotation])
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")
			framework.ExpectConsistOf(util.PodIPs(pod), strings.Split(pod.Annotations[util.IPAddressAnnotation], ","))
		}
	})

	framework.ConformanceIt("should consider statefulset's start ordinal", func() {
		f.SkipVersionPriorTo(1, 11, "Support for start ordinal was introduced in v1.11")

		replicas, startOrdinal := int32(3), int32(10)
		labels := map[string]string{"app": stsName}

		ginkgo.By("Creating statefulset " + stsName + " with start ordinal " + strconv.Itoa(int(startOrdinal)))
		sts := framework.MakeStatefulSet(stsName, stsName, replicas, labels, framework.PauseImage)
		sts.Spec.Ordinals = &appsv1.StatefulSetOrdinals{Start: startOrdinal}
		sts = stsClient.CreateSync(sts)

		ginkgo.By("Getting pods for statefulset " + stsName)
		pods := stsClient.GetPods(sts)
		framework.ExpectHaveLen(pods.Items, int(replicas))

		ips := make([]string, 0, int(replicas))
		for _, pod := range pods.Items {
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
			framework.ExpectMAC(pod.Annotations[util.MacAddressAnnotation])
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")
			framework.ExpectConsistOf(util.PodIPs(pod), strings.Split(pod.Annotations[util.IPAddressAnnotation], ","))
			ips = append(ips, pod.Annotations[util.IPAddressAnnotation])
		}

		ginkgo.By("Deleting pods for statefulset " + stsName)
		for _, pod := range pods.Items {
			err := podClient.Delete(pod.Name)
			framework.ExpectNoError(err, "failed to delete pod "+pod.Name)
		}
		stsClient.WaitForRunningAndReady(sts)

		ginkgo.By("Getting pods for statefulset " + stsName)
		pods = stsClient.GetPods(sts)
		framework.ExpectHaveLen(pods.Items, int(replicas))

		for i, pod := range pods.Items {
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.IPAddressAnnotation, ips[i])
			framework.ExpectMAC(pod.Annotations[util.MacAddressAnnotation])
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")
		}
	})

	framework.ConformanceIt("should support IPPool feature", func() {
		f.SkipVersionPriorTo(1, 12, "Support for IPPool feature was introduced in v1.12")

		ipsCount := 12
		ips := framework.RandomIPPool(cidr, ipsCount)
		ipv4, ipv6 := util.SplitIpsByProtocol(ips)
		if f.HasIPv4() {
			framework.ExpectHaveLen(ipv4, ipsCount)
		}
		if f.HasIPv6() {
			framework.ExpectHaveLen(ipv6, ipsCount)
		}

		ipv4Range, err := ipam.NewIPRangeListFrom(ipv4...)
		framework.ExpectNoError(err)
		ipv6Range, err := ipam.NewIPRangeListFrom(ipv6...)
		framework.ExpectNoError(err)

		excludeV4, excludeV6 := util.SplitIpsByProtocol(subnet.Spec.ExcludeIps)
		excludeV4Range, err := ipam.NewIPRangeListFrom(excludeV4...)
		framework.ExpectNoError(err)
		excludeV6Range, err := ipam.NewIPRangeListFrom(excludeV6...)
		framework.ExpectNoError(err)

		ipv4Range = ipv4Range.Separate(excludeV4Range)
		ipv6Range = ipv6Range.Separate(excludeV6Range)

		ginkgo.By(fmt.Sprintf("Creating ippool %s with ips %v", ippoolName, ips))
		ippool := framework.MakeIPPool(ippoolName, subnetName, ips, nil)
		ippool = ippoolClient.CreateSync(ippool)

		ginkgo.By("Validating ippool status")
		framework.WaitUntil(2*time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
			if !ippool.Status.V4UsingIPs.EqualInt64(0) {
				framework.Logf("unexpected .status.v4UsingIPs: %s", ippool.Status.V4UsingIPs)
				return false, nil
			}
			if !ippool.Status.V6UsingIPs.EqualInt64(0) {
				framework.Logf("unexpected .status.v6UsingIPs: %s", ippool.Status.V6UsingIPs)
				return false, nil
			}
			if ippool.Status.V4UsingIPRange != "" {
				framework.Logf("unexpected .status.v4UsingIPRange: %s", ippool.Status.V4UsingIPRange)
				return false, nil
			}
			if ippool.Status.V6UsingIPRange != "" {
				framework.Logf("unexpected .status.v6UsingIPRange: %s", ippool.Status.V6UsingIPRange)
				return false, nil
			}
			if !ippool.Status.V4AvailableIPs.Equal(ipv4Range.Count()) {
				framework.Logf(".status.v4AvailableIPs mismatch: expect %s, actual %s", ipv4Range.Count(), ippool.Status.V4AvailableIPs)
				return false, nil
			}
			if !ippool.Status.V6AvailableIPs.Equal(ipv6Range.Count()) {
				framework.Logf(".status.v6AvailableIPs mismatch: expect %s, actual %s", ipv6Range.Count(), ippool.Status.V6AvailableIPs)
				return false, nil
			}
			if ippool.Status.V4AvailableIPRange != ipv4Range.String() {
				framework.Logf(".status.v4AvailableIPRange mismatch: expect %s, actual %s", ipv4Range, ippool.Status.V4AvailableIPRange)
				return false, nil
			}
			if ippool.Status.V6AvailableIPRange != ipv6Range.String() {
				framework.Logf(".status.v6AvailableIPRange mismatch: expect %s, actual %s", ipv6Range, ippool.Status.V6AvailableIPRange)
				return false, nil
			}
			return true, nil
		}, "")

		ginkgo.By("Creating deployment " + deployName + " within ippool " + ippoolName)
		replicas := 3
		labels := map[string]string{"app": deployName}
		annotations := map[string]string{util.IPPoolAnnotation: ippoolName}
		deploy := framework.MakeDeployment(deployName, int32(replicas), labels, annotations, "pause", framework.PauseImage, "")
		deploy = deployClient.CreateSync(deploy)

		checkFn := func() {
			ginkgo.GinkgoHelper()

			ginkgo.By("Getting pods for deployment " + deployName)
			pods, err := deployClient.GetPods(deploy)
			framework.ExpectNoError(err, "failed to get pods for deployment "+deployName)
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
			framework.WaitUntil(2*time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
				ippool = ippoolClient.Get(ippoolName)
				v4Available, v6Available := ipv4Range.Separate(v4Using), ipv6Range.Separate(v6Using)
				if !ippool.Status.V4UsingIPs.Equal(v4Using.Count()) {
					framework.Logf(".status.v4UsingIPs mismatch: expect %s, actual %s", v4Using.Count(), ippool.Status.V4UsingIPs)
					return false, nil
				}
				if !ippool.Status.V6UsingIPs.Equal(v6Using.Count()) {
					framework.Logf(".status.v6UsingIPs mismatch: expect %s, actual %s", v6Using.Count(), ippool.Status.V6UsingIPs)
					return false, nil
				}
				if ippool.Status.V4UsingIPRange != v4Using.String() {
					framework.Logf(".status.v4UsingIPRange mismatch: expect %s, actual %s", v4Using, ippool.Status.V4UsingIPRange)
					return false, nil
				}
				if ippool.Status.V6UsingIPRange != v6Using.String() {
					framework.Logf(".status.v6UsingIPRange mismatch: expect %s, actual %s", v6Using, ippool.Status.V6UsingIPRange)
					return false, nil
				}
				if !ippool.Status.V4AvailableIPs.Equal(v4Available.Count()) {
					framework.Logf(".status.v4AvailableIPs mismatch: expect %s, actual %s", v4Available.Count(), ippool.Status.V4AvailableIPs)
					return false, nil
				}
				if !ippool.Status.V6AvailableIPs.Equal(v6Available.Count()) {
					framework.Logf(".status.v6AvailableIPs mismatch: expect %s, actual %s", v6Available.Count(), ippool.Status.V6AvailableIPs)
					return false, nil
				}
				if ippool.Status.V4AvailableIPRange != v4Available.String() {
					framework.Logf(".status.v4AvailableIPRange mismatch: expect %s, actual %s", v4Available, ippool.Status.V4AvailableIPRange)
					return false, nil
				}
				if ippool.Status.V6AvailableIPRange != v6Available.String() {
					framework.Logf(".status.v6AvailableIPRange mismatch: expect %s, actual %s", v6Available, ippool.Status.V6AvailableIPRange)
					return false, nil
				}
				return true, nil
			}, "")
		}
		checkFn()

		ginkgo.By("Restarting deployment " + deployName)
		deploy = deployClient.RestartSync(deploy)
		checkFn()

		ginkgo.By("Adding namespace " + namespaceName + " to ippool " + ippoolName)
		patchedIPPool := ippool.DeepCopy()
		patchedIPPool.Spec.Namespaces = []string{namespaceName}
		ippool = ippoolClient.Patch(ippool, patchedIPPool, 10*time.Second)

		ginkgo.By("Validating namespace annotations")
		framework.WaitUntil(2*time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
			ns := nsClient.Get(namespaceName)
			return len(ns.Annotations) != 0 && ns.Annotations[util.IPPoolAnnotation] == ippoolName, nil
		}, "")

		ginkgo.By("Patching deployment " + deployName)
		deploy = deployClient.RestartSync(deploy)
		patchedDeploy := deploy.DeepCopy()
		patchedDeploy.Spec.Template.Annotations = nil
		deploy = deployClient.PatchSync(deploy, patchedDeploy)
		checkFn()
	})

	framework.ConformanceIt("should allocate right IPs for the statefulset when there are multiple IP Pools added to its namespace", func() {
		f.SkipVersionPriorTo(1, 14, "Multiple IP Pools per namespace support was introduced in v1.14")
		replicas := 1
		ipsCount := 12

		ginkgo.By("Creating a new subnet " + subnetName2)
		testCidr := framework.RandomCIDR(f.ClusterIPFamily)
		testSubnet := framework.MakeSubnet(subnetName2, "", testCidr, "", "", "", nil, nil, []string{namespaceName})
		testSubnet = subnetClient.CreateSync(testSubnet)

		ginkgo.By("Creating IPPool resources")
		ipsRange1 := framework.RandomIPPool(cidr, ipsCount)
		ipsRange2 := framework.RandomIPPool(testCidr, ipsCount)
		ippool1 := framework.MakeIPPool(ippoolName, subnetName, ipsRange1, []string{namespaceName})
		ippool2 := framework.MakeIPPool(ippoolName2, subnetName2, ipsRange2, []string{namespaceName})
		ippoolClient.CreateSync(ippool1)
		ippoolClient.CreateSync(ippool2)

		ginkgo.By("Creating statefulset " + stsName + " with logical switch annotation and no ippool annotation")
		labels := map[string]string{"app": stsName}
		sts := framework.MakeStatefulSet(stsName, stsName, int32(replicas), labels, framework.PauseImage)
		sts.Spec.Template.Annotations = map[string]string{util.LogicalSwitchAnnotation: subnetName2}
		sts = stsClient.CreateSync(sts)

		ginkgo.By("Getting pods for statefulset " + stsName)
		pods := stsClient.GetPods(sts)
		framework.ExpectHaveLen(pods.Items, replicas)

		for _, pod := range pods.Items {
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.CidrAnnotation, testSubnet.Spec.CIDRBlock)
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.GatewayAnnotation, testSubnet.Spec.Gateway)
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.LogicalSwitchAnnotation, subnetName2)
			framework.ExpectIPInCIDR(pod.Annotations[util.IPAddressAnnotation], testSubnet.Spec.CIDRBlock)
			framework.ExpectMAC(pod.Annotations[util.MacAddressAnnotation])
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")
		}
	})

	framework.ConformanceIt("should allocate right IPs for the statefulset when there are multiple ippools added in the namespace and there are no available ips in the first ippool", func() {
		f.SkipVersionPriorTo(1, 14, "Multiple IP Pools per namespace support was introduced in v1.14")
		replicas := 1
		ipsCount := 1

		ginkgo.By("Creating IPPool resources")
		ipsRange := framework.RandomIPPool(cidr, ipsCount*2)
		ipv4Range, ipv6Range := util.SplitIpsByProtocol(ipsRange)
		var ipsRange1, ipsRange2 []string
		if f.HasIPv4() {
			ipsRange1, ipsRange2 = slices.Clone(ipv4Range[:ipsCount]), slices.Clone(ipv4Range[ipsCount:])
		}
		if f.HasIPv6() {
			ipsRange1 = append(ipsRange1, ipv6Range[:ipsCount]...)
			ipsRange2 = append(ipsRange2, ipv6Range[ipsCount:]...)
		}
		ippool1 := framework.MakeIPPool(ippoolName, subnetName, ipsRange1, []string{namespaceName})
		ippool2 := framework.MakeIPPool(ippoolName2, subnetName, ipsRange2, []string{namespaceName})
		ippoolClient.CreateSync(ippool1)
		ippoolClient.CreateSync(ippool2)

		ginkgo.By("Creating first statefulset " + stsName + " with logical switch annotation and no ippool annotation")
		sts := framework.MakeStatefulSet(stsName, stsName, int32(replicas), map[string]string{"app": stsName}, framework.PauseImage)
		sts.Spec.Template.Annotations = map[string]string{util.LogicalSwitchAnnotation: subnetName}
		sts = stsClient.CreateSync(sts)

		ginkgo.By("Getting pods for the first statefulset " + stsName)
		pods := stsClient.GetPods(sts)
		framework.ExpectHaveLen(pods.Items, replicas)

		for _, pod := range pods.Items {
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.CidrAnnotation, subnet.Spec.CIDRBlock)
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.GatewayAnnotation, subnet.Spec.Gateway)
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.LogicalSwitchAnnotation, subnetName)
			framework.ExpectIPInCIDR(pod.Annotations[util.IPAddressAnnotation], subnet.Spec.CIDRBlock)
			framework.ExpectMAC(pod.Annotations[util.MacAddressAnnotation])
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")
			for _, ip := range util.PodIPs(pod) {
				framework.ExpectContainElement(append(ipsRange1, ipsRange2...), ip)
			}
		}

		ginkgo.By("Creating second statefulset " + stsName2 + " with logical switch annotation and no ippool annotation")
		sts2 := framework.MakeStatefulSet(stsName2, stsName2, int32(replicas), map[string]string{"app": stsName2}, framework.PauseImage)
		sts2.Spec.Template.Annotations = map[string]string{util.LogicalSwitchAnnotation: subnetName}
		sts2 = stsClient.CreateSync(sts2)

		ginkgo.By("Getting pods for the second statefulset " + stsName2)
		pods2 := stsClient.GetPods(sts2)
		framework.ExpectHaveLen(pods2.Items, replicas)

		for _, pod := range pods2.Items {
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.CidrAnnotation, subnet.Spec.CIDRBlock)
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.GatewayAnnotation, subnet.Spec.Gateway)
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.LogicalSwitchAnnotation, subnetName)
			framework.ExpectIPInCIDR(pod.Annotations[util.IPAddressAnnotation], subnet.Spec.CIDRBlock)
			framework.ExpectMAC(pod.Annotations[util.MacAddressAnnotation])
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")
			for _, ip := range util.PodIPs(pod) {
				framework.ExpectContainElement(append(ipsRange1, ipsRange2...), ip)
			}
		}
	})

	framework.ConformanceIt("should block IP allocation if the ippool bound by namespace annotation has no available IPs", func() {
		f.SkipVersionPriorTo(1, 14, "This feature was introduced in v1.14")

		ginkgo.By("Creating IPPool " + ippoolName)
		ipsCount := 1
		ips := framework.RandomIPPool(cidr, ipsCount)
		ippool := framework.MakeIPPool(ippoolName, subnetName, ips, []string{namespaceName})
		_ = ippoolClient.CreateSync(ippool)

		ginkgo.By("Creating deployment " + deployName + " with replicas equal to the number of IPs in the ippool")
		labels := map[string]string{"app": deployName}
		deploy := framework.MakeDeployment(deployName, int32(ipsCount), labels, nil, "pause", framework.PauseImage, "")
		_ = deployClient.CreateSync(deploy)

		ginkgo.By("Creating pod " + podName + " which should be blocked for IP allocation")
		pod := framework.MakePod(namespaceName, podName, nil, nil, "", nil, nil)
		_ = podClient.Create(pod)

		ginkgo.By("Waiting for pod " + podName + " to have event indicating IP allocation failure")
		eventClient := f.EventClient()
		_ = eventClient.WaitToHaveEvent("Pod", podName, "Warning", "AcquireAddressFailed", "kube-ovn-controller", "")
	})

	framework.ConformanceIt("should be able to allocate IP from IPPools in different subnets", func() {
		f.SkipVersionPriorTo(1, 14, "This feature was introduced in v1.14")
		ipsCount := 1

		ginkgo.By("Creating subnet " + subnetName2)
		cidr2 := framework.RandomCIDR(f.ClusterIPFamily)
		subnet2 := framework.MakeSubnet(subnetName2, "", cidr2, "", "", "", nil, nil, []string{namespaceName})
		_ = subnetClient.CreateSync(subnet2)

		ginkgo.By("Creating IPPool " + ippoolName)
		ips := framework.RandomIPPool(cidr, ipsCount)
		ippool := framework.MakeIPPool(ippoolName, subnetName, ips, []string{namespaceName})
		_ = ippoolClient.CreateSync(ippool)

		ginkgo.By("Creating IPPool " + ippoolName2)
		ips2 := framework.RandomIPPool(cidr2, ipsCount)
		ippool2 := framework.MakeIPPool(ippoolName2, subnetName2, ips2, []string{namespaceName})
		_ = ippoolClient.CreateSync(ippool2)

		ginkgo.By("Creating deployment " + deployName + " with replicas equal to the number of IPs in the ippool " + ippoolName)
		labels := map[string]string{"app": deployName}
		deploy := framework.MakeDeployment(deployName, int32(ipsCount), labels, nil, "pause", framework.PauseImage, "")
		_ = deployClient.CreateSync(deploy)

		ginkgo.By("Creating pod " + podName + " which should have IP allocated from ippool " + ippoolName2)
		pod := framework.MakePod(namespaceName, podName, nil, nil, "", nil, nil)
		_ = podClient.CreateSync(pod)
	})

	framework.ConformanceIt("should manage address set when EnableAddressSet is true", func() {
		ginkgo.By("Creating ippool " + ippoolName + " with EnableAddressSet enabled")
		poolIPs := framework.RandomIPPool(cidr, 4)
		framework.ExpectTrue(len(poolIPs) >= 2, "expected at least two IPs in pool")
		ippool := framework.MakeIPPool(ippoolName, subnetName, poolIPs, []string{namespaceName})
		ippool.Spec.EnableAddressSet = true
		ippool = ippoolClient.CreateSync(ippool)

		ginkgo.By("Verifying address set contains pool IPs")
		framework.ExpectNoError(framework.WaitForAddressSetIPs(ippoolName, poolIPs))

		ginkgo.By("Updating ippool to remove one IP entry")
		updated := ippool.DeepCopy()
		updated.Spec.IPs = updated.Spec.IPs[:len(updated.Spec.IPs)-1]
		updated = ippoolClient.UpdateSync(updated, metav1.UpdateOptions{}, ippoolUpdateTimeout)

		ginkgo.By("Checking address set reflects IP removal")
		framework.ExpectNoError(framework.WaitForAddressSetIPs(ippoolName, updated.Spec.IPs))

		ginkgo.By("Disabling EnableAddressSet to trigger address set deletion")
		updated.Spec.EnableAddressSet = false
		updated = ippoolClient.UpdateSync(updated, metav1.UpdateOptions{}, ippoolUpdateTimeout)
		framework.ExpectNoError(framework.WaitForAddressSetDeletion(ippoolName))
	})
})
