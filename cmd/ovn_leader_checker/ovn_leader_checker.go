package ovn_leader_checker

import (
	"github.com/kubeovn/kube-ovn/pkg/ovn_leader_checker"
	"os"
)

func CmdMain() {
	cfg, err := ovn_leader_checker.ParseFlags()
	if err != nil {
		os.Exit(-1)
	}

	err = ovn_leader_checker.KubeClientInit(cfg)
	if err != nil {
		os.Exit(-1)
	}

	err = ovn_leader_checker.StartOvnLeaderCheck(cfg)
	if err != nil {
		os.Exit(-1)
	}

	os.Exit(0)
}
