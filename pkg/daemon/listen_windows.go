package daemon

import (
	"net"

	"github.com/Microsoft/go-winio"
	"k8s.io/klog/v2"
)

func listen(socket string) (net.Listener, func(), error) {
	listener, err := winio.ListenPipe(socket, nil)
	if err != nil {
		klog.Errorf("failed to listen pipe %s: %v", socket, err)
		return nil, nil, err
	}

	return listener, func() {}, nil
}
