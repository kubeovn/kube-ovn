package vpc_endpoint

import (
	"context"
	"fmt"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	clientset "k8s.io/client-go/kubernetes"
	e2epodoutput "k8s.io/kubernetes/test/e2e/framework/pod/output"

	"github.com/onsi/ginkgo/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

var _ = framework.Describe("[group:vpc-endpoint]", func() {
	f := framework.NewDefaultFramework("vpc-endpoint")

	var cs clientset.Interface
	var nsClient *framework.NamespaceClient
	var vpcClient *framework.VpcClient
	var subnetClient *framework.SubnetClient
	var vesClient *framework.VpcEndpointServiceClient
	var vepClient *framework.VpcEndpointClient

	var providerNS, consumerNS string
	var providerVPC, consumerVPC string
	var providerSubnet, consumerSubnet string
	var svcName, deployName, clientPod string
	var vesName, vepName string
	var cidr string

	ginkgo.BeforeEach(func() {
		f.SkipVersionPriorTo(1, 17, "VpcEndpoint stitcher datapath requires v1.17+")
		cs = f.ClientSet
		nsClient = f.NamespaceClient()
		vpcClient = f.VpcClient()
		subnetClient = f.SubnetClient()
		vesClient = f.VpcEndpointServiceClient()
		vepClient = f.VpcEndpointClient()

		// Dual-homed stitcher pods need Multus (same dependency as vpc-nat-gw).
		if f.AttachNetClient == nil {
			ginkgo.Skip("Multus NetworkAttachmentDefinition client is not configured")
		}
		_, err := f.AttachNetClient.K8sCniCncfIoV1().NetworkAttachmentDefinitions(metav1.NamespaceSystem).List(context.Background(), metav1.ListOptions{Limit: 1})
		if err != nil {
			ginkgo.Skip(fmt.Sprintf("Multus NetworkAttachmentDefinition API unavailable: %v", err))
		}

		suffix := framework.RandomSuffix()
		providerNS = "ep-provider-" + suffix
		consumerNS = "ep-consumer-" + suffix
		providerVPC = "ep-provider-" + suffix
		consumerVPC = "ep-consumer-" + suffix
		providerSubnet = "ep-provider-subnet-" + suffix
		consumerSubnet = "ep-consumer-subnet-" + suffix
		svcName = "provider-web-" + suffix
		deployName = "provider-web-" + suffix
		clientPod = "ep-client-" + suffix
		vesName = "provider-web-eps-" + suffix
		vepName = "provider-web-ep-" + suffix
		// Overlapping tenant CIDRs are the motivating case for VPC endpoints.
		cidr = "10.210.0.0/24"
		if f.ClusterIPFamily != framework.IPv4 {
			ginkgo.Skip("VpcEndpoint overlapping-CIDR e2e currently covers IPv4 only")
		}
	})

	ginkgo.AfterEach(func() {
		if vepName != "" {
			vepClient.Delete(vepName)
		}
		if vesName != "" {
			vesClient.Delete(vesName)
		}
		if clientPod != "" && consumerNS != "" {
			_ = cs.CoreV1().Pods(consumerNS).Delete(context.Background(), clientPod, metav1.DeleteOptions{})
		}
		if svcName != "" && providerNS != "" {
			_ = cs.CoreV1().Services(providerNS).Delete(context.Background(), svcName, metav1.DeleteOptions{})
		}
		if deployName != "" && providerNS != "" {
			_ = cs.AppsV1().Deployments(providerNS).Delete(context.Background(), deployName, metav1.DeleteOptions{})
		}
		if providerSubnet != "" {
			subnetClient.Delete(providerSubnet)
		}
		if consumerSubnet != "" {
			subnetClient.Delete(consumerSubnet)
		}
		if providerVPC != "" {
			vpcClient.Delete(providerVPC)
		}
		if consumerVPC != "" {
			vpcClient.Delete(consumerVPC)
		}
		if providerNS != "" {
			nsClient.Delete(providerNS)
		}
		if consumerNS != "" {
			nsClient.Delete(consumerNS)
		}
	})

	framework.ConformanceIt("should expose a provider Service to a consumer VPC with an overlapping CIDR", func() {
		ginkgo.By("Creating provider/consumer namespaces")
		_ = nsClient.Create(framework.MakeNamespace(providerNS, map[string]string{util.VpcAnnotation: providerVPC}, map[string]string{util.VpcAnnotation: providerVPC}))
		_ = nsClient.Create(framework.MakeNamespace(consumerNS, map[string]string{util.VpcAnnotation: consumerVPC}, map[string]string{util.VpcAnnotation: consumerVPC}))

		ginkgo.By("Creating overlapping VPCs and subnets")
		_ = vpcClient.CreateSync(framework.MakeVpc(providerVPC, "", false, false, []string{providerNS}))
		_ = vpcClient.CreateSync(framework.MakeVpc(consumerVPC, "", false, false, []string{consumerNS}))
		_ = subnetClient.CreateSync(framework.MakeSubnet(providerSubnet, "", cidr, "", providerVPC, "", nil, nil, []string{providerNS}))
		_ = subnetClient.CreateSync(framework.MakeSubnet(consumerSubnet, "", cidr, "", consumerVPC, "", nil, nil, []string{consumerNS}))

		ginkgo.By("Creating provider Deployment and Service")
		labels := map[string]string{"app": deployName}
		annotations := map[string]string{
			util.VpcAnnotation:           providerVPC,
			util.LogicalSwitchAnnotation: providerSubnet,
		}
		port := int32(80)
		deploy := framework.MakeDeployment(deployName, 2, labels, annotations, "web", framework.AgnhostImage, appsv1.RollingUpdateDeploymentStrategyType)
		deploy.Spec.Template.Spec.Containers[0].Args = []string{"netexec", "--http-port", strconv.Itoa(int(port))}
		deploy.Spec.Template.Spec.Containers[0].Ports = []corev1.ContainerPort{{ContainerPort: port}}
		_ = f.DeploymentClientNS(providerNS).CreateSync(deploy)

		svc := framework.MakeService(svcName, corev1.ServiceTypeClusterIP, map[string]string{
			util.VpcAnnotation:           providerVPC,
			util.LogicalSwitchAnnotation: providerSubnet,
		}, labels, []corev1.ServicePort{{
			Name:       "http",
			Protocol:   corev1.ProtocolTCP,
			Port:       port,
			TargetPort: intstr.FromInt32(port),
		}}, corev1.ServiceAffinityNone)
		svcClient := f.ServiceClientNS(providerNS)
		_ = svcClient.CreateSync(svc, func(s *corev1.Service) (bool, error) {
			return s.Spec.ClusterIP != "" && s.Spec.ClusterIP != corev1.ClusterIPNone, nil
		}, "cluster IP assigned")

		ginkgo.By("Publishing VpcEndpointService and consuming via VpcEndpoint")
		ves := vesClient.CreateSync(framework.MakeVpcEndpointService(vesName, providerVPC, providerNS, svcName, []string{consumerVPC}))
		framework.ExpectNotEmpty(ves.Status.TransitVIP)

		vep := vepClient.CreateSync(framework.MakeVpcEndpoint(vepName, consumerVPC, consumerSubnet, vesName))
		framework.ExpectNotEmpty(vep.Status.LocalVIP)
		framework.ExpectEqual(vep.Status.TransitVIP, ves.Status.TransitVIP)

		ginkgo.By("Creating consumer client and curling LocalVIP")
		clientAnnotations := map[string]string{
			util.VpcAnnotation:           consumerVPC,
			util.LogicalSwitchAnnotation: consumerSubnet,
		}
		pod := framework.MakePod(consumerNS, clientPod, nil, clientAnnotations, framework.AgnhostImage, nil, nil)
		pod = f.PodClientNS(consumerNS).CreateSync(pod)

		cmd := fmt.Sprintf("curl -s -o /dev/null -w '%%{http_code}' --connect-timeout 3 http://%s/", vep.Status.LocalVIP)
		output := e2epodoutput.RunHostCmdOrDie(pod.Namespace, pod.Name, cmd)
		framework.ExpectEqual(output, "200")

		ginkgo.By("Checking stitcher ConfigMap is managed by the controller")
		_, err := cs.CoreV1().ConfigMaps(framework.KubeOvnNamespace).Get(context.Background(), "vpc-endpoint-stitcher", metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			_, err = cs.CoreV1().ConfigMaps(providerNS).Get(context.Background(), "vpc-endpoint-stitcher", metav1.GetOptions{})
		}
		framework.ExpectNoError(err)
	})
})
