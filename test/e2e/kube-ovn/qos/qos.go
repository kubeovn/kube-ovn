package qos

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"

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

func getOvsPodOnNode(ctx context.Context, f *framework.Framework, node string) *corev1.Pod {
	ginkgo.GinkgoHelper()

	daemonSetClient := f.DaemonSetClientNS(framework.KubeOvnNamespace)
	ds := daemonSetClient.Get(ctx, "ovs-ovn")
	pod, err := daemonSetClient.GetPodOnNode(ctx, ds, node)
	framework.ExpectNoError(err)
	return pod
}

func getOvsQosForPod(ctx context.Context, f *framework.Framework, table string, pod *corev1.Pod) map[string]string {
	ginkgo.GinkgoHelper()

	ovsPod := getOvsPodOnNode(ctx, f, pod.Spec.NodeName)
	cmd := fmt.Sprintf(`ovs-vsctl --no-heading --columns=other_config --bare find %s external_ids:pod="%s/%s"`, table, pod.Namespace, pod.Name)
	output, _ := framework.KubectlExecOrDie(ctx, ovsPod.Namespace, ovsPod.Name, cmd)
	return parseConfig(table, string(output))
}

func waitOvsQosForPod(ctx context.Context, f *framework.Framework, table string, pod *corev1.Pod, expected map[string]string) map[string]string {
	ginkgo.GinkgoHelper()

	ovsPod := getOvsPodOnNode(ctx, f, pod.Spec.NodeName)
	cmd := fmt.Sprintf(`ovs-vsctl --no-heading --columns=other_config --bare find %s external_ids:pod="%s/%s"`, table, pod.Namespace, pod.Name)

	var config map[string]string
	framework.WaitUntil(ctx, 2*time.Minute, func(ctx context.Context) (bool, error) {
		output, _, err := framework.KubectlExec(ctx, ovsPod.Namespace, ovsPod.Name, cmd)
		if err != nil {
			return false, err
		}
		result := string(bytes.TrimSpace(output))
		if result == "" {
			return false, nil
		}
		kvs := parseConfig(table, result)
		for k, v := range expected {
			if kvs[k] != v {
				return false, nil
			}
		}

		config = kvs
		return true, nil
	}, "")

	return config
}

var _ = framework.Describe("[group:qos]", func() {
	f := framework.NewDefaultFramework("qos")

	var podName, namespaceName string
	var podClient *framework.PodClient

	ginkgo.BeforeEach(ginkgo.NodeTimeout(time.Second), func(_ ginkgo.SpecContext) {
		podClient = f.PodClient()
		namespaceName = f.Namespace.Name
		podName = "pod-" + framework.RandomSuffix()
	})
	ginkgo.AfterEach(ginkgo.NodeTimeout(15*time.Second), func(ctx ginkgo.SpecContext) {
		ginkgo.By("Deleting pod " + podName)
		podClient.DeleteSync(ctx, podName)
	})

	framework.ConformanceIt("should support netem QoS", ginkgo.SpecTimeout(30*time.Second), func(ctx ginkgo.SpecContext) {
		f.SkipVersionPriorTo(1, 9, "Support for netem QoS was introduced in v1.9")

		ginkgo.By("Creating pod " + podName)
		latency, jitter, limit, loss := 600, 400, 2000, 10
		annotations := map[string]string{
			util.NetemQosLatencyAnnotation: strconv.Itoa(latency),
			util.NetemQosLimitAnnotation:   strconv.Itoa(limit),
			util.NetemQosLossAnnotation:    strconv.Itoa(loss),
		}
		if !f.VersionPriorTo(1, 12) {
			annotations[util.NetemQosJitterAnnotation] = strconv.Itoa(jitter)
		}
		pod := framework.MakePod(namespaceName, podName, nil, annotations, "", nil, nil)
		pod = podClient.CreateSync(ctx, pod)

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
		qos := getOvsQosForPod(ctx, f, "qos", pod)
		framework.ExpectHaveKeyWithValue(qos, "latency", strconv.Itoa(latency*1000))
		if !f.VersionPriorTo(1, 12) {
			framework.ExpectHaveKeyWithValue(qos, "jitter", strconv.Itoa(jitter*1000))
		}
		framework.ExpectHaveKeyWithValue(qos, "limit", strconv.Itoa(limit))
		framework.ExpectHaveKeyWithValue(qos, "loss", strconv.Itoa(loss))
	})

	framework.ConformanceIt("should be able to update netem QoS", ginkgo.SpecTimeout(40*time.Second), func(ctx ginkgo.SpecContext) {
		f.SkipVersionPriorTo(1, 9, "Support for netem QoS was introduced in v1.9")

		ginkgo.By("Creating pod " + podName + " without QoS")
		pod := framework.MakePod(namespaceName, podName, nil, nil, "", nil, nil)
		pod = podClient.CreateSync(ctx, pod)

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
		pod = podClient.Patch(ctx, pod, modifiedPod)

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
		qos := waitOvsQosForPod(ctx, f, "qos", pod, nil)
		framework.ExpectHaveKeyWithValue(qos, "latency", strconv.Itoa(latency*1000))
		if !f.VersionPriorTo(1, 12) {
			framework.ExpectHaveKeyWithValue(qos, "jitter", strconv.Itoa(jitter*1000))
		}
		framework.ExpectHaveKeyWithValue(qos, "limit", strconv.Itoa(limit))
		framework.ExpectHaveKeyWithValue(qos, "loss", strconv.Itoa(loss))
	})

	framework.ConformanceIt("should support htb QoS", ginkgo.SpecTimeout(20*time.Second), func(ctx ginkgo.SpecContext) {
		f.SkipVersionPriorTo(1, 9, "Support for htb QoS with priority was introduced in v1.9")

		ginkgo.By("Creating pod " + podName)
		ingressRate := 300
		annotations := map[string]string{
			util.IngressRateAnnotation: strconv.Itoa(ingressRate),
		}
		pod := framework.MakePod(namespaceName, podName, nil, annotations, "", nil, nil)
		pod = podClient.CreateSync(ctx, pod)

		ginkgo.By("Validating pod annotations")
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.IngressRateAnnotation, strconv.Itoa(ingressRate))

		ginkgo.By("Validating OVS Queue")
		queue := getOvsQosForPod(ctx, f, "queue", pod)
		framework.ExpectHaveKeyWithValue(queue, "max-rate", strconv.Itoa(ingressRate*1000*1000))
	})
})
