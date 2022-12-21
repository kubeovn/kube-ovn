package ipam

import (
	"context"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/kubernetes/test/e2e/framework/deployment"
	e2epod "k8s.io/kubernetes/test/e2e/framework/pod"
	"k8s.io/kubernetes/test/e2e/framework/statefulset"

	"github.com/onsi/ginkgo/v2"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

var _ = framework.Describe("[group:ipam]", func() {
	f := framework.NewDefaultFramework("ipam")

	var cs clientset.Interface
	var podClient *framework.PodClient
	var subnetClient *framework.SubnetClient
	var namespaceName, subnetName string
	var subnet *apiv1.Subnet
	var cidr string

	ginkgo.BeforeEach(func() {
		cs = f.ClientSet
		podClient = f.PodClient()
		subnetClient = f.SubnetClient()
		namespaceName = f.Namespace.Name
		subnetName = namespaceName
		cidr = framework.RandomCIDR(f.ClusterIpFamily)

		ginkgo.By("Creating subnet " + subnetName)
		subnet = framework.MakeSubnet(subnetName, "", cidr, "", nil, nil, []string{namespaceName})
		subnet = subnetClient.CreateSync(subnet)
	})
	ginkgo.AfterEach(func() {
		ginkgo.By("Deleting subnet " + subnetName)
		subnetClient.DeleteSync(subnetName)
	})

	framework.ConformanceIt("should allocate static ipv4 and mac for pod", func() {
		name := "pod-" + framework.RandomSuffix()
		mac := util.GenerateMac()
		ip := framework.RandomIPPool(cidr, 1)

		ginkgo.By("Creating pod " + name + " with ip " + ip + " and mac " + mac)
		annotations := map[string]string{
			util.IpAddressAnnotation:  ip,
			util.MacAddressAnnotation: mac,
		}
		pod := framework.MakePod(namespaceName, name, nil, annotations, "", nil, nil)
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

		ginkgo.By("Deleting pod " + name)
		podClient.DeleteSync(pod.Name)
	})

	framework.ConformanceIt("should allocate static ipv4 for deployment with ippool", func() {
		replicas := 3
		name := "deployment-" + framework.RandomSuffix()
		ippool := framework.RandomIPPool(cidr, replicas)
		labels := map[string]string{"app": name}

		ginkgo.By("Creating deployment " + name + " with ippool " + ippool)
		deploy := deployment.NewDeployment(name, int32(replicas), labels, "pause", framework.PauseImage, "")
		deploy.Spec.Template.Annotations = map[string]string{util.IpPoolAnnotation: ippool}
		deploy, err := cs.AppsV1().Deployments(namespaceName).Create(context.TODO(), deploy, metav1.CreateOptions{})
		framework.ExpectNoError(err, "failed to to create deployment")
		err = deployment.WaitForDeploymentComplete(cs, deploy)
		framework.ExpectNoError(err, "deployment failed to complete")

		ginkgo.By("Getting pods for deployment " + name)
		pods, err := deployment.GetPodsForDeployment(cs, deploy)
		framework.ExpectNoError(err, "failed to get pods for deployment "+name)
		framework.ExpectHaveLen(pods.Items, replicas)

		ips := strings.Split(ippool, ";")
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

		ginkgo.By("Deleting pods for deployment " + name)
		for _, pod := range pods.Items {
			err = podClient.Delete(context.TODO(), pod.Name, metav1.DeleteOptions{})
			framework.ExpectNoError(err, "failed to delete pod "+pod.Name)
		}
		err = deployment.WaitForDeploymentComplete(cs, deploy)
		framework.ExpectNoError(err, "deployment failed to complete")

		ginkgo.By("Waiting for new pods to be ready")
		err = e2epod.WaitForPodsRunningReady(cs, namespaceName, *deploy.Spec.Replicas, 0, time.Minute, nil)
		framework.ExpectNoError(err, "timed out waiting for pods to be ready")

		ginkgo.By("Getting pods for deployment " + name + " after deletion")
		pods, err = deployment.GetPodsForDeployment(cs, deploy)
		framework.ExpectNoError(err, "failed to get pods for deployment "+name)
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

		ginkgo.By("Deleting deployment " + name)
		err = cs.AppsV1().Deployments(namespaceName).Delete(context.TODO(), name, metav1.DeleteOptions{})
		framework.ExpectNoError(err, "failed to delete deployment "+name)
	})

	framework.ConformanceIt("should allocate static ipv4 for statefulset", func() {
		replicas := 3
		name := "statefulset-" + framework.RandomSuffix()
		labels := map[string]string{"app": name}

		ginkgo.By("Creating statefulset " + name)
		sts := statefulset.NewStatefulSet(name, namespaceName, name, int32(replicas), nil, nil, labels)
		sts.Spec.Template.Spec.Containers[0].Image = framework.PauseImage
		sts, err := cs.AppsV1().StatefulSets(namespaceName).Create(context.TODO(), sts, metav1.CreateOptions{})
		framework.ExpectNoError(err, "failed to to create statefulset")
		statefulset.WaitForRunningAndReady(cs, int32(replicas), sts)

		ginkgo.By("Getting pods for statefulset " + name)
		pods := statefulset.GetPodList(cs, sts)
		framework.ExpectHaveLen(pods.Items, replicas)
		statefulset.SortStatefulPods(pods)

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

		ginkgo.By("Deleting pods for statefulset " + name)
		for _, pod := range pods.Items {
			err = podClient.Delete(context.TODO(), pod.Name, metav1.DeleteOptions{})
			framework.ExpectNoError(err, "failed to delete pod "+pod.Name)
		}
		statefulset.WaitForRunningAndReady(cs, int32(replicas), sts)

		ginkgo.By("Getting pods for statefulset " + name)
		pods = statefulset.GetPodList(cs, sts)
		framework.ExpectHaveLen(pods.Items, replicas)
		statefulset.SortStatefulPods(pods)

		for i, pod := range pods.Items {
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.CidrAnnotation, subnet.Spec.CIDRBlock)
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.GatewayAnnotation, subnet.Spec.Gateway)
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.IpAddressAnnotation, ips[i])
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.LogicalSwitchAnnotation, subnet.Name)
			framework.ExpectMAC(pod.Annotations[util.MacAddressAnnotation])
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")
		}

		ginkgo.By("Deleting statefulset " + name)
		err = cs.AppsV1().StatefulSets(namespaceName).Delete(context.TODO(), name, metav1.DeleteOptions{})
		framework.ExpectNoError(err, "failed to delete statefulset "+name)
	})

	framework.ConformanceIt("should allocate static ipv4 for statefulset with ippool", func() {
		replicas := 3
		name := "statefulset-" + framework.RandomSuffix()
		ippool := framework.RandomIPPool(cidr, replicas)
		labels := map[string]string{"app": name}

		ginkgo.By("Creating statefulset " + name + " with ippool " + ippool)
		sts := statefulset.NewStatefulSet(name, namespaceName, name, int32(replicas), nil, nil, labels)
		sts.Spec.Template.Spec.Containers[0].Image = framework.PauseImage
		sts.Spec.Template.Annotations = map[string]string{util.IpPoolAnnotation: ippool}
		sts, err := cs.AppsV1().StatefulSets(namespaceName).Create(context.TODO(), sts, metav1.CreateOptions{})
		framework.ExpectNoError(err, "failed to to create statefulset")
		statefulset.WaitForRunningAndReady(cs, int32(replicas), sts)

		ginkgo.By("Getting pods for statefulset " + name)
		pods := statefulset.GetPodList(cs, sts)
		framework.ExpectHaveLen(pods.Items, replicas)
		statefulset.SortStatefulPods(pods)

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
		framework.ExpectConsistOf(ips, strings.Split(ippool, ";"))

		ginkgo.By("Deleting pods for statefulset " + name)
		for _, pod := range pods.Items {
			err = podClient.Delete(context.TODO(), pod.Name, metav1.DeleteOptions{})
			framework.ExpectNoError(err, "failed to delete pod "+pod.Name)
		}
		statefulset.WaitForRunningAndReady(cs, int32(replicas), sts)

		ginkgo.By("Getting pods for statefulset " + name)
		pods = statefulset.GetPodList(cs, sts)
		framework.ExpectHaveLen(pods.Items, replicas)
		statefulset.SortStatefulPods(pods)

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

		ginkgo.By("Deleting statefulset " + name)
		err = cs.AppsV1().StatefulSets(namespaceName).Delete(context.TODO(), name, metav1.DeleteOptions{})
		framework.ExpectNoError(err, "failed to delete statefulset "+name)
	})
})
