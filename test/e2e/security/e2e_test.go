package security

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/test/e2e"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/config"
	"k8s.io/kubernetes/test/e2e/framework/deployment"
	e2enode "k8s.io/kubernetes/test/e2e/framework/node"

	"github.com/onsi/ginkgo/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/docker"
)

func init() {
	klog.SetOutput(ginkgo.GinkgoWriter)

	// Register flags.
	config.CopyFlags(config.Flags, flag.CommandLine)
	k8sframework.RegisterCommonFlags(flag.CommandLine)
	k8sframework.RegisterClusterFlags(flag.CommandLine)
}

func TestE2E(t *testing.T) {
	k8sframework.AfterReadingAllFlags(&k8sframework.TestContext)
	e2e.RunE2ETests(t)
}

func checkDeployment(f *framework.Framework, name, process string, ports ...string) {
	ginkgo.GinkgoHelper()

	ginkgo.By("Getting deployment " + name)
	deploy, err := f.ClientSet.AppsV1().Deployments(framework.KubeOvnNamespace).Get(context.TODO(), name, metav1.GetOptions{})
	framework.ExpectNoError(err, "failed to get deployment")
	err = deployment.WaitForDeploymentComplete(f.ClientSet, deploy)
	framework.ExpectNoError(err, "deployment failed to complete")
	deploy, err = f.ClientSet.AppsV1().Deployments(framework.KubeOvnNamespace).Get(context.TODO(), name, metav1.GetOptions{})
	framework.ExpectNoError(err, "failed to get deployment")

	ginkgo.By("Getting pods")
	pods, err := deployment.GetPodsForDeployment(context.Background(), f.ClientSet, deploy)
	framework.ExpectNoError(err, "failed to get pods")
	framework.ExpectNotEmpty(pods.Items)

	checkPods(f, pods.Items, process, ports...)
}

func checkPods(f *framework.Framework, pods []corev1.Pod, process string, ports ...string) {
	ginkgo.GinkgoHelper()

	ginkgo.By("Parsing environment variable")
	var envValue string
	for _, env := range pods[0].Spec.Containers[0].Env {
		if env.Name == "ENABLE_BIND_LOCAL_IP" {
			envValue = env.Value
			break
		}
	}
	if envValue == "" {
		envValue = "false"
	}
	listenPodIP, err := strconv.ParseBool(envValue)
	framework.ExpectNoError(err)

	if listenPodIP &&
		(len(pods[0].Status.PodIPs) != 1 && (!strings.HasPrefix(process, "kube-ovn-") || f.VersionPriorTo(1, 13))) &&
		(process != "ovsdb-server" || f.VersionPriorTo(1, 12)) {
		// ovn db processes support listening on both ipv4 and ipv6 addresses in versions >= 1.12
		listenPodIP = false
	}

	ginkgo.By("Validating " + process + " listen addresses")
	cmd := fmt.Sprintf(`ss -Hntpl | grep -wE pid=$(pidof %s | sed "s/ /|pid=/g") | awk '{print $4}'`, process)
	if len(ports) != 0 {
		cmd += fmt.Sprintf(`| grep -E ':%s$'`, strings.Join(ports, `$|:`))
	}
	for _, pod := range pods {
		framework.ExpectTrue(pod.Spec.HostNetwork, "pod %s/%s is not using host network", pod.Namespace, pod.Name)

		c, err := docker.ContainerInspect(pod.Spec.NodeName)
		framework.ExpectNoError(err)

		var lastReason string
		err = wait.PollUntilContextTimeout(context.Background(), 2*time.Second, time.Minute, true, func(_ context.Context) (bool, error) {
			stdout, _, err := docker.Exec(c.ID, nil, "sh", "-c", cmd)
			if err != nil {
				lastReason = err.Error()
				return false, nil
			}

			listenAddresses := strings.Split(string(bytes.TrimSpace(stdout)), "\n")
			if len(ports) != 0 {
				expected := expectedListenAddresses(pod, listenPodIP, ports...)
				if !sameStringElements(listenAddresses, expected) {
					lastReason = fmt.Sprintf("got listen addresses %v, expected %v", listenAddresses, expected)
					return false, nil
				}
			} else {
				podIPPrefix := strings.TrimSuffix(net.JoinHostPort(pod.Status.PodIP, "999"), "999")
				for _, addr := range listenAddresses {
					if listenPodIP && !strings.HasPrefix(addr, podIPPrefix) {
						lastReason = fmt.Sprintf("got listen address %q, expected prefix %q", addr, podIPPrefix)
						return false, nil
					}
					if !listenPodIP && !strings.HasPrefix(addr, "*:") {
						lastReason = fmt.Sprintf("got listen address %q, expected wildcard address", addr)
						return false, nil
					}
				}
			}
			return true, nil
		})
		framework.ExpectNoError(err, "failed to validate %s listen addresses in pod %s/%s on node %s: %s", process, pod.Namespace, pod.Name, pod.Spec.NodeName, lastReason)
	}
}

