package service

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os/exec"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	clientset "k8s.io/client-go/kubernetes"
	e2enode "k8s.io/kubernetes/test/e2e/framework/node"
	e2epodoutput "k8s.io/kubernetes/test/e2e/framework/pod/output"

	"github.com/onsi/ginkgo/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

var _ = framework.Describe("[group:service]", func() {
	f := framework.NewDefaultFramework("service")

	var cs clientset.Interface
	var serviceClient *framework.ServiceClient
	var podClient *framework.PodClient
	var subnetClient *framework.SubnetClient
	var namespaceName, serviceName, podName, hostPodName, subnetName, cidr, image string

	ginkgo.BeforeEach(func() {
		cs = f.ClientSet
		serviceClient = f.ServiceClient()
		podClient = f.PodClient()
		subnetClient = f.SubnetClient()
		namespaceName = f.Namespace.Name
		serviceName = "service-" + framework.RandomSuffix()
		podName = "pod-" + framework.RandomSuffix()
		hostPodName = "pod-" + framework.RandomSuffix()
		subnetName = "subnet-" + framework.RandomSuffix()
		cidr = framework.RandomCIDR(f.ClusterIPFamily)
		if image == "" {
			image = framework.GetKubeOvnImage(cs)
		}
	})
	ginkgo.AfterEach(func() {
		ginkgo.By("Deleting service " + serviceName)
		serviceClient.DeleteSync(serviceName)

		ginkgo.By("Deleting pod " + podName)
		podClient.DeleteSync(podName)

		ginkgo.By("Deleting pod " + hostPodName)
		podClient.DeleteSync(hostPodName)

		ginkgo.By("Deleting subnet " + subnetName)
		subnetClient.DeleteSync(subnetName)
	})

	framework.ConformanceIt("should be able to connect to NodePort service with external traffic policy set to Local from other nodes", func() {
		f.SkipVersionPriorTo(1, 9, "This case is not adapted before v1.9")
		ginkgo.By("Creating subnet " + subnetName)
		subnet := framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, nil)
		_ = subnetClient.CreateSync(subnet)

		ginkgo.By("Creating pod " + podName)
		podLabels := map[string]string{"app": podName}
		annotations := map[string]string{
			util.LogicalSwitchAnnotation: subnetName,
		}
		port := 8000 + rand.Int32N(1000)
		portStr := strconv.Itoa(int(port))
		args := []string{"netexec", "--http-port", portStr}
		pod := framework.MakePod(namespaceName, podName, podLabels, annotations, framework.AgnhostImage, nil, args)
		_ = podClient.CreateSync(pod)

		ginkgo.By("Creating service " + serviceName)
		ports := []corev1.ServicePort{{
			Name:       "tcp",
			Protocol:   corev1.ProtocolTCP,
			Port:       port,
			TargetPort: intstr.FromInt32(port),
		}}
		service := framework.MakeService(serviceName, corev1.ServiceTypeNodePort, nil, podLabels, ports, "")
		service.Spec.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicyLocal
		service = serviceClient.CreateSync(service, func(s *corev1.Service) (bool, error) {
			return len(s.Spec.Ports) != 0 && s.Spec.Ports[0].NodePort != 0, nil
		}, "node port is allocated")

		ginkgo.By("Creating pod " + hostPodName + " with host network")
		cmd := []string{"sh", "-c", "sleep infinity"}
		hostPod := framework.MakePod(namespaceName, hostPodName, nil, nil, image, cmd, nil)
		hostPod.Spec.HostNetwork = true
		_ = podClient.CreateSync(hostPod)

		ginkgo.By("Getting nodes")
		nodeList, err := e2enode.GetReadySchedulableNodes(context.Background(), cs)
		framework.ExpectNoError(err)

		nodePort := service.Spec.Ports[0].NodePort
		fnCheck := func(nodeName, nodeIP string, nodePort int32) {
			if nodeIP == "" {
				return
			}
			protocol := strings.ToLower(util.CheckProtocol(nodeIP))
			ginkgo.By("Checking " + protocol + " connection via node " + nodeName)
			cmd := fmt.Sprintf("curl -q -s --connect-timeout 5 %s/clientip", util.JoinHostPort(nodeIP, nodePort))
			framework.WaitUntil(2*time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
				ginkgo.By(fmt.Sprintf(`Executing %q in pod %s/%s`, cmd, namespaceName, hostPodName))
				_, err := e2epodoutput.RunHostCmd(namespaceName, hostPodName, cmd)
				return err == nil, nil
			}, "")
		}
		for _, node := range nodeList.Items {
			ipv4, ipv6 := util.GetNodeInternalIP(node)
			fnCheck(node.Name, ipv4, nodePort)
			fnCheck(node.Name, ipv6, nodePort)
		}
	})

	framework.ConformanceIt("should ovn nb change vip when dual-stack service removes the cluster ip", func() {
		if !f.IsDual() {
			ginkgo.Skip("this case only support dual mode")
		}
		f.SkipVersionPriorTo(1, 11, "This case is support in v1.11")
		ginkgo.By("Creating service " + serviceName)
		port := 8000 + rand.Int32N(1000)
		ports := []corev1.ServicePort{{
			Name:       "tcp",
			Protocol:   corev1.ProtocolTCP,
			Port:       port,
			TargetPort: intstr.FromInt32(port),
		}}

		selector := map[string]string{"app": "svc-dual"}
		service := framework.MakeService(serviceName, corev1.ServiceTypeClusterIP, nil, selector, ports, corev1.ServiceAffinityNone)
		service.Namespace = namespaceName
		service = serviceClient.CreateSync(service, func(s *corev1.Service) (bool, error) {
			return len(s.Spec.ClusterIPs) != 0, nil
		}, "cluster ips are not empty")
		v6ClusterIP := service.Spec.ClusterIPs[1]
		originService := service.DeepCopy()

		ginkgo.By("Creating pod " + podName)
		podBackend := framework.MakePod(namespaceName, podName, selector, nil, framework.PauseImage, nil, nil)
		_ = podClient.CreateSync(podBackend)

		checkContainsClusterIP := func(v6ClusterIP string, isContain bool) {
			execCmd := "kubectl ko nbctl --format=csv --data=bare --no-heading --columns=vips find Load_Balancer name=cluster-tcp-loadbalancer"
			framework.WaitUntil(2*time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
				output, err := exec.Command("bash", "-c", execCmd).CombinedOutput()
				framework.ExpectNoError(err)
				framework.Logf("output is %q", output)
				framework.Logf("v6ClusterIP is %q", v6ClusterIP)
				vips := strings.Fields(string(output))
				prefix := util.JoinHostPort(v6ClusterIP, port) + "="
				var found bool
				for _, vip := range vips {
					if strings.HasPrefix(vip, prefix) {
						found = true
						break
					}
				}
				if found == isContain {
					return true, nil
				}
				return false, nil
			}, "")

			output, err := exec.Command("bash", "-c", execCmd).CombinedOutput()
			framework.ExpectNoError(err)
			framework.ExpectEqual(strings.Contains(string(output), v6ClusterIP), isContain)
		}

		ginkgo.By("check service from dual stack should have cluster ip")
		checkContainsClusterIP(v6ClusterIP, true)

		ginkgo.By("change service from dual stack to single stack")
		modifyService := service.DeepCopy()
		*modifyService.Spec.IPFamilyPolicy = corev1.IPFamilyPolicySingleStack
		modifyService.Spec.IPFamilies = []corev1.IPFamily{corev1.IPv4Protocol}
		modifyService.Spec.ClusterIPs = []string{service.Spec.ClusterIP}
		service = serviceClient.Patch(service, modifyService)
		checkContainsClusterIP(v6ClusterIP, false)

		ginkgo.By("recover service from single stack to dual stack")
		recoverService := service.DeepCopy()
		*recoverService.Spec.IPFamilyPolicy = *originService.Spec.IPFamilyPolicy
		recoverService.Spec.IPFamilies = originService.Spec.IPFamilies
		recoverService.Spec.ClusterIPs = originService.Spec.ClusterIPs
		_ = serviceClient.Patch(service, recoverService)
		checkContainsClusterIP(v6ClusterIP, true)
	})
})
