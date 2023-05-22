package vpc_internal_lb

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/kubernetes/test/e2e"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	"github.com/onsi/ginkgo/v2"
)

func generateSwitchLBRuleName(ruleName string) string {
	return "lb-" + ruleName
}

func generateServiceName(slrName string) string {
	return "slr-" + slrName
}

func generatePodName(name string) string {
	return "pod-" + name
}

func TestE2E(t *testing.T) {
	if k8sframework.TestContext.KubeConfig == "" {
		k8sframework.TestContext.KubeConfig = filepath.Join(os.Getenv("HOME"), ".kube", "config")
	}
	k8sframework.AfterReadingAllFlags(&k8sframework.TestContext)

	e2e.RunE2ETests(t)
}

var _ = framework.Describe("[group:vpc-internal-lb]", func() {
	f := framework.NewDefaultFramework("vpc-internal-lb")

	var (
		slrName, svcName, podName, namespaceName, image, suffix string
		switchLBRuleClient                                      *framework.SwitchLBRuleClient
		endpointsClient                                         *framework.EndpointsClient
		serviceClient                                           *framework.ServiceClient
		podClient                                               *framework.PodClient
		clientset                                               clientset.Interface
	)

	ginkgo.BeforeEach(func() {
		switchLBRuleClient = f.SwitchLBRuleClient()
		endpointsClient = f.EndpointClient()
		serviceClient = f.ServiceClient()
		podClient = f.PodClient()
		clientset = f.ClientSet

		if image == "" {
			image = framework.GetKubeOvnImage(clientset)
		}
		suffix = framework.RandomSuffix()

		namespaceName = f.Namespace.Name
		slrName = generateSwitchLBRuleName(suffix)
		svcName = generateServiceName(slrName)
		podName = generatePodName(suffix)

		var (
			pod     *corev1.Pod
			command []string
			labels  map[string]string
		)
		labels = map[string]string{"app": "test"}

		ginkgo.By("Creating pod " + podName)
		command = []string{"sh", "-c", "sleep infinity"}

		pod = framework.MakePod(namespaceName, podName, labels, nil, image, command, nil)
		podClient.CreateSync(pod)
	})

	ginkgo.AfterEach(func() {
		ginkgo.By("Deleting pod " + podName)
		podClient.DeleteSync(podName)

		ginkgo.By("Deleting switch-lb-rule " + slrName)
		switchLBRuleClient.DeleteSync(slrName)
	})

	framework.ConformanceIt("should create switch-lb-rule with selector for vpc-internal-lb", func() {
		var (
			pod *corev1.Pod
			err error
		)

		ginkgo.By("Get pod " + podName)
		pod, err = podClient.PodInterface.Get(context.TODO(), podName, metav1.GetOptions{})
		framework.ExpectNil(err)
		framework.ExpectNotNil(pod)

		ginkgo.By("Creating SwitchLBRule " + slrName)
		var (
			rule                *apiv1.SwitchLBRule
			selector, endpoints []string
			ports               []apiv1.SlrPort
			sessionAffinity     corev1.ServiceAffinity
			vip                 string
		)

		vip = "1.1.1.1"
		sessionAffinity = corev1.ServiceAffinityClientIP
		ports = []apiv1.SlrPort{
			{
				Name:       "dns",
				Port:       8888,
				TargetPort: 80,
				Protocol:   "TCP",
			},
		}
		selector = []string{
			"app:test",
		}

		rule = framework.MakeSwitchLBRule(slrName, namespaceName, vip, sessionAffinity, nil, selector, endpoints, ports)
		_ = switchLBRuleClient.Create(rule)

		ginkgo.By("Waiting for switch-lb-rule " + slrName + " to be ready")
		framework.WaitUntil(2*time.Second, time.Minute, func(_ context.Context) (bool, error) {
			_, err = switchLBRuleClient.SwitchLBRuleInterface.Get(context.TODO(), slrName, metav1.GetOptions{})
			if err == nil {
				return true, nil
			}
			if k8serrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}, fmt.Sprintf("switch-lb-rule %s is created", slrName))

		var (
			svc *corev1.Service
			eps *corev1.Endpoints
		)

		ginkgo.By("Waiting for headless service " + svcName + " to be ready")
		framework.WaitUntil(2*time.Second, time.Minute, func(_ context.Context) (bool, error) {
			svc, err = serviceClient.ServiceInterface.Get(context.TODO(), svcName, metav1.GetOptions{})
			if err == nil {
				return true, nil
			}
			if k8serrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}, fmt.Sprintf("service %s is created", svcName))
		framework.ExpectNotNil(svc)

		ginkgo.By("Waiting for endpoints " + svcName + " to be ready")
		framework.WaitUntil(2*time.Second, time.Minute, func(_ context.Context) (bool, error) {
			eps, err = endpointsClient.EndpointsInterface.Get(context.TODO(), svcName, metav1.GetOptions{})
			if err == nil {
				return true, nil
			}
			if k8serrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}, fmt.Sprintf("endpoints %s is created", svcName))
		framework.ExpectNotNil(eps)

		for _, subset := range eps.Subsets {
			var (
				ips       []string
				tps       []int32
				protocols = make(map[int32]string)
			)

			ginkgo.By("Checking endpoint address")
			for _, address := range subset.Addresses {
				ips = append(ips, address.IP)
			}
			framework.ExpectContainElement(ips, pod.Status.PodIP)

			ginkgo.By("Checking endpoint ports")
			for _, port := range subset.Ports {
				tps = append(tps, port.Port)
				protocols[port.Port] = string(port.Protocol)
			}
			for _, port := range ports {
				framework.ExpectContainElement(tps, port.TargetPort)
				framework.ExpectEqual(protocols[port.TargetPort], port.Protocol)
			}
		}
	})

	framework.ConformanceIt("should create switch-lb-rule with endpoints for vpc-internal-lb", func() {
		var (
			pod *corev1.Pod
			err error
		)

		ginkgo.By("Get pod " + podName)
		pod, err = podClient.PodInterface.Get(context.TODO(), podName, metav1.GetOptions{})
		framework.ExpectNil(err)
		framework.ExpectNotNil(pod)

		ginkgo.By("Creating SwitchLBRule " + slrName)
		var (
			rule                *apiv1.SwitchLBRule
			annotations         map[string]string
			selector, endpoints []string
			ports               []apiv1.SlrPort
			sessionAffinity     corev1.ServiceAffinity
			vip                 string
		)

		vip = "1.1.1.1"
		sessionAffinity = corev1.ServiceAffinityClientIP
		ports = []apiv1.SlrPort{
			{
				Name:       "dns",
				Port:       8888,
				TargetPort: 80,
				Protocol:   "TCP",
			},
		}
		endpoints = []string{
			pod.Status.PodIP,
		}

		rule = framework.MakeSwitchLBRule(slrName, namespaceName, vip, sessionAffinity, annotations, selector, endpoints, ports)
		_ = switchLBRuleClient.Create(rule)

		ginkgo.By("Waiting for switch-lb-rule " + slrName + " to be ready")
		framework.WaitUntil(2*time.Second, time.Minute, func(_ context.Context) (bool, error) {
			_, err := switchLBRuleClient.SwitchLBRuleInterface.Get(context.TODO(), slrName, metav1.GetOptions{})
			if err == nil {
				return true, nil
			}
			if k8serrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}, fmt.Sprintf("switch-lb-rule %s is created", slrName))

		var (
			svc *corev1.Service
			eps *corev1.Endpoints
		)

		ginkgo.By("Waiting for headless service " + svcName + " to be ready")
		framework.WaitUntil(2*time.Second, time.Minute, func(_ context.Context) (bool, error) {
			svc, err = serviceClient.ServiceInterface.Get(context.TODO(), svcName, metav1.GetOptions{})
			if err == nil {
				return true, nil
			}
			if k8serrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}, fmt.Sprintf("service %s is created", svcName))
		framework.ExpectNotNil(svc)

		ginkgo.By("Waiting for endpoints " + svcName + " to be ready")
		framework.WaitUntil(2*time.Second, time.Minute, func(_ context.Context) (bool, error) {
			eps, err = endpointsClient.EndpointsInterface.Get(context.TODO(), svcName, metav1.GetOptions{})
			if err == nil {
				return true, nil
			}
			if k8serrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}, fmt.Sprintf("endpoints %s is created", svcName))
		framework.ExpectNotNil(eps)

		for _, subset := range eps.Subsets {
			var (
				ips       []string
				tps       []int32
				protocols = make(map[int32]string)
			)

			ginkgo.By("Checking endpoint address")
			for _, address := range subset.Addresses {
				ips = append(ips, address.IP)
			}
			framework.ExpectContainElement(ips, pod.Status.PodIP)

			ginkgo.By("Checking endpoint ports")
			for _, port := range subset.Ports {
				tps = append(tps, port.Port)
				protocols[port.Port] = string(port.Protocol)
			}
			for _, port := range ports {
				framework.ExpectContainElement(tps, port.TargetPort)
				framework.ExpectEqual(protocols[port.TargetPort], port.Protocol)
			}
		}
	})
})
