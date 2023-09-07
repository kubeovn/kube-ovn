package ipam

import (
	"context"
	"fmt"
	"strings"
	"time"

	clientset "k8s.io/client-go/kubernetes"
	e2epod "k8s.io/kubernetes/test/e2e/framework/pod"

	"github.com/onsi/ginkgo/v2"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ipam"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

var _ = framework.Describe("[group:ipam]", func() {
	f := framework.NewDefaultFramework("ipam")

	var cs clientset.Interface
	var nsClient *framework.NamespaceClient
	var podClient *framework.PodClient
	var deployClient *framework.DeploymentClient
	var stsClient *framework.StatefulSetClient
	var subnetClient *framework.SubnetClient
	var ippoolClient *framework.IPPoolClient
	var namespaceName, subnetName, ippoolName, podName, deployName, stsName string
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
		ippoolName = "ippool-" + framework.RandomSuffix()
		podName = "pod-" + framework.RandomSuffix()
		deployName = "deploy-" + framework.RandomSuffix()
		stsName = "sts-" + framework.RandomSuffix()
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

		ginkgo.By("Deleting statefulset " + stsName)
		stsClient.DeleteSync(stsName)

		ginkgo.By("Deleting ippool " + ippoolName)
		ippoolClient.DeleteSync(ippoolName)

		ginkgo.By("Deleting subnet " + subnetName)
		subnetClient.DeleteSync(subnetName)
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

		podIPs := make([]string, 0, len(pod.Status.PodIPs))
		for _, podIP := range pod.Status.PodIPs {
			podIPs = append(podIPs, podIP.IP)
		}
		framework.ExpectConsistOf(podIPs, strings.Split(ip, ","))
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

			podIPs := make([]string, 0, len(pod.Status.PodIPs))
			for _, podIP := range pod.Status.PodIPs {
				podIPs = append(podIPs, podIP.IP)
			}
			framework.ExpectConsistOf(podIPs, strings.Split(pod.Annotations[util.IPAddressAnnotation], ","))
		}

		ginkgo.By("Deleting pods for deployment " + deployName)
		for _, pod := range pods.Items {
			err = podClient.Delete(pod.Name)
			framework.ExpectNoError(err, "failed to delete pod "+pod.Name)
		}
		err = deployClient.WaitToComplete(deploy)
		framework.ExpectNoError(err)

		ginkgo.By("Waiting for new pods to be ready")
		err = e2epod.WaitForPodsRunningReady(context.Background(), cs, namespaceName, *deploy.Spec.Replicas, 0, time.Minute)
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

			podIPs := make([]string, 0, len(pod.Status.PodIPs))
			for _, podIP := range pod.Status.PodIPs {
				podIPs = append(podIPs, podIP.IP)
			}
			framework.ExpectConsistOf(podIPs, strings.Split(pod.Annotations[util.IPAddressAnnotation], ","))
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

			podIPs := make([]string, 0, len(pod.Status.PodIPs))
			for _, podIP := range pod.Status.PodIPs {
				podIPs = append(podIPs, podIP.IP)
			}
			framework.ExpectConsistOf(podIPs, strings.Split(pod.Annotations[util.IPAddressAnnotation], ","))
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

				podIPs := make([]string, 0, len(pod.Status.PodIPs))
				for _, podIP := range pod.Status.PodIPs {
					podIPs = append(podIPs, podIP.IP)
				}
				framework.ExpectConsistOf(podIPs, strings.Split(pod.Annotations[util.IPAddressAnnotation], ","))
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

				podIPs := make([]string, 0, len(pod.Status.PodIPs))
				for _, podIP := range pod.Status.PodIPs {
					podIPs = append(podIPs, podIP.IP)
				}
				framework.ExpectConsistOf(podIPs, strings.Split(pod.Annotations[util.IPAddressAnnotation], ","))
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

			podIPs := make([]string, 0, len(pod.Status.PodIPs))
			for _, podIP := range pod.Status.PodIPs {
				podIPs = append(podIPs, podIP.IP)
			}
			framework.ExpectConsistOf(podIPs, strings.Split(pod.Annotations[util.IPAddressAnnotation], ","))
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

			podIPs := make([]string, 0, len(pod.Status.PodIPs))
			for _, podIP := range pod.Status.PodIPs {
				podIPs = append(podIPs, podIP.IP)
			}
			framework.ExpectConsistOf(podIPs, strings.Split(pod.Annotations[util.IPAddressAnnotation], ","))
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
})
