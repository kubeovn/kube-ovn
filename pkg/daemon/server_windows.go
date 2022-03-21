package daemon

import (
	"net/http"

	"github.com/Microsoft/go-winio"
	"k8s.io/klog/v2"
)

// RunServer runs the cniserver
func RunServer(config *Configuration, controller *Controller) {
	listener, err := winio.ListenPipe(config.BindSocket, nil)
	if err != nil {
		klog.Errorf("failed to listen pipe %s: %v", config.BindSocket, err)
		return
	}

	nodeName = config.NodeName
	csh := createCniServerHandler(config, controller)
	server := http.Server{
		Handler: createHandler(csh),
	}

	klog.Infof("start listen on %s", config.BindSocket)
	klog.Fatal(server.Serve(listener))
}
