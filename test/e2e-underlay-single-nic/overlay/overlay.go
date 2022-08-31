package overlay

import (
	"context"
	"fmt"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

const testImage = "kubeovn/pause:3.2"

var _ = Describe("[Overlay]", func() {
	Context("[Connectivity]", func() {
		It("u2o", func() {
			f := framework.NewFramework("overlay", fmt.Sprintf("%s/.kube/config", os.Getenv("HOME")))

			By("get default subnet")
			cachedSubnet, err := f.OvnClientSet.KubeovnV1().Subnets().Get(context.Background(), "ovn-default", metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			if cachedSubnet.Spec.Protocol == kubeovnv1.ProtocolIPv6 {
				return
			}

			By("enable u2oRouting")
			if !cachedSubnet.Spec.U2oRouting {
				subnet := cachedSubnet.DeepCopy()
				subnet.Spec.U2oRouting = true
				_, err = f.OvnClientSet.KubeovnV1().Subnets().Update(context.Background(), subnet, metav1.UpdateOptions{})
				Expect(err).NotTo(HaveOccurred())
			}

			By("create overlay namespace")
			namespace := "e2e-overlay"
			_, err = f.KubeClientSet.CoreV1().Namespaces().Create(context.Background(), &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   namespace,
					Labels: map[string]string{"e2e": "true"}}}, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			By("create overlay subnet")
			subnetName := "e2e-overlay"
			s := kubeovnv1.Subnet{
				ObjectMeta: metav1.ObjectMeta{
					Name:   subnetName,
					Labels: map[string]string{"e2e": "true"},
				},
				Spec: kubeovnv1.SubnetSpec{
					CIDRBlock:  "12.10.0.0/16",
					Namespaces: []string{namespace},
					Protocol:   util.CheckProtocol("12.10.0.0/16"),
				},
			}
			_, err = f.OvnClientSet.KubeovnV1().Subnets().Create(context.Background(), &s, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			err = f.WaitSubnetReady(subnetName)
			Expect(err).NotTo(HaveOccurred())

			By("create underlay pod")
			var autoMount bool
			upod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      f.GetName(),
					Namespace: "default",
					Labels:    map[string]string{"e2e": "true"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            f.GetName(),
							Image:           testImage,
							ImagePullPolicy: corev1.PullIfNotPresent,
						},
					},
					AutomountServiceAccountToken: &autoMount,
				},
			}
			_, err = f.KubeClientSet.CoreV1().Pods(upod.Namespace).Create(context.Background(), upod, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			upod, err = f.WaitPodReady(upod.Name, upod.Namespace)
			Expect(err).NotTo(HaveOccurred())
			Expect(upod.Spec.NodeName).NotTo(BeEmpty())

			By("create overlay pod")
			opod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      f.GetName(),
					Namespace: namespace,
					Labels:    map[string]string{"e2e": "true"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            f.GetName(),
							Image:           testImage,
							ImagePullPolicy: corev1.PullIfNotPresent,
						},
					},
					AutomountServiceAccountToken: &autoMount,
				},
			}
			_, err = f.KubeClientSet.CoreV1().Pods(opod.Namespace).Create(context.Background(), opod, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			opod, err = f.WaitPodReady(opod.Name, opod.Namespace)
			Expect(err).NotTo(HaveOccurred())

			By("get kube-ovn-cni pod")
			podList, err := f.KubeClientSet.CoreV1().Pods("kube-system").List(context.Background(), metav1.ListOptions{LabelSelector: "app=kube-ovn-cni"})
			Expect(err).NotTo(HaveOccurred())
			Expect(podList).NotTo(BeNil())

			var cniPod *corev1.Pod
			for i, pod := range podList.Items {
				if pod.Spec.NodeName == upod.Spec.NodeName {
					cniPod = &podList.Items[i]
					break
				}
			}
			Expect(cniPod).NotTo(BeNil())

			By("get underlay pod's netns")
			cmd := fmt.Sprintf("ovs-vsctl --no-heading --columns=external_ids find interface external-ids:pod_name=%s external-ids:pod_namespace=%s", upod.Name, upod.Namespace)
			stdout, _, err := f.ExecToPodThroughAPI(cmd, "cni-server", cniPod.Name, cniPod.Namespace, nil)
			Expect(err).NotTo(HaveOccurred())
			var netns string
			for _, field := range strings.Fields(stdout) {
				if strings.HasPrefix(field, "pod_netns=") {
					netns = strings.TrimPrefix(field, "pod_netns=")
					netns = strings.Trim(netns[:len(netns)-1], `"`)
					break
				}
			}
			Expect(netns).NotTo(BeEmpty())

			By("ping overlay pod")
			cmd = fmt.Sprintf("nsenter --net=%s ping -c1 -W1 %s", netns, opod.Status.PodIP)
			stdout, _, err = f.ExecToPodThroughAPI(cmd, "cni-server", cniPod.Name, cniPod.Namespace, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(stdout).To(ContainSubstring(" 0% packet loss"))

			By("delete underlay pod")
			err = f.KubeClientSet.CoreV1().Pods(upod.Namespace).Delete(context.Background(), upod.Name, metav1.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())

			By("delete overlay pod")
			err = f.KubeClientSet.CoreV1().Pods(opod.Namespace).Delete(context.Background(), opod.Name, metav1.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())

			By("delete overlay subnet")
			err = f.OvnClientSet.KubeovnV1().Subnets().Delete(context.Background(), subnetName, metav1.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())

			By("delete overlay namespace")
			err = f.KubeClientSet.CoreV1().Namespaces().Delete(context.Background(), opod.Namespace, metav1.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
