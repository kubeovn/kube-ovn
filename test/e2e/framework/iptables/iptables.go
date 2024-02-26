package iptables

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	e2epodoutput "k8s.io/kubernetes/test/e2e/framework/pod/output"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

func CheckIptablesRulesOnNode(f *framework.Framework, node, table, chain, protocol string, expectedRules []string, shouldExist bool) {
	ovsPod := getOvsPodOnNode(f, node)

	iptBin := "iptables"
	if protocol == apiv1.ProtocolIPv6 {
		iptBin = "ip6tables"
	}

	cmd := fmt.Sprintf(`%s -t %s -S `, iptBin, table)
	if chain != "" {
		cmd += chain
	}
	framework.WaitUntil(2*time.Second, time.Minute, func(_ context.Context) (bool, error) {
		output := e2epodoutput.RunHostCmdOrDie(ovsPod.Namespace, ovsPod.Name, cmd)
		rules := strings.Split(output, "\n")
		for _, r := range expectedRules {
			framework.Logf("checking rule %s", r)
			ok, err := gomega.ContainElement(gomega.HavePrefix(r)).Match(rules)
			if err != nil || ok != shouldExist {
				return false, err
			}
		}
		return true, nil
	}, "")
}

func getOvsPodOnNode(f *framework.Framework, node string) *corev1.Pod {
	daemonSetClient := f.DaemonSetClientNS(framework.KubeOvnNamespace)
	ds := daemonSetClient.Get("ovs-ovn")
	pod, err := daemonSetClient.GetPodOnNode(ds, node)
	framework.ExpectNoError(err)
	return pod
}
