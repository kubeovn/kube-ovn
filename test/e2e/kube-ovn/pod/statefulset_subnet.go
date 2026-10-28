package pod

import (
	"context"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	podutil "k8s.io/kubernetes/pkg/api/v1/pod"

	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

var _ = framework.Describe("[group:pod] StatefulSet subnet preservation", func() {
	f := framework.NewDefaultFramework("sts-subnet")

	framework.ConformanceIt("should keep the IP when recreating a StatefulSet pod in an exhausted subnet", func() {
		f.SkipVersionPriorTo(1, 15, "StatefulSet subnet preservation regression requires Kube-OVN v1.15 or later")
		if !f.IsIPv4() {
			ginkgo.Skip("the multiple-subnet regression uses IPv4 /29 subnets")
		}

		podClient := f.PodClient()
		stsClient := f.StatefulSetClient()
		ipClient := f.IPClient()
		subnetNames := createStatefulSetNamespaceSubnets(f)
		stsName := "sts-" + framework.RandomSuffix()
		podName := stsName + "-0"
		portName := ovs.PodNameToPortName(podName, f.Namespace.Name, util.OvnProvider)
		ginkgo.DeferCleanup(func() {
			stsClient.DeleteSync(stsName)
			podClient.WaitForNotFound(podName)
			framework.ExpectNoError(ipClient.WaitToDisappear(portName, time.Second, 2*time.Minute))
		})

		ginkgo.By("Creating StatefulSet " + stsName)
		sts := framework.MakeStatefulSet(stsName, stsName, 1, map[string]string{"app": stsName}, framework.PauseImage)
		_ = stsClient.CreateSync(sts)
		originalPod := podClient.GetPod(podName)
		originalIP := ipClient.Get(portName)
		framework.ExpectNotEmpty(originalIP.Spec.IPAddress)
		framework.ExpectContainElement(subnetNames, originalIP.Spec.Subnet)
		framework.ExpectMAC(originalPod.Annotations[util.MacAddressAnnotation])
		exhaustStatefulSetSubnet(f, originalIP.Spec.Subnet)

		ginkgo.By("Deleting the StatefulSet pod and waiting for a new Pod UID")
		framework.ExpectNoError(podClient.Delete(podName))
		var recreatedPod *corev1.Pod
		gomega.Eventually(func(g gomega.Gomega) {
			pod, err := podClient.Get(context.TODO(), podName, metav1.GetOptions{})
			if err != nil {
				g.Expect(err).NotTo(gomega.HaveOccurred())
				return
			}
			g.Expect(pod.UID).NotTo(gomega.Equal(originalPod.UID))
			g.Expect(podutil.IsPodReady(pod)).To(gomega.BeTrue())
			recreatedPod = pod
		}).WithTimeout(2 * time.Minute).WithPolling(time.Second).Should(gomega.Succeed())
		framework.ExpectEqual(recreatedPod.Status.PodIPs, originalPod.Status.PodIPs)
		framework.ExpectEqual(recreatedPod.Annotations[util.MacAddressAnnotation], originalPod.Annotations[util.MacAddressAnnotation])
		framework.ExpectEqual(recreatedPod.Annotations[util.LogicalSwitchAnnotation], originalIP.Spec.Subnet)

		ginkgo.By("Verifying the retained IP record belongs to the recreated pod")
		gomega.Eventually(func(g gomega.Gomega) {
			ip := ipClient.Get(portName)
			g.Expect(ip.UID).To(gomega.Equal(originalIP.UID))
			g.Expect(ip.Spec.IPAddress).To(gomega.Equal(originalIP.Spec.IPAddress))
			g.Expect(ip.Spec.MacAddress).To(gomega.Equal(originalIP.Spec.MacAddress))
			g.Expect(ip.Spec.Subnet).To(gomega.Equal(originalIP.Spec.Subnet))
			g.Expect(ip.Spec.PodName).To(gomega.Equal(podName))
			g.Expect(ip.Spec.NodeName).To(gomega.Equal(recreatedPod.Spec.NodeName))
		}).WithTimeout(time.Minute).WithPolling(time.Second).Should(gomega.Succeed())
	})
})

func createStatefulSetNamespaceSubnets(f *framework.Framework) []string {
	ginkgo.GinkgoHelper()
	subnetClient := f.SubnetClient()
	subnetNames := make([]string, 0, 2)
	for range 2 {
		name := "sts-subnet-" + framework.RandomSuffix()
		prefix, err := netip.ParsePrefix(framework.RandomCIDR(framework.IPv4))
		framework.ExpectNoError(err)
		cidr := netip.PrefixFrom(prefix.Addr(), 29).String()
		ginkgo.DeferCleanup(subnetClient.DeleteSync, name)
		ginkgo.By("Creating namespace subnet " + name)
		subnetClient.CreateSync(framework.MakeSubnet(name, "", cidr, "", "", "", nil, nil, []string{f.Namespace.Name}))
		subnetNames = append(subnetNames, name)
	}
	// Subnet readiness can precede the namespace annotation update.
	gomega.Eventually(func(g gomega.Gomega) {
		ns, err := f.ClientSet.CoreV1().Namespaces().Get(context.TODO(), f.Namespace.Name, metav1.GetOptions{})
		if err != nil {
			g.Expect(err).NotTo(gomega.HaveOccurred())
			return
		}
		g.Expect(strings.Split(ns.Annotations[util.LogicalSwitchAnnotation], ",")).To(gomega.ContainElements(subnetNames))
	}).WithTimeout(time.Minute).WithPolling(time.Second).Should(gomega.Succeed())
	return subnetNames
}

func exhaustStatefulSetSubnet(f *framework.Framework, subnetName string) {
	ginkgo.GinkgoHelper()
	podClient := f.PodClient()
	subnetClient := f.SubnetClient()
	ginkgo.By("Exhausting StatefulSet subnet " + subnetName)
	for i := range 8 {
		available := subnetClient.Get(subnetName).Status.V4AvailableIPs.Int64()
		if available == 0 {
			return
		}
		name := fmt.Sprintf("sts-filler-%d-%s", i, framework.RandomSuffix())
		ginkgo.DeferCleanup(podClient.DeleteSync, name)
		podClient.CreateSync(framework.MakePod(f.Namespace.Name, name, nil, map[string]string{
			util.LogicalSwitchAnnotation: subnetName,
		}, framework.PauseImage, nil, nil))
		gomega.Eventually(func() int64 {
			return subnetClient.Get(subnetName).Status.V4AvailableIPs.Int64()
		}).WithTimeout(time.Minute).WithPolling(time.Second).Should(gomega.BeNumerically("<", available))
	}
	framework.ExpectEqual(subnetClient.Get(subnetName).Status.V4AvailableIPs.Int64(), int64(0))
}
