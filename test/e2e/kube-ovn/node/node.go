package node

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	clientset "k8s.io/client-go/kubernetes"
	e2enode "k8s.io/kubernetes/test/e2e/framework/node"
	e2epodoutput "k8s.io/kubernetes/test/e2e/framework/pod/output"

	"github.com/onsi/ginkgo/v2"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/iproute"
)

var _ = framework.OrderedDescribe("[group:node]", func() {
	f := framework.NewDefaultFramework("node")

	var subnet *apiv1.Subnet
	var cs clientset.Interface
	var podClient *framework.PodClient
	var serviceClient *framework.ServiceClient
	var subnetClient *framework.SubnetClient
	var podName, hostPodName, serviceName, namespaceName, subnetName string
	var cidr string
	ginkgo.BeforeEach(func() {
		cs = f.ClientSet
		podClient = f.PodClient()
		serviceClient = f.ServiceClient()
		subnetClient = f.SubnetClient()
		namespaceName = f.Namespace.Name
		podName = "pod-" + framework.RandomSuffix()
		hostPodName = "pod-" + framework.RandomSuffix()
		serviceName = "service-" + framework.RandomSuffix()
		subnetName = "subnet-" + framework.RandomSuffix()
		cidr = framework.RandomCIDR(f.ClusterIPFamily)
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

	framework.ConformanceIt("should allocate ip in join subnet to node", func() {
		ginkgo.By("Getting join subnet")
		join := subnetClient.Get("join")

		ginkgo.By("Getting nodes")
		nodeList, err := e2enode.GetReadySchedulableNodes(context.Background(), cs)
		framework.ExpectNoError(err)

		ginkgo.By("Validating node annotations")
		for _, node := range nodeList.Items {
			framework.ExpectHaveKeyWithValue(node.Annotations, util.AllocatedAnnotation, "true")
			framework.ExpectUUID(node.Annotations[util.ChassisAnnotation])
			framework.ExpectHaveKeyWithValue(node.Annotations, util.CidrAnnotation, join.Spec.CIDRBlock)
			framework.ExpectHaveKeyWithValue(node.Annotations, util.GatewayAnnotation, join.Spec.Gateway)
			framework.ExpectIPInCIDR(node.Annotations[util.IPAddressAnnotation], join.Spec.CIDRBlock)
			framework.ExpectHaveKeyWithValue(node.Annotations, util.LogicalSwitchAnnotation, join.Name)
			framework.ExpectMAC(node.Annotations[util.MacAddressAnnotation])
			framework.ExpectHaveKeyWithValue(node.Annotations, util.PortNameAnnotation, util.NodeLspName(node.Name))

			podName = "pod-" + framework.RandomSuffix()
			ginkgo.By("Creating pod " + podName + " with host network on node " + node.Name)
			cmd := []string{"sleep", "infinity"}
			pod := framework.MakePrivilegedPod(namespaceName, podName, nil, nil, f.KubeOVNImage, cmd, nil)
			pod.Spec.NodeName = node.Name
			pod.Spec.HostNetwork = true
			pod = podClient.CreateSync(pod)

			ginkgo.By("Checking ip addresses on " + util.NodeNic)
			links, err := iproute.AddressShow(util.NodeNic, func(cmd ...string) ([]byte, []byte, error) {
				return framework.KubectlExec(pod.Namespace, pod.Name, cmd...)
			})
			framework.ExpectNoError(err)
			framework.ExpectHaveLen(links, 1)
			ipCIDRs, err := util.GetIPAddrWithMask(node.Annotations[util.IPAddressAnnotation], join.Spec.CIDRBlock)
			framework.ExpectNoError(err)
			framework.Logf("node %q join ip address with prefix: %q", node.Name, ipCIDRs)
			ips := strings.Split(ipCIDRs, ",")
			framework.ExpectConsistOf(links[0].NonLinkLocalAddresses(), ips)

			err = podClient.Delete(podName)
			framework.ExpectNoError(err)
		}
	})

	framework.ConformanceIt("should access overlay pods using node ip", func() {
		f.SkipVersionPriorTo(1, 12, "This feature was introduced in v1.12")

		ginkgo.By("Creating subnet " + subnetName)
		subnet = framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, nil)
		subnet = subnetClient.CreateSync(subnet)

		ginkgo.By("Creating pod " + podName)
		annotations := map[string]string{
			util.LogicalSwitchAnnotation: subnetName,
		}
		port := strconv.Itoa(8000 + rand.IntN(1000))
		args := []string{"netexec", "--http-port", port}
		pod := framework.MakePod(namespaceName, podName, nil, annotations, framework.AgnhostImage, nil, args)
		pod = podClient.CreateSync(pod)

		ginkgo.By("Creating pod " + hostPodName + " with host network")
		cmd := []string{"sleep", "infinity"}
		hostPod := framework.MakePod(namespaceName, hostPodName, nil, nil, f.KubeOVNImage, cmd, nil)
		hostPod.Spec.HostNetwork = true
		hostPod = podClient.CreateSync(hostPod)

		ginkgo.By("Validating client ip")
		nodeIPs := util.PodIPs(*hostPod)
		for _, podIP := range pod.Status.PodIPs {
			ip := podIP.IP
			protocol := strings.ToLower(util.CheckProtocol(ip))
			ginkgo.By("Checking connection from " + hostPodName + " to " + podName + " via " + protocol)
			cmd := fmt.Sprintf("curl -q -s --connect-timeout 2 --max-time 2 %s/clientip", net.JoinHostPort(ip, port))
			ginkgo.By(fmt.Sprintf(`Executing %q in pod %s/%s`, cmd, namespaceName, hostPodName))
			output := e2epodoutput.RunHostCmdOrDie(namespaceName, hostPodName, cmd)
			client, _, err := net.SplitHostPort(strings.TrimSpace(output))
			framework.ExpectNoError(err)
			framework.ExpectContainElement(nodeIPs, client)
		}
	})

	framework.ConformanceIt("should access overlay services using node ip", func() {
		f.SkipVersionPriorTo(1, 12, "This feature was introduced in v1.12")

		ginkgo.By("Creating subnet " + subnetName)
		subnet = framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, nil)
		subnet = subnetClient.CreateSync(subnet)

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
		service := framework.MakeService(serviceName, "", nil, podLabels, ports, "")
		_ = serviceClient.CreateSync(service, func(s *corev1.Service) (bool, error) {
			return len(s.Spec.ClusterIPs) != 0, nil
		}, "cluster ips are not empty")

		ginkgo.By("Creating pod " + hostPodName + " with host network")
		cmd := []string{"sleep", "infinity"}
		hostPod := framework.MakePod(namespaceName, hostPodName, nil, nil, f.KubeOVNImage, cmd, nil)
		hostPod.Spec.HostNetwork = true
		hostPod = podClient.CreateSync(hostPod)

		ginkgo.By("Validating client ip")
		nodeIPs := util.PodIPs(*hostPod)
		service = serviceClient.Get(serviceName)
		for _, ip := range util.ServiceClusterIPs(*service) {
			protocol := strings.ToLower(util.CheckProtocol(ip))
			ginkgo.By("Checking connection from " + hostPodName + " to " + serviceName + " via " + protocol)
			cmd := fmt.Sprintf("curl -q -s --connect-timeout 2 --max-time 2 %s/clientip", net.JoinHostPort(ip, portStr))
			ginkgo.By(fmt.Sprintf(`Executing %q in pod %s/%s`, cmd, namespaceName, hostPodName))
			output := e2epodoutput.RunHostCmdOrDie(namespaceName, hostPodName, cmd)
			client, _, err := net.SplitHostPort(strings.TrimSpace(output))
			framework.ExpectNoError(err)
			framework.ExpectContainElement(nodeIPs, client)
		}
	})
})

