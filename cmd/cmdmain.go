package main

import (
	"os"
	"strings"

	"k8s.io/klog"

	"github.com/kubeovn/kube-ovn/cmd/controller"
	"github.com/kubeovn/kube-ovn/cmd/daemon"
	"github.com/kubeovn/kube-ovn/cmd/ovn_monitor"
	"github.com/kubeovn/kube-ovn/cmd/pinger"
	"github.com/kubeovn/kube-ovn/cmd/speaker"
	"github.com/kubeovn/kube-ovn/cmd/webhook"
)

const (
	CmdController = "kube-ovn-controller"
	CmdDaemon     = "kube-ovn-daemon"
	CmdMonitor    = "kube-ovn-monitor"
	CmdPinger     = "kube-ovn-pinger"
	CmdSpeaker    = "kube-ovn-speaker"
	CmdWebHook    = "kube-ovn-webhook"
)

func main() {
	cmds := strings.Split(os.Args[0], "/")
	cmd := cmds[len(cmds)-1]
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
	default:
		klog.Fatalf("%s is an unknown command", cmd)
	}
}
