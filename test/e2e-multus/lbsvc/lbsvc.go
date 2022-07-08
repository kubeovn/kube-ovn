package lbsvc

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	ATTACHMENT_NAME = "lb-svc-attachment"
	ATTACHMENT_NS   = "kube-system"
)

func genLbSvcDpName(name string) string {
	return fmt.Sprintf("lb-svc-%s", name)
}

var _ = Describe("Lbsvc", func() {
	f := framework.NewFramework("lbsvc", fmt.Sprintf("%s/.kube/config", os.Getenv("HOME")))

	It("dynamic lb svc", func() {
		name := "dynamic-service"
		namespace := "lb-test"

		var val intstr.IntOrString
		val.IntVal = 80
		var port corev1.ServicePort
		port.Name = "test"
		port.Protocol = corev1.ProtocolTCP
		port.Port = 80
		port.TargetPort = val

		By("create service")
		svc := corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:        name,
				Namespace:   namespace,
				Labels:      map[string]string{"e2e": "true", "app": "dynamic"},
				Annotations: map[string]string{"lb-svc-attachment.kube-system.kubernetes.io/logical_switch": "attach-subnet"},
			},
			Spec: corev1.ServiceSpec{
				Ports:           []corev1.ServicePort{port},
				Selector:        map[string]string{"app": "dynamic"},
				SessionAffinity: corev1.ServiceAffinityNone,
				Type:            corev1.ServiceTypeLoadBalancer,
			},
		}
		_, err := f.KubeClientSet.CoreV1().Services(namespace).Create(context.Background(), &svc, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		time.Sleep(time.Second * 15)

		By("check deployment")
		dpName := genLbSvcDpName(name)
		deploy, err := f.KubeClientSet.AppsV1().Deployments(namespace).Get(context.Background(), dpName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(deploy.Status.AvailableReplicas).To(Equal(int32(1)))

		By("wait pod running")
		var pod corev1.Pod
		found := false

		pods, _ := f.KubeClientSet.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{})
		for _, pod = range pods.Items {
			if strings.Contains(pod.Name, dpName) {
				found = true
				break
			}
		}
		Expect(found).To(Equal(true))

		_, err = f.WaitPodReady(pod.Name, namespace)
		Expect(err).NotTo(HaveOccurred())

		By("check pod annotation")
		providerName := fmt.Sprintf("%s.%s", ATTACHMENT_NAME, ATTACHMENT_NS)
		allocateAnnotation := fmt.Sprintf(util.AllocatedAnnotationTemplate, providerName)
		Expect(pod.Annotations[allocateAnnotation]).To(Equal("true"))

		attchCidrAnnotation := fmt.Sprintf(util.CidrAnnotationTemplate, providerName)
		attchIpAnnotation := fmt.Sprintf(util.IpAddressAnnotationTemplate, providerName)
		result := util.CIDRContainIP(pod.Annotations[attchCidrAnnotation], pod.Annotations[attchIpAnnotation])
		Expect(result).To(Equal(true))

		By("check svc externalIP")
		checkSvc, err := f.KubeClientSet.CoreV1().Services(namespace).Get(context.Background(), name, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		lbIP := checkSvc.Status.LoadBalancer.Ingress[0].IP
		Expect(pod.Annotations[attchIpAnnotation]).To(Equal(lbIP))

		By("Delete svc")
		err = f.KubeClientSet.CoreV1().Services(namespace).Delete(context.Background(), svc.Name, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())
	})

	It("static lb svc", func() {
		name := "static-service"
		namespace := "lb-test"
		staticIP := "172.18.0.99"

		var val intstr.IntOrString
		val.IntVal = 80
		var port corev1.ServicePort
		port.Name = "test"
		port.Protocol = corev1.ProtocolTCP
		port.Port = 80
		port.TargetPort = val

		By("create service")
		svc := corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:        name,
				Namespace:   namespace,
				Labels:      map[string]string{"e2e": "true", "app": "static"},
				Annotations: map[string]string{"lb-svc-attachment.kube-system.kubernetes.io/logical_switch": "attach-subnet"},
			},
			Spec: corev1.ServiceSpec{
				Ports:           []corev1.ServicePort{port},
				Selector:        map[string]string{"app": "static"},
				SessionAffinity: corev1.ServiceAffinityNone,
				Type:            corev1.ServiceTypeLoadBalancer,
				LoadBalancerIP:  staticIP,
			},
		}

		_, err := f.KubeClientSet.CoreV1().Services(namespace).Create(context.Background(), &svc, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		time.Sleep(time.Second * 10)

		By("check deployment")
		dpName := genLbSvcDpName(name)
		deploy, err := f.KubeClientSet.AppsV1().Deployments(namespace).Get(context.Background(), dpName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(deploy.Status.AvailableReplicas).To(Equal(int32(1)))

		By("wait pod running")
		var pod corev1.Pod
		found := false

		pods, _ := f.KubeClientSet.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{})
		for _, pod = range pods.Items {
			if strings.Contains(pod.Name, dpName) {
				found = true
				break
			}
		}
		Expect(found).To(Equal(true))

		_, err = f.WaitPodReady(pod.Name, namespace)
		Expect(err).NotTo(HaveOccurred())

		By("check pod annotation")
		providerName := fmt.Sprintf("%s.%s", ATTACHMENT_NAME, ATTACHMENT_NS)
		allocateAnnotation := fmt.Sprintf(util.AllocatedAnnotationTemplate, providerName)
		Expect(pod.Annotations[allocateAnnotation]).To(Equal("true"))

		attchCidrAnnotation := fmt.Sprintf(util.CidrAnnotationTemplate, providerName)
		attchIpAnnotation := fmt.Sprintf(util.IpAddressAnnotationTemplate, providerName)
		result := util.CIDRContainIP(pod.Annotations[attchCidrAnnotation], pod.Annotations[attchIpAnnotation])
		Expect(result).To(Equal(true))

		By("check svc externalIP")
		checkSvc, err := f.KubeClientSet.CoreV1().Services(namespace).Get(context.Background(), name, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		lbIP := checkSvc.Status.LoadBalancer.Ingress[0].IP
		Expect(pod.Annotations[attchIpAnnotation]).To(Equal(lbIP))
		Expect(staticIP).To(Equal(lbIP))

		By("Delete svc")
		err = f.KubeClientSet.CoreV1().Services(namespace).Delete(context.Background(), svc.Name, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())
	})
})