var _ = framework.SerialDescribe("[group:node]", func() {
	f := framework.NewDefaultFramework("node")

	var cs clientset.Interface
	var podClient *framework.PodClient
	var subnetClient *framework.SubnetClient
	var podName, namespaceName string
	ginkgo.BeforeEach(func() {
		cs = f.ClientSet
		podClient = f.PodClient()
		subnetClient = f.SubnetClient()
		namespaceName = f.Namespace.Name
		podName = "pod-" + framework.RandomSuffix()
	})
	ginkgo.AfterEach(func() {
		ginkgo.By("Deleting pod " + podName)
		podClient.DeleteSync(podName)
	})

	framework.ConformanceIt("should add missing routes on node for the join subnet", func() {
		f.SkipVersionPriorTo(1, 9, "This feature was introduced in v1.9")
		ginkgo.By("Getting join subnet")
		join := subnetClient.Get("join")

		ginkgo.By("Getting nodes")
		nodeList, err := e2enode.GetReadySchedulableNodes(context.Background(), cs)
		framework.ExpectNoError(err)

		ginkgo.By("Validating node annotations")
		node := nodeList.Items[0]
		framework.ExpectHaveKeyWithValue(node.Annotations, util.AllocatedAnnotation, "true")
		framework.ExpectHaveKeyWithValue(node.Annotations, util.CidrAnnotation, join.Spec.CIDRBlock)
		framework.ExpectHaveKeyWithValue(node.Annotations, util.GatewayAnnotation, join.Spec.Gateway)
		framework.ExpectIPInCIDR(node.Annotations[util.IPAddressAnnotation], join.Spec.CIDRBlock)
		framework.ExpectHaveKeyWithValue(node.Annotations, util.LogicalSwitchAnnotation, join.Name)

		podName = "pod-" + framework.RandomSuffix()
		ginkgo.By("Creating pod " + podName + " with host network")
		cmd := []string{"sleep", "infinity"}
		pod := framework.MakePrivilegedPod(namespaceName, podName, nil, nil, f.KubeOVNImage, cmd, nil)
		pod.Spec.NodeName = node.Name
		pod.Spec.HostNetwork = true
		pod = podClient.CreateSync(pod)

		ginkgo.By("Getting node routes on " + util.NodeNic)
		cidrs := strings.Split(join.Spec.CIDRBlock, ",")
		execFunc := func(cmd ...string) ([]byte, []byte, error) {
			return framework.KubectlExec(pod.Namespace, pod.Name, cmd...)
		}
		routes, err := iproute.RouteShow("", util.NodeNic, execFunc)
		framework.ExpectNoError(err)
		found := make([]bool, len(cidrs))
		for i, cidr := range cidrs {
			for _, route := range routes {
				if route.Dst == cidr {
					framework.Logf("Found route for cidr " + cidr + " on " + util.NodeNic)
					found[i] = true
					break
				}
			}
		}
		for i, cidr := range cidrs {
			framework.ExpectTrue(found[i], "Route for cidr "+cidr+" not found on "+util.NodeNic)
		}

		for _, cidr := range strings.Split(join.Spec.CIDRBlock, ",") {
			ginkgo.By("Deleting route for " + cidr + " on node " + node.Name)
			err = iproute.RouteDel("", cidr, execFunc)
			framework.ExpectNoError(err)
		}

		ginkgo.By("Waiting for routes for subnet " + join.Name + " to be created")
		framework.WaitUntil(2*time.Second, 10*time.Second, func(_ context.Context) (bool, error) {
			if routes, err = iproute.RouteShow("", util.NodeNic, execFunc); err != nil {
				return false, err
			}

			found = make([]bool, len(cidrs))
			for i, cidr := range cidrs {
				for _, route := range routes {
					if route.Dst == cidr {
						framework.Logf("Found route for cidr " + cidr + " on " + util.NodeNic)
						found[i] = true
						break
					}
				}
			}
			for i, cidr := range cidrs {
				if !found[i] {
					framework.Logf("Route for cidr " + cidr + " not found on " + util.NodeNic)
					return false, nil
				}
			}
			return true, nil
		}, "")

		err = podClient.Delete(podName)
		framework.ExpectNoError(err)
	})
})
