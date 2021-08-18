package service

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	kubeovn "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

func curlArgs(ip string, port int32) string {
	if util.CheckProtocol(ip) == kubeovn.ProtocolIPv6 {
		ip = fmt.Sprintf("-g -6 [%s]", ip)
	}
	return fmt.Sprintf("-s -m 3 -o /dev/null -w %%{http_code} %s:%d/metrics", ip, port)
}

func kubectlArgs(pod, ip string, port int32) string {
	return fmt.Sprintf("-n kube-system exec %s -- curl %s", pod, curlArgs(ip, port))
}

var _ = Describe("[Service]", func() {
	if runtime.GOOS != "linux" {
		return
	}

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

	localEtpService, err := f.KubeClientSet.CoreV1().Services("kube-system").Get(context.Background(), "kube-ovn-monitor", metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred())
	localEtpService.Spec.Type = corev1.ServiceTypeNodePort
	localEtpService.Spec.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicyTypeLocal
	localEtpService, err = f.KubeClientSet.CoreV1().Services("kube-system").Update(context.Background(), localEtpService, metav1.UpdateOptions{})
	Expect(err).NotTo(HaveOccurred())

	ipvsMode := true
	cm, err := f.KubeClientSet.CoreV1().ConfigMaps("kube-system").Get(context.Background(), "kube-proxy", metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred())
	if cm.Data != nil {
		ipvsMode = strings.Contains(cm.Data["config.conf"], "mode: ipvs")
	}

	Context("service with host network endpoints", func() {
		It("container to ClusterIP", func() {
			ip, port := hostService.Spec.ClusterIP, hostService.Spec.Ports[0].Port
			for _, pod := range containerPods.Items {
				output, err := exec.Command("kubectl", strings.Fields(kubectlArgs(pod.Name, ip, port))...).CombinedOutput()
				outputStr := string(bytes.TrimSpace(output))
				Expect(err).NotTo(HaveOccurred(), outputStr)
				Expect(outputStr).To(Equal("200"))
			}
		})

		It("host to ClusterIP", func() {
			ip, port := hostService.Spec.ClusterIP, hostService.Spec.Ports[0].Port
			for _, pod := range hostPods.Items {
				output, err := exec.Command("kubectl", strings.Fields(kubectlArgs(pod.Name, ip, port))...).CombinedOutput()
				outputStr := string(bytes.TrimSpace(output))
				Expect(err).NotTo(HaveOccurred(), outputStr)
				Expect(outputStr).To(Equal("200"))
			}
		})

		It("container to NodePort", func() {
			port := hostService.Spec.Ports[0].Port
			for _, pod := range containerPods.Items {
				nodes, err := f.KubeClientSet.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
				Expect(err).NotTo(HaveOccurred())
				for _, node := range nodes.Items {
					nodeIP := util.GetNodeInternalIP(node)
					output, err := exec.Command("kubectl", strings.Fields(kubectlArgs(pod.Name, nodeIP, port))...).CombinedOutput()
					outputStr := string(bytes.TrimSpace(output))
					Expect(err).NotTo(HaveOccurred(), outputStr)
					Expect(outputStr).To(Equal("200"))
				}
			}
		})

		It("host to NodePort", func() {
			port := hostService.Spec.Ports[0].Port
			for _, pod := range hostPods.Items {
				nodes, err := f.KubeClientSet.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
				Expect(err).NotTo(HaveOccurred())
				for _, node := range nodes.Items {
					nodeIP := util.GetNodeInternalIP(node)
					output, err := exec.Command("kubectl", strings.Fields(kubectlArgs(pod.Name, nodeIP, port))...).CombinedOutput()
					outputStr := string(bytes.TrimSpace(output))
					Expect(err).NotTo(HaveOccurred(), outputStr)
					Expect(outputStr).To(Equal("200"))
				}
			}
		})

		It("external to NodePort", func() {
			port := hostService.Spec.Ports[0].Port
			nodes, err := f.KubeClientSet.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred())
			for _, node := range nodes.Items {
				nodeIP := util.GetNodeInternalIP(node)
				output, err := exec.Command("curl", strings.Fields(curlArgs(nodeIP, port))...).CombinedOutput()
				outputStr := string(bytes.TrimSpace(output))
				Expect(err).NotTo(HaveOccurred(), outputStr)
				Expect(outputStr).To(Equal("200"))
			}
		})
	})

	Context("service with container network endpoints", func() {
		It("container to ClusterIP", func() {
			ip, port := hostService.Spec.ClusterIP, hostService.Spec.Ports[0].Port
			for _, pod := range containerPods.Items {
				output, err := exec.Command("kubectl", strings.Fields(kubectlArgs(pod.Name, ip, port))...).CombinedOutput()
				outputStr := string(bytes.TrimSpace(output))
				Expect(err).NotTo(HaveOccurred(), outputStr)
				Expect(outputStr).To(Equal("200"))
			}
		})

		It("host to ClusterIP", func() {
			ip, port := hostService.Spec.ClusterIP, hostService.Spec.Ports[0].Port
			for _, pod := range hostPods.Items {
				output, err := exec.Command("kubectl", strings.Fields(kubectlArgs(pod.Name, ip, port))...).CombinedOutput()
				outputStr := string(bytes.TrimSpace(output))
				Expect(err).NotTo(HaveOccurred(), outputStr)
				Expect(outputStr).To(Equal("200"))
			}
		})

		It("container to NodePort", func() {
			port := containerService.Spec.Ports[0].NodePort
			for _, pod := range containerPods.Items {
				nodes, err := f.KubeClientSet.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
				Expect(err).NotTo(HaveOccurred())
				for _, node := range nodes.Items {
					nodeIP := util.GetNodeInternalIP(node)
					output, err := exec.Command("kubectl", strings.Fields(kubectlArgs(pod.Name, nodeIP, port))...).CombinedOutput()
					outputStr := string(bytes.TrimSpace(output))
					Expect(err).NotTo(HaveOccurred(), outputStr)
					Expect(outputStr).To(Equal("200"))
				}
			}
		})

		It("host to NodePort", func() {
			port := containerService.Spec.Ports[0].NodePort
			for _, pod := range hostPods.Items {
				nodes, err := f.KubeClientSet.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
				Expect(err).NotTo(HaveOccurred())
				for _, node := range nodes.Items {
					nodeIP := util.GetNodeInternalIP(node)
					output, err := exec.Command("kubectl", strings.Fields(kubectlArgs(pod.Name, nodeIP, port))...).CombinedOutput()
					outputStr := string(bytes.TrimSpace(output))
					Expect(err).NotTo(HaveOccurred(), outputStr)
					Expect(outputStr).To(Equal("200"))
				}
			}
		})

		It("external to NodePort", func() {
			port := containerService.Spec.Ports[0].NodePort
			nodes, err := f.KubeClientSet.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred())
			for _, node := range nodes.Items {
				nodeIP := util.GetNodeInternalIP(node)
				output, err := exec.Command("curl", strings.Fields(curlArgs(nodeIP, port))...).CombinedOutput()
				outputStr := string(bytes.TrimSpace(output))
				Expect(err).NotTo(HaveOccurred(), outputStr)
				Expect(outputStr).To(Equal("200"))
			}
		})
	})

	Context("service with local external traffic policy", func() {
		It("container to ClusterIP", func() {
			ip, port := hostService.Spec.ClusterIP, hostService.Spec.Ports[0].Port
			for _, pod := range containerPods.Items {
				output, err := exec.Command("kubectl", strings.Fields(kubectlArgs(pod.Name, ip, port))...).CombinedOutput()
				outputStr := string(bytes.TrimSpace(output))
				Expect(err).NotTo(HaveOccurred(), outputStr)
				Expect(outputStr).To(Equal("200"))
			}
		})

		It("host to ClusterIP", func() {
			ip, port := hostService.Spec.ClusterIP, hostService.Spec.Ports[0].Port
			for _, pod := range hostPods.Items {
				output, err := exec.Command("kubectl", strings.Fields(kubectlArgs(pod.Name, ip, port))...).CombinedOutput()
				outputStr := string(bytes.TrimSpace(output))
				Expect(err).NotTo(HaveOccurred(), outputStr)
				Expect(outputStr).To(Equal("200"))
			}
		})

		It("container to NodePort", func() {
			port := localEtpService.Spec.Ports[0].NodePort

			endpoints, err := f.KubeClientSet.CoreV1().Endpoints("kube-system").Get(context.Background(), localEtpService.Name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())

			for _, pod := range containerPods.Items {
				nodes, err := f.KubeClientSet.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
				Expect(err).NotTo(HaveOccurred())
				for _, node := range nodes.Items {
					var hasEndpoint bool
					for _, subset := range endpoints.Subsets {
						for _, addr := range subset.Addresses {
							if addr.NodeName != nil && *addr.NodeName == node.Name {
								hasEndpoint = true
								break
							}
						}
						if hasEndpoint {
							break
						}
					}

					nodeIP := util.GetNodeInternalIP(node)
					output, err := exec.Command("kubectl", strings.Fields(kubectlArgs(pod.Name, nodeIP, port))...).CombinedOutput()
					outputStr := string(bytes.TrimSpace(output))
					if hasEndpoint {
						Expect(err).NotTo(HaveOccurred(), outputStr)
						Expect(outputStr).To(Equal("200"))
					} else {
						Expect(err).To(HaveOccurred())
						Expect(outputStr).To(HavePrefix("000"))
					}
				}
			}
		})

		It("host to NodePort", func() {
			port := localEtpService.Spec.Ports[0].NodePort

			endpoints, err := f.KubeClientSet.CoreV1().Endpoints("kube-system").Get(context.Background(), localEtpService.Name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())

			for _, pod := range hostPods.Items {
				nodes, err := f.KubeClientSet.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
				Expect(err).NotTo(HaveOccurred())
				for _, node := range nodes.Items {
					shouldSucceed := !ipvsMode && pod.Spec.NodeName == node.Name
					if !shouldSucceed {
						for _, subset := range endpoints.Subsets {
							for _, addr := range subset.Addresses {
								if addr.NodeName != nil && *addr.NodeName == node.Name {
									shouldSucceed = true
									break
								}
							}
							if shouldSucceed {
								break
							}
						}
					}

					nodeIP := util.GetNodeInternalIP(node)
					output, err := exec.Command("kubectl", strings.Fields(kubectlArgs(pod.Name, nodeIP, port))...).CombinedOutput()
					outputStr := string(bytes.TrimSpace(output))
					if shouldSucceed {
						Expect(err).NotTo(HaveOccurred(), outputStr)
						Expect(outputStr).To(Equal("200"))
					} else {
						Expect(err).To(HaveOccurred())
						Expect(outputStr).To(HavePrefix("000"))
					}
				}
			}
		})

		It("external to NodePort", func() {
			port := localEtpService.Spec.Ports[0].NodePort

			endpoints, err := f.KubeClientSet.CoreV1().Endpoints("kube-system").Get(context.Background(), localEtpService.Name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())

			nodes, err := f.KubeClientSet.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred())
			for _, node := range nodes.Items {
				var hasEndpoint bool
				for _, subset := range endpoints.Subsets {
					for _, addr := range subset.Addresses {
						if addr.NodeName != nil && *addr.NodeName == node.Name {
							hasEndpoint = true
							break
						}
					}
					if hasEndpoint {
						break
					}
				}

				nodeIP := util.GetNodeInternalIP(node)
				output, err := exec.Command("curl", strings.Fields(curlArgs(nodeIP, port))...).CombinedOutput()
				outputStr := string(bytes.TrimSpace(output))
				if hasEndpoint {
					Expect(err).NotTo(HaveOccurred(), outputStr)
					Expect(outputStr).To(Equal("200"))
				} else {
					Expect(err).To(HaveOccurred())
					Expect(outputStr).To(Equal("000"))
				}
			}
		})
	})
})
