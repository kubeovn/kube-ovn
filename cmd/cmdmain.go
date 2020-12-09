package main

import (
	"github.com/alauda/kube-ovn/cmd/controller"
	"github.com/alauda/kube-ovn/cmd/daemon"
	"github.com/alauda/kube-ovn/cmd/ovn_monitor"
	"github.com/alauda/kube-ovn/cmd/pinger"
	"github.com/alauda/kube-ovn/cmd/speaker"
	"k8s.io/klog"
	"os"
	"strings"
)

const (
	CmdController = "kube-ovn-controller"
	CmdDaemon     = "kube-ovn-daemon"
	CmdMonitor    = "kube-ovn-monitor"
	CmdPinger     = "kube-ovn-pinger"
	CmdSpeaker    = "kube-ovn-speaker"
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
	default:
		klog.Fatalf("%s is an unknown command", cmd)
	}
}
