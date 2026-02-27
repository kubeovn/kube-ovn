package connectivity

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand/v2"
	"net/url"
	"strconv"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/component-base/logs"
	"k8s.io/klog/v2"
	commontest "k8s.io/kubernetes/test/e2e/common"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/config"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/onsi/gomega/format"

	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	frameworkhttp "github.com/kubeovn/kube-ovn/test/e2e/framework/http"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/rpc"
)

const rpcAddr = "127.0.0.1:17070"

type RPCNotify struct {
	ch chan struct{}
}

func (r *RPCNotify) ProcessExit(_ struct{}, _ *struct{}) error {
	select {
	case r.ch <- struct{}{}:
	default:
	}
	return nil
}

func init() {
	klog.SetOutput(ginkgo.GinkgoWriter)

	// Register flags.
	config.CopyFlags(config.Flags, flag.CommandLine)
	k8sframework.RegisterCommonFlags(flag.CommandLine)
	k8sframework.RegisterClusterFlags(flag.CommandLine)
}

func TestE2E(t *testing.T) {
	k8sframework.AfterReadingAllFlags(&k8sframework.TestContext)

	logs.InitLogs()
	defer logs.FlushLogs()
	klog.EnableContextualLogging(true)

	gomega.RegisterFailHandler(k8sframework.Fail)

	// Run tests through the Ginkgo runner with output to console + JUnit for Jenkins
	suiteConfig, reporterConfig := k8sframework.CreateGinkgoConfig()
	klog.Infof("Starting e2e run %q on Ginkgo node %d", k8sframework.RunID, suiteConfig.ParallelProcess)
	ginkgo.RunSpecs(t, "Kube-OVN e2e suite", suiteConfig, reporterConfig)
}

type suiteContext struct {
	Node     string
	NodeIP   string
	NodePort int32
}

var suiteCtx suiteContext

var _ = ginkgo.SynchronizedBeforeSuite(func() []byte {
	// Reference common test to make the import valid.
	commontest.CurrentSuite = commontest.E2E

	namespaceName := "ns-" + framework.RandomSuffix()
	serviceName := "service-" + framework.RandomSuffix()
	deploymentName := "deploy-" + framework.RandomSuffix()

	cs, err := k8sframework.LoadClientset()
	framework.ExpectNoError(err)
	namespaceClient := framework.NewNamespaceClient(cs)
	deploymentClient := framework.NewDeploymentClient(cs, namespaceName)
	serviceClient := framework.NewServiceClient(cs, namespaceName)

	ginkgo.By("Creating namespace " + namespaceName)
	_ = namespaceClient.Create(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespaceName}})

	ginkgo.By("Creating deployment " + deploymentName)
	podLabels := map[string]string{"app": deploymentName}
	port := 8000 + rand.Int32N(1000)
	portStr := strconv.Itoa(int(port))
	args := []string{"netexec", "--http-port", portStr}
	deploy := framework.MakeDeployment(deploymentName, 1, podLabels, nil, "server", framework.AgnhostImage, "")
	deploy.Spec.Template.Spec.Containers[0].Args = args
	deploy = deploymentClient.CreateSync(deploy)

	pods, err := deploymentClient.GetPods(deploy)
	framework.ExpectNoError(err)
	framework.ExpectNotNil(pods)
	framework.ExpectNotEmpty(pods.Items, "no pod found in deployment "+deploymentName)
	suiteCtx.NodeIP = pods.Items[0].Status.HostIP
	suiteCtx.Node = pods.Items[0].Spec.NodeName

	ginkgo.By("Getting all nodes")
	nodes, err := cs.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	framework.ExpectNoError(err)
	framework.ExpectNotNil(nodes)
	framework.ExpectNotEmpty(nodes.Items)

	// use the internal IP of the node that is not the same as the pod node
	for _, node := range nodes.Items {
		if node.Name != suiteCtx.Node {
			ipv4, ipv6 := util.GetNodeInternalIP(node)
			if ipv4 != "" {
				suiteCtx.NodeIP = ipv4
			} else {
				suiteCtx.NodeIP = ipv6
			}
			break
		}
	}

	ginkgo.By("Creating service " + serviceName)
	ports := []corev1.ServicePort{{
		Name:       "tcp",
		Protocol:   corev1.ProtocolTCP,
		Port:       port,
		TargetPort: intstr.FromInt32(port),
	}}
	service := framework.MakeService(serviceName, corev1.ServiceTypeNodePort, nil, podLabels, ports, "")
	service = serviceClient.CreateSync(service, func(s *corev1.Service) (bool, error) {
		return len(s.Spec.Ports) != 0 && s.Spec.Ports[0].NodePort != 0, nil
	}, "node port is allocated")
	suiteCtx.NodePort = service.Spec.Ports[0].NodePort

	ginkgo.DeferCleanup(func() {
		ginkgo.By("Deleting service " + serviceName)
		serviceClient.DeleteSync(serviceName)

		ginkgo.By("Deleting deployment " + deploymentName)
		deploymentClient.DeleteSync(deploymentName)

		ginkgo.By("Deleting namespace " + namespaceName)
		namespaceClient.DeleteSync(namespaceName)
	})

	data, err := json.Marshal(&suiteCtx)
	framework.ExpectNoError(err)
	return data
}, func(data []byte) {
	err := json.Unmarshal(data, &suiteCtx)
	framework.ExpectNoError(err)
	framework.Logf("suite context: %#v", suiteCtx)
})

