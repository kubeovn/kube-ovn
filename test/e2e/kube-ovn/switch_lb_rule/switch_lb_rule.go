package switch_lb_rule

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	clientset "k8s.io/client-go/kubernetes"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	"github.com/onsi/ginkgo/v2"
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

func netcatSvc(f *framework.Framework, clientPodName, slrVip string, port int32, svc *corev1.Service, isSlr bool) string {
	var stsV4IP, stsV6IP, v4cmd, v6cmd, vip string
	if f.IsIPv6() {
		if !isSlr {
			stsV6IP = svc.Spec.ClusterIPs[0]
			vip = stsV6IP
		} else {
			stsV6IP = slrVip
		}
		v6cmd = fmt.Sprintf("nc -6nvz %s %d", stsV6IP, port)
		ginkgo.By("Waiting for client pod " + clientPodName + " " + v6cmd + " to be ok")
		netcat(f, clientPodName, v6cmd)
	} else if f.IsIPv4() {
		if !isSlr {
			stsV4IP = svc.Spec.ClusterIPs[0]
			vip = stsV4IP
		} else {
			stsV4IP = slrVip
		}
		v4cmd = fmt.Sprintf("nc -nvz %s %d", stsV4IP, port)
		ginkgo.By("Waiting for client pod " + clientPodName + " " + v4cmd + " to be ok")
		netcat(f, clientPodName, v4cmd)
	} else {
		if !isSlr {
			stsV4IP := svc.Spec.ClusterIPs[0]
			vip = stsV4IP
			v4cmd = fmt.Sprintf("nc -nvz %s %d", stsV4IP, port)
			ginkgo.By("Waiting for client pod " + clientPodName + " " + v4cmd + " to be ok")
			netcat(f, clientPodName, v4cmd)
			if !isSlr {
				stsV6IP = svc.Spec.ClusterIPs[1]
				v6cmd = fmt.Sprintf("nc -6nvz %s %d", stsV6IP, port)
				ginkgo.By("Waiting for client pod " + clientPodName + " " + v6cmd + " to be ok")
				netcat(f, clientPodName, v6cmd)
			}
		} else {
			// TODO:// slr support dual-stack
			stsV4IP = slrVip
			v4cmd = fmt.Sprintf("nc -nvz %s %d", stsV4IP, port)
			ginkgo.By("Waiting for client pod " + clientPodName + " " + v4cmd + " to be ok")
			netcat(f, clientPodName, v4cmd)
		}
	}
	return vip
}
func netcat(f *framework.Framework, clientPodName, cmd string) {
	framework.Logf("testing %s", cmd)
	stdOutput, errOutput, err := framework.ExecShellInPod(context.Background(), f, clientPodName, cmd)
	if err == nil {
		framework.Logf("tcp netcat %s successfully", cmd)
		framework.Logf("output:\n%s", stdOutput)
	} else {
		err = fmt.Errorf("failed to tcp netcat %s ", cmd)
		framework.Logf("output:\n%s", stdOutput)
		framework.Logf("errOutput:\n%s", errOutput)
		framework.ExpectNoError(err)
	}
}

