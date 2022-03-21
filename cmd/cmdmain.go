package main

import (
	"os"
	"path/filepath"

	"k8s.io/klog/v2"

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
	filename := filepath.Base(os.Args[0])
	ext := filepath.Ext(filename)
	cmd := filename[:len(filename)-len(ext)]
	switch cmd {
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