var _ = framework.Describe("[group:connectivity]", func() {
	var server *rpc.Server
	var notify *RPCNotify

	ginkgo.BeforeEach(ginkgo.OncePerOrdered, func() {
		ginkgo.By("Creating RPC server listening on " + rpcAddr + " to receive ginkgo process notification")
		var err error
		notify = &RPCNotify{ch: make(chan struct{}, 1)}
		server, err = rpc.NewServer(rpcAddr, notify)
		framework.ExpectNoError(err)
	})
	ginkgo.AfterEach(ginkgo.OncePerOrdered, func() {
		ginkgo.By("Closing RPC server")
		framework.ExpectNoError(server.Shutdown(context.Background()))
	})

	ginkgo.It("Continuous NodePort HTTP testing", func() {
		u := url.URL{
			Scheme: "http",
			Host:   util.JoinHostPort(suiteCtx.NodeIP, suiteCtx.NodePort),
			Path:   "/clientip",
		}
		ginkgo.By("GET " + u.String())
		reports, err := frameworkhttp.Loop(nil, "NodePort", u.String(), "GET", 0, 100, 5000, 200, notify.ch)
		framework.ExpectNoError(err)

		if len(reports) == 0 {
			framework.Logf("no HTTP failure reported")
			return
		}

		framework.Logf("HTTP failures:")
		for _, r := range reports {
			framework.Logf("index = %03d, timestamp = %v, message = %v", r.Index, r.Timestamp, r.Attachments)
		}

		framework.Failf("%d HTTP failures occurred", len(reports))
	})
})

