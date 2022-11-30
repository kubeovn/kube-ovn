package controller_health_check

import (
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func CmdMain() {
	content, err := os.ReadFile("/var/run/ovn/ovn-nbctl.pid")
	if err != nil {
		util.LogFatalAndExit(err, "failed to get ovn-nbctl daemon pid")
	}
	daemonPid := strings.TrimSuffix(string(content), "\n")
	if err := os.Setenv("OVN_NB_DAEMON", fmt.Sprintf("/var/run/ovn/ovn-nbctl.%s.ctl", daemonPid)); err != nil {
		util.LogFatalAndExit(err, "failed to set env OVN_NB_DAEMON")
	}
	if err := ovs.CheckAlive(); err != nil {
		os.Exit(1)
	}
	conn, err := net.DialTimeout("tcp", "127.0.0.1:10660", 3*time.Second)
	if err != nil {
		util.LogFatalAndExit(err, "failed to probe the socket")
	}
	err = conn.Close()
	if err != nil {
		util.LogFatalAndExit(err, "failed to close connection")
	}
}
