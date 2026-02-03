package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/pprof"
	"syscall"
	"time"

	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/cmd/ovn_ic_controller"
	"github.com/kubeovn/kube-ovn/cmd/ovn_leader_checker"
	"github.com/kubeovn/kube-ovn/cmd/ovn_monitor"
	"github.com/kubeovn/kube-ovn/cmd/speaker"
	"github.com/kubeovn/kube-ovn/cmd/webhook"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const (
	CmdMonitor          = "kube-ovn-monitor"
	CmdSpeaker          = "kube-ovn-speaker"
	CmdWebhook          = "kube-ovn-webhook"
	CmdOvnLeaderChecker = "kube-ovn-leader-checker"
	CmdOvnICController  = "kube-ovn-ic-controller"
)

type subcommand struct {
	name          string
	mainFn        func()
	enableProfile bool
}

var subcommands = []subcommand{
	{CmdMonitor, ovn_monitor.CmdMain, true},
	{CmdSpeaker, speaker.CmdMain, true},
	{CmdWebhook, webhook.CmdMain, false},
	{CmdOvnLeaderChecker, ovn_leader_checker.CmdMain, false},
	{CmdOvnICController, ovn_ic_controller.CmdMain, false},
}

const timeFormat = "2006-01-02_15:04:05"

func dumpProfile() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGUSR1, syscall.SIGUSR2)
	go func() {
		for sig := range ch {
			switch sig {
			case syscall.SIGUSR1:
				name := fmt.Sprintf("cpu-profile-%s.pprof", time.Now().Format(timeFormat))
				path := filepath.Join(os.TempDir(), name)
				f, err := os.Create(path) // #nosec G303,G304
				if err != nil {
					klog.Errorf("failed to create cpu profile file: %v", err)
					continue
				}
				if err = pprof.StartCPUProfile(f); err != nil {
					klog.Errorf("failed to start cpu profile: %v", err)
					if err = f.Close(); err != nil {
						klog.Errorf("failed to close file %q: %v", path, err)
					}
					continue
				}
				time.Sleep(30 * time.Second)
				pprof.StopCPUProfile()
				if err = f.Close(); err != nil {
					klog.Errorf("failed to close file %q: %v", path, err)
				}
			case syscall.SIGUSR2:
				name := fmt.Sprintf("mem-profile-%s.pprof", time.Now().Format(timeFormat))
				path := filepath.Join(os.TempDir(), name)
				f, err := os.Create(path) // #nosec G303,G304
				if err != nil {
					klog.Errorf("failed to create memory profile file: %v", err)
					continue
				}
				if err = pprof.WriteHeapProfile(f); err != nil {
					klog.Errorf("failed to write memory profile file: %v", err)
					if err = f.Close(); err != nil {
						klog.Errorf("failed to close file %q: %v", path, err)
					}
					continue
				}
				if err = f.Close(); err != nil {
					klog.Errorf("failed to close file %q: %v", path, err)
				}
			}
		}
	}()
}

func main() {
	cmd := filepath.Base(os.Args[0])
	for _, sc := range subcommands {
		if sc.name == cmd {
			if sc.enableProfile {
				dumpProfile()
			}
			sc.mainFn()
			return
		}
	}
	util.LogFatalAndExit(nil, "%s is an unknown command", cmd)
}
