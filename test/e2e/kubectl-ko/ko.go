package kubectl_ko

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	kubeovn "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
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
		dst := "114.114.114.114"
		if util.CheckProtocol(pod.Status.PodIP) == kubeovn.ProtocolIPv6 {
			dst = "2400:3200::1"
		}

		output, err := exec.Command("kubectl", "ko", "trace", fmt.Sprintf("kube-system/%s", pod.Name), dst, "icmp").CombinedOutput()
		Expect(err).NotTo(HaveOccurred(), string(output))

		output, err = exec.Command("kubectl", "ko", "trace", fmt.Sprintf("kube-system/%s", pod.Name), dst, "tcp", "80").CombinedOutput()
		Expect(err).NotTo(HaveOccurred(), string(output))

		output, err = exec.Command("kubectl", "ko", "trace", fmt.Sprintf("kube-system/%s", pod.Name), dst, "udp", "53").CombinedOutput()
		Expect(err).NotTo(HaveOccurred(), string(output))
	})

	It("nb/sb operation", func() {
		output, err := exec.Command("kubectl", "ko", "nb", "status").CombinedOutput()
		Expect(err).NotTo(HaveOccurred(), string(output))

		output, err = exec.Command("kubectl", "ko", "sb", "status").CombinedOutput()
		Expect(err).NotTo(HaveOccurred(), string(output))

		output, err = exec.Command("kubectl", "ko", "nb", "backup").CombinedOutput()
		Expect(err).NotTo(HaveOccurred(), string(output))

		output, err = exec.Command("kubectl", "ko", "sb", "backup").CombinedOutput()
		Expect(err).NotTo(HaveOccurred(), string(output))
	})
})
