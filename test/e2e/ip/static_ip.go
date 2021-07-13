package ip

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

const (
	TestImage = "kubeovn/pause:3.2"
)

var _ = Describe("[IP Allocation]", func() {
	namespace := "static-ip"
	f := framework.NewFramework("ip allocation", fmt.Sprintf("%s/.kube/config", os.Getenv("HOME")))

	Describe("static pod ip", func() {
		It("normal ip", func() {
			name := f.GetName()
			autoMount := false
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
					Annotations: map[string]string{
						util.IpAddressAnnotation:  "12.10.0.10",
						util.MacAddressAnnotation: "00:00:00:53:6B:B6",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            name,
							Image:           TestImage,
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

			pod, err = f.KubeClientSet.CoreV1().Pods(namespace).Get(context.Background(), name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(pod.Annotations[util.AllocatedAnnotation]).To(Equal("true"))
			Expect(pod.Annotations[util.RoutedAnnotation]).To(Equal("true"))

			time.Sleep(1 * time.Second)
			ip, err := f.OvnClientSet.KubeovnV1().IPs().Get(context.Background(), fmt.Sprintf("%s.%s", name, namespace), metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(ip.Spec.V4IPAddress).To(Equal("12.10.0.10"))
			Expect(ip.Spec.MacAddress).To(Equal("00:00:00:53:6B:B6"))
		})

		It("deployment with ippool", func() {
			name := f.GetName()
			var replicas int32 = 3
			autoMount := false
			deployment := appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: &replicas,
					Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"apps": name}},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"apps": name},
							Annotations: map[string]string{
								util.IpPoolAnnotation: "12.10.0.20, 12.10.0.21, 12.10.0.22",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:            name,
									Image:           TestImage,
									ImagePullPolicy: corev1.PullIfNotPresent,
								},
							},
							AutomountServiceAccountToken: &autoMount,
						},
					},
				},
			}

			By("Create deployment")
			_, err := f.KubeClientSet.AppsV1().Deployments(namespace).Create(context.Background(), &deployment, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			err = f.WaitDeploymentReady(name, namespace)
			Expect(err).NotTo(HaveOccurred())

			pods, err := f.KubeClientSet.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{LabelSelector: labels.SelectorFromSet(deployment.Spec.Template.Labels).String()})
			Expect(err).NotTo(HaveOccurred())
			Expect(pods.Items).To(HaveLen(3))

			pod1, pod2, pod3 := pods.Items[0], pods.Items[1], pods.Items[2]
			Expect(pod1.Status.PodIP).NotTo(Equal(pod2.Status.PodIP))
			Expect(pod2.Status.PodIP).NotTo(Equal(pod3.Status.PodIP))
			Expect(pod1.Status.PodIP).NotTo(Equal(pod3.Status.PodIP))
			Expect([]string{"12.10.0.20", "12.10.0.21", "12.10.0.22"}).To(ContainElement(pod1.Status.PodIP))
			Expect([]string{"12.10.0.20", "12.10.0.21", "12.10.0.22"}).To(ContainElement(pod2.Status.PodIP))
			Expect([]string{"12.10.0.20", "12.10.0.21", "12.10.0.22"}).To(ContainElement(pod3.Status.PodIP))

			By("Delete pods and recreate")
			err = f.KubeClientSet.CoreV1().Pods(namespace).DeleteCollection(context.Background(), metav1.DeleteOptions{}, metav1.ListOptions{LabelSelector: labels.SelectorFromSet(deployment.Spec.Template.Labels).String()})
			Expect(err).NotTo(HaveOccurred())

			err = f.WaitDeploymentReady(name, namespace)
			Expect(err).NotTo(HaveOccurred())

			pods, err = f.KubeClientSet.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{LabelSelector: labels.SelectorFromSet(deployment.Spec.Template.Labels).String()})
			Expect(err).NotTo(HaveOccurred())
			Expect(pods.Items).To(HaveLen(3))

			pod1, pod2, pod3 = pods.Items[0], pods.Items[1], pods.Items[2]
			Expect(pod1.Status.PodIP).NotTo(Equal(pod2.Status.PodIP))
			Expect(pod2.Status.PodIP).NotTo(Equal(pod3.Status.PodIP))
			Expect(pod1.Status.PodIP).NotTo(Equal(pod3.Status.PodIP))
			Expect([]string{"12.10.0.20", "12.10.0.21", "12.10.0.22"}).To(ContainElement(pod1.Status.PodIP))
			Expect([]string{"12.10.0.20", "12.10.0.21", "12.10.0.22"}).To(ContainElement(pod2.Status.PodIP))
			Expect([]string{"12.10.0.20", "12.10.0.21", "12.10.0.22"}).To(ContainElement(pod3.Status.PodIP))
		})

		It("statefulset with ippool", func() {
			name := f.GetName()
			var replicas int32 = 3
			autoMount := false
			ss := appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas: &replicas,
					Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"apps": name}},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"apps": name},
							Annotations: map[string]string{
								util.IpPoolAnnotation: "12.10.0.31, 12.10.0.32, 12.10.0.30",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:            name,
									Image:           TestImage,
									ImagePullPolicy: corev1.PullIfNotPresent,
								},
							},
							AutomountServiceAccountToken: &autoMount,
						},
					},
				},
			}

			By("Create statefulset")
			_, err := f.KubeClientSet.AppsV1().StatefulSets(namespace).Create(context.Background(), &ss, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			err = f.WaitStatefulsetReady(name, namespace)
			Expect(err).NotTo(HaveOccurred())

			for i := 0; i < 3; i++ {
				pod, err := f.KubeClientSet.CoreV1().Pods(namespace).Get(context.Background(), fmt.Sprintf("%s-%d", name, i), metav1.GetOptions{})
				Expect(err).NotTo(HaveOccurred())
				Expect(pod.Status.PodIP).To(Equal([]string{"12.10.0.31", "12.10.0.32", "12.10.0.30"}[i]))
			}
		})

		It("statefulset without ippool", func() {
			name := f.GetName()
			var replicas int32 = 3
			autoMount := false
			ss := appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas: &replicas,
					Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"apps": name}},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"apps": name},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:            name,
									Image:           TestImage,
									ImagePullPolicy: corev1.PullIfNotPresent,
								},
							},
							AutomountServiceAccountToken: &autoMount,
						},
					},
				},
			}

			By("Create statefulset")
			_, err := f.KubeClientSet.AppsV1().StatefulSets(namespace).Create(context.Background(), &ss, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			err = f.WaitStatefulsetReady(name, namespace)
			Expect(err).NotTo(HaveOccurred())

			ips := make([]string, 0, 3)
			for i := 0; i < 3; i++ {
				pod, err := f.KubeClientSet.CoreV1().Pods(namespace).Get(context.Background(), fmt.Sprintf("%s-%d", name, i), metav1.GetOptions{})
				Expect(err).NotTo(HaveOccurred())
				ips = append(ips, pod.Status.PodIP)
			}

			err = f.KubeClientSet.CoreV1().Pods(namespace).DeleteCollection(context.Background(), metav1.DeleteOptions{}, metav1.ListOptions{LabelSelector: labels.SelectorFromSet(ss.Spec.Template.Labels).String()})
			Expect(err).NotTo(HaveOccurred())

			err = f.WaitStatefulsetReady(name, namespace)
			Expect(err).NotTo(HaveOccurred())
			for i := 0; i < 3; i++ {
				pod, err := f.KubeClientSet.CoreV1().Pods(namespace).Get(context.Background(), fmt.Sprintf("%s-%d", name, i), metav1.GetOptions{})
				Expect(err).NotTo(HaveOccurred())
				Expect(pod.Status.PodIP).To(Equal(ips[i]))
			}
		})
	})
})
