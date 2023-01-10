package controller_health_check

import (
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
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

	addr := "127.0.0.1:10660"
	if os.Getenv("ENABLE_BIND_LOCAL_IP") == "true" {
		podIpsEnv := os.Getenv("POD_IPS")
		podIps := strings.Split(podIpsEnv, ",")
		// when pod in dual mode, golang can't support bind v4 and v6 address in the same time,
		// so not support bind local ip when in dual mode
		if len(podIps) == 1 {
			addr = fmt.Sprintf("%s:10660", podIps[0])
			if util.CheckProtocol(podIps[0]) == kubeovnv1.ProtocolIPv6 {
				addr = fmt.Sprintf("[%s]:10660", podIps[0])
			}
		}
	}

	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		util.LogFatalAndExit(err, "failed to probe the socket")
	}
	err = conn.Close()
	if err != nil {
		util.LogFatalAndExit(err, "failed to close connection")
	}
}
