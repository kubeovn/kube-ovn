package daemon

import (
	"net"
	"os"

	"k8s.io/klog/v2"
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
