package pod

import (
	"cmp"
	"context"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/onsi/ginkgo/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

var _ = framework.SerialDescribe("[group:pod]", func() {
	f := framework.NewDefaultFramework("pod")

	var podClient *framework.PodClient
	var namespaceName, podName string

	ginkgo.BeforeEach(func() {
		podClient = f.PodClient()
		namespaceName = f.Namespace.Name
		podName = "pod-" + framework.RandomSuffix()
	})
	ginkgo.AfterEach(func() {
		ginkgo.By("Deleting pod " + podName)
		podClient.DeleteSync(podName)
	})

	framework.ConformanceIt("should handle pod creation during kube-ovn-controller is down", func() {
		ginkgo.By("Creating pod " + podName)
		pod := framework.MakePod(namespaceName, podName, nil, nil, framework.PauseImage, nil, nil)
		pod = podClient.CreateSync(pod)

		ginkgo.By("Validating pod annoations")
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
		framework.ExpectMAC(pod.Annotations[util.MacAddressAnnotation])
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")
		mac := pod.Annotations[util.MacAddressAnnotation]

		portName := ovs.PodNameToPortName(podName, pod.Namespace, util.OvnProvider)
		ginkgo.By("Getting ips " + portName)
		ipClient := f.IPClient()
		ip := ipClient.Get(portName)

		ginkgo.By("Validating ips " + ip.Name)
		framework.ExpectEqual(ip.Spec.MacAddress, mac)
		framework.ExpectEqual(ip.Spec.IPAddress, pod.Annotations[util.IPAddressAnnotation])

		ginkgo.By("Getting deployment kube-ovn-controller")
		deployClient := f.DeploymentClientNS(framework.KubeOvnNamespace)
		deploy := deployClient.Get("kube-ovn-controller")
		framework.ExpectNotNil(deploy.Spec.Replicas)

		ginkgo.By("Getting kube-ovn-controller pods")
		kubePodClient := f.PodClientNS(framework.KubeOvnNamespace)
		framework.ExpectNotNil(deploy.Spec.Replicas)
		pods, err := kubePodClient.List(context.Background(), metav1.ListOptions{LabelSelector: metav1.FormatLabelSelector(deploy.Spec.Selector)})
		framework.ExpectNoError(err, "failed to list kube-ovn-controller pods")
		framework.ExpectNotNil(pods)
		podNames := make([]string, 0, len(pods.Items))
		for _, pod := range pods.Items {
			podNames = append(podNames, pod.Name)
		}
		framework.Logf("Got kube-ovn-controller pods: %s", strings.Join(podNames, ", "))

		ginkgo.By("Stopping kube-ovn-controller by setting its replicas to zero")
		deployClient.SetScale(deploy.Name, 0)

		ginkgo.By("Waiting for kube-ovn-controller pods to disappear")
		for _, pod := range podNames {
			ginkgo.By("Waiting for pod " + pod + " to disappear")
			kubePodClient.WaitForNotFound(pod)
		}

		ginkgo.By("Deleting pod " + podName)
		podClient.DeleteSync(podName)

		ginkgo.By("Recreating pod " + podName)
		pod = framework.MakePod(namespaceName, podName, nil, nil, framework.PauseImage, nil, nil)
		_ = podClient.Create(pod)

		ginkgo.By("Starting kube-ovn-controller by restore its replicas")
		deployClient.SetScale(deploy.Name, cmp.Or(*deploy.Spec.Replicas, 1))

		ginkgo.By("Waiting for kube-ovn-controller to be ready")
		_ = deployClient.RolloutStatus(deploy.Name)

		ginkgo.By("Waiting for pod " + podName + " to be running")
		podClient.WaitForRunning(podName)

		ginkgo.By("Validating pod annoations")
		pod = podClient.GetPod(podName)
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
		framework.ExpectMAC(pod.Annotations[util.MacAddressAnnotation])
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")
		framework.ExpectNotEqual(pod.Annotations[util.MacAddressAnnotation], mac)

		ginkgo.By("Getting ips " + portName)
		ip = ipClient.Get(portName)

		ginkgo.By("Validating ips " + ip.Name)
		framework.ExpectEqual(ip.Spec.MacAddress, pod.Annotations[util.MacAddressAnnotation])
		framework.ExpectEqual(ip.Spec.IPAddress, pod.Annotations[util.IPAddressAnnotation])
	})
})
