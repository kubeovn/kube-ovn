package ip

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

const testImage = "kubeovn/pause:3.2"

var _ = Describe("[IP Allocation]", func() {
	namespace := "static-ip"
	f := framework.NewFramework("ip allocation", fmt.Sprintf("%s/.kube/config", os.Getenv("HOME")))

	Describe("static pod ip", func() {
		It("normal ip", func() {
			name := f.GetName()
			ip, mac := "12.10.0.10", "00:00:00:53:6B:B6"
			autoMount := false
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
					Labels:    map[string]string{"e2e": "true"},
					Annotations: map[string]string{
						util.IpAddressAnnotation:  ip,
						util.MacAddressAnnotation: mac,
					},
				},
				Spec: corev1.PodSpec{
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

			pod, err = f.KubeClientSet.CoreV1().Pods(namespace).Get(context.Background(), name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(pod.Annotations[util.AllocatedAnnotation]).To(Equal("true"))
			Expect(pod.Annotations[util.RoutedAnnotation]).To(Equal("true"))

			time.Sleep(1 * time.Second)
			podIP, err := f.OvnClientSet.KubeovnV1().IPs().Get(context.Background(), fmt.Sprintf("%s.%s", name, namespace), metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(podIP.Spec.V4IPAddress).To(Equal(ip))
			Expect(podIP.Spec.MacAddress).To(Equal(mac))

			By("Delete pod")
			err = f.KubeClientSet.CoreV1().Pods(namespace).Delete(context.Background(), pod.Name, metav1.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
		})

		It("deployment with ippool", func() {
			name := f.GetName()
			var replicas int32 = 3
			ips := []string{"12.10.0.20", "12.10.0.21", "12.10.0.22"}
			autoMount := false
			deployment := appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
					Labels:    map[string]string{"e2e": "true"},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: &replicas,
					Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"apps": name}},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"apps": name,
								"e2e":  "true",
							},
							Annotations: map[string]string{
								util.IpPoolAnnotation: strings.Join(ips, ","),
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:            name,
									Image:           testImage,
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
			Expect(pods.Items).To(HaveLen(int(replicas)))

			podIPs := make([]string, replicas)
			for i := range pods.Items {
				podIPs[i] = pods.Items[i].Status.PodIP
			}
			sort.Strings(podIPs)
			Expect(podIPs).To(Equal(ips))

			By("Delete pods and recreate")
			err = f.KubeClientSet.CoreV1().Pods(namespace).DeleteCollection(context.Background(), metav1.DeleteOptions{}, metav1.ListOptions{LabelSelector: labels.SelectorFromSet(deployment.Spec.Template.Labels).String()})
			Expect(err).NotTo(HaveOccurred())

			err = f.WaitDeploymentReady(name, namespace)
			Expect(err).NotTo(HaveOccurred())

			pods, err = f.KubeClientSet.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{LabelSelector: labels.SelectorFromSet(deployment.Spec.Template.Labels).String()})
			Expect(err).NotTo(HaveOccurred())
			Expect(pods.Items).To(HaveLen(int(replicas)))

			for i := range pods.Items {
				podIPs[i] = pods.Items[i].Status.PodIP
			}
			sort.Strings(podIPs)
			Expect(podIPs).To(Equal(ips))

			By("Delete deployment")
			err = f.KubeClientSet.AppsV1().Deployments(namespace).Delete(context.Background(), deployment.Name, metav1.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
		})

		It("statefulset with ippool", func() {
			name := f.GetName()
			var replicas int32 = 3
			ips := []string{"12.10.0.31", "12.10.0.32", "12.10.0.30"}
			autoMount := false
			sts := appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
					Labels:    map[string]string{"e2e": "true"},
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas: &replicas,
					Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"apps": name}},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"apps": name,
								"e2e":  "true",
							},
							Annotations: map[string]string{
								util.IpPoolAnnotation: strings.Join(ips, ","),
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:            name,
									Image:           testImage,
									ImagePullPolicy: corev1.PullIfNotPresent,
								},
							},
							AutomountServiceAccountToken: &autoMount,
						},
					},
				},
			}

			By("Create statefulset")
			_, err := f.KubeClientSet.AppsV1().StatefulSets(namespace).Create(context.Background(), &sts, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			err = f.WaitStatefulsetReady(name, namespace)
			Expect(err).NotTo(HaveOccurred())

			for i := range ips {
				pod, err := f.KubeClientSet.CoreV1().Pods(namespace).Get(context.Background(), fmt.Sprintf("%s-%d", name, i), metav1.GetOptions{})
				Expect(err).NotTo(HaveOccurred())
				Expect(pod.Status.PodIP).To(Equal(ips[i]))
			}

			By("Delete statefulset")
			err = f.KubeClientSet.AppsV1().StatefulSets(namespace).Delete(context.Background(), sts.Name, metav1.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
		})

		It("statefulset without ippool", func() {
			name := f.GetName()
			var replicas int32 = 3
			autoMount := false
			sts := appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
					Labels:    map[string]string{"e2e": "true"},
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas: &replicas,
					Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"apps": name}},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"apps": name,
								"e2e":  "true",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:            name,
									Image:           testImage,
									ImagePullPolicy: corev1.PullIfNotPresent,
								},
							},
							AutomountServiceAccountToken: &autoMount,
						},
					},
				},
			}

			By("Create statefulset")
			_, err := f.KubeClientSet.AppsV1().StatefulSets(namespace).Create(context.Background(), &sts, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			err = f.WaitStatefulsetReady(name, namespace)
			Expect(err).NotTo(HaveOccurred())

			ips := make([]string, replicas)
			for i := range ips {
				pod, err := f.KubeClientSet.CoreV1().Pods(namespace).Get(context.Background(), fmt.Sprintf("%s-%d", name, i), metav1.GetOptions{})
				Expect(err).NotTo(HaveOccurred())
				ips[i] = pod.Status.PodIP
			}

			err = f.KubeClientSet.CoreV1().Pods(namespace).DeleteCollection(context.Background(), metav1.DeleteOptions{}, metav1.ListOptions{LabelSelector: labels.SelectorFromSet(sts.Spec.Template.Labels).String()})
			Expect(err).NotTo(HaveOccurred())

			err = f.WaitStatefulsetReady(name, namespace)
			Expect(err).NotTo(HaveOccurred())
			for i := range ips {
				pod, err := f.KubeClientSet.CoreV1().Pods(namespace).Get(context.Background(), fmt.Sprintf("%s-%d", name, i), metav1.GetOptions{})
				Expect(err).NotTo(HaveOccurred())
				Expect(pod.Status.PodIP).To(Equal(ips[i]))
			}

			By("Delete statefulset")
			err = f.KubeClientSet.AppsV1().StatefulSets(namespace).Delete(context.Background(), sts.Name, metav1.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
