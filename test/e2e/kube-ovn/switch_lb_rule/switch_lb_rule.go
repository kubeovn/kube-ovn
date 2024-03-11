package switch_lb_rule

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/onsi/ginkgo/v2"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	e2epodoutput "k8s.io/kubernetes/test/e2e/framework/pod/output"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

func generateSwitchLBRuleName(ruleName string) string {
	return "lr-" + ruleName
}

func generateServiceName(slrName string) string {
	return "slr-" + slrName
}

func generateVpcName(name string) string {
	return "vpc-" + name
}

func generateSubnetName(name string) string {
	return "subnet-" + name
}

func curlSvc(f *framework.Framework, clientPodName, vip string, port int32) {
	cmd := fmt.Sprintf("curl %s", util.JoinHostPort(vip, port))
	ginkgo.By(fmt.Sprintf(`Executing %q in pod %s/%s`, cmd, f.Namespace.Name, clientPodName))
	e2epodoutput.RunHostCmdOrDie(f.Namespace.Name, clientPodName, cmd)
}

var _ = framework.Describe("[group:slr]", func() {
	f := framework.NewDefaultFramework("slr")

	var (
		switchLBRuleClient *framework.SwitchLBRuleClient
		endpointsClient    *framework.EndpointsClient
		serviceClient      *framework.ServiceClient
		stsClient          *framework.StatefulSetClient
		podClient          *framework.PodClient
		subnetClient       *framework.SubnetClient
		vpcClient          *framework.VpcClient

		namespaceName, suffix                     string
		vpcName, subnetName, clientPodName, label string
		stsName, stsSvcName                       string
		selSlrName, selSvcName                    string
		epSlrName, epSvcName                      string
		overlaySubnetCidr, vip                    string
		// TODO:// slr support dual-stack
		frontPort, selSlrFrontPort, epSlrFrontPort, backendPort int32
	)

	ginkgo.BeforeEach(func() {
		switchLBRuleClient = f.SwitchLBRuleClient()
		endpointsClient = f.EndpointClient()
		serviceClient = f.ServiceClient()
		stsClient = f.StatefulSetClient()
		podClient = f.PodClient()
		subnetClient = f.SubnetClient()
		vpcClient = f.VpcClient()

		suffix = framework.RandomSuffix()

		namespaceName = f.Namespace.Name
		selSlrName = "sel-" + generateSwitchLBRuleName(suffix)
		selSvcName = generateServiceName(selSlrName)
		epSlrName = "ep-" + generateSwitchLBRuleName(suffix)
		epSvcName = generateServiceName(epSlrName)
		stsName = "sts-" + suffix
		stsSvcName = stsName
		label = "slr"
		clientPodName = "client-" + suffix
		subnetName = generateSubnetName(suffix)
		vpcName = generateVpcName(suffix)
		frontPort = 8090
		selSlrFrontPort = 8091
		epSlrFrontPort = 8092
		backendPort = 80
		vip = ""
		overlaySubnetCidr = framework.RandomCIDR(f.ClusterIPFamily)
		ginkgo.By("Creating custom vpc")
		vpc := framework.MakeVpc(vpcName, "", false, false, []string{namespaceName})
		_ = vpcClient.CreateSync(vpc)
		ginkgo.By("Creating custom overlay subnet")
		overlaySubnet := framework.MakeSubnet(subnetName, "", overlaySubnetCidr, "", vpcName, "", nil, nil, nil)
		_ = subnetClient.CreateSync(overlaySubnet)
		annotations := map[string]string{
			util.LogicalSwitchAnnotation: subnetName,
		}
		ginkgo.By("Creating client pod " + clientPodName)
		clientPod := framework.MakePod(namespaceName, clientPodName, nil, annotations, framework.AgnhostImage, nil, nil)
		podClient.CreateSync(clientPod)
	})

	ginkgo.AfterEach(func() {
		ginkgo.By("Deleting client pod " + clientPodName)
		podClient.DeleteSync(clientPodName)
		ginkgo.By("Deleting statefulset " + stsName)
		stsClient.DeleteSync(stsName)
		ginkgo.By("Deleting service " + stsSvcName)
		serviceClient.DeleteSync(stsSvcName)
		ginkgo.By("Deleting switch-lb-rule " + selSlrName)
		switchLBRuleClient.DeleteSync(selSlrName)
		ginkgo.By("Deleting switch-lb-rule " + epSlrName)
		switchLBRuleClient.DeleteSync(epSlrName)
		ginkgo.By("Deleting subnet " + subnetName)
		subnetClient.DeleteSync(subnetName)
		ginkgo.By("Deleting vpc " + vpcName)
		vpcClient.DeleteSync(vpcName)
	})

	framework.ConformanceIt("should access sts and slr svc ok", func() {
		f.SkipVersionPriorTo(1, 12, "This feature was introduced in v1.12")
		ginkgo.By("1. Creating sts svc with slr")
		var (
			clientPod             *corev1.Pod
			err                   error
			stsSvc, selSvc, epSvc *corev1.Service
			selSlrEps, epSlrEps   *corev1.Endpoints
		)
		replicas := 1
		labels := map[string]string{"app": label}
		ginkgo.By("Creating statefulset " + stsName + " with subnet " + subnetName)
		sts := framework.MakeStatefulSet(stsName, stsSvcName, int32(replicas), labels, framework.AgnhostImage)
		ginkgo.By("Creating sts " + stsName)
		sts.Spec.Template.Annotations = map[string]string{
			util.LogicalSwitchAnnotation: subnetName,
		}
		portStr := strconv.Itoa(80)
		webServerCmd := []string{"/agnhost", "netexec", "--http-port", portStr}
		sts.Spec.Template.Spec.Containers[0].Command = webServerCmd
		_ = stsClient.CreateSync(sts)
		ginkgo.By("Creating service " + stsSvcName)
		ports := []corev1.ServicePort{{
			Name:       "http",
			Protocol:   corev1.ProtocolTCP,
			Port:       frontPort,
			TargetPort: intstr.FromInt32(80),
		}}
		selector := map[string]string{"app": label}
		annotations := map[string]string{
			util.LogicalSwitchAnnotation: subnetName,
		}
		stsSvc = framework.MakeService(stsSvcName, corev1.ServiceTypeClusterIP, annotations, selector, ports, corev1.ServiceAffinityNone)
		stsSvc = serviceClient.CreateSync(stsSvc, func(s *corev1.Service) (bool, error) {
			return len(s.Spec.ClusterIPs) != 0, nil
		}, "cluster ips are not empty")

		ginkgo.By("Waiting for sts service " + stsSvcName + " to be ready")
		framework.WaitUntil(2*time.Second, time.Minute, func(_ context.Context) (bool, error) {
			stsSvc, err = serviceClient.ServiceInterface.Get(context.TODO(), stsSvcName, metav1.GetOptions{})
			if err == nil {
				return true, nil
			}
			if k8serrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}, fmt.Sprintf("service %s is created", stsSvcName))
		framework.ExpectNotNil(stsSvc)

		ginkgo.By("Get client pod " + clientPodName)
		clientPod, err = podClient.Get(context.TODO(), clientPodName, metav1.GetOptions{})
		framework.ExpectNil(err)
		framework.ExpectNotNil(clientPod)
		ginkgo.By("Checking sts service " + stsSvc.Name)
		for _, ip := range stsSvc.Spec.ClusterIPs {
			curlSvc(f, clientPodName, ip, frontPort)
		}
		vip = stsSvc.Spec.ClusterIP

		ginkgo.By("2. Creating switch-lb-rule with selector with lb front vip " + vip)
		ginkgo.By("Creating selector SwitchLBRule " + epSlrName)
		var (
			selRule           *kubeovnv1.SwitchLBRule
			slrSlector        []string
			slrPorts, epPorts []kubeovnv1.SlrPort
			sessionAffinity   corev1.ServiceAffinity
		)
		sessionAffinity = corev1.ServiceAffinityNone
		slrPorts = []kubeovnv1.SlrPort{
			{
				Name:       "http",
				Port:       selSlrFrontPort,
				TargetPort: backendPort,
				Protocol:   "TCP",
			},
		}
		slrSlector = []string{fmt.Sprintf("app:%s", label)}
		selRule = framework.MakeSwitchLBRule(selSlrName, namespaceName, vip, sessionAffinity, nil, slrSlector, nil, slrPorts)
		_ = switchLBRuleClient.Create(selRule)

		ginkgo.By("Waiting for switch-lb-rule " + selSlrName + " to be ready")
		framework.WaitUntil(2*time.Second, time.Minute, func(_ context.Context) (bool, error) {
			_, err = switchLBRuleClient.SwitchLBRuleInterface.Get(context.TODO(), selSlrName, metav1.GetOptions{})
			if err == nil {
				return true, nil
			}
			if k8serrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}, fmt.Sprintf("switch-lb-rule %s is created", selSlrName))

		ginkgo.By("Waiting for headless service " + selSvcName + " to be ready")
		framework.WaitUntil(2*time.Second, time.Minute, func(_ context.Context) (bool, error) {
			selSvc, err = serviceClient.ServiceInterface.Get(context.TODO(), selSvcName, metav1.GetOptions{})
			if err == nil {
				return true, nil
			}
			if k8serrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}, fmt.Sprintf("service %s is created", selSvcName))
		framework.ExpectNotNil(selSvc)

		ginkgo.By("Waiting for endpoints " + selSvcName + " to be ready")
		framework.WaitUntil(2*time.Second, time.Minute, func(_ context.Context) (bool, error) {
			selSlrEps, err = endpointsClient.EndpointsInterface.Get(context.TODO(), selSvcName, metav1.GetOptions{})
			if err == nil {
				return true, nil
			}
			if k8serrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}, fmt.Sprintf("endpoints %s is created", selSvcName))
		framework.ExpectNotNil(selSlrEps)

		pods := stsClient.GetPods(sts)
		framework.ExpectHaveLen(pods.Items, replicas)

		for i, subset := range selSlrEps.Subsets {
			var (
				ips       []string
				tps       []int32
				protocols = make(map[int32]string)
			)

			ginkgo.By("Checking endpoint address")
			for _, address := range subset.Addresses {
				ips = append(ips, address.IP)
			}
			framework.ExpectContainElement(ips, pods.Items[i].Status.PodIP)

			ginkgo.By("Checking endpoint ports")
			for _, port := range subset.Ports {
				tps = append(tps, port.Port)
				protocols[port.Port] = string(port.Protocol)
			}
			for _, port := range slrPorts {
				framework.ExpectContainElement(tps, port.TargetPort)
				framework.ExpectHaveKeyWithValue(protocols, port.TargetPort, port.Protocol)
			}
		}

		ginkgo.By("Checking selector switch lb service " + selSvc.Name)
		curlSvc(f, clientPodName, vip, selSlrFrontPort)

		ginkgo.By("3. Creating switch-lb-rule with endpoints with lb front vip " + vip)
		ginkgo.By("Creating endpoint SwitchLBRule " + epSlrName)
		sessionAffinity = corev1.ServiceAffinityClientIP
		epPorts = []kubeovnv1.SlrPort{
			{
				Name:       "http",
				Port:       epSlrFrontPort,
				TargetPort: backendPort,
				Protocol:   "TCP",
			},
		}
		presetEndpoints := []string{}
		for _, pod := range pods.Items {
			presetEndpoints = append(presetEndpoints, pod.Status.PodIP)
		}
		epRule := framework.MakeSwitchLBRule(epSlrName, namespaceName, vip, sessionAffinity, annotations, nil, presetEndpoints, epPorts)
		_ = switchLBRuleClient.Create(epRule)

		ginkgo.By("Waiting for switch-lb-rule " + epSlrName + " to be ready")
		framework.WaitUntil(2*time.Second, time.Minute, func(_ context.Context) (bool, error) {
			_, err := switchLBRuleClient.SwitchLBRuleInterface.Get(context.TODO(), epSlrName, metav1.GetOptions{})
			if err == nil {
				return true, nil
			}
			if k8serrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}, fmt.Sprintf("switch-lb-rule %s is created", epSlrName))

		ginkgo.By("Waiting for headless service " + epSvcName + " to be ready")
		framework.WaitUntil(2*time.Second, time.Minute, func(_ context.Context) (bool, error) {
			epSvc, err = serviceClient.ServiceInterface.Get(context.TODO(), epSvcName, metav1.GetOptions{})
			if err == nil {
				return true, nil
			}
			if k8serrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}, fmt.Sprintf("service %s is created", epSvcName))
		framework.ExpectNotNil(epSvc)

		ginkgo.By("Waiting for endpoints " + epSvcName + " to be ready")
		framework.WaitUntil(2*time.Second, time.Minute, func(_ context.Context) (bool, error) {
			epSlrEps, err = endpointsClient.EndpointsInterface.Get(context.TODO(), epSvcName, metav1.GetOptions{})
			if err == nil {
				return true, nil
			}
			if k8serrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}, fmt.Sprintf("endpoints %s is created", epSvcName))
		framework.ExpectNotNil(epSlrEps)

		for i, subset := range epSlrEps.Subsets {
			var (
				ips       []string
				tps       []int32
				protocols = make(map[int32]string)
			)

			ginkgo.By("Checking endpoint address")
			for _, address := range subset.Addresses {
				ips = append(ips, address.IP)
			}
			framework.ExpectContainElement(ips, pods.Items[i].Status.PodIP)

			ginkgo.By("Checking endpoint ports")
			for _, port := range subset.Ports {
				tps = append(tps, port.Port)
				protocols[port.Port] = string(port.Protocol)
			}
			for _, port := range epPorts {
				framework.ExpectContainElement(tps, port.TargetPort)
				framework.ExpectHaveKeyWithValue(protocols, port.TargetPort, port.Protocol)
			}
		}
		ginkgo.By("Checking endpoint switch lb service " + epSvc.Name)
		curlSvc(f, clientPodName, vip, epSlrFrontPort)
	})
})
