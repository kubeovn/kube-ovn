package qos

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	e2epodoutput "k8s.io/kubernetes/test/e2e/framework/pod/output"

	"github.com/onsi/ginkgo/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

func parseConfig(table, config string) map[string]string {
	kvs := make(map[string]string, 3)
	for _, s := range strings.Fields(config) {
		kv := strings.Split(s, "=")
		if len(kv) != 2 {
			framework.Logf("ignore %s config %s", table, s)
			continue
		}
		kvs[kv[0]] = kv[1]
	}

	return kvs
}

func getOvsPodOnNode(f *framework.Framework, node string) *corev1.Pod {
	daemonSetClient := f.DaemonSetClientNS(framework.KubeOvnNamespace)
	ds := daemonSetClient.Get("ovs-ovn")
	pod, err := daemonSetClient.GetPodOnNode(ds, node)
	framework.ExpectNoError(err)
	return pod
}

func getOvsQosForPod(f *framework.Framework, table string, pod *corev1.Pod) map[string]string {
	ovsPod := getOvsPodOnNode(f, pod.Spec.NodeName)
	cmd := fmt.Sprintf(`ovs-vsctl --no-heading --columns=other_config --bare find %s external_ids:pod="%s/%s"`, table, pod.Namespace, pod.Name)
	output := e2epodoutput.RunHostCmdOrDie(ovsPod.Namespace, ovsPod.Name, cmd)
	return parseConfig(table, output)
}

func waitOvsQosForPod(f *framework.Framework, table string, pod *corev1.Pod, expected map[string]string) map[string]string {
	ovsPod := getOvsPodOnNode(f, pod.Spec.NodeName)
	cmd := fmt.Sprintf(`ovs-vsctl --no-heading --columns=other_config --bare find %s external_ids:pod="%s/%s"`, table, pod.Namespace, pod.Name)

	var config map[string]string
	err := wait.PollImmediate(2*time.Second, 2*time.Minute, func() (bool, error) {
		output, err := e2epodoutput.RunHostCmd(ovsPod.Namespace, ovsPod.Name, cmd)
		if err != nil {
			return false, err
		}
		if output == "" {
			return false, nil
		}
		kvs := parseConfig(table, output)
		for k, v := range expected {
			if kvs[k] != v {
				return false, nil
			}
		}

		config = kvs
		return true, nil
	})
	framework.ExpectNoError(err, "timed out getting ovs %s config for pod %s/%s", table, pod.Namespace, pod.Name)

	return config
}

