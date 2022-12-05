package ovn_leader_checker

import (
	"github.com/kubeovn/kube-ovn/pkg/ovn_leader_checker"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func CmdMain() {
	cfg, err := ovn_leader_checker.ParseFlags()
	if err != nil {
		util.LogFatalAndExit(err, "failed to parse flags")
	}
	if err = ovn_leader_checker.KubeClientInit(cfg); err != nil {
		util.LogFatalAndExit(err, "failed to initialize kube client")
	}
	ovn_leader_checker.StartOvnLeaderCheck(cfg)
}
