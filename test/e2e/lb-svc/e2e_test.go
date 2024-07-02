package lb_svc

import (
	"context"
	"flag"
	"fmt"
	"math/big"
	"math/rand/v2"
	"net"
	"strconv"
	"testing"
	"time"

	dockernetwork "github.com/docker/docker/api/types/network"
	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/test/e2e"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/config"
	e2enode "k8s.io/kubernetes/test/e2e/framework/node"
	e2epodoutput "k8s.io/kubernetes/test/e2e/framework/pod/output"
	"k8s.io/utils/ptr"

	"github.com/onsi/ginkgo/v2"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/docker"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/kind"
)

func init() {
	klog.SetOutput(ginkgo.GinkgoWriter)

	// Register flags.
	config.CopyFlags(config.Flags, flag.CommandLine)
	k8sframework.RegisterCommonFlags(flag.CommandLine)
	k8sframework.RegisterClusterFlags(flag.CommandLine)
}

func TestE2E(t *testing.T) {
	k8sframework.AfterReadingAllFlags(&k8sframework.TestContext)
	e2e.RunE2ETests(t)
}

func lbSvcDeploymentName(serviceName string) string {
	return "lb-svc-" + serviceName
}

var _ = framework.SerialDescribe("[group:lb-svc]", func() {
	f := framework.NewDefaultFramework("lb-svc")

	var skip bool
	var cs clientset.Interface
	var podClient *framework.PodClient
	var subnetClient *framework.SubnetClient
	var serviceClient *framework.ServiceClient
	var deploymentClient *framework.DeploymentClient
	var nadClient *framework.NetworkAttachmentDefinitionClient
	var provider, nadName, clusterName, subnetName, namespaceName, serviceName, deploymentName, serverPodName, clientPodName string
	var dockerNetwork *dockernetwork.Inspect
	var cidr, gateway string
	ginkgo.BeforeEach(func() {
		cs = f.ClientSet
		podClient = f.PodClient()
		subnetClient = f.SubnetClient()
		serviceClient = f.ServiceClient()
		deploymentClient = f.DeploymentClient()
		nadClient = f.NetworkAttachmentDefinitionClient()
		namespaceName = f.Namespace.Name
		nadName = "nad-" + framework.RandomSuffix()
		subnetName = "subnet-" + framework.RandomSuffix()
		serviceName = "service-" + framework.RandomSuffix()
		serverPodName = "pod-" + framework.RandomSuffix()
		clientPodName = "pod-" + framework.RandomSuffix()
		deploymentName = lbSvcDeploymentName(serviceName)

		if skip {
			ginkgo.Skip("lb svc spec only runs on kind clusters")
		}

		if clusterName == "" {
			ginkgo.By("Getting k8s nodes")
			k8sNodes, err := e2enode.GetReadySchedulableNodes(context.Background(), cs)
			framework.ExpectNoError(err)

			cluster, ok := kind.IsKindProvided(k8sNodes.Items[0].Spec.ProviderID)
			if !ok {
				skip = true
				ginkgo.Skip("lb svc spec only runs on kind clusters")
			}
			clusterName = cluster
		}

		if dockerNetwork == nil {
			ginkgo.By("Getting docker network " + kind.NetworkName)
			network, err := docker.NetworkInspect(kind.NetworkName)
			framework.ExpectNoError(err, "getting docker network "+kind.NetworkName)
			dockerNetwork = network
		}

		provider = fmt.Sprintf("%s.%s", nadName, namespaceName)

		ginkgo.By("Creating network attachment definition " + nadName)
		nad := framework.MakeMacvlanNetworkAttachmentDefinition(nadName, namespaceName, "eth0", "bridge", provider, nil)
		nad = nadClient.Create(nad)
		framework.Logf("created network attachment definition config:\n%s", nad.Spec.Config)

		ginkgo.By("Creating subnet " + subnetName)
		for _, config := range dockerNetwork.IPAM.Config {
			if util.CheckProtocol(config.Subnet) == apiv1.ProtocolIPv4 {
				cidr = config.Subnet
				gateway = config.Gateway
				break
			}
		}
		excludeIPs := make([]string, 0, len(dockerNetwork.Containers))
		for _, container := range dockerNetwork.Containers {
			if container.IPv4Address != "" {
				ip, _, _ := net.ParseCIDR(container.IPv4Address)
				excludeIPs = append(excludeIPs, ip.String())
			}
		}
		subnet := framework.MakeSubnet(subnetName, "", cidr, gateway, "", "", excludeIPs, nil, []string{namespaceName})
		subnet.Spec.Provider = provider
		_ = subnetClient.Create(subnet)
	})
	ginkgo.AfterEach(func() {
		ginkgo.By("Deleting pod " + clientPodName)
		podClient.DeleteSync(clientPodName)

		ginkgo.By("Deleting pod " + serverPodName)
		podClient.DeleteSync(serverPodName)

		ginkgo.By("Deleting service " + serviceName)
		serviceClient.DeleteSync(serviceName)

		ginkgo.By("Deleting deployment " + deploymentName)
		deploymentClient.DeleteSync(deploymentName)

		ginkgo.By("Deleting subnet " + subnetName)
		subnetClient.DeleteSync(subnetName)

		ginkgo.By("Deleting network attachment definition " + nadName)
		nadClient.Delete(nadName)
	})

	framework.ConformanceIt("should allocate dynamic external IP for service", func() {
		ginkgo.By("Creating server pod " + serverPodName)
		labels := map[string]string{"app": serviceName}
		port := 8000 + rand.Int32N(1000)
		args := []string{"netexec", "--http-port", strconv.Itoa(int(port))}
		serverPod := framework.MakePod(namespaceName, serverPodName, labels, nil, framework.AgnhostImage, nil, args)
		_ = podClient.CreateSync(serverPod)

		ginkgo.By("Creating service " + serviceName)
		ports := []corev1.ServicePort{{
			Name:       "tcp",
			Protocol:   corev1.ProtocolTCP,
			Port:       port,
			TargetPort: intstr.FromInt32(port),
		}}
		annotations := map[string]string{
			util.AttachmentProvider: provider,
		}
		service := framework.MakeService(serviceName, corev1.ServiceTypeLoadBalancer, annotations, labels, ports, corev1.ServiceAffinityNone)
		service.Spec.AllocateLoadBalancerNodePorts = ptr.To(false)
		service = serviceClient.CreateSync(service, func(s *corev1.Service) (bool, error) {
			return len(s.Spec.ClusterIPs) != 0, nil
		}, "cluster ips are not empty")

		ginkgo.By("Waiting for LB deployment " + deploymentName + " to be ready")
		framework.WaitUntil(2*time.Second, time.Minute, func(ctx context.Context) (bool, error) {
			_, err := deploymentClient.DeploymentInterface.Get(ctx, deploymentName, metav1.GetOptions{})
			if err == nil {
				return true, nil
			}
			ginkgo.By("deployment " + deploymentName + " still not ready")
			if k8serrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}, fmt.Sprintf("deployment %s is created", deploymentName))
		deployment := deploymentClient.Get(deploymentName)
		err := deploymentClient.WaitToComplete(deployment)
		framework.ExpectNoError(err, "deployment failed to complete")

		ginkgo.By("Getting pods for deployment " + deploymentName)
		pods, err := deploymentClient.GetPods(deployment)
		framework.ExpectNoError(err)
		framework.ExpectHaveLen(pods.Items, 1)

		ginkgo.By("Checking LB pod annotations")
		pod := &pods.Items[0]
		key := fmt.Sprintf(util.AllocatedAnnotationTemplate, provider)
		framework.ExpectHaveKeyWithValue(pod.Annotations, key, "true")
		cidrKey := fmt.Sprintf(util.CidrAnnotationTemplate, provider)
		ipKey := fmt.Sprintf(util.IPAddressAnnotationTemplate, provider)
		framework.ExpectHaveKey(pod.Annotations, cidrKey)
		framework.ExpectHaveKey(pod.Annotations, ipKey)
		lbIP := pod.Annotations[ipKey]
		framework.ExpectIPInCIDR(lbIP, pod.Annotations[cidrKey])

		ginkgo.By("Checking service status")
		framework.WaitUntil(2*time.Second, time.Minute, func(_ context.Context) (bool, error) {
			service = serviceClient.Get(serviceName)
			return len(service.Status.LoadBalancer.Ingress) != 0, nil
		}, ".status.loadBalancer.ingress is not empty")
		framework.ExpectHaveLen(service.Status.LoadBalancer.Ingress, 1)
		framework.ExpectEqual(service.Status.LoadBalancer.Ingress[0].IP, lbIP)

		ginkgo.By("Creating client pod " + clientPodName)
		annotations = map[string]string{nadv1.NetworkAttachmentAnnot: fmt.Sprintf("%s/%s", namespaceName, nadName)}
		cmd := []string{"sh", "-c", "sleep infinity"}
		clientPod := framework.MakePod(namespaceName, clientPodName, nil, annotations, f.KubeOVNImage, cmd, nil)
		clientPod = podClient.CreateSync(clientPod)

		ginkgo.By("Checking service connectivity from client pod " + clientPodName)
		curlCmd := fmt.Sprintf("curl -q -s --connect-timeout 2 --max-time 2 %s/clientip", util.JoinHostPort(lbIP, port))
		ginkgo.By(fmt.Sprintf(`Executing %q in pod %s/%s`, curlCmd, clientPod.Namespace, clientPod.Name))
		_ = e2epodoutput.RunHostCmdOrDie(clientPod.Namespace, clientPod.Name, curlCmd)

		ginkgo.By("Deleting lb svc pod " + pod.Name)
		podClient.DeleteSync(pod.Name)

		ginkgo.By("Waiting for LB deployment " + deploymentName + " to be ready")
		err = deploymentClient.WaitToComplete(deployment)
		framework.ExpectNoError(err, "deployment failed to complete")

		ginkgo.By("Getting pods for deployment " + deploymentName)
		pods, err = deploymentClient.GetPods(deployment)
		framework.ExpectNoError(err)
		framework.ExpectHaveLen(pods.Items, 1)
		lbIP = pods.Items[0].Annotations[ipKey]

		ginkgo.By("Checking service connectivity from client pod " + clientPodName)
		curlCmd = fmt.Sprintf("curl -q -s --connect-timeout 2 --max-time 2 %s/clientip", util.JoinHostPort(lbIP, port))
		framework.WaitUntil(2*time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
			ginkgo.By(fmt.Sprintf(`Executing %q in pod %s/%s`, curlCmd, clientPod.Namespace, clientPod.Name))
			_, err = e2epodoutput.RunHostCmd(clientPod.Namespace, clientPod.Name, curlCmd)
			return err == nil, nil
		}, "")

		ginkgo.By("Deleting service " + serviceName)
		serviceClient.DeleteSync(serviceName)

		ginkgo.By("Waiting for LB deployment " + deploymentName + " to be deleted automatically")
		err = deploymentClient.WaitToDisappear(deploymentName, 2*time.Second, 2*time.Minute)
		framework.ExpectNoError(err, "deployment failed to disappear")
	})

	framework.ConformanceIt("should allocate static external IP for service", func() {
		ginkgo.By("Creating server pod " + serverPodName)
		labels := map[string]string{"app": serviceName}
		port := 8000 + rand.Int32N(1000)
		args := []string{"netexec", "--http-port", strconv.Itoa(int(port))}
		serverPod := framework.MakePod(namespaceName, serverPodName, labels, nil, framework.AgnhostImage, nil, args)
		_ = podClient.CreateSync(serverPod)

		ginkgo.By("Creating service " + serviceName)
		ports := []corev1.ServicePort{{
			Name:       "tcp",
			Protocol:   corev1.ProtocolTCP,
			Port:       port,
			TargetPort: intstr.FromInt32(port),
		}}
		annotations := map[string]string{
			util.AttachmentProvider: provider,
		}
		base := util.IP2BigInt(gateway)
		lbIP := util.BigInt2Ip(base.Add(base, big.NewInt(50+rand.Int64N(50))))
		service := framework.MakeService(serviceName, corev1.ServiceTypeLoadBalancer, annotations, labels, ports, corev1.ServiceAffinityNone)
		service.Spec.LoadBalancerIP = lbIP
		service.Spec.AllocateLoadBalancerNodePorts = ptr.To(false)
		_ = serviceClient.Create(service)

		ginkgo.By("Waiting for LB deployment " + deploymentName + " to be ready")
		framework.WaitUntil(2*time.Second, time.Minute, func(ctx context.Context) (bool, error) {
			_, err := deploymentClient.DeploymentInterface.Get(ctx, deploymentName, metav1.GetOptions{})
			if err == nil {
				return true, nil
			}
			if k8serrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}, fmt.Sprintf("deployment %s is created", deploymentName))
		deployment := deploymentClient.Get(deploymentName)
		err := deploymentClient.WaitToComplete(deployment)
		framework.ExpectNoError(err, "deployment failed to complete")

		ginkgo.By("Getting pods for deployment " + deploymentName)
		pods, err := deploymentClient.GetPods(deployment)
		framework.ExpectNoError(err)
		framework.ExpectHaveLen(pods.Items, 1)

		ginkgo.By("Checking LB pod annotations")
		pod := &pods.Items[0]
		key := fmt.Sprintf(util.AllocatedAnnotationTemplate, provider)
		framework.ExpectHaveKeyWithValue(pod.Annotations, key, "true")
		ipKey := fmt.Sprintf(util.IPAddressAnnotationTemplate, provider)
		framework.ExpectHaveKeyWithValue(pod.Annotations, ipKey, lbIP)
		cidrKey := fmt.Sprintf(util.CidrAnnotationTemplate, provider)
		framework.ExpectHaveKey(pod.Annotations, cidrKey)
		framework.ExpectIPInCIDR(lbIP, pod.Annotations[cidrKey])

		ginkgo.By("Checking service status")
		framework.WaitUntil(2*time.Second, time.Minute, func(_ context.Context) (bool, error) {
			service = serviceClient.Get(serviceName)
			return len(service.Status.LoadBalancer.Ingress) != 0, nil
		}, ".status.loadBalancer.ingress is not empty")
		framework.ExpectHaveLen(service.Status.LoadBalancer.Ingress, 1)
		framework.ExpectEqual(service.Status.LoadBalancer.Ingress[0].IP, lbIP)

		ginkgo.By("Creating client pod " + clientPodName)
		annotations = map[string]string{nadv1.NetworkAttachmentAnnot: fmt.Sprintf("%s/%s", namespaceName, nadName)}
		cmd := []string{"sh", "-c", "sleep infinity"}
		clientPod := framework.MakePod(namespaceName, clientPodName, nil, annotations, f.KubeOVNImage, cmd, nil)
		clientPod = podClient.CreateSync(clientPod)

		ginkgo.By("Checking service connectivity from client pod " + clientPodName)
		curlCmd := fmt.Sprintf("curl -q -s --connect-timeout 2 --max-time 2 %s/clientip", util.JoinHostPort(lbIP, port))
		ginkgo.By(fmt.Sprintf(`Executing %q in pod %s/%s`, curlCmd, clientPod.Namespace, clientPod.Name))
		_ = e2epodoutput.RunHostCmdOrDie(clientPod.Namespace, clientPod.Name, curlCmd)

		ginkgo.By("Deleting lb svc pod " + pod.Name)
		podClient.DeleteSync(pod.Name)

		ginkgo.By("Waiting for LB deployment " + deploymentName + " to be ready")
		err = deploymentClient.WaitToComplete(deployment)
		framework.ExpectNoError(err, "deployment failed to complete")

		ginkgo.By("Checking service connectivity from client pod " + clientPodName)
		framework.WaitUntil(2*time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
			ginkgo.By(fmt.Sprintf(`Executing %q in pod %s/%s`, curlCmd, clientPod.Namespace, clientPod.Name))
			_, err = e2epodoutput.RunHostCmd(clientPod.Namespace, clientPod.Name, curlCmd)
			return err == nil, nil
		}, "")

		ginkgo.By("Deleting service " + serviceName)
		serviceClient.DeleteSync(serviceName)

		ginkgo.By("Waiting for LB deployment " + deploymentName + " to be deleted automatically")
		err = deploymentClient.WaitToDisappear(deploymentName, 2*time.Second, 2*time.Minute)
		framework.ExpectNoError(err, "deployment failed to disappear")
	})
})
