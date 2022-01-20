package ovn_leader_checker

import (
	"github.com/kubeovn/kube-ovn/pkg/ovn_leader_checker"
	"k8s.io/klog/v2"
)

func CmdMain() {
	cfg, err := ovn_leader_checker.ParseFlags()
	if err != nil {
		klog.Errorf("ovn_leader_checker parseFlags error %v", err)
	}
	err = ovn_leader_checker.KubeClientInit(cfg)
	if err != nil {
		klog.Errorf("KubeClientInit err %v", err)
	}
	ovn_leader_checker.StartOvnLeaderCheck(cfg)
}
