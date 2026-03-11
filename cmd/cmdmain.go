package main

import (
	"os"
	"path/filepath"

	"github.com/kubeovn/kube-ovn/cmd/ovn_ic_controller"
	"github.com/kubeovn/kube-ovn/cmd/ovn_leader_checker"
	"github.com/kubeovn/kube-ovn/cmd/ovn_monitor"
	"github.com/kubeovn/kube-ovn/cmd/speaker"
	"github.com/kubeovn/kube-ovn/cmd/webhook"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/pkg/util/profiling"
)

const (
	CmdMonitor          = "kube-ovn-monitor"
	CmdSpeaker          = "kube-ovn-speaker"
	CmdWebhook          = "kube-ovn-webhook"
	CmdOvnLeaderChecker = "kube-ovn-leader-checker"
	CmdOvnICController  = "kube-ovn-ic-controller"
)

func main() {
	cmd := filepath.Base(os.Args[0])
	switch cmd {
	case CmdMonitor:
		profiling.DumpProfile()
		ovn_monitor.CmdMain()
	case CmdSpeaker:
		profiling.DumpProfile()
		speaker.CmdMain()
	case CmdWebhook:
		webhook.CmdMain()
	case CmdOvnLeaderChecker:
		ovn_leader_checker.CmdMain()
	case CmdOvnICController:
		ovn_ic_controller.CmdMain()
	default:
		util.LogFatalAndExit(nil, "%s is an unknown command", cmd)
	}
}
