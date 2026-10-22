package iptables

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	e2epodoutput "k8s.io/kubernetes/test/e2e/framework/pod/output"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

func CheckIptablesRulesOnNode(f *framework.Framework, node, table, _, protocol string, expectedRules []string, shouldExist bool) {
	ovsPod := getOvsPodOnNode(f, node)

	iptBin := "iptables"
	if protocol == apiv1.ProtocolIPv6 {
		iptBin = "ip6tables"
	}

	// Query the full table so a chain that is still being created does not
	// turn an expected transient state into an iptables command failure.
	cmd := fmt.Sprintf(`%s -t %s -S`, iptBin, table)
	framework.WaitUntil(time.Second, time.Minute, func(_ context.Context) (bool, error) {
		output, err := e2epodoutput.RunHostCmd(ovsPod.Namespace, ovsPod.Name, cmd)
		if err != nil {
			// The tproxy worker creates and removes chains asynchronously. Keep
			// polling if the host command is temporarily unavailable while the
			// desired rule is converging.
			framework.Logf("failed to read iptables rules, retrying: %v", err)
			return false, nil
		}

		return matchRules(output, expectedRules, shouldExist)
	}, "")
}

func matchRules(output string, expectedRules []string, shouldExist bool) (bool, error) {
	rules := strings.Split(output, "\n")
	for _, r := range expectedRules {
		ok, err := gomega.ContainElement(gomega.HavePrefix(r)).Match(rules)
		if err != nil || ok != shouldExist {
			return false, err
		}
	}
	return true, nil
}

func getOvsPodOnNode(f *framework.Framework, node string) *corev1.Pod {
	ginkgo.GinkgoHelper()

	daemonSetClient := f.DaemonSetClientNS(framework.KubeOvnNamespace)
	ds := daemonSetClient.Get("ovs-ovn")
	pod, err := daemonSetClient.GetPodOnNode(ds, node)
	framework.ExpectNoError(err)
	return pod
}
