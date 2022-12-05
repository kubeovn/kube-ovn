package qos

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	kubeovn "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

const testImage = "kubeovn/pause:3.2"

var _ = Describe("[Qos]", func() {
	namespace := "qos"
	f := framework.NewFramework("qos", fmt.Sprintf("%s/.kube/config", os.Getenv("HOME")))

	ns := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   namespace,
			Labels: map[string]string{"e2e": "true"},
		},
	}
	if _, err := f.KubeClientSet.CoreV1().Namespaces().Create(context.Background(), &ns, metav1.CreateOptions{}); err != nil {
		Fail(err.Error())
	}

	It("create netem qos", func() {
		name := f.GetName()
		autoMount := false
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels: map[string]string{
					"e2e":                    "true",
					"kubernetes.io/hostname": "kube-ovn-control-plane",
				},
				Annotations: map[string]string{
					util.NetemQosLatencyAnnotation: "600",
					util.NetemQosLimitAnnotation:   "2000",
					util.NetemQosLossAnnotation:    "10",
				},
			},
			Spec: corev1.PodSpec{
				NodeName: "kube-ovn-control-plane",
				Containers: []corev1.Container{
					{
						Name:            name,
						Image:           testImage,
						ImagePullPolicy: corev1.PullIfNotPresent,
					},
				},
				AutomountServiceAccountToken: &autoMount,
			},
		}

		By("Create pod")
		_, err := f.KubeClientSet.CoreV1().Pods(namespace).Create(context.Background(), pod, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		_, err = f.WaitPodReady(name, namespace)
		Expect(err).NotTo(HaveOccurred())

		By("Check Qos annotation")
		pod, err = f.KubeClientSet.CoreV1().Pods(namespace).Get(context.Background(), name, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(pod.Annotations[util.AllocatedAnnotation]).To(Equal("true"))
		Expect(pod.Annotations[util.NetemQosLatencyAnnotation]).To(Equal("600"))
		Expect(pod.Annotations[util.NetemQosLimitAnnotation]).To(Equal("2000"))
		Expect(pod.Annotations[util.NetemQosLossAnnotation]).To(Equal("10"))

		By("Check Ovs Qos Para")
		time.Sleep(3 * time.Second)
		qos, err := framework.GetPodNetemQosPara(name, namespace)
		Expect(err).NotTo(HaveOccurred())
		Expect(qos.Latency).To(Equal("600000"))
		Expect(qos.Limit).To(Equal("2000"))
		Expect(qos.Loss).To(Equal("10"))

		By("Delete pod")
		err = f.KubeClientSet.CoreV1().Pods(namespace).Delete(context.Background(), pod.Name, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())
	})

	It("update netem qos", func() {
		name := f.GetName()
		autoMount := false
		oriPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels: map[string]string{
					"e2e":                    "true",
					"kubernetes.io/hostname": "kube-ovn-control-plane",
				},
			},
			Spec: corev1.PodSpec{
				NodeName: "kube-ovn-control-plane",
				Containers: []corev1.Container{
					{
						Name:            name,
						Image:           testImage,
						ImagePullPolicy: corev1.PullIfNotPresent,
					},
				},
				AutomountServiceAccountToken: &autoMount,
			},
		}

		By("Create pod")
		_, err := f.KubeClientSet.CoreV1().Pods(namespace).Create(context.Background(), oriPod, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		oriPod, err = f.WaitPodReady(name, namespace)
		Expect(err).NotTo(HaveOccurred())
		pod := oriPod.DeepCopy()

		By("Annotate pod with netem qos")
		pod.Annotations[util.NetemQosLatencyAnnotation] = "600"
		pod.Annotations[util.NetemQosLimitAnnotation] = "2000"
		pod.Annotations[util.NetemQosLossAnnotation] = "10"
		patch, err := util.GenerateStrategicMergePatchPayload(oriPod, pod)
		Expect(err).NotTo(HaveOccurred())

		_, err = f.KubeClientSet.CoreV1().Pods(namespace).Patch(context.Background(), name, types.StrategicMergePatchType, patch, metav1.PatchOptions{}, "")
		Expect(err).NotTo(HaveOccurred())

		pod, err = f.KubeClientSet.CoreV1().Pods(namespace).Get(context.Background(), name, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(pod.Annotations[util.AllocatedAnnotation]).To(Equal("true"))
		Expect(pod.Annotations[util.NetemQosLatencyAnnotation]).To(Equal("600"))
		Expect(pod.Annotations[util.NetemQosLimitAnnotation]).To(Equal("2000"))
		Expect(pod.Annotations[util.NetemQosLossAnnotation]).To(Equal("10"))

		By("Check ovs qos")
		time.Sleep(3 * time.Second)
		qos, err := framework.GetPodNetemQosPara(name, namespace)
		Expect(err).NotTo(HaveOccurred())
		Expect(qos.Latency).To(Equal("600000"))
		Expect(qos.Limit).To(Equal("2000"))
		Expect(qos.Loss).To(Equal("10"))

		By("Delete pod")
		err = f.KubeClientSet.CoreV1().Pods(namespace).Delete(context.Background(), pod.Name, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())
	})

	It("create htb qos", func() {
		name := f.GetName()
		autoMount := false
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels: map[string]string{
					"e2e":                    "true",
					"kubernetes.io/hostname": "kube-ovn-control-plane",
				},
				Annotations: map[string]string{
					util.PriorityAnnotation:    "50",
					util.IngressRateAnnotation: "300",
				},
			},
			Spec: corev1.PodSpec{
				NodeName: "kube-ovn-control-plane",
				Containers: []corev1.Container{
					{
						Name:            name,
						Image:           testImage,
						ImagePullPolicy: corev1.PullIfNotPresent,
					},
				},
				AutomountServiceAccountToken: &autoMount,
			},
		}

		By("Create pod")
		_, err := f.KubeClientSet.CoreV1().Pods(namespace).Create(context.Background(), pod, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		_, err = f.WaitPodReady(name, namespace)
		Expect(err).NotTo(HaveOccurred())

		By("Check Qos annotation")
		pod, err = f.KubeClientSet.CoreV1().Pods(namespace).Get(context.Background(), name, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(pod.Annotations[util.AllocatedAnnotation]).To(Equal("true"))
		Expect(pod.Annotations[util.PriorityAnnotation]).To(Equal("50"))
		Expect(pod.Annotations[util.IngressRateAnnotation]).To(Equal("300"))

		By("Check Ovs Qos Para")
		time.Sleep(3 * time.Second)
		priority, rate, err := framework.GetPodHtbQosPara(name, namespace)
		Expect(err).NotTo(HaveOccurred())
		Expect(priority).To(Equal("50"))
		Expect(rate).To(Equal("300000000"))

		By("Delete pod")
		err = f.KubeClientSet.CoreV1().Pods(namespace).Delete(context.Background(), pod.Name, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())
	})

	It("update htb qos", func() {
		name := f.GetName()
		isIPv6 := strings.EqualFold(os.Getenv("IPV6"), "true")
		cidr := "20.6.0.0/16"
		if isIPv6 {
			cidr = "fc00:20:6::/112"
		}

		By("create subnet with htbqos")
		s := kubeovn.Subnet{
			ObjectMeta: metav1.ObjectMeta{
				Name:   name,
				Labels: map[string]string{"e2e": "true"},
			},
			Spec: kubeovn.SubnetSpec{
				CIDRBlock:  cidr,
				Protocol:   util.CheckProtocol(cidr),
				HtbQos:     util.HtbQosLow,
				Namespaces: []string{namespace},
			},
		}
		_, err := f.OvnClientSet.KubeovnV1().Subnets().Create(context.Background(), &s, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		err = f.WaitSubnetReady(name)
		Expect(err).NotTo(HaveOccurred())

		subnet, err := f.OvnClientSet.KubeovnV1().Subnets().Get(context.Background(), name, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(subnet.Spec.HtbQos).To(Equal(util.HtbQosLow))

		autoMount := false
		oriPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels: map[string]string{
					"e2e":                    "true",
					"kubernetes.io/hostname": "kube-ovn-control-plane",
				},
				Annotations: map[string]string{
					util.LogicalSwitchAnnotation: subnet.Name,
				},
			},
			Spec: corev1.PodSpec{
				NodeName: "kube-ovn-control-plane",
				Containers: []corev1.Container{
					{
						Name:            name,
						Image:           testImage,
						ImagePullPolicy: corev1.PullIfNotPresent,
					},
				},
				AutomountServiceAccountToken: &autoMount,
			},
		}

		By("Create pod")
		_, err = f.KubeClientSet.CoreV1().Pods(namespace).Create(context.Background(), oriPod, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		oriPod, err = f.WaitPodReady(name, namespace)
		Expect(err).NotTo(HaveOccurred())

		By("Check Ovs Qos Para, same as subnet")
		time.Sleep(3 * time.Second)
		priority, _, err := framework.GetPodHtbQosPara(name, namespace)
		Expect(err).NotTo(HaveOccurred())
		Expect(priority).To(Equal("5"))

		By("Annotate pod with priority")
		pod := oriPod.DeepCopy()
		pod.Annotations[util.PriorityAnnotation] = "2"

		patch, err := util.GenerateStrategicMergePatchPayload(oriPod, pod)
		Expect(err).NotTo(HaveOccurred())

		_, err = f.KubeClientSet.CoreV1().Pods(namespace).Patch(context.Background(), name, types.StrategicMergePatchType, patch, metav1.PatchOptions{}, "")
		Expect(err).NotTo(HaveOccurred())

		pod, err = f.KubeClientSet.CoreV1().Pods(namespace).Get(context.Background(), name, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(pod.Annotations[util.PriorityAnnotation]).To(Equal("2"))

		By("Check Ovs Qos Para")
		time.Sleep(3 * time.Second)
		priority, _, err = framework.GetPodHtbQosPara(name, namespace)
		Expect(err).NotTo(HaveOccurred())
		Expect(priority).To(Equal("2"))

		By("Delete Pod priority annotation")
		testPod := pod.DeepCopy()
		delete(testPod.Annotations, util.PriorityAnnotation)

		patch, err = util.GenerateStrategicMergePatchPayload(pod, testPod)
		Expect(err).NotTo(HaveOccurred())

		_, err = f.KubeClientSet.CoreV1().Pods(namespace).Patch(context.Background(), name, types.StrategicMergePatchType, patch, metav1.PatchOptions{}, "")
		Expect(err).NotTo(HaveOccurred())

		_, err = f.KubeClientSet.CoreV1().Pods(namespace).Get(context.Background(), name, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Check Ovs Qos Para, priority from subnet")
		time.Sleep(3 * time.Second)
		priority, _, err = framework.GetPodHtbQosPara(name, namespace)
		Expect(err).NotTo(HaveOccurred())
		Expect(priority).To(Equal("5"))

		By("Delete pod")
		err = f.KubeClientSet.CoreV1().Pods(namespace).Delete(context.Background(), pod.Name, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())
	})
})
