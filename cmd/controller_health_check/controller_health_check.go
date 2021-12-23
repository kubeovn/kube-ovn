package controller_health_check

import (
	"fmt"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"k8s.io/klog/v2"
	"net"
	"os"
	"strings"
	"time"
)

func CmdMain() {
	content, err := os.ReadFile("/var/run/ovn/ovn-nbctl.pid")
	if err != nil {
		klog.Fatalf("failed to get ovn-nbctl daemon pid, %s", err)
	}
	daemonPid := strings.TrimSuffix(string(content), "\n")
	if err := os.Setenv("OVN_NB_DAEMON", fmt.Sprintf("/var/run/ovn/ovn-nbctl.%s.ctl", daemonPid)); err != nil {
		klog.Fatalf("failed to set env OVN_NB_DAEMON, %v", err)
	}
	if err := ovs.CheckAlive(); err != nil {
		os.Exit(1)
	}
	conn, err := net.DialTimeout("tcp", "127.0.0.1:10660", 3*time.Second)
	if err != nil {
		klog.Fatalf("failed to probe the socket, %s", err)
	}
	err = conn.Close()
	if err != nil {
		klog.Fatalf("Unexpected error closing TCP probe socket: %v (%#v)", err, err)
	}
}
