package daemon

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/alauda/kube-ovn/pkg/request"
	"github.com/emicklei/go-restful"
	"k8s.io/klog"
)

var requestLogString = "[%s] Incoming %s %s %s request"
var responseLogString = "[%s] Outgoing response %s %s with %d status code in %vms"

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

func createHandler(csh *cniServerHandler) http.Handler {
	wsContainer := restful.NewContainer()
	wsContainer.EnableContentEncoding(true)

	ws := new(restful.WebService)
	ws.Path("/api/v1").
		Consumes(restful.MIME_JSON).
		Produces(restful.MIME_JSON)
	wsContainer.Add(ws)

	ws.Route(
		ws.POST("/add").
			To(csh.handleAdd).
			Reads(request.CniRequest{}))
	ws.Route(
		ws.POST("/del").
			To(csh.handleDel).
			Reads(request.CniRequest{}))

	ws.Filter(requestAndResponseLogger)

	return wsContainer
}

// web-service filter function used for request and response logging.
func requestAndResponseLogger(request *restful.Request, response *restful.Response,
	chain *restful.FilterChain) {
	klog.Infof(formatRequestLog(request))
	start := time.Now()
	chain.ProcessFilter(request, response)
	elapsed := float64((time.Since(start)) / time.Millisecond)
	cniOperationHistogram.WithLabelValues(
		nodeName,
		getRequestURI(request),
		fmt.Sprintf("%d", response.StatusCode())).Observe(elapsed / 1000)
	klog.Infof(formatResponseLog(response, request, elapsed))
}

// formatRequestLog formats request log string.
func formatRequestLog(request *restful.Request) string {
	return fmt.Sprintf(requestLogString, time.Now().Format(time.RFC3339), request.Request.Proto,
		request.Request.Method, getRequestURI(request))
}

// formatResponseLog formats response log string.
func formatResponseLog(response *restful.Response, request *restful.Request, reqTime float64) string {
	return fmt.Sprintf(responseLogString, time.Now().Format(time.RFC3339),
		request.Request.Method, getRequestURI(request), response.StatusCode(), reqTime)
}

// getRequestURI get the request uri
func getRequestURI(request *restful.Request) (uri string) {
	if request.Request.URL != nil {
		uri = request.Request.URL.RequestURI()
	}
	return
}
