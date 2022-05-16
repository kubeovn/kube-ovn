package service

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	kubeadmconstants "k8s.io/kubernetes/cmd/kubeadm/app/constants"
	kubeproxyconfig "k8s.io/kubernetes/pkg/proxy/apis/config"
	kubeproxyscheme "k8s.io/kubernetes/pkg/proxy/apis/config/scheme"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

const namespace = "kube-system"

var dockerArgs = []string{"exec", "kube-ovn-e2e", "curl"}

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

func curlArgs(ip string, port int32) string {
	return fmt.Sprintf("-s -m 3 -o /dev/null -w %%{http_code} %s/metrics", util.JoinHostPort(ip, port))
}

func kubectlArgs(pod, ip string, port int32) string {
	return fmt.Sprintf("-n kube-system exec %s -- curl %s", pod, curlArgs(ip, port))
}

func setSvcTypeToNodePort(kubeClientSet kubernetes.Interface, name string) (*corev1.Service, error) {
	svc, err := kubeClientSet.CoreV1().Services(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if svc.Spec.Type == corev1.ServiceTypeNodePort {
		return svc, nil
	}

	newSvc := svc.DeepCopy()
	newSvc.Spec.Type = corev1.ServiceTypeNodePort
	return kubeClientSet.CoreV1().Services(svc.Namespace).Update(context.Background(), newSvc, metav1.UpdateOptions{})
}

func setSvcEtpToLocal(kubeClientSet kubernetes.Interface, name string) (*corev1.Service, error) {
	svc, err := kubeClientSet.CoreV1().Services(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if svc.Spec.Type == corev1.ServiceTypeNodePort && svc.Spec.ExternalTrafficPolicy == corev1.ServiceExternalTrafficPolicyTypeLocal {
		return svc, nil
	}

	newSvc := svc.DeepCopy()
	newSvc.Spec.Type = corev1.ServiceTypeNodePort
	newSvc.Spec.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicyTypeLocal
	return kubeClientSet.CoreV1().Services(svc.Namespace).Update(context.Background(), newSvc, metav1.UpdateOptions{})
}

func hasEndpoint(node string, endpoints *corev1.Endpoints) bool {
	for _, subset := range endpoints.Subsets {
		for _, addr := range subset.Addresses {
			if addr.NodeName != nil && *addr.NodeName == node {
				return true
			}
		}
	}
	return false
}

func checkService(checkCount int, shouldSucceed bool, cmd string, args ...string) {
	for i := 0; i < checkCount; i++ {
		c := exec.Command(cmd, args...)
		var stdout, stderr bytes.Buffer
		c.Stdout, c.Stderr = &stdout, &stderr
		err := c.Run()
		output := strings.TrimSpace(stdout.String())
		if shouldSucceed {
			Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("stdout: %s, stderr: %s", output, strings.TrimSpace(stderr.String())))
			Expect(output).To(Equal("200"))
		} else {
			Expect(err).To(HaveOccurred())
			Expect(output).To(Equal("000"))
		}
	}
}

var _ = Describe("[Service]", func() {
	f := framework.NewFramework("service", fmt.Sprintf("%s/.kube/config", os.Getenv("HOME")))
	hostPods, err := f.KubeClientSet.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{LabelSelector: "app=ovs"})
	Expect(err).NotTo(HaveOccurred())

	containerPods, err := f.KubeClientSet.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{LabelSelector: "app=kube-ovn-pinger"})
	Expect(err).NotTo(HaveOccurred())

	hostService, err := setSvcTypeToNodePort(f.KubeClientSet, "kube-ovn-cni")
	Expect(err).NotTo(HaveOccurred())

	containerService, err := setSvcTypeToNodePort(f.KubeClientSet, "kube-ovn-pinger")
	Expect(err).NotTo(HaveOccurred())

	localEtpHostService, err := setSvcEtpToLocal(f.KubeClientSet, "kube-ovn-monitor")
	Expect(err).NotTo(HaveOccurred())

	localEtpHostEndpoints, err := f.KubeClientSet.CoreV1().Endpoints(namespace).Get(context.Background(), localEtpHostService.Name, metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred())

	nodes, err := f.KubeClientSet.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	Expect(err).NotTo(HaveOccurred())
	checkCount := len(nodes.Items)

	var ciliumChaining, proxyIpvsMode bool
	_, err = f.KubeClientSet.AppsV1().DaemonSets(namespace).Get(context.Background(), "cilium", metav1.GetOptions{})
	if err == nil {
		ciliumChaining = true
	} else {
		Expect(errors.IsNotFound(err)).To(BeTrue())

		kubeProxyConfigMap, err := f.KubeClientSet.CoreV1().ConfigMaps(metav1.NamespaceSystem).Get(context.Background(), kubeadmconstants.KubeProxyConfigMap, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		kubeProxyConfig := &kubeproxyconfig.KubeProxyConfiguration{}
		err = k8sruntime.DecodeInto(kubeproxyscheme.Codecs.UniversalDecoder(), []byte(kubeProxyConfigMap.Data[kubeadmconstants.KubeProxyConfigMapKey]), kubeProxyConfig)
		Expect(err).NotTo(HaveOccurred())

		proxyIpvsMode = kubeProxyConfig.Mode == kubeproxyconfig.ProxyModeIPVS
	}

	Context("service with host network endpoints", func() {
		It("container to ClusterIP", func() {
			port := hostService.Spec.Ports[0].Port
			for _, ip := range hostService.Spec.ClusterIPs {
				for _, pod := range containerPods.Items {
					checkService(checkCount, true, "kubectl", strings.Fields(kubectlArgs(pod.Name, ip, port))...)
				}
			}
		})

		It("host to ClusterIP", func() {
			port := hostService.Spec.Ports[0].Port
			for _, ip := range hostService.Spec.ClusterIPs {
				for _, pod := range hostPods.Items {
					checkService(checkCount, true, "kubectl", strings.Fields(kubectlArgs(pod.Name, ip, port))...)
				}
			}
		})

		It("external to ClusterIP", func() {
			if ciliumChaining {
				return
			}

			port := hostService.Spec.Ports[0].Port
			for _, ip := range hostService.Spec.ClusterIPs {
				checkService(checkCount, true, "docker", append(dockerArgs, strings.Fields(curlArgs(ip, port))...)...)
			}
		})

		It("container to NodePort", func() {
			port := hostService.Spec.Ports[0].NodePort
			for _, pod := range containerPods.Items {
				for _, node := range nodes.Items {
					for _, nodeIP := range nodeIPs(node) {
						checkService(checkCount, true, "kubectl", strings.Fields(kubectlArgs(pod.Name, nodeIP, port))...)
					}
				}
			}
		})

		It("host to NodePort", func() {
			port := hostService.Spec.Ports[0].NodePort
			for _, pod := range hostPods.Items {
				for _, node := range nodes.Items {
					for _, nodeIP := range nodeIPs(node) {
						checkService(checkCount, true, "kubectl", strings.Fields(kubectlArgs(pod.Name, nodeIP, port))...)
					}
				}
			}
		})

		It("external to NodePort", func() {
			if ciliumChaining {
				return
			}

			port := hostService.Spec.Ports[0].NodePort
			for _, node := range nodes.Items {
				for _, nodeIP := range nodeIPs(node) {
					checkService(checkCount, true, "docker", append(dockerArgs, strings.Fields(curlArgs(nodeIP, port))...)...)
				}
			}
		})
	})

	Context("service with container network endpoints", func() {
		It("container to ClusterIP", func() {
			port := containerService.Spec.Ports[0].Port
			for _, ip := range containerService.Spec.ClusterIPs {
				for _, pod := range containerPods.Items {
					checkService(checkCount, true, "kubectl", strings.Fields(kubectlArgs(pod.Name, ip, port))...)
				}
			}
		})

		It("host to ClusterIP", func() {
			port := containerService.Spec.Ports[0].Port
			for _, ip := range containerService.Spec.ClusterIPs {
				for _, pod := range hostPods.Items {
					checkService(checkCount, true, "kubectl", strings.Fields(kubectlArgs(pod.Name, ip, port))...)
				}
			}
		})

		It("external to ClusterIP", func() {
			port := containerService.Spec.Ports[0].Port
			for _, ip := range containerService.Spec.ClusterIPs {
				checkService(checkCount, true, "docker", append(dockerArgs, strings.Fields(curlArgs(ip, port))...)...)
			}
		})

		It("container to NodePort", func() {
			port := containerService.Spec.Ports[0].NodePort
			for _, pod := range containerPods.Items {
				for _, node := range nodes.Items {
					for _, nodeIP := range nodeIPs(node) {
						checkService(checkCount, true, "kubectl", strings.Fields(kubectlArgs(pod.Name, nodeIP, port))...)
					}
				}
			}
		})

		It("host to NodePort", func() {
			port := containerService.Spec.Ports[0].NodePort
			for _, pod := range hostPods.Items {
				for _, node := range nodes.Items {
					for _, nodeIP := range nodeIPs(node) {
						checkService(checkCount, true, "kubectl", strings.Fields(kubectlArgs(pod.Name, nodeIP, port))...)
					}
				}
			}
		})

		It("external to NodePort", func() {
			if ciliumChaining {
				return
			}

			port := containerService.Spec.Ports[0].NodePort
			for _, node := range nodes.Items {
				for _, nodeIP := range nodeIPs(node) {
					checkService(checkCount, true, "docker", append(dockerArgs, strings.Fields(curlArgs(nodeIP, port))...)...)
				}
			}
		})
	})

	Context("host service with local external traffic policy", func() {
		It("container to ClusterIP", func() {
			port := localEtpHostService.Spec.Ports[0].Port
			for _, pod := range containerPods.Items {
				for _, ip := range localEtpHostService.Spec.ClusterIPs {
					checkService(checkCount, true, "kubectl", strings.Fields(kubectlArgs(pod.Name, ip, port))...)
				}
			}
		})

		It("host to ClusterIP", func() {
			port := localEtpHostService.Spec.Ports[0].Port
			for _, pod := range hostPods.Items {
				for _, ip := range localEtpHostService.Spec.ClusterIPs {
					checkService(checkCount, true, "kubectl", strings.Fields(kubectlArgs(pod.Name, ip, port))...)
				}
			}
		})

		It("external to ClusterIP", func() {
			if ciliumChaining {
				return
			}

			port := localEtpHostService.Spec.Ports[0].Port
			for _, ip := range localEtpHostService.Spec.ClusterIPs {
				checkService(checkCount, true, "docker", append(dockerArgs, strings.Fields(curlArgs(ip, port))...)...)
			}
		})

		It("container to NodePort", func() {
			if ciliumChaining {
				return
			}

			port := localEtpHostService.Spec.Ports[0].NodePort
			for _, node := range nodes.Items {
				hasEndpoint := hasEndpoint(node.Name, localEtpHostEndpoints)
				for _, pod := range containerPods.Items {
					shoudSucceed := hasEndpoint || !proxyIpvsMode
					for _, nodeIP := range nodeIPs(node) {
						checkService(checkCount, shoudSucceed, "kubectl", strings.Fields(kubectlArgs(pod.Name, nodeIP, port))...)
					}
				}
			}
		})

		It("host to NodePort", func() {
			if ciliumChaining {
				return
			}

			port := localEtpHostService.Spec.Ports[0].NodePort
			for _, node := range nodes.Items {
				hasEndpoint := hasEndpoint(node.Name, localEtpHostEndpoints)
				for _, pod := range hostPods.Items {
					shoudSucceed := hasEndpoint || (!proxyIpvsMode && pod.Spec.NodeName == node.Name)
					for _, nodeIP := range nodeIPs(node) {
						checkService(checkCount, shoudSucceed, "kubectl", strings.Fields(kubectlArgs(pod.Name, nodeIP, port))...)
					}
				}
			}
		})

		It("external to NodePort", func() {
			if ciliumChaining {
				return
			}

			port := localEtpHostService.Spec.Ports[0].NodePort
			for _, node := range nodes.Items {
				shouldSucceed := hasEndpoint(node.Name, localEtpHostEndpoints)
				for _, nodeIP := range nodeIPs(node) {
					checkService(checkCount, shouldSucceed, "docker", append(dockerArgs, strings.Fields(curlArgs(nodeIP, port))...)...)
				}
			}
		})
	})
})
