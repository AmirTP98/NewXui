package controller

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/alireza0/x-ui/database/model"
	"github.com/alireza0/x-ui/web/service"
	"github.com/alireza0/x-ui/web/session"

	"github.com/gin-gonic/gin"
)

// LocationController exposes the multi-location API under /xui/API/locations.
type LocationController struct {
	locationService service.LocationService
}

func (a *LocationController) getLocations(c *gin.Context) {
	locations, err := a.locationService.GetAllLocations()
	jsonObj(c, locations, err)
}

func (a *LocationController) getLocation(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, "get location", err)
		return
	}
	loc, err := a.locationService.GetLocation(id)
	jsonObj(c, loc, err)
}

// addLocation creates the location's inbound (bound from the same fields the
// Add Inbound form posts) plus the country/flag/remark metadata.
func (a *LocationController) addLocation(c *gin.Context) {
	inbound := &model.Inbound{}
	if err := c.ShouldBind(inbound); err != nil {
		jsonMsg(c, "add location", err)
		return
	}

	loc := &model.Location{
		Country: c.PostForm("country"),
		Flag:    c.PostForm("flag"),
		Remark:  c.PostForm("remark"),
		Domain:  c.PostForm("domain"),
		Enable:  true,
	}

	user := session.GetLoginUser(c)
	inbound.Id = 0
	inbound.UserId = user.Id
	inbound.Enable = true
	inbound.Remark = strings.TrimSpace(loc.Flag + " " + loc.Remark)
	if inbound.Listen == "" || inbound.Listen == "0.0.0.0" || inbound.Listen == "::" || inbound.Listen == "::0" {
		inbound.Tag = fmt.Sprintf("inbound-%v", inbound.Port)
	} else {
		inbound.Tag = fmt.Sprintf("inbound-%v:%v", inbound.Listen, inbound.Port)
	}

	result, err := a.locationService.AddLocation(loc, inbound)
	jsonMsgObj(c, "add location", result, err)
}

func (a *LocationController) updateLocation(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, "update location", err)
		return
	}
	loc, err := a.locationService.GetLocation(id)
	if err != nil {
		jsonMsg(c, "update location", err)
		return
	}
	if err := c.ShouldBind(loc); err != nil {
		jsonMsg(c, "update location", err)
		return
	}
	loc.Id = id
	err = a.locationService.UpdateLocation(loc)
	jsonMsgObj(c, "update location", loc, err)
}

func (a *LocationController) delLocation(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, "delete location", err)
		return
	}
	err = a.locationService.DeleteLocation(id)
	jsonMsg(c, "delete location", err)
}

func (a *LocationController) getMaster(c *gin.Context) {
	jsonObj(c, a.locationService.GetMasterInboundIds(), nil)
}

func (a *LocationController) setMaster(c *gin.Context) {
	// Accept either repeated form values (inboundIds=1&inboundIds=2) or a single
	// comma-separated value, so both the UI and a plain API/bot call work.
	raw := c.PostFormArray("inboundIds")
	if len(raw) == 1 {
		raw = strings.Split(raw[0], ",")
	}
	ids := make([]int, 0, len(raw))
	for _, r := range raw {
		if n, err := strconv.Atoi(strings.TrimSpace(r)); err == nil && n != 0 {
			ids = append(ids, n)
		}
	}
	err := a.locationService.SetMasterInboundIds(ids)
	jsonMsg(c, "set master inbounds", err)
}

func (a *LocationController) getSyncInterval(c *gin.Context) {
	jsonObj(c, a.locationService.GetSyncInterval(), nil)
}

func (a *LocationController) setSyncInterval(c *gin.Context) {
	seconds, err := strconv.Atoi(c.PostForm("interval"))
	if err != nil {
		jsonMsg(c, "set sync interval", err)
		return
	}
	err = a.locationService.SetSyncInterval(seconds)
	jsonMsg(c, "set sync interval", err)
}
