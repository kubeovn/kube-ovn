package ipam

import (
	"context"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	e2epod "k8s.io/kubernetes/test/e2e/framework/pod"

	"github.com/onsi/ginkgo/v2"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

var _ = framework.Describe("[group:ipam]", func() {
	f := framework.NewDefaultFramework("ipam")

	var cs clientset.Interface
	var podClient *framework.PodClient
	var deployClient *framework.DeploymentClient
	var stsClient *framework.StatefulSetClient
	var subnetClient *framework.SubnetClient
	var namespaceName, subnetName, podName, deployName, stsName string
	var subnet *apiv1.Subnet
	var cidr string

	ginkgo.BeforeEach(func() {
		cs = f.ClientSet
		podClient = f.PodClient()
		deployClient = f.DeploymentClient()
		stsClient = f.StatefulSetClient()
		subnetClient = f.SubnetClient()
		namespaceName = f.Namespace.Name
		subnetName = "subnet-" + framework.RandomSuffix()
		podName = "pod-" + framework.RandomSuffix()
		deployName = "deploy-" + framework.RandomSuffix()
		stsName = "sts-" + framework.RandomSuffix()
		cidr = framework.RandomCIDR(f.ClusterIpFamily)

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

		ginkgo.By("Deleting subnet " + subnetName)
		subnetClient.DeleteSync(subnetName)
	})

	framework.ConformanceIt("should allocate static ipv4 and mac for pod", func() {
		mac := util.GenerateMac()
		ip := framework.RandomIPPool(cidr, ";", 1)

		ginkgo.By("Creating pod " + podName + " with ip " + ip + " and mac " + mac)
		annotations := map[string]string{
			util.IpAddressAnnotation:  ip,
			util.MacAddressAnnotation: mac,
		}
		pod := framework.MakePod(namespaceName, podName, nil, annotations, "", nil, nil)
		pod = podClient.CreateSync(pod)

		framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.CidrAnnotation, subnet.Spec.CIDRBlock)
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.GatewayAnnotation, subnet.Spec.Gateway)
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.IpAddressAnnotation, ip)
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
		if f.ClusterIpFamily == "dual" {
			ginkgo.Skip("Comma separated ippool is not supported for dual stack")
		}

		pool := framework.RandomIPPool(cidr, ",", 3)
		ginkgo.By("Creating pod " + podName + " with ippool " + pool)
		annotations := map[string]string{util.IpPoolAnnotation: pool}
		pod := framework.MakePod(namespaceName, podName, nil, annotations, "", nil, nil)
		pod = podClient.CreateSync(pod)

		framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.CidrAnnotation, subnet.Spec.CIDRBlock)
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.GatewayAnnotation, subnet.Spec.Gateway)
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.IpPoolAnnotation, pool)
		framework.ExpectEqual(pod.Annotations[util.IpAddressAnnotation], pod.Status.PodIP)
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.LogicalSwitchAnnotation, subnet.Name)
		framework.ExpectMAC(pod.Annotations[util.MacAddressAnnotation])
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")
		framework.ExpectContainElement(strings.Split(pool, ","), pod.Status.PodIP)
	})

	framework.ConformanceIt("should allocate static ip for deployment with ippool", func() {
		ippoolSep := ";"
		if f.ClusterVersionMajor < 1 ||
			(f.ClusterVersionMajor == 1 && f.ClusterVersionMinor < 11) {
			if f.ClusterIpFamily == "dual" {
				ginkgo.Skip("Support for dual stack ippool was introduced in v1.11")
			}
			ippoolSep = ","
		}

		replicas := 3
		ippool := framework.RandomIPPool(cidr, ippoolSep, replicas)

		ginkgo.By("Creating deployment " + deployName + " with ippool " + ippool)
		labels := map[string]string{"app": deployName}
		annotations := map[string]string{util.IpPoolAnnotation: ippool}
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
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.IpPoolAnnotation, ippool)
			framework.ExpectContainElement(ips, pod.Annotations[util.IpAddressAnnotation])
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.LogicalSwitchAnnotation, subnet.Name)
			framework.ExpectMAC(pod.Annotations[util.MacAddressAnnotation])
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")

			podIPs := make([]string, 0, len(pod.Status.PodIPs))
			for _, podIP := range pod.Status.PodIPs {
				podIPs = append(podIPs, podIP.IP)
			}
			framework.ExpectConsistOf(podIPs, strings.Split(pod.Annotations[util.IpAddressAnnotation], ","))
		}

		ginkgo.By("Deleting pods for deployment " + deployName)
		for _, pod := range pods.Items {
			err = podClient.Delete(context.TODO(), pod.Name, metav1.DeleteOptions{})
			framework.ExpectNoError(err, "failed to delete pod "+pod.Name)
		}
		err = deployClient.WaitToComplete(deploy)
		framework.ExpectNoError(err)

		ginkgo.By("Waiting for new pods to be ready")
		err = e2epod.WaitForPodsRunningReady(cs, namespaceName, *deploy.Spec.Replicas, 0, time.Minute, nil)
		framework.ExpectNoError(err, "timed out waiting for pods to be ready")

		ginkgo.By("Getting pods for deployment " + deployName + " after deletion")
		pods, err = deployClient.GetPods(deploy)
		framework.ExpectNoError(err, "failed to get pods for deployment "+deployName)
		framework.ExpectHaveLen(pods.Items, replicas)
		for _, pod := range pods.Items {
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.CidrAnnotation, subnet.Spec.CIDRBlock)
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.GatewayAnnotation, subnet.Spec.Gateway)
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.IpPoolAnnotation, ippool)
			framework.ExpectContainElement(ips, pod.Annotations[util.IpAddressAnnotation])
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.LogicalSwitchAnnotation, subnet.Name)
			framework.ExpectMAC(pod.Annotations[util.MacAddressAnnotation])
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")

			podIPs := make([]string, 0, len(pod.Status.PodIPs))
			for _, podIP := range pod.Status.PodIPs {
				podIPs = append(podIPs, podIP.IP)
			}
			framework.ExpectConsistOf(podIPs, strings.Split(pod.Annotations[util.IpAddressAnnotation], ","))
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
			framework.ExpectConsistOf(podIPs, strings.Split(pod.Annotations[util.IpAddressAnnotation], ","))
			ips = append(ips, pod.Annotations[util.IpAddressAnnotation])
		}

		ginkgo.By("Deleting pods for statefulset " + stsName)
		for _, pod := range pods.Items {
			err := podClient.Delete(context.TODO(), pod.Name, metav1.DeleteOptions{})
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
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.IpAddressAnnotation, ips[i])
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.LogicalSwitchAnnotation, subnet.Name)
			framework.ExpectMAC(pod.Annotations[util.MacAddressAnnotation])
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")
		}
	})

	framework.ConformanceIt("should allocate static ip for statefulset with ippool", func() {
		ippoolSep := ";"
		if f.ClusterVersionMajor <= 1 && f.ClusterVersionMinor < 11 {
			if f.ClusterIpFamily == "dual" {
				ginkgo.Skip("Support for dual stack ippool was introduced in v1.11")
			}
			ippoolSep = ","
		}

		replicas := 3
		ippool := framework.RandomIPPool(cidr, ippoolSep, replicas)
		labels := map[string]string{"app": stsName}

		ginkgo.By("Creating statefulset " + stsName + " with ippool " + ippool)
		sts := framework.MakeStatefulSet(stsName, stsName, int32(replicas), labels, framework.PauseImage)
		sts.Spec.Template.Annotations = map[string]string{util.IpPoolAnnotation: ippool}
		sts = stsClient.CreateSync(sts)

		ginkgo.By("Getting pods for statefulset " + stsName)
		pods := stsClient.GetPods(sts)
		framework.ExpectHaveLen(pods.Items, replicas)

		ips := make([]string, 0, replicas)
		for _, pod := range pods.Items {
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.CidrAnnotation, subnet.Spec.CIDRBlock)
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.GatewayAnnotation, subnet.Spec.Gateway)
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.IpPoolAnnotation, ippool)
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.LogicalSwitchAnnotation, subnet.Name)
			framework.ExpectMAC(pod.Annotations[util.MacAddressAnnotation])
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")

			podIPs := make([]string, 0, len(pod.Status.PodIPs))
			for _, podIP := range pod.Status.PodIPs {
				podIPs = append(podIPs, podIP.IP)
			}
			framework.ExpectConsistOf(podIPs, strings.Split(pod.Annotations[util.IpAddressAnnotation], ","))
			ips = append(ips, pod.Annotations[util.IpAddressAnnotation])
		}
		framework.ExpectConsistOf(ips, strings.Split(ippool, ippoolSep))

		ginkgo.By("Deleting pods for statefulset " + stsName)
		for _, pod := range pods.Items {
			err := podClient.Delete(context.TODO(), pod.Name, metav1.DeleteOptions{})
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
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.IpPoolAnnotation, ippool)
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.IpAddressAnnotation, ips[i])
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.LogicalSwitchAnnotation, subnet.Name)
			framework.ExpectMAC(pod.Annotations[util.MacAddressAnnotation])
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")

			podIPs := make([]string, 0, len(pod.Status.PodIPs))
			for _, podIP := range pod.Status.PodIPs {
				podIPs = append(podIPs, podIP.IP)
			}
			framework.ExpectConsistOf(podIPs, strings.Split(pod.Annotations[util.IpAddressAnnotation], ","))
		}
	})

	// separate ippool annotation by comma
	framework.ConformanceIt("should allocate static ip for statefulset with ippool separated by comma", func() {
		if f.ClusterIpFamily == "dual" {
			ginkgo.Skip("Comma separated ippool is not supported for dual stack")
		}

		ippoolSep := ","
		replicas := 3
		ippool := framework.RandomIPPool(cidr, ippoolSep, replicas)
		labels := map[string]string{"app": stsName}

		ginkgo.By("Creating statefulset " + stsName + " with ippool " + ippool)
		sts := framework.MakeStatefulSet(stsName, stsName, int32(replicas), labels, framework.PauseImage)
		sts.Spec.Template.Annotations = map[string]string{util.IpPoolAnnotation: ippool}
		sts = stsClient.CreateSync(sts)

		ginkgo.By("Getting pods for statefulset " + stsName)
		pods := stsClient.GetPods(sts)
		framework.ExpectHaveLen(pods.Items, replicas)

		ips := make([]string, 0, replicas)
		for _, pod := range pods.Items {
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.CidrAnnotation, subnet.Spec.CIDRBlock)
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.GatewayAnnotation, subnet.Spec.Gateway)
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.IpPoolAnnotation, ippool)
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.LogicalSwitchAnnotation, subnet.Name)
			framework.ExpectMAC(pod.Annotations[util.MacAddressAnnotation])
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")

			podIPs := make([]string, 0, len(pod.Status.PodIPs))
			for _, podIP := range pod.Status.PodIPs {
				podIPs = append(podIPs, podIP.IP)
			}
			framework.ExpectConsistOf(podIPs, strings.Split(pod.Annotations[util.IpAddressAnnotation], ","))
			ips = append(ips, pod.Annotations[util.IpAddressAnnotation])
		}
		framework.ExpectConsistOf(ips, strings.Split(ippool, ippoolSep))

		ginkgo.By("Deleting pods for statefulset " + stsName)
		for _, pod := range pods.Items {
			err := podClient.Delete(context.TODO(), pod.Name, metav1.DeleteOptions{})
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
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.IpPoolAnnotation, ippool)
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.IpAddressAnnotation, ips[i])
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.LogicalSwitchAnnotation, subnet.Name)
			framework.ExpectMAC(pod.Annotations[util.MacAddressAnnotation])
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")

			podIPs := make([]string, 0, len(pod.Status.PodIPs))
			for _, podIP := range pod.Status.PodIPs {
				podIPs = append(podIPs, podIP.IP)
			}
			framework.ExpectConsistOf(podIPs, strings.Split(pod.Annotations[util.IpAddressAnnotation], ","))
		}
	})
})