func expectedListenAddresses(pod corev1.Pod, listenPodIP bool, ports ...string) []string {
	expected := make([]string, 0, len(pod.Status.PodIPs)*len(ports))
	for _, port := range ports {
		if listenPodIP {
			for _, ip := range pod.Status.PodIPs {
				expected = append(expected, net.JoinHostPort(ip.IP, port))
			}
		} else {
			expected = append(expected, net.JoinHostPort("*", port))
		}
	}
	return expected
}

func sameStringElements(actual, expected []string) bool {
	if len(actual) != len(expected) {
		return false
	}

	counts := make(map[string]int, len(expected))
	for _, value := range expected {
		counts[value]++
	}
	for _, value := range actual {
		if counts[value] == 0 {
			return false
		}
		counts[value]--
	}
	return true
}

func containerEnvValues(pods []corev1.Pod, containerName string) map[string]string {
	ginkgo.GinkgoHelper()
	framework.ExpectNotEmpty(pods, "no pods found")

	envs := map[string]string{}
	for _, container := range pods[0].Spec.Containers {
		if container.Name != containerName {
			continue
		}
		for _, env := range container.Env {
			envs[env.Name] = env.Value
		}
		return envs
	}
	framework.Failf("container %s not found in pod %s/%s", containerName, pods[0].Namespace, pods[0].Name)
	return nil
}

const ovnDBSSLConfigCommand = `set -euo pipefail
. /kube-ovn/ovn-db-ssl-options.sh
expected() {
  ovn_db_ssl_args /var/run/tls | sed -n "s/^--ovn-$1-db-ssl-$2=//p" | head -n1
}
actual() {
  tr '\000' '\n' < /proc/"$(cat /var/run/ovn/ovn"$1"_db.pid)"/cmdline | sed -n "s/^--ssl-$2=//p" | head -n1
}
openssl_probe() {
  port="$1"
  shift
  connect="$POD_IP:$port"
  case "$POD_IP" in
    *:*) connect="[$POD_IP]:$port" ;;
  esac
  timeout 10 openssl s_client -connect "$connect" -cert /var/run/tls/cert -key /var/run/tls/key -CAfile /var/run/tls/cacert -verify_return_error -brief "$@" </dev/null >/tmp/ovn-db-openssl.out 2>&1
}
listening() {
  ss -Hntl | awk '{print $4}' | grep -Eq "(\]|:)$1$"
}
check() {
  db="$1"
  field="$2"
  expected_value="$(expected "$db" "$field")"
  actual_value="$(actual "$db" "$field")"
  if [ "$actual_value" != "$expected_value" ]; then
    echo "ovn-$db ssl_$field: expected <$expected_value>, got <$actual_value>"
    exit 1
  fi
}
supports_protocol() {
  protocols="$1"
  protocol="$2"
  [ -z "$protocols" ] || [ "$protocols" != "${protocols#*$protocol}" ]
}
reject_protocol() {
  db="$1"
  port="$2"
  protocol="$3"
  option="$4"
  if openssl_probe "$port" "$option"; then
    echo "ovn-$db unexpectedly accepted $protocol"
    exit 1
  fi
}
check_handshake() {
  db="$1"
  port="$2"
  required="${3:-true}"
  if ! listening "$port"; then
    if [ "$required" = "true" ]; then
      echo "ovn-$db is not listening on port $port"
      exit 1
    fi
    return 0
  fi
  protocols="$(actual "$db" protocols)"
  cipher="$(actual "$db" ciphers | cut -d: -f1)"
  ciphersuite="$(actual "$db" ciphersuites | cut -d: -f1)"

  if [ -n "$cipher" ] && supports_protocol "$protocols" TLSv1.2; then
    openssl_probe "$port" -tls1_2 -cipher "$cipher" || {
      echo "ovn-$db rejected expected TLSv1.2 cipher $cipher"
      cat /tmp/ovn-db-openssl.out
      exit 1
    }
  fi
  if [ -n "$ciphersuite" ] && supports_protocol "$protocols" TLSv1.3; then
    openssl_probe "$port" -tls1_3 -ciphersuites "$ciphersuite" || {
      echo "ovn-$db rejected expected TLSv1.3 ciphersuite $ciphersuite"
      cat /tmp/ovn-db-openssl.out
      exit 1
    }
  fi

  case "${TLS_MIN_VERSION:-}" in
    1.2 | "TLS 1.2" | TLS12)
      reject_protocol "$db" "$port" TLSv1.1 -tls1_1
      ;;
    1.3 | "TLS 1.3" | TLS13)
      reject_protocol "$db" "$port" TLSv1.1 -tls1_1
      reject_protocol "$db" "$port" TLSv1.2 -tls1_2
      ;;
  esac
}
check nb protocols
check nb ciphers
check nb ciphersuites
check sb protocols
check sb ciphers
check sb ciphersuites
check_handshake nb "${NB_PORT:-6641}"
check_handshake sb "${SB_PORT:-6642}"
check_handshake nb "${NB_CLUSTER_PORT:-6643}" false
check_handshake sb "${SB_CLUSTER_PORT:-6644}" false
`

