package controller

import (
	"github.com/emicklei/go-restful"
	"log"
	"net/http"
)

func RunServer(config *Configuration) {
	oh, err := CreateOvnHandler(config)
	if err != nil {
		log.Fatal(err)
		return
	}
	log.Fatal(http.ListenAndServe(config.BindAddress, CreateHandler(oh)))
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

	return wsContainer
}
