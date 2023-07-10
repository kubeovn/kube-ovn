//go:build !windows
// +build !windows

package daemon

import (
	"fmt"
	"net"
	"os"
	"strings"

	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func listen(socket string) (net.Listener, func(), error) {
	listener, err := net.Listen("unix", socket)
	if err != nil {
		klog.Errorf("failed to bind socket to %s: %v", socket, err)
		return nil, nil, err
	}

	return listener, func() {
		if err := os.Remove(socket); err != nil {
			klog.Error(err)
		}
	}, nil
}

func GetDefaultListenPort() string {
	addr := "0.0.0.0"
	if os.Getenv("ENABLE_BIND_LOCAL_IP") == "true" {
		podIpsEnv := os.Getenv("POD_IPS")
		podIps := strings.Split(podIpsEnv, ",")
		// when pod in dual mode, golang can't support bind v4 and v6 address in the same time,
		// so not support bind local ip when in dual mode
		if len(podIps) == 1 {
			addr = podIps[0]
			if util.CheckProtocol(podIps[0]) == kubeovnv1.ProtocolIPv6 {
				addr = fmt.Sprintf("[%s]", podIps[0])
			}
		}
	}
	return addr
}
