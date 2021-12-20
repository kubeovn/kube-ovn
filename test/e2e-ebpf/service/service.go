package service

import (
	"bytes"
	"context"
	"fmt"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"os"
	"os/exec"
	"runtime"
	"strings"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func nodeIPs(node corev1.Node) []string {
	nodeIPv4, nodeIPv6 := util.GetNodeInternalIP(node)
	var nodeIPs []string
	if nodeIPv4 != "" {
		nodeIPs = append(nodeIPs, nodeIPv4)
	}
	if nodeIPv6 != "" {
		nodeIPs = append(nodeIPs, nodeIPv6)
	}
	return nodeIPs
}

func kubectlArgs(pod, ip string, port int32) string {
	return fmt.Sprintf("-n kube-system exec %s -- curl %s", pod, curlArgs(ip, port))
}

func curlArgs(ip string, port int32) string {
	return fmt.Sprintf("-s -m 3 -o /dev/null -w %%{http_code} %s/metrics", util.JoinHostPort(ip, port))
}

var _ = Describe("[Service]", func() {
	f := framework.NewFramework("service", fmt.Sprintf("%s/.kube/config", os.Getenv("HOME")))

	hostPods, err := f.KubeClientSet.CoreV1().Pods("kube-system").List(context.Background(), metav1.ListOptions{LabelSelector: "app=ovs"})
	Expect(err).NotTo(HaveOccurred())
	containerPods, err := f.KubeClientSet.CoreV1().Pods("kube-system").List(context.Background(), metav1.ListOptions{LabelSelector: "app=kube-ovn-pinger"})
	Expect(err).NotTo(HaveOccurred())

	containerService, err := f.KubeClientSet.CoreV1().Services("kube-system").Get(context.Background(), "kube-ovn-pinger", metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred())
	containerService.Spec.Type = corev1.ServiceTypeNodePort
	containerService, err = f.KubeClientSet.CoreV1().Services("kube-system").Update(context.Background(), containerService, metav1.UpdateOptions{})
	Expect(err).NotTo(HaveOccurred())

	Context("service with container network endpoints", func() {
		It("container to ClusterIP", func() {
			port := containerService.Spec.Ports[0].Port
			for _, ip := range containerService.Spec.ClusterIPs {
				for _, pod := range containerPods.Items {
					output, err := exec.Command("kubectl", strings.Fields(kubectlArgs(pod.Name, ip, port))...).CombinedOutput()
					outputStr := string(bytes.TrimSpace(output))
					Expect(err).NotTo(HaveOccurred(), outputStr)
					Expect(outputStr).To(Equal("200"))
				}
			}
		})

		It("host to ClusterIP", func() {
			port := containerService.Spec.Ports[0].Port
			for _, ip := range containerService.Spec.ClusterIPs {
				for _, pod := range hostPods.Items {
					output, err := exec.Command("kubectl", strings.Fields(kubectlArgs(pod.Name, ip, port))...).CombinedOutput()
					outputStr := string(bytes.TrimSpace(output))
					Expect(err).NotTo(HaveOccurred(), outputStr)
					Expect(outputStr).To(Equal("200"))
				}
			}
		})

		It("container to NodePort", func() {
			port := containerService.Spec.Ports[0].NodePort
			for _, pod := range containerPods.Items {
				nodes, err := f.KubeClientSet.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
				Expect(err).NotTo(HaveOccurred())
				for _, node := range nodes.Items {
					for _, nodeIP := range nodeIPs(node) {
						output, err := exec.Command("kubectl", strings.Fields(kubectlArgs(pod.Name, nodeIP, port))...).CombinedOutput()
						outputStr := string(bytes.TrimSpace(output))
						Expect(err).NotTo(HaveOccurred(), outputStr)
						Expect(outputStr).To(Equal("200"))
					}
				}
			}
		})

		It("host to NodePort", func() {
			port := containerService.Spec.Ports[0].NodePort
			for _, pod := range hostPods.Items {
				nodes, err := f.KubeClientSet.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
				Expect(err).NotTo(HaveOccurred())
				for _, node := range nodes.Items {
					for _, nodeIP := range nodeIPs(node) {
						output, err := exec.Command("kubectl", strings.Fields(kubectlArgs(pod.Name, nodeIP, port))...).CombinedOutput()
						outputStr := string(bytes.TrimSpace(output))
						Expect(err).NotTo(HaveOccurred(), outputStr)
						Expect(outputStr).To(Equal("200"))
					}
				}
			}
		})

		It("external to NodePort", func() {
			if runtime.GOOS != "linux" {
				return
			}

			port := containerService.Spec.Ports[0].NodePort
			nodes, err := f.KubeClientSet.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred())
			for _, node := range nodes.Items {
				for _, nodeIP := range nodeIPs(node) {
					output, err := exec.Command("curl", strings.Fields(curlArgs(nodeIP, port))...).CombinedOutput()
					outputStr := string(bytes.TrimSpace(output))
					Expect(err).To(HaveOccurred(), outputStr)
				}
			}
		})
	})
})
