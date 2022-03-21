package daemon

import (
	"net"
	"net/http"
	"os"

	"k8s.io/klog/v2"
)

// RunServer runs the cniserver
func RunServer(config *Configuration, controller *Controller) {
	nodeName = config.NodeName
	csh := createCniServerHandler(config, controller)
	server := http.Server{
		Handler: createHandler(csh),
	}
	unixListener, err := net.Listen("unix", config.BindSocket)
	if err != nil {
		klog.Errorf("bind socket to %s failed %v", config.BindSocket, err)
		return
	}
	defer os.Remove(config.BindSocket)
	klog.Infof("start listen on %s", config.BindSocket)
	klog.Fatal(server.Serve(unixListener))
}
