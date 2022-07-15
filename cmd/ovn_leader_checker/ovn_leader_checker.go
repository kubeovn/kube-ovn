package ovn_leader_checker

import (
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovn_leader_checker"
)

func CmdMain() {
	cfg, err := ovn_leader_checker.ParseFlags()
	if err != nil {
		klog.Fatalf("ovn_leader_checker parseFlags error %v", err)
	}
	if err = ovn_leader_checker.KubeClientInit(cfg); err != nil {
		klog.Fatalf("KubeClientInit err %v", err)
	}
	ovn_leader_checker.StartOvnLeaderCheck(cfg)
}
