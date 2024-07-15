package iptables

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

func CheckIptablesRulesOnNode(ctx context.Context, f *framework.Framework, node, table, chain, protocol string, expectedRules []string, shouldExist bool) {
	ovsPod := getOvsPodOnNode(ctx, f, node)

	iptBin := "iptables"
	if protocol == apiv1.ProtocolIPv6 {
		iptBin = "ip6tables"
	}

	cmd := fmt.Sprintf(`%s -t %s -S `, iptBin, table)
	if chain != "" {
		cmd += chain
	}
	framework.WaitUntil(ctx, time.Minute, func(ctx context.Context) (bool, error) {
		output, _, err := framework.KubectlExec(ctx, ovsPod.Namespace, ovsPod.Name, cmd)
		framework.ExpectNoError(err)
		rules := strings.Split(string(output), "\n")
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

func getOvsPodOnNode(ctx context.Context, f *framework.Framework, node string) *corev1.Pod {
	ginkgo.GinkgoHelper()

	daemonSetClient := f.DaemonSetClientNS(framework.KubeOvnNamespace)
	ds := daemonSetClient.Get(ctx, "ovs-ovn")
	pod, err := daemonSetClient.GetPodOnNode(ctx, ds, node)
	framework.ExpectNoError(err)
	return pod
}