var _ = framework.Describe("[group:qos]", func() {
	f := framework.NewDefaultFramework("qos")

	var subnetName, namespaceName string
	var podClient *framework.PodClient
	var subnetClient *framework.SubnetClient

	ginkgo.BeforeEach(func() {
		podClient = f.PodClient()
		subnetClient = f.SubnetClient()
		namespaceName = f.Namespace.Name
	})
	ginkgo.AfterEach(func() {
		if subnetName != "" {
			ginkgo.By("Deleting subnet " + subnetName)
			subnetClient.DeleteSync(subnetName)
		}
	})

	framework.ConformanceIt("should support netem QoS", func() {
		f.SkipVersionPriorTo(1, 9, "Support for netem QoS was introduced in v1.9")

		name := "pod-" + framework.RandomSuffix()
		ginkgo.By("Creating pod " + name)
		latency, jitter, limit, loss := 600, 400, 2000, 10
		annotations := map[string]string{
			util.NetemQosLatencyAnnotation: strconv.Itoa(latency),
			util.NetemQosLimitAnnotation:   strconv.Itoa(limit),
			util.NetemQosLossAnnotation:    strconv.Itoa(loss),
		}
		if !f.VersionPriorTo(1, 12) {
			annotations[util.NetemQosJitterAnnotation] = strconv.Itoa(jitter)
		}
		pod := framework.MakePod(namespaceName, name, nil, annotations, "", nil, nil)
		pod = podClient.CreateSync(pod)

		ginkgo.By("Validating pod annotations")
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.NetemQosLatencyAnnotation, strconv.Itoa(latency))
		if !f.VersionPriorTo(1, 12) {
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.NetemQosJitterAnnotation, strconv.Itoa(jitter))
		}
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.NetemQosLimitAnnotation, strconv.Itoa(limit))
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.NetemQosLossAnnotation, strconv.Itoa(loss))

		ginkgo.By("Validating OVS QoS")
		qos := getOvsQosForPod(f, "qos", pod)
		framework.ExpectHaveKeyWithValue(qos, "latency", strconv.Itoa(latency*1000))
		if !f.VersionPriorTo(1, 12) {
			framework.ExpectHaveKeyWithValue(qos, "jitter", strconv.Itoa(jitter*1000))
		}
		framework.ExpectHaveKeyWithValue(qos, "limit", strconv.Itoa(limit))
		framework.ExpectHaveKeyWithValue(qos, "loss", strconv.Itoa(loss))

		ginkgo.By("Deleting pod " + name)
		podClient.DeleteSync(pod.Name)
	})

	framework.ConformanceIt("should be able to update netem QoS", func() {
		f.SkipVersionPriorTo(1, 9, "Support for netem QoS was introduced in v1.9")

		name := "pod-" + framework.RandomSuffix()
		ginkgo.By("Creating pod " + name + " without QoS")
		pod := framework.MakePod(namespaceName, name, nil, nil, "", nil, nil)
		pod = podClient.CreateSync(pod)

		ginkgo.By("Validating pod annotations")
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")
		framework.ExpectNotHaveKey(pod.Annotations, util.NetemQosLatencyAnnotation)
		framework.ExpectNotHaveKey(pod.Annotations, util.NetemQosJitterAnnotation)
		framework.ExpectNotHaveKey(pod.Annotations, util.NetemQosLimitAnnotation)
		framework.ExpectNotHaveKey(pod.Annotations, util.NetemQosLossAnnotation)

		ginkgo.By("Adding netem QoS to pod annotations")
		latency, jitter, limit, loss := 600, 400, 2000, 10
		modifiedPod := pod.DeepCopy()
		modifiedPod.Annotations[util.NetemQosLatencyAnnotation] = strconv.Itoa(latency)
		if !f.VersionPriorTo(1, 12) {
			modifiedPod.Annotations[util.NetemQosJitterAnnotation] = strconv.Itoa(jitter)
		}
		modifiedPod.Annotations[util.NetemQosLimitAnnotation] = strconv.Itoa(limit)
		modifiedPod.Annotations[util.NetemQosLossAnnotation] = strconv.Itoa(loss)
		pod = podClient.PatchPod(pod, modifiedPod)

		ginkgo.By("Validating pod annotations")
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.NetemQosLatencyAnnotation, strconv.Itoa(latency))
		if !f.VersionPriorTo(1, 12) {
			framework.ExpectHaveKeyWithValue(pod.Annotations, util.NetemQosJitterAnnotation, strconv.Itoa(jitter))
		}
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.NetemQosLimitAnnotation, strconv.Itoa(limit))
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.NetemQosLossAnnotation, strconv.Itoa(loss))

		ginkgo.By("Validating OVS QoS")
		qos := waitOvsQosForPod(f, "qos", pod, nil)
		framework.ExpectHaveKeyWithValue(qos, "latency", strconv.Itoa(latency*1000))
		if !f.VersionPriorTo(1, 12) {
			framework.ExpectHaveKeyWithValue(qos, "jitter", strconv.Itoa(jitter*1000))
		}
		framework.ExpectHaveKeyWithValue(qos, "limit", strconv.Itoa(limit))
		framework.ExpectHaveKeyWithValue(qos, "loss", strconv.Itoa(loss))

		ginkgo.By("Deleting pod " + name)
		podClient.DeleteSync(pod.Name)
	})

	framework.ConformanceIt("should support htb QoS", func() {
		f.SkipVersionPriorTo(1, 9, "Support for htb QoS with priority was introduced in v1.9")

		name := "pod-" + framework.RandomSuffix()
		ginkgo.By("Creating pod " + name)
		ingressRate := 300
		annotations := map[string]string{
			util.IngressRateAnnotation: strconv.Itoa(ingressRate),
		}
		pod := framework.MakePod(namespaceName, name, nil, annotations, "", nil, nil)
		pod = podClient.CreateSync(pod)

		ginkgo.By("Validating pod annotations")
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.IngressRateAnnotation, strconv.Itoa(ingressRate))

		ginkgo.By("Validating OVS Queue")
		queue := getOvsQosForPod(f, "queue", pod)
		framework.ExpectHaveKeyWithValue(queue, "max-rate", strconv.Itoa(ingressRate*1000*1000))

		ginkgo.By("Deleting pod " + name)
		podClient.DeleteSync(pod.Name)
	})
})