var _ = framework.Describe("[group:security]", func() {
	f := framework.NewDefaultFramework("security")
	f.SkipNamespaceCreation = true

	var cs clientset.Interface
	ginkgo.BeforeEach(func() {
		f.SkipVersionPriorTo(1, 9, "Support for listening on Pod IP was introduced in v1.9")
		cs = f.ClientSet
	})

	framework.ConformanceIt("ovn db should listen on specified addresses for client connections", func() {
		checkDeployment(f, "ovn-central", "ovsdb-server", strconv.Itoa(int(util.NBDatabasePort)), strconv.Itoa(int(util.SBDatabasePort)))
	})

	framework.ConformanceIt("ovn db should apply configured server TLS options", func() {
		f.SkipVersionPriorTo(1, 15, "OVN DB server TLS options were introduced in v1.15")

		ginkgo.By("Getting deployment ovn-central")
		deploy, err := f.ClientSet.AppsV1().Deployments(framework.KubeOvnNamespace).Get(context.TODO(), "ovn-central", metav1.GetOptions{})
		framework.ExpectNoError(err, "failed to get deployment")
		err = deployment.WaitForDeploymentComplete(f.ClientSet, deploy)
		framework.ExpectNoError(err, "deployment failed to complete")
		deploy, err = f.ClientSet.AppsV1().Deployments(framework.KubeOvnNamespace).Get(context.TODO(), "ovn-central", metav1.GetOptions{})
		framework.ExpectNoError(err, "failed to get deployment")

		ginkgo.By("Getting pods")
		pods, err := deployment.GetPodsForDeployment(context.Background(), f.ClientSet, deploy)
		framework.ExpectNoError(err, "failed to get pods")
		framework.ExpectNotEmpty(pods.Items)

		envs := containerEnvValues(pods.Items, "ovn-central")
		if envs["ENABLE_SSL"] != "true" {
			ginkgo.Skip("kube-ovn TLS is disabled")
		}
		if envs["TLS_MIN_VERSION"] == "" && envs["TLS_MAX_VERSION"] == "" && envs["TLS_CIPHER_SUITES"] == "" {
			ginkgo.Skip("OVN DB server TLS options are not configured")
		}

		for _, pod := range pods.Items {
			stdout, stderr, err := framework.ExecCommandInContainer(f, pod.Namespace, pod.Name, "ovn-central", "bash", "-c", ovnDBSSLConfigCommand)
			framework.ExpectNoError(err, "failed to validate OVN DB server TLS options in pod %s/%s\nstdout:\n%s\nstderr:\n%s", pod.Namespace, pod.Name, stdout, stderr)
		}
	})

	framework.ConformanceIt("kube-ovn-controller should listen on specified addresses", func() {
		checkDeployment(f, "kube-ovn-controller", "kube-ovn-controller", "10660")
	})

	framework.ConformanceIt("kube-ovn-monitor should listen on specified addresses", func() {
		checkDeployment(f, "kube-ovn-monitor", "kube-ovn-monitor", "10661")
	})

	framework.ConformanceIt("kube-ovn-cni should listen on specified addresses", func() {
		ginkgo.By("Getting nodes")
		nodeList, err := e2enode.GetReadySchedulableNodes(context.Background(), cs)
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(nodeList.Items)

		ginkgo.By("Getting daemonset kube-ovn-cni")
		daemonSetClient := f.DaemonSetClientNS(framework.KubeOvnNamespace)
		ds := daemonSetClient.Get("kube-ovn-cni")

		ginkgo.By("Getting kube-ovn-cni pods")
		pods := make([]corev1.Pod, 0, len(nodeList.Items))
		for _, node := range nodeList.Items {
			pod, err := daemonSetClient.GetPodOnNode(ds, node.Name)
			framework.ExpectNoError(err, "failed to get kube-ovn-cni pod running on node %s", node.Name)
			pods = append(pods, *pod)
		}

		checkPods(f, pods, "kube-ovn-daemon", "10665")
	})
})
