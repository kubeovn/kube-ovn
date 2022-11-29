package main

import (
	"os"
	"path/filepath"

	"github.com/kubeovn/kube-ovn/cmd/cni"
	"github.com/kubeovn/kube-ovn/cmd/controller"
	"github.com/kubeovn/kube-ovn/cmd/controller_health_check"
	"github.com/kubeovn/kube-ovn/cmd/daemon"
	"github.com/kubeovn/kube-ovn/cmd/ovn_leader_checker"
	"github.com/kubeovn/kube-ovn/cmd/ovn_monitor"
	"github.com/kubeovn/kube-ovn/cmd/pinger"
	"github.com/kubeovn/kube-ovn/cmd/speaker"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const (
	CmdCNI                   = "kube-ovn"
	CmdController            = "kube-ovn-controller"
	CmdDaemon                = "kube-ovn-daemon"
	CmdMonitor               = "kube-ovn-monitor"
	CmdPinger                = "kube-ovn-pinger"
	CmdSpeaker               = "kube-ovn-speaker"
	CmdControllerHealthCheck = "kube-ovn-controller-healthcheck"
	CmdOvnLeaderChecker      = "kube-ovn-leader-checker"
)

func main() {
	cmd := filepath.Base(os.Args[0])
	switch cmd {
	case CmdCNI:
		cni.CmdMain()
	case CmdController:
		controller.CmdMain()
	case CmdDaemon:
		daemon.CmdMain()
	case CmdMonitor:
		ovn_monitor.CmdMain()
	case CmdPinger:
		pinger.CmdMain()
	case CmdSpeaker:
		speaker.CmdMain()
	case CmdControllerHealthCheck:
		controller_health_check.CmdMain()
	case CmdOvnLeaderChecker:
		ovn_leader_checker.CmdMain()
	default:
		util.LogFatalAndExit(nil, "%s is an unknown command", cmd)
	}
}
