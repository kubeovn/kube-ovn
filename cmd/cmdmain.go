package main

import (
	"os"
	"strings"

	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/cmd/cni"
	"github.com/kubeovn/kube-ovn/cmd/controller"
	"github.com/kubeovn/kube-ovn/cmd/controller_health_check"
	"github.com/kubeovn/kube-ovn/cmd/daemon"
	"github.com/kubeovn/kube-ovn/cmd/ovn_leader_checker"
	"github.com/kubeovn/kube-ovn/cmd/ovn_monitor"
	"github.com/kubeovn/kube-ovn/cmd/pinger"
	"github.com/kubeovn/kube-ovn/cmd/speaker"
	"github.com/kubeovn/kube-ovn/cmd/webhook"
)

const (
	CmdCNI                   = "kube-ovn"
	CmdController            = "kube-ovn-controller"
	CmdDaemon                = "kube-ovn-daemon"
	CmdMonitor               = "kube-ovn-monitor"
	CmdPinger                = "kube-ovn-pinger"
	CmdSpeaker               = "kube-ovn-speaker"
	CmdWebHook               = "kube-ovn-webhook"
	CmdControllerHealthCheck = "kube-ovn-controller-healthcheck"
	CmdOvnLeaderChecker      = "kube-ovn-leader-checker"
)

func main() {
	cmds := strings.Split(os.Args[0], "/")
	cmd := cmds[len(cmds)-1]
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
	case CmdWebHook:
		webhook.CmdMain()
	case CmdControllerHealthCheck:
		controller_health_check.CmdMain()
	case CmdOvnLeaderChecker:
		ovn_leader_checker.CmdMain()
	default:
		klog.Fatalf("%s is an unknown command", cmd)
	}
}
