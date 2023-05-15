package lb_svc

import (
	"context"
	"flag"
	"fmt"
	"math/big"
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	dockertypes "github.com/docker/docker/api/types"
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

	"github.com/onsi/ginkgo/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/docker"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/kind"
)

const subnetProvider = "lb-svc-attachment.kube-system"

func init() {
	klog.SetOutput(ginkgo.GinkgoWriter)

	// Register flags.
	config.CopyFlags(config.Flags, flag.CommandLine)
	k8sframework.RegisterCommonFlags(flag.CommandLine)
	k8sframework.RegisterClusterFlags(flag.CommandLine)
}

func TestE2E(t *testing.T) {
	if k8sframework.TestContext.KubeConfig == "" {
		k8sframework.TestContext.KubeConfig = filepath.Join(os.Getenv("HOME"), ".kube", "config")
	}
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
	var subnetClient *framework.SubnetClient
	var serviceClient *framework.ServiceClient
	var deploymentClient *framework.DeploymentClient
	var clusterName, subnetName, namespaceName, serviceName, deploymentName string
	var dockerNetwork *dockertypes.NetworkResource
	var cidr, gateway string
	ginkgo.BeforeEach(func() {
		cs = f.ClientSet
		subnetClient = f.SubnetClient()
		serviceClient = f.ServiceClient()
		deploymentClient = f.DeploymentClient()
		namespaceName = f.Namespace.Name
		subnetName = "subnet-" + framework.RandomSuffix()
		serviceName = "service-" + framework.RandomSuffix()
		deploymentName = lbSvcDeploymentName(serviceName)

		if skip {
			ginkgo.Skip("lb svc spec only runs on kind clusters")
		}

		if clusterName == "" {
			ginkgo.By("Getting k8s nodes")
			k8sNodes, err := e2enode.GetReadySchedulableNodes(cs)
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

		ginkgo.By("Creating subnet " + subnetName)
		for _, config := range dockerNetwork.IPAM.Config {
			if !strings.ContainsRune(config.Subnet, ':') {
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
		subnet.Spec.Provider = subnetProvider
		_ = subnetClient.Create(subnet)
	})
	ginkgo.AfterEach(func() {
		ginkgo.By("Deleting deployment " + deploymentName)
		deploymentClient.DeleteSync(deploymentName)

		ginkgo.By("Deleting service " + serviceName)
		serviceClient.DeleteSync(serviceName)

		ginkgo.By("Deleting subnet " + subnetName)
		subnetClient.DeleteSync(subnetName)
	})

	framework.ConformanceIt("should allocate dynamic external IP for service", func() {
		ginkgo.By("Creating service " + serviceName)
		ports := []corev1.ServicePort{{
			Name:       "tcp",
			Protocol:   corev1.ProtocolTCP,
			Port:       80,
			TargetPort: intstr.FromInt(80),
		}}
		annotations := map[string]string{
			subnetProvider + ".kubernetes.io/logical_switch": subnetName,
		}
		selector := map[string]string{"app": "lb-svc-dynamic"}
		service := framework.MakeService(serviceName, corev1.ServiceTypeLoadBalancer, annotations, selector, ports, corev1.ServiceAffinityNone)
		_ = serviceClient.CreateSync(service, func(s *corev1.Service) (bool, error) {
			return len(s.Spec.ClusterIPs) != 0, nil
		}, "cluster ips are not empty")

		ginkgo.By("Waiting for deployment " + deploymentName + " to be ready")
		framework.WaitUntil(func() (bool, error) {
			_, err := deploymentClient.DeploymentInterface.Get(context.TODO(), deploymentName, metav1.GetOptions{})
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

		ginkgo.By("Checking pod annotations")
		key := fmt.Sprintf(util.AllocatedAnnotationTemplate, subnetProvider)
		framework.ExpectHaveKeyWithValue(pods.Items[0].Annotations, key, "true")
		cidrKey := fmt.Sprintf(util.CidrAnnotationTemplate, subnetProvider)
		ipKey := fmt.Sprintf(util.IpAddressAnnotationTemplate, subnetProvider)
		framework.ExpectHaveKey(pods.Items[0].Annotations, cidrKey)
		framework.ExpectHaveKey(pods.Items[0].Annotations, ipKey)
		cidr := pods.Items[0].Annotations[cidrKey]
		ip := pods.Items[0].Annotations[ipKey]
		framework.ExpectTrue(util.CIDRContainIP(cidr, ip))

		ginkgo.By("Checking service external IP")
		framework.WaitUntil(func() (bool, error) {
			service = serviceClient.Get(serviceName)
			return len(service.Status.LoadBalancer.Ingress) != 0, nil
		}, ".status.loadBalancer.ingress is not empty")
		framework.ExpectEqual(service.Status.LoadBalancer.Ingress[0].IP, ip)
	})

	framework.ConformanceIt("should allocate static external IP for service", func() {
		ginkgo.By("Creating service " + serviceName)
		base := util.Ip2BigInt(gateway)
		lbIP := util.BigInt2Ip(base.Add(base, big.NewInt(50+rand.Int63n(50))))
		ports := []corev1.ServicePort{{
			Name:       "tcp",
			Protocol:   corev1.ProtocolTCP,
			Port:       80,
			TargetPort: intstr.FromInt(80),
		}}
		annotations := map[string]string{
			subnetProvider + ".kubernetes.io/logical_switch": subnetName,
		}
		selector := map[string]string{"app": "lb-svc-static"}
		service := framework.MakeService(serviceName, corev1.ServiceTypeLoadBalancer, annotations, selector, ports, corev1.ServiceAffinityNone)
		service.Spec.LoadBalancerIP = lbIP
		_ = serviceClient.Create(service)

		ginkgo.By("Waiting for deployment " + deploymentName + " to be ready")
		framework.WaitUntil(func() (bool, error) {
			_, err := deploymentClient.DeploymentInterface.Get(context.TODO(), deploymentName, metav1.GetOptions{})
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

		ginkgo.By("Checking pod annotations")
		key := fmt.Sprintf(util.AllocatedAnnotationTemplate, subnetProvider)
		framework.ExpectHaveKeyWithValue(pods.Items[0].Annotations, key, "true")
		ipKey := fmt.Sprintf(util.IpAddressAnnotationTemplate, subnetProvider)
		framework.ExpectHaveKeyWithValue(pods.Items[0].Annotations, ipKey, lbIP)
		cidr := pods.Items[0].Annotations[fmt.Sprintf(util.CidrAnnotationTemplate, subnetProvider)]
		framework.ExpectTrue(util.CIDRContainIP(cidr, lbIP))

		ginkgo.By("Checking service external IP")
		framework.WaitUntil(func() (bool, error) {
			service = serviceClient.Get(serviceName)
			return len(service.Status.LoadBalancer.Ingress) != 0, nil
		}, ".status.loadBalancer.ingress is not empty")
		framework.ExpectEqual(service.Status.LoadBalancer.Ingress[0].IP, lbIP)
	})
})
