package controller

import (
	"fmt"
	"github.com/emicklei/go-restful"
	"k8s.io/klog"
	"net/http"
	"time"
)

var RequestLogString = "[%s] Incoming %s %s %s request from %s"
var ResponseLogString = "[%s] Outcoming response to %s %s %s with %d status code in %vms"

func RunServer(config *Configuration) {
	oh, err := CreateOvnHandler(config)
	if err != nil {
		klog.Fatalf("create ovn handler failed %v", err)
		return
	}
	klog.Infof("start listen on %s:%d", config.BindAddress)
	klog.Fatal(http.ListenAndServe(config.BindAddress, CreateHandler(oh)))
}

func CreateHandler(oh *OvnHandler) http.Handler {
	wsContainer := restful.NewContainer()
	wsContainer.EnableContentEncoding(true)

	ws := new(restful.WebService)
	ws.Path("/api/v1").
		Consumes(restful.MIME_JSON).
		Produces(restful.MIME_JSON)
	wsContainer.Add(ws)

	// Logical Switch Handler
	ws.Route(
		ws.GET("/switches").
			To(oh.handleListSwitch))
	ws.Route(
		ws.GET("/switches/{name}").
			To(oh.handleGetSwitch))
	ws.Route(
		ws.POST("/switches").
			To(oh.handleCreateSwitch).
			Reads(CreateSwitchRequest{}))
	ws.Route(
		ws.PUT("/switches/{name}").
			To(oh.handleUpdateSwitch))
	ws.Route(
		ws.DELETE("/switches/{name}").
			To(oh.handleDeleteSwitch))

	// Port Handler
	ws.Route(
		ws.GET("/ports").
			To(oh.handleListPort))
	ws.Route(
		ws.POST("/ports").
			To(oh.handleCreatePort).
			Reads(CreatePortRequest{}))
	ws.Route(
		ws.GET("/ports/{name}").
			To(oh.handleGetPort))
	ws.Route(
		ws.PUT("/ports/{name}").
			To(oh.handleUpdatePort))
	ws.Route(
		ws.DELETE("/ports/{name}").
			To(oh.handleDeletePort))

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
	klog.Infof(formatResponseLog(response, request, elapsed))
}

// formatRequestLog formats request log string.
func formatRequestLog(request *restful.Request) string {
	uri := ""
	if request.Request.URL != nil {
		uri = request.Request.URL.RequestURI()
	}

	return fmt.Sprintf(RequestLogString, time.Now().Format(time.RFC3339), request.Request.Proto,
		request.Request.Method, uri, request.Request.RemoteAddr)
}

// formatResponseLog formats response log string.
func formatResponseLog(response *restful.Response, request *restful.Request, reqTime float64) string {
	uri := ""
	if request.Request.URL != nil {
		uri = request.Request.URL.RequestURI()
	}
	return fmt.Sprintf(ResponseLogString, time.Now().Format(time.RFC3339),
		request.Request.RemoteAddr, request.Request.Method, uri, response.StatusCode(), reqTime)
}
