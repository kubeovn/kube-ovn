package lb_svc

import (
	"context"
	"flag"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	dockertypes "github.com/docker/docker/api/types"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/test/e2e"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/config"
	"k8s.io/kubernetes/test/e2e/framework/deployment"
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

	// Parse all the flags
	flag.Parse()
	if k8sframework.TestContext.KubeConfig == "" {
		k8sframework.TestContext.KubeConfig = filepath.Join(os.Getenv("HOME"), ".kube", "config")
	}
	k8sframework.AfterReadingAllFlags(&k8sframework.TestContext)
}

func TestE2E(t *testing.T) {
	e2e.RunE2ETests(t)
}

func lbSvcDeploymentName(serviceName string) string {
	return "lb-svc-" + serviceName
}

var _ = framework.Describe("[group:lb-svc]", func() {
	f := framework.NewDefaultFramework("lb-svc")

	var skip bool
	var cs clientset.Interface
	var subnetClient *framework.SubnetClient
	var serviceClient *framework.ServiceClient
	var clusterName, subnetName, namespaceName, serviceName string
	var dockerNetwork *dockertypes.NetworkResource
	var cidr, gateway string
	ginkgo.BeforeEach(func() {
		cs = f.ClientSet
		subnetClient = f.SubnetClient()
		serviceClient = f.ServiceClient()
		namespaceName = f.Namespace.Name
		subnetName = "subnet-" + framework.RandomSuffix()
		serviceName = "service-" + framework.RandomSuffix()

		if skip {
			ginkgo.Skip("underlay spec only runs on kind clusters")
		}

		if clusterName == "" {
			ginkgo.By("Getting k8s nodes")
			k8sNodes, err := e2enode.GetReadySchedulableNodes(cs)
			framework.ExpectNoError(err)

			cluster, ok := kind.IsKindProvided(k8sNodes.Items[0].Spec.ProviderID)
			if !ok {
				skip = true
				ginkgo.Skip("underlay spec only runs on kind clusters")
			}
			clusterName = cluster
		}

		if dockerNetwork == nil {
			ginkgo.By("Getting docker network " + kind.NetworkName)
			network, err := docker.NetworkGet(kind.NetworkName)
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
				excludeIPs = append(excludeIPs, container.IPv4Address)
			}
		}
		subnet := framework.MakeSubnet(subnetName, "", cidr, gateway, excludeIPs, nil, []string{namespaceName})
		subnet.Spec.Provider = subnetProvider
		_ = subnetClient.Create(subnet)
	})
	ginkgo.AfterEach(func() {
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
		_ = serviceClient.CreateSync(service)

		ginkgo.By("Waiting for 5 seconds")
		time.Sleep(5 * time.Second)

		deploymentName := lbSvcDeploymentName(serviceName)
		ginkgo.By("Getting deployment " + deploymentName)
		deploy, err := cs.AppsV1().Deployments(namespaceName).Get(context.Background(), deploymentName, metav1.GetOptions{})
		framework.ExpectNoError(err, "failed to get deployment")
		framework.ExpectEqual(deploy.Status.AvailableReplicas, int32(1))

		ginkgo.By("Waiting for deployment " + deploymentName + " to be ready")
		err = deployment.WaitForDeploymentComplete(cs, deploy)
		framework.ExpectNoError(err, "deployment failed to complete")

		ginkgo.By("Getting pods for deployment " + deploymentName)
		pods, err := deployment.GetPodsForDeployment(cs, deploy)
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
		service = serviceClient.Get(serviceName)
		framework.ExpectNotEmpty(service.Status.LoadBalancer.Ingress)
		framework.ExpectEqual(service.Status.LoadBalancer.Ingress[0].IP, ip)
	})

	framework.ConformanceIt("should allocate static external IP for service", func() {
		ginkgo.By("Creating service " + serviceName)
		base := util.Ip2BigInt(gateway)
		lbIP := util.BigInt2Ip(base.Add(base, big.NewInt(100)))
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

		ginkgo.By("Waiting for 10 seconds")
		time.Sleep(10 * time.Second)

		deploymentName := lbSvcDeploymentName(serviceName)
		ginkgo.By("Getting deployment " + deploymentName)
		deploy, err := cs.AppsV1().Deployments(namespaceName).Get(context.Background(), deploymentName, metav1.GetOptions{})
		framework.ExpectNoError(err, "failed to get deployment")
		framework.ExpectEqual(deploy.Status.AvailableReplicas, int32(1))

		ginkgo.By("Waiting for deployment " + deploymentName + " to be ready")
		err = deployment.WaitForDeploymentComplete(cs, deploy)
		framework.ExpectNoError(err, "deployment failed to complete")

		ginkgo.By("Getting pods for deployment " + deploymentName)
		pods, err := deployment.GetPodsForDeployment(cs, deploy)
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
		service = serviceClient.Get(serviceName)
		framework.ExpectNotEmpty(service.Status.LoadBalancer.Ingress)
		framework.ExpectEqual(service.Status.LoadBalancer.Ingress[0].IP, lbIP)
	})
})
