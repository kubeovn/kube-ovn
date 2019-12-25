package ip

import (
	"fmt"
	"github.com/alauda/kube-ovn/pkg/util"
	"github.com/alauda/kube-ovn/test/e2e/framework"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"os"
)

var _ = Describe("[IP Allocation]", func() {
	namespace := "static-ip"
	f := framework.NewFramework("ip allocation", fmt.Sprintf("%s/.kube/config", os.Getenv("HOME")))

	Describe("static pod ip", func() {
		BeforeEach(func() {
			f.KubeClientSet.CoreV1().Pods(namespace).Delete(f.GetName(), &metav1.DeleteOptions{})
		})
		AfterEach(func() {
			f.KubeClientSet.CoreV1().Pods(namespace).Delete(f.GetName(), &metav1.DeleteOptions{})
		})

		It("normal ip", func() {
			name := f.GetName()
			autoMount := false
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:        name,
					Namespace:   namespace,
					Annotations: map[string]string{
						util.IpAddressAnnotation: "12.10.0.10",
						util.MacAddressAnnotation: "00:00:00:53:6B:B6",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						corev1.Container{
							Name:  name,
							Image: "nginx:alpine",
						},
					},
					AutomountServiceAccountToken: &autoMount,
				},
			}

			By("Create pod")
			_, err := f.KubeClientSet.CoreV1().Pods(namespace).Create(pod)
			Expect(err).NotTo(HaveOccurred())

			err = f.WaitPodReady(name, namespace)
			Expect(err).NotTo(HaveOccurred())

			pod, err = f.KubeClientSet.CoreV1().Pods(namespace).Get(name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(pod.Annotations[util.AllocatedAnnotation]).To(Equal("true"))

			ip, err := f.OvnClientSet.KubeovnV1().IPs().Get(fmt.Sprintf("%s.%s", name, namespace), metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(ip.Spec.IPAddress).To(Equal("12.10.0.10"))
			Expect(ip.Spec.MacAddress).To(Equal("00:00:00:53:6B:B6"))
		})
	})
})
