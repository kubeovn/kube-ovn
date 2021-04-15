package kubectl_ko

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("[kubectl-ko]", func() {
	f := framework.NewFramework("kubectl-ko", fmt.Sprintf("%s/.kube/config", os.Getenv("HOME")))
	It("nb show", func() {
		output, err := exec.Command("kubectl", "ko", "nbctl", "show").CombinedOutput()
		Expect(err).NotTo(HaveOccurred(), string(output))
	})

	It("sb show", func() {
		output, err := exec.Command("kubectl", "ko", "sbctl", "show").CombinedOutput()
		Expect(err).NotTo(HaveOccurred(), string(output))
	})

	It("vsctl show", func() {
		nodes, err := f.KubeClientSet.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())
		for _, node := range nodes.Items {
			output, err := exec.Command("kubectl", "ko", "vsctl", node.Name, "show").CombinedOutput()
			Expect(err).NotTo(HaveOccurred(), string(output))
		}
	})

	It("ofctl show", func() {
		nodes, err := f.KubeClientSet.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())
		for _, node := range nodes.Items {
			output, err := exec.Command("kubectl", "ko", "ofctl", node.Name, "show", "br-int").CombinedOutput()
			Expect(err).NotTo(HaveOccurred(), string(output))
		}
	})

	It("dpctl show", func() {
		nodes, err := f.KubeClientSet.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())
		for _, node := range nodes.Items {
			output, err := exec.Command("kubectl", "ko", "dpctl", node.Name, "show").CombinedOutput()
			Expect(err).NotTo(HaveOccurred(), string(output))
		}
	})

	It("appctl list-commands", func() {
		nodes, err := f.KubeClientSet.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())
		for _, node := range nodes.Items {
			output, err := exec.Command("kubectl", "ko", "appctl", node.Name, "list-commands").CombinedOutput()
			Expect(err).NotTo(HaveOccurred(), string(output))
		}
	})

	It("tcpdump", func() {
		pods, err := f.KubeClientSet.CoreV1().Pods("kube-system").List(context.Background(), metav1.ListOptions{LabelSelector: " app=kube-ovn-pinger"})
		Expect(err).NotTo(HaveOccurred())
		pod := pods.Items[0]
		output, err := exec.Command("kubectl", "ko", "tcpdump", fmt.Sprintf("kube-system/%s", pod.Name), "-c", "1").CombinedOutput()
		Expect(err).NotTo(HaveOccurred(), string(output))
	})

	It("trace", func() {
		pods, err := f.KubeClientSet.CoreV1().Pods("kube-system").List(context.Background(), metav1.ListOptions{LabelSelector: " app=kube-ovn-pinger"})
		Expect(err).NotTo(HaveOccurred())
		pod := pods.Items[0]

		output, err := exec.Command("kubectl", "ko", "trace", fmt.Sprintf("kube-system/%s", pod.Name), "114.114.114.114", "icmp").CombinedOutput()
		Expect(err).NotTo(HaveOccurred(), string(output))

		output, err = exec.Command("kubectl", "ko", "trace", fmt.Sprintf("kube-system/%s", pod.Name), "114.114.114.114", "tcp", "80").CombinedOutput()
		Expect(err).NotTo(HaveOccurred(), string(output))

		output, err = exec.Command("kubectl", "ko", "trace", fmt.Sprintf("kube-system/%s", pod.Name), "114.114.114.114", "udp", "53").CombinedOutput()
		Expect(err).NotTo(HaveOccurred(), string(output))
	})
})