var _ = framework.OrderedDescribe("[group:disaster]", func() {
	var cs clientset.Interface
	var err error

	ginkgo.BeforeAll(func() {
		ginkgo.By("Waiting 3s")
		time.Sleep(3 * time.Second)

		cs, err = k8sframework.LoadClientset()
		framework.ExpectNoError(err)
	})

	ginkgo.AfterAll(func() {
		ginkgo.By("Waiting 3s")
		time.Sleep(3 * time.Second)

		ginkgo.By("Notify ginkgo process exit")
		reply := &struct{}{}
		err := rpc.Call(rpcAddr, "RPCNotify.ProcessExit", struct{}{}, reply)
		framework.ExpectNoError(err)
	})

	framework.DisruptiveIt("Recreating ovs-ovn pod", func() {
		ginkgo.By("Getting DaemonSet ovs-ovn")
		dsClient := framework.NewDaemonSetClient(cs, framework.KubeOvnNamespace)
		ds := dsClient.Get("ovs-ovn")

		ginkgo.By("Getting pods of DaemonSet ovs-ovn")
		pods, err := dsClient.GetPods(ds)
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(pods.Items, "no pod found in DaemonSet ovs-ovn")

		ginkgo.By("Getting ovs-ovn pod running on node " + suiteCtx.Node)
		var pod *corev1.Pod
		for i := range pods.Items {
			framework.Logf("pod %s is running on %s", pods.Items[i].Name, pods.Items[i].Spec.NodeName)
			if pods.Items[i].Spec.NodeName == suiteCtx.Node {
				pod = &pods.Items[i]
				break
			}
		}
		framework.ExpectNotNil(pod, "no ovs-ovn pod running on node "+suiteCtx.Node)

		ginkgo.By("Deleting pod " + pod.Name)
		err = cs.CoreV1().Pods(pod.Namespace).Delete(context.TODO(), pod.Name, metav1.DeleteOptions{})
		framework.ExpectNoError(err, "failed to delete pod "+pod.Name)

		ginkgo.By("Waiting for DaemonSet ovs-ovn to be ready")
		dsClient.RolloutStatus(ds.Name)

		ginkgo.By("Waiting for newly created ovs-ovn pod running on node " + suiteCtx.Node)
		pod = nil
		framework.WaitUntil(2*time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
			ds = dsClient.Get("ovs-ovn")
			pods, err = dsClient.GetPods(ds)
			if err != nil {
				return false, err
			}
			for i := range pods.Items {
				if pods.Items[i].Spec.NodeName == suiteCtx.Node &&
					pods.Items[i].Status.Phase == corev1.PodRunning &&
					len(pods.Items[i].Status.ContainerStatuses) != 0 &&
					pods.Items[i].Status.ContainerStatuses[0].Ready {
					pod = &pods.Items[i]
					return true, nil
				}
			}
			return false, nil
		}, "waiting for ovs-ovn pod on node "+suiteCtx.Node+" to be running and ready")
		pod.ManagedFields = nil
		framework.Logf("newly created ovs-ovn pod %s:\n%s", pod.Name, format.Object(pod, 2))
	})

	framework.DisruptiveIt("Recreating ovn-central pod", func() {
		ginkgo.By("Getting deployment ovn-central")
		deploymentClient := framework.NewDeploymentClient(cs, framework.KubeOvnNamespace)
		deploy := deploymentClient.Get("ovn-central")

		ginkgo.By("Getting pods of deployment ovn-central")
		pods, err := deploymentClient.GetPods(deploy)
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(pods.Items, "no pod found in deployment ovn-central")

		for _, pod := range pods.Items {
			ginkgo.By("Deleting pod " + pod.Name + " running on " + pod.Spec.NodeName)
			err = cs.CoreV1().Pods(pod.Namespace).Delete(context.TODO(), pod.Name, metav1.DeleteOptions{})
			framework.ExpectNoError(err, "failed to delete pod "+pod.Name)
		}

		ginkgo.By("Waiting for deployment ovn-central to be ready")
		deploymentClient.RolloutStatus(deploy.Name)

		ginkgo.By("Getting pods of deployment ovn-central")
		pods, err = deploymentClient.GetPods(deploy)
		framework.ExpectNoError(err)

		for _, pod := range pods.Items {
			pod.ManagedFields = nil
			framework.Logf("new created pod %s running on node %s:\n%s", pod.Name, pod.Spec.NodeName, format.Object(pod, 2))
		}
	})

	framework.DisruptiveIt("Stop ovn sb process", func() {
		ginkgo.By("Getting deployment ovn-central")
		deploymentClient := framework.NewDeploymentClient(cs, framework.KubeOvnNamespace)
		deploy := deploymentClient.Get("ovn-central")

		ginkgo.By("Getting pods of deployment ovn-central")
		pods, err := deploymentClient.GetPods(deploy)
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(pods.Items, "no pod found in deployment ovn-central")

		for _, pod := range pods.Items {
			ginkgo.By("Getting ovn sb pid of pod " + pod.Name + " running on " + pod.Spec.NodeName)
			cmd := []string{"cat", "/run/ovn/ovnsb_db.pid"}
			stdout, stderr, err := framework.KubectlExec(pod.Namespace, pod.Name, cmd...)
			framework.ExpectNoError(err, "failed to get ovn sb pid: %v, %s", err, string(stderr))
			pid := string(bytes.TrimSpace(stdout))
			framework.Logf("ovn sb pid: %s", pid)

			ginkgo.By("Stopping ovn sb process by sending a STOP signal")
			cmd = []string{"sh", "-c", fmt.Sprintf(`"kill -STOP %s"`, pid)}
			_, stderr, err = framework.KubectlExec(pod.Namespace, pod.Name, cmd...)
			framework.ExpectNoError(err, "failed to send STOP signal to ovn sb process: %v, %s", err, string(stderr))
		}

		ginkgo.By("Waiting 60s")
		time.Sleep(60 * time.Second)

		for _, pod := range pods.Items {
			ginkgo.By("Getting ovn sb pid of pod " + pod.Name + " running on " + pod.Spec.NodeName)
			cmd := []string{"cat", "/run/ovn/ovnsb_db.pid"}
			stdout, stderr, err := framework.KubectlExec(pod.Namespace, pod.Name, cmd...)
			framework.ExpectNoError(err, "failed to get ovn sb pid: %v, %s", err, string(stderr))
			pid := string(bytes.TrimSpace(stdout))
			framework.Logf("ovn sb pid: %s", pid)

			ginkgo.By("Stopping ovn sb process by sending a CONT signal")
			cmd = []string{"sh", "-c", fmt.Sprintf(`"kill -CONT %s"`, pid)}
			_, stderr, err = framework.KubectlExec(pod.Namespace, pod.Name, cmd...)
			framework.ExpectNoError(err, "failed to send CONT signal to ovn sb process: %v, %s", err, string(stderr))
		}
	})

	framework.DisruptiveIt("Stop ovn-controller process", func() {
		ginkgo.By("Getting DaemonSet ovs-ovn")
		dsClient := framework.NewDaemonSetClient(cs, framework.KubeOvnNamespace)
		ds := dsClient.Get("ovs-ovn")

		ginkgo.By("Getting pods of DaemonSet ovs-ovn")
		pods, err := dsClient.GetPods(ds)
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(pods.Items, "no pod found in DaemonSet ovs-ovn")

		ginkgo.By("Getting ovs-ovn pod running on node " + suiteCtx.Node)
		var pod *corev1.Pod
		for i := range pods.Items {
			framework.Logf("pod %s is running on %s", pods.Items[i].Name, pods.Items[i].Spec.NodeName)
			if pods.Items[i].Spec.NodeName == suiteCtx.Node {
				pod = &pods.Items[i]
				break
			}
		}
		framework.ExpectNotNil(pod, "no ovs-ovn pod running on node "+suiteCtx.Node)

		ginkgo.By("Getting ovn-controller pid")
		cmd := []string{"sh", "-c", `"pidof -s ovn-controller"`}
		stdout, stderr, err := framework.KubectlExec(pod.Namespace, pod.Name, cmd...)
		framework.ExpectNoError(err, "failed to get ovn-controller pid: %v, %s", err, string(stderr))
		pid := string(bytes.TrimSpace(stdout))
		framework.Logf("ovn-controller pid: %s", pid)

		ginkgo.By("Stopping ovn-controller process by sending a STOP signal")
		cmd = []string{"sh", "-c", fmt.Sprintf(`"kill -STOP %s"`, pid)}
		_, stderr, err = framework.KubectlExec(pod.Namespace, pod.Name, cmd...)
		framework.ExpectNoError(err, "failed to send STOP signal to ovn-controller process: %v, %s", err, string(stderr))

		ginkgo.By("Waiting 60s")
		time.Sleep(60 * time.Second)

		ginkgo.By("Continuing the stopped ovn-controller process by sending a CONT signal")
		cmd = []string{"sh", "-c", fmt.Sprintf(`"kill -CONT %s"`, pid)}
		_, stderr, err = framework.KubectlExec(pod.Namespace, pod.Name, cmd...)
		framework.ExpectNoError(err, "failed to send CONT signal to ovn-controller process: %v, %s", err, string(stderr))
	})

	framework.DisruptiveIt("Stop ovs-vswitchd process", func() {
		ginkgo.By("Getting DaemonSet ovs-ovn")
		dsClient := framework.NewDaemonSetClient(cs, framework.KubeOvnNamespace)
		ds := dsClient.Get("ovs-ovn")

		ginkgo.By("Getting pods of DaemonSet ovs-ovn")
		pods, err := dsClient.GetPods(ds)
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(pods.Items, "no pod found in DaemonSet ovs-ovn")

		ginkgo.By("Getting ovs-ovn pod running on node " + suiteCtx.Node)
		var pod *corev1.Pod
		for i := range pods.Items {
			framework.Logf("pod %s is running on %s", pods.Items[i].Name, pods.Items[i].Spec.NodeName)
			if pods.Items[i].Spec.NodeName == suiteCtx.Node {
				pod = &pods.Items[i]
				break
			}
		}
		framework.ExpectNotNil(pod, "no ovs-ovn pod running on node "+suiteCtx.Node)

		ginkgo.By("Getting ovs-vswitchd pid")
		cmd := []string{"sh", "-c", `"pidof -s ovs-vswitchd"`}
		stdout, stderr, err := framework.KubectlExec(pod.Namespace, pod.Name, cmd...)
		framework.ExpectNoError(err, "failed to get ovs-vswitchd pid: %v, %s", err, string(stderr))
		pid := string(bytes.TrimSpace(stdout))
		framework.Logf("ovs-vswitchd pid: %s", pid)

		ginkgo.By("Stopping ovs-vswitchd process by sending a STOP signal")
		cmd = []string{"sh", "-c", fmt.Sprintf(`"kill -STOP %s"`, pid)}
		_, stderr, err = framework.KubectlExec(pod.Namespace, pod.Name, cmd...)
		framework.ExpectNoError(err, "failed to send STOP signal to ovs-vswitchd process: %v, %s", err, string(stderr))

		ginkgo.By("Waiting 60s")
		time.Sleep(60 * time.Second)

		ginkgo.By("Continuing the stopped ovs-vswitchd process by sending a CONT signal")
		cmd = []string{"sh", "-c", fmt.Sprintf(`"kill -CONT %s"`, pid)}
		_, stderr, err = framework.KubectlExec(pod.Namespace, pod.Name, cmd...)
		framework.ExpectNoError(err, "failed to send CONT signal to ovs-vswitchd process: %v, %s", err, string(stderr))
	})
})
