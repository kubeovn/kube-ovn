package service

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("[Service]", func() {
	f := framework.NewFramework("service", fmt.Sprintf("%s/.kube/config", os.Getenv("HOME")))
	hostPods, err := f.KubeClientSet.CoreV1().Pods("kube-system").List(context.Background(), metav1.ListOptions{LabelSelector: "app=ovs"})
	Expect(err).NotTo(HaveOccurred())

	containerPods, err := f.KubeClientSet.CoreV1().Pods("kube-system").List(context.Background(), metav1.ListOptions{LabelSelector: "app=kube-ovn-pinger"})
	Expect(err).NotTo(HaveOccurred())

	hostService, err := f.KubeClientSet.CoreV1().Services("kube-system").Get(context.Background(), "kube-ovn-cni", metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred())
	hostService.Spec.Type = corev1.ServiceTypeNodePort
	hostService, err = f.KubeClientSet.CoreV1().Services("kube-system").Update(context.Background(), hostService, metav1.UpdateOptions{})
	Expect(err).NotTo(HaveOccurred())

	containerService, err := f.KubeClientSet.CoreV1().Services("kube-system").Get(context.Background(), "kube-ovn-pinger", metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred())
	containerService.Spec.Type = corev1.ServiceTypeNodePort
	containerService, err = f.KubeClientSet.CoreV1().Services("kube-system").Update(context.Background(), containerService, metav1.UpdateOptions{})
	Expect(err).NotTo(HaveOccurred())

	It("host to host clusterIP", func() {
		for _, pod := range hostPods.Items {
			output, err := exec.Command(
				"kubectl", "exec", "-n", "kube-system", pod.Name, "--",
				"curl", "-s", "-w", "%{http_code}", fmt.Sprintf("%s:%d/metrics", hostService.Spec.ClusterIP, hostService.Spec.Ports[0].Port), "-o", "/dev/null",
			).CombinedOutput()
			Expect(err).NotTo(HaveOccurred(), string(output))
			Expect(string(output)).To(Equal("200"))
		}
	})

	It("host to host NodePort", func() {
		for _, pod := range hostPods.Items {
			nodes, err := f.KubeClientSet.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred())
			for _, node := range nodes.Items {
				nodeIP := util.GetNodeInternalIP(&node)
				output, err := exec.Command(
					"kubectl", "exec", "-n", "kube-system", pod.Name, "--",
					"curl", "-s", "-w", "%{http_code}", fmt.Sprintf("%s:%d/metrics", nodeIP, hostService.Spec.Ports[0].NodePort), "-o", "/dev/null",
				).CombinedOutput()
				Expect(err).NotTo(HaveOccurred(), string(output))
				Expect(string(output)).To(Equal("200"))
			}
		}
	})

	It("host to container clusterIP", func() {
		for _, pod := range hostPods.Items {
			output, err := exec.Command(
				"kubectl", "exec", "-n", "kube-system", pod.Name, "--",
				"curl", "-s", "-w", "%{http_code}", fmt.Sprintf("%s:%d/metrics", containerService.Spec.ClusterIP, containerService.Spec.Ports[0].Port), "-o", "/dev/null",
			).CombinedOutput()
			Expect(err).NotTo(HaveOccurred(), string(output))
			Expect(string(output)).To(Equal("200"))
		}
	})

	It("host to container NodePort", func() {
		for _, pod := range hostPods.Items {
			nodes, err := f.KubeClientSet.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred())
			for _, node := range nodes.Items {
				nodeIP := util.GetNodeInternalIP(&node)
				output, err := exec.Command(
					"kubectl", "exec", "-n", "kube-system", pod.Name, "--",
					"curl", "-s", "-w", "%{http_code}", fmt.Sprintf("%s:%d/metrics", nodeIP, containerService.Spec.Ports[0].NodePort), "-o", "/dev/null",
				).CombinedOutput()
				Expect(err).NotTo(HaveOccurred(), string(output))
				Expect(string(output)).To(Equal("200"))
			}
		}
	})

	It("container to container clusterIP", func() {
		for _, pod := range containerPods.Items {
			output, err := exec.Command(
				"kubectl", "exec", "-n", "kube-system", pod.Name, "--",
				"curl", "-s", "-w", "%{http_code}", fmt.Sprintf("%s:%d/metrics", containerService.Spec.ClusterIP, containerService.Spec.Ports[0].Port), "-o", "/dev/null",
			).CombinedOutput()
			Expect(err).NotTo(HaveOccurred(), string(output))
			Expect(string(output)).To(Equal("200"))
		}
	})

	It("container to container NodePort", func() {
		for _, pod := range containerPods.Items {
			nodes, err := f.KubeClientSet.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred())
			for _, node := range nodes.Items {
				nodeIP := util.GetNodeInternalIP(&node)
				output, err := exec.Command(
					"kubectl", "exec", "-n", "kube-system", pod.Name, "--",
					"curl", "-s", "-w", "%{http_code}", fmt.Sprintf("%s:%d/metrics", nodeIP, containerService.Spec.Ports[0].NodePort), "-o", "/dev/null",
				).CombinedOutput()
				Expect(err).NotTo(HaveOccurred(), string(output))
				Expect(string(output)).To(Equal("200"))
			}
		}
	})

	It("container to host clusterIP", func() {
		for _, pod := range containerPods.Items {
			output, err := exec.Command(
				"kubectl", "exec", "-n", "kube-system", pod.Name, "--",
				"curl", "-s", "-w", "%{http_code}", fmt.Sprintf("%s:%d/metrics", hostService.Spec.ClusterIP, hostService.Spec.Ports[0].Port), "-o", "/dev/null",
			).CombinedOutput()
			Expect(err).NotTo(HaveOccurred(), string(output))
			Expect(string(output)).To(Equal("200"))
		}
	})

	It("container to host NodePort", func() {
		for _, pod := range containerPods.Items {
			nodes, err := f.KubeClientSet.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred())
			for _, node := range nodes.Items {
				nodeIP := util.GetNodeInternalIP(&node)
				output, err := exec.Command(
					"kubectl", "exec", "-n", "kube-system", pod.Name, "--",
					"curl", "-s", "-w", "%{http_code}", fmt.Sprintf("%s:%d/metrics", nodeIP, hostService.Spec.Ports[0].NodePort), "-o", "/dev/null",
				).CombinedOutput()
				Expect(err).NotTo(HaveOccurred(), string(output))
				Expect(string(output)).To(Equal("200"))
			}
		}
	})

})
