package main

import (
	"os"
	"path/filepath"

	"github.com/kubeovn/kube-ovn/cmd/pinger"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/pkg/util/profiling"
)

const (
	CmdController = "kube-ovn-controller"
	CmdPinger     = "kube-ovn-pinger"
)

func main() {
	cmd := filepath.Base(os.Args[0])
	switch cmd {
	case CmdController:
		profiling.DumpProfile()
		CmdMain()
	case CmdPinger:
		profiling.DumpProfile()
		pinger.CmdMain()
	default:
		util.LogFatalAndExit(nil, "%s is an unknown command", cmd)
	}
}