var _ = framework.Describe("[group:slr]", func() {
	f := framework.NewDefaultFramework("slr")

	var (
		switchLBRuleClient *framework.SwitchLBRuleClient
		endpointsClient    *framework.EndpointsClient
		serviceClient      *framework.ServiceClient
		stsClient          *framework.StatefulSetClient
		podClient          *framework.PodClient
		clientset          clientset.Interface
		subnetClient       *framework.SubnetClient
		vpcClient          *framework.VpcClient

		namespaceName, ovnImg, podImg, suffix     string
		vpcName, subnetName, clientPodName, label string
		stsName, stsSvcName                       string
		selSlrName, selSvcName                    string
		epSlrName, epSvcName                      string
		overlaySubnetV4Cidr, vip                  string
		// TODO:// slr support dual-stack
		frontPort, selSlrFrontPort, epSlrFrontPort, backendPort int32
	)

	ginkgo.BeforeEach(func() {
		switchLBRuleClient = f.SwitchLBRuleClient()
		endpointsClient = f.EndpointClient()
		serviceClient = f.ServiceClient()
		stsClient = f.StatefulSetClient()
		podClient = f.PodClient()
		clientset = f.ClientSet
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
		overlaySubnetV4Cidr = framework.RandomCIDR(f.ClusterIpFamily)
		var (
			clientPod *corev1.Pod
			command   []string
			labels    map[string]string
		)
		if ovnImg == "" {
			ovnImg = framework.GetKubeOvnImage(clientset)
			version := strings.Split(ovnImg, ":")[1]
			podImg = fmt.Sprintf(framework.BaseImageTemp, version)
		}
		ginkgo.By("Creating custom vpc")
		vpc := framework.MakeVpc(vpcName, "", false, false, []string{namespaceName})
		_ = vpcClient.CreateSync(vpc)
		ginkgo.By("Creating custom overlay subnet")
		overlaySubnet := framework.MakeSubnet(subnetName, "", overlaySubnetV4Cidr, "", vpcName, "", nil, nil, nil)
		_ = subnetClient.CreateSync(overlaySubnet)
		labels = map[string]string{"app": "client"}
		annotations := map[string]string{
			util.LogicalSwitchAnnotation: subnetName,
		}
		ginkgo.By("Creating nc client pod " + clientPodName)
		command = []string{"sh", "-c", "sleep infinity"}
		clientPod = framework.MakePod(namespaceName, clientPodName, labels, annotations, podImg, command, nil)
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
		f.SkipVersionPriorTo(1, 12, "This feature was introduce in v1.12")
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
		sts := framework.MakeStatefulSet(stsName, stsSvcName, int32(replicas), labels, podImg)
		pool := framework.RandomIPs(overlaySubnetV4Cidr, ";", replicas)
		ginkgo.By("Creating sts " + stsName + " with ip pool " + pool)
		sts.Spec.Template.Annotations = map[string]string{
			util.LogicalSwitchAnnotation: subnetName,
			util.IpPoolAnnotation:        pool,
		}
		if f.IsIPv4() {
			sts.Spec.Template.Spec.Containers[0].Command = []string{"sh", "-c", fmt.Sprintf("cd /tmp && python3 -m http.server %d", backendPort)}
		}
		if f.IsIPv6() {
			sts.Spec.Template.Spec.Containers[0].Command = []string{"sh", "-c", fmt.Sprintf("cd /tmp && python3 -m http.server --bind :: %d", backendPort)}
		}
		if f.IsDual() {
			ipSplits := strings.Split(pool, ",")
			sts.Spec.Template.Spec.Containers[0].Command = []string{"sh", "-c", fmt.Sprintf("cd /tmp && python3 -m http.server --bind %s %d", ipSplits[0], backendPort)}
			// add ipv6 container
			sts.Spec.Template.Spec.Containers = append(sts.Spec.Template.Spec.Containers, sts.Spec.Template.Spec.Containers[0])
			sts.Spec.Template.Spec.Containers[1].Command = []string{"sh", "-c", fmt.Sprintf("cd /tmp && python3 -m http.server --bind %s %d", ipSplits[1], backendPort)}
			sts.Spec.Template.Spec.Containers[1].Name = "webv6"
			sts.Spec.Template.Spec.Containers[0].Env = []corev1.EnvVar{
				{
					Name:  "IPV4_ADDR",
					Value: ipSplits[0],
				},
			}
			sts.Spec.Template.Spec.Containers[1].Env = []corev1.EnvVar{
				{
					Name:  "IPV6_ADDR",
					Value: ipSplits[1],
				},
			}
		}
		_ = stsClient.CreateSync(sts)
		ginkgo.By("Creating service " + stsSvcName)
		ports := []corev1.ServicePort{{
			Name:       "netcat",
			Protocol:   corev1.ProtocolTCP,
			Port:       frontPort,
			TargetPort: intstr.FromInt(80),
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
		ginkgo.By("Netcating sts service " + stsSvc.Name)
		vip = netcatSvc(f, clientPodName, "", frontPort, stsSvc, false)

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
				Name:       "netcat",
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
				framework.ExpectEqual(protocols[port.TargetPort], port.Protocol)
			}
		}

		ginkgo.By("Netcating selector switch lb service " + selSvc.Name)
		netcatSvc(f, clientPodName, vip, selSlrFrontPort, selSvc, true)

		ginkgo.By("3. Creating switch-lb-rule with endpoints with lb front vip " + vip)
		ginkgo.By("Creating endpoint SwitchLBRule " + epSlrName)
		sessionAffinity = corev1.ServiceAffinityClientIP
		epPorts = []kubeovnv1.SlrPort{
			{
				Name:       "netcat",
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
				framework.ExpectEqual(protocols[port.TargetPort], port.Protocol)
			}
		}
		ginkgo.By("Netcating endpoint switch lb service " + epSvc.Name)
		netcatSvc(f, clientPodName, vip, epSlrFrontPort, epSvc, true)
	})
})
