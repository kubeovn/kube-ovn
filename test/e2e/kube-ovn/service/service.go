package service

import (
	"os/exec"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	"github.com/onsi/ginkgo/v2"
)

var _ = framework.Describe("[group:service]", func() {
	f := framework.NewDefaultFramework("service")

	var serviceClient *framework.ServiceClient
	var podClient *framework.PodClient
	var namespaceName, serviceName, podName string

	ginkgo.BeforeEach(func() {
		serviceClient = f.ServiceClient()
		podClient = f.PodClient()
		namespaceName = f.Namespace.Name
		serviceName = "service-" + framework.RandomSuffix()
		podName = "pod-" + framework.RandomSuffix()
	})
	ginkgo.AfterEach(func() {
		ginkgo.By("Deleting pod " + podName)
		podClient.DeleteSync(podName)

		ginkgo.By("Deleting service " + serviceName)
		serviceClient.DeleteSync(serviceName)
	})

	framework.ConformanceIt("should ovn nb change vip when dual-stack service removes the cluster ip ", func() {
		if f.ClusterIpFamily != "dual" {
			ginkgo.Skip("this case only support dual mode")
		}
		f.SkipVersionPriorTo(1, 11, "This case is support in v1.11")
		ginkgo.By("Creating service " + serviceName)
		ports := []corev1.ServicePort{{
			Name:       "tcp",
			Protocol:   corev1.ProtocolTCP,
			Port:       80,
			TargetPort: intstr.FromInt(80),
		}}

		selector := map[string]string{"app": "svc-dual"}
		service := framework.MakeService(serviceName, corev1.ServiceTypeClusterIP, nil, selector, ports, corev1.ServiceAffinityNone)
		service.Namespace = namespaceName
		service.Spec.IPFamilyPolicy = new(corev1.IPFamilyPolicy)
		*service.Spec.IPFamilyPolicy = corev1.IPFamilyPolicyPreferDualStack
		service = serviceClient.CreateSync(service, func(s *corev1.Service) (bool, error) {
			return len(s.Spec.ClusterIPs) != 0, nil
		}, "cluster ips are not empty")
		v6ClusterIp := service.Spec.ClusterIPs[1]
		originService := service.DeepCopy()

		podBackend := framework.MakePod(namespaceName, podName, selector, nil, framework.PauseImage, nil, nil)
		_ = podClient.CreateSync(podBackend)

		checkContainsClusterIP := func(v6ClusterIp string, isContain bool) {
			execCmd := "kubectl ko nbctl --format=csv --data=bare --no-heading --columns=vips find Load_Balancer name=cluster-tcp-loadbalancer"
			_ = wait.PollImmediate(time.Second, 30*time.Second, func() (bool, error) {
				output, err := exec.Command("bash", "-c", execCmd).CombinedOutput()
				framework.Logf("output is %s ", output)
				framework.Logf("v6ClusterIp is %s ", v6ClusterIp)
				framework.ExpectNoError(err)
				if (isContain && strings.Contains(string(output), v6ClusterIp)) ||
					(!isContain && !strings.Contains(string(output), v6ClusterIp)) {
					return true, nil
				}
				return false, nil
			})

			output, err := exec.Command("bash", "-c", execCmd).CombinedOutput()
			framework.ExpectNoError(err)
			framework.ExpectEqual(strings.Contains(string(output), v6ClusterIp), isContain)
		}

		ginkgo.By("check service from dual stack should have cluster ip ")
		checkContainsClusterIP(v6ClusterIp, true)

		ginkgo.By("change service from dual stack to single stack ")
		modifyService := service.DeepCopy()
		*modifyService.Spec.IPFamilyPolicy = corev1.IPFamilyPolicySingleStack
		modifyService.Spec.IPFamilies = []corev1.IPFamily{corev1.IPv4Protocol}
		modifyService.Spec.ClusterIPs = []string{service.Spec.ClusterIP}
		service = serviceClient.Patch(service, modifyService)
		checkContainsClusterIP(v6ClusterIp, false)

		ginkgo.By("recover service from single stack to dual stack ")
		recoverService := service.DeepCopy()
		*recoverService.Spec.IPFamilyPolicy = *originService.Spec.IPFamilyPolicy
		recoverService.Spec.IPFamilies = originService.Spec.IPFamilies
		recoverService.Spec.ClusterIPs = originService.Spec.ClusterIPs
		_ = serviceClient.Patch(service, recoverService)
		checkContainsClusterIP(v6ClusterIp, true)
	})
})
