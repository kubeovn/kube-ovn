package controller_health_check

import (
	"net"
	"os"
	"time"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func CmdMain() {
	addr := "127.0.0.1:10660"
	if os.Getenv("ENABLE_BIND_LOCAL_IP") == "true" {
		addr = util.JoinHostPort(os.Getenv("POD_IP"), 10660)
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
