package controller

import (
	"github.com/alireza0/x-ui/database"
	"github.com/alireza0/x-ui/web/service"

	"github.com/gin-gonic/gin"
)

type APIController struct {
	BaseController
	inboundController  *InboundController
	serverController   *ServerController
	locationController *LocationController
	Tgbot              service.Tgbot
}

func NewAPIController(g *gin.RouterGroup, s *ServerController) *APIController {
	a := &APIController{
		serverController: s,
	}
	a.initRouter(g)
	return a
}

func (a *APIController) initRouter(g *gin.RouterGroup) {
	api := g.Group("/xui/API")
	api.Use(a.checkLogin)

	a.inboundApi(api)
	a.serverApi(api)
	a.locationApi(api)
}

func (a *APIController) locationApi(api *gin.RouterGroup) {
	locationsApi := api.Group("/locations")

	a.locationController = &LocationController{}

	routes := []struct {
		Method  string
		Path    string
		Handler gin.HandlerFunc
	}{
		{"GET", "/", a.locationController.getLocations},
		{"GET", "/get/:id", a.locationController.getLocation},
		{"POST", "/add", a.locationController.addLocation},
		{"POST", "/update/:id", a.locationController.updateLocation},
		{"POST", "/del/:id", a.locationController.delLocation},
		{"GET", "/master", a.locationController.getMaster},
		{"POST", "/master", a.locationController.setMaster},
		{"GET", "/syncInterval", a.locationController.getSyncInterval},
		{"POST", "/syncInterval", a.locationController.setSyncInterval},
		{"POST", "/generateReality", a.locationController.generateReality},
		{"POST", "/bulkRealityDest", a.locationController.bulkRealityDest},
	}

	for _, route := range routes {
		locationsApi.Handle(route.Method, route.Path, route.Handler)
	}
}

func (a *APIController) inboundApi(api *gin.RouterGroup) {
	inboundsApi := api.Group("/inbounds")

	a.inboundController = &InboundController{}

	inboundRoutes := []struct {
		Method  string
		Path    string
		Handler gin.HandlerFunc
	}{
		{"GET", "/", a.inboundController.getInbounds},
		{"GET", "/get/:id", a.inboundController.getInbound},
		{"GET", "/getClientTraffics/:email", a.inboundController.getClientTraffics},
		{"GET", "/getClientTrafficsById/:id", a.inboundController.getClientTrafficsById},
		{"POST", "/add", a.inboundController.addInbound},
		{"POST", "/del/:id", a.inboundController.delInbound},
		{"POST", "/update/:id", a.inboundController.updateInbound},
		{"POST", "/addClient", a.inboundController.addInboundClient},
		{"POST", "/:id/delClient/:clientId", a.inboundController.delInboundClient},
		{"POST", "/updateClient/:clientId", a.inboundController.updateInboundClient},
		{"POST", "/:id/resetClientTraffic/:email", a.inboundController.resetClientTraffic},
		{"POST", "/resetAllTraffics", a.inboundController.resetAllTraffics},
		{"POST", "/resetAllClientTraffics/:id", a.inboundController.resetAllClientTraffics},
		{"POST", "/delDepletedClients/:id", a.inboundController.delDepletedClients},
		{"POST", "/onlines", a.inboundController.onlines},
	}

	for _, route := range inboundRoutes {
		inboundsApi.Handle(route.Method, route.Path, route.Handler)
	}
}

func (a *APIController) serverApi(api *gin.RouterGroup) {
	serverApi := api.Group("/server")

	serverRoutes := []struct {
		Method  string
		Path    string
		Handler gin.HandlerFunc
	}{
		{"GET", "/status", a.serverController.status},
		{"GET", "/getDb", a.serverController.getDb},
		{"GET", "/createbackup", a.createBackup},
		{"GET", "/getConfigJson", a.serverController.getConfigJson},
		{"GET", "/getXrayVersion", a.serverController.getXrayVersion},
		{"GET", "/getNewVlessEnc", a.serverController.getNewVlessEnc},
		{"GET", "/getNewX25519Cert", a.serverController.getNewX25519Cert},
		{"GET", "/getNewmldsa65", a.serverController.getNewmldsa65},

		{"POST", "/getNewEchCert", a.serverController.getNewEchCert},
		{"POST", "/importDB", a.serverController.importDB},
		{"POST", "/stopXrayService", a.serverController.stopXrayService},
		{"POST", "/restartXrayService", a.serverController.restartXrayService},
		{"POST", "/installXray/:version", a.serverController.installXray},
		{"POST", "/logs/:count", a.serverController.getLogs},
		{"GET", "/dbHealth", a.dbHealth},
		{"POST", "/dbRepair", a.dbRepair},
		{"GET", "/syncStatus", a.syncStatus},
	}

	for _, route := range serverRoutes {
		serverApi.Handle(route.Method, route.Path, route.Handler)
	}
}

func (a *APIController) createBackup(c *gin.Context) {
	a.Tgbot.SendBackupToAdmins()
}

func (a *APIController) syncStatus(c *gin.Context) {
	remaining := service.GetNextSyncSeconds()
	jsonObj(c, map[string]interface{}{
		"nextSyncSeconds": remaining,
		"intervalSeconds": 600,
	}, nil)
}

func (a *APIController) dbHealth(c *gin.Context) {
	db := database.GetDB()
	var result string
	if err := db.Raw("PRAGMA integrity_check;").Scan(&result).Error; err != nil {
		jsonObj(c, map[string]interface{}{"healthy": false, "error": err.Error()}, nil)
		return
	}
	healthy := result == "ok"
	jsonObj(c, map[string]interface{}{"healthy": healthy, "result": result}, nil)
}

func (a *APIController) dbRepair(c *gin.Context) {
	db := database.GetDB()
	db.Exec("REINDEX")
	var result string
	db.Raw("PRAGMA integrity_check;").Scan(&result)
	healthy := result == "ok"
	jsonObj(c, map[string]interface{}{"repaired": healthy, "result": result}, nil)
}
