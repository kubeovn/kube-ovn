package metrics

import (
	"context"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/onsi/ginkgo/v2"

	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

const (
	// the exporters populate their gauges on a polling loop, so a scrape right after
	// the pod has started may not expose the samples we are looking for yet
	scrapeInterval = 2 * time.Second
	scrapeTimeout  = time.Minute
)

// sampleValue returns the value of the first sample of the given metric whose line contains
// all the given labels, e.g. `hostname="node1"`. Matching the labels individually keeps the
// lookup working whatever their order is and whenever a new label is added to the metric.
func sampleValue(metrics, name string, labels ...string) (float64, bool) {
	for line := range strings.Lines(metrics) {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, name+"{") && !strings.HasPrefix(line, name+" ") {
			continue
		}
		if slices.ContainsFunc(labels, func(label string) bool { return !strings.Contains(line, label) }) {
			continue
		}
		// the value is the last field: a label value may contain spaces
		index := strings.LastIndexByte(line, ' ')
		if index < 0 {
			continue
		}
		if value, err := strconv.ParseFloat(line[index+1:], 64); err == nil {
			return value, true
		}
	}
	return 0, false
}

// predicate is an assertion on the value of a sample.
type predicate struct {
	desc  string
	match func(float64) bool
}

func equalTo(expected float64) predicate {
	return predicate{
		desc:  fmt.Sprintf("to be %v", expected),
		match: func(value float64) bool { return value == expected },
	}
}

func oneOf(expected ...float64) predicate {
	return predicate{
		desc:  fmt.Sprintf("to be one of %v", expected),
		match: func(value float64) bool { return slices.Contains(expected, value) },
	}
}

func nonZero() predicate {
	return predicate{
		desc:  "to be non-zero",
		match: func(value float64) bool { return value != 0 },
	}
}

// expectation is an assertion on the sample of a metric identified by its labels.
type expectation struct {
	name   string
	labels []string
	pred   predicate
}

func (e expectation) String() string {
	return fmt.Sprintf("%s{%s}", e.name, strings.Join(e.labels, ","))
}

// waitForMetrics scrapes the metrics exposed by the given container from within the pod
// itself until all the expectations are met.
func waitForMetrics(f *framework.Framework, pod *corev1.Pod, container, port string, secure bool, expectations []expectation) {
	ginkgo.GinkgoHelper()

	scheme, curlArgs := "http", "-fsS"
	if secure {
		scheme, curlArgs = "https", "-fsSk"
	}
	url := fmt.Sprintf("%s://%s/metrics", scheme, net.JoinHostPort(pod.Status.PodIP, port))

	framework.WaitUntil(scrapeInterval, scrapeTimeout, func(_ context.Context) (bool, error) {
		metrics, _, err := framework.ExecShellInContainer(f, pod.Namespace, pod.Name, container, "curl "+curlArgs+" "+url)
		if err != nil {
			framework.Logf("failed to scrape %s: %v", url, err)
			return false, nil
		}

		for _, e := range expectations {
			value, ok := sampleValue(metrics, e.name, e.labels...)
			if !ok {
				framework.Logf("sample %s has not been exposed yet", e)
				return false, nil
			}
			if !e.pred.match(value) {
				framework.Logf("expected sample %s %s, got %v", e, e.pred.desc, value)
				return false, nil
			}
		}
		return true, nil
	}, fmt.Sprintf("metrics exposed at %s to meet all the expectations", url))
}

var _ = framework.Describe("[group:metrics]", func() {
	f := framework.NewDefaultFramework("metrics")

	// kube-ovn-pinger runs with hostPID disabled and as a non-root user, so it must resolve
	// the control socket of the ovs/ovn daemons from their pid files by itself
	framework.ConformanceIt("kube-ovn-pinger should report ovs and ovn-controller as up", func() {
		daemonSetClient := f.DaemonSetClientNS(framework.KubeOvnNamespace)
		pods, err := daemonSetClient.GetPods(daemonSetClient.Get("kube-ovn-pinger"))
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(pods.Items)

		for _, pod := range pods.Items {
			node := fmt.Sprintf("%q", pod.Spec.NodeName)
			ginkgo.By("Checking metrics exposed by pinger pod " + pod.Name + " on node " + pod.Spec.NodeName)
			expectations := []expectation{
				{name: "pinger_ovs_up", labels: []string{"nodeName=" + node}, pred: equalTo(1)},
				{name: "pinger_ovs_down", labels: []string{"nodeName=" + node}, pred: equalTo(0)},
				{name: "pinger_ovn_controller_up", labels: []string{"nodeName=" + node}, pred: equalTo(1)},
				{name: "pinger_ovn_controller_down", labels: []string{"nodeName=" + node}, pred: equalTo(0)},
				// collected from ovs-vswitchd via dpctl/dump-dps
				{name: "kube_ovn_dp_total", labels: []string{"hostname=" + node}, pred: nonZero()},
			}
			for _, component := range []string{"ovsdb-server", "ovs-vswitchd"} {
				expectations = append(expectations, expectation{
					name:   "kube_ovn_ovs_status",
					labels: []string{fmt.Sprintf("component=%q", component), "hostname=" + node},
					pred:   equalTo(1),
				})
			}
			waitForMetrics(f, &pod, "pinger", "8080", false, expectations)
		}
	})

	// kube-ovn-monitor runs in a pod of its own, so it must not rely on sharing a pid
	// namespace with ovn-northd to reach its control socket
	framework.ConformanceIt("kube-ovn-monitor should report ovn-northd as healthy", func() {
		deploymentClient := f.DeploymentClientNS(framework.KubeOvnNamespace)
		deploy := deploymentClient.Get("kube-ovn-monitor")
		pods, err := deploymentClient.GetPods(deploy)
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(pods.Items)

		const container = "kube-ovn-monitor"
		secure := false
		for _, c := range deploy.Spec.Template.Spec.Containers {
			if c.Name == container {
				secure = slices.Contains(c.Args, "--secure-serving=true")
				break
			}
		}

		for _, pod := range pods.Items {
			node := fmt.Sprintf("%q", pod.Spec.NodeName)
			ginkgo.By("Checking metrics exposed by monitor pod " + pod.Name + " on node " + pod.Spec.NodeName)
			// 1 stands for active/leader and 2 for standby/follower, 0 means unhealthy
			var expectations []expectation
			for _, component := range []string{"ovn-northd", "ovsdb-server-northbound", "ovsdb-server-southbound"} {
				expectations = append(expectations, expectation{
					name:   "kube_ovn_ovn_status",
					labels: []string{fmt.Sprintf("component=%q", component), "hostname=" + node},
					pred:   oneOf(1, 2),
				})
			}
			waitForMetrics(f, &pod, container, "10661", secure, expectations)
		}
	})
})
