package controller

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/alireza0/x-ui/database"
	"github.com/alireza0/x-ui/database/model"
	"github.com/alireza0/x-ui/logger"
	"github.com/alireza0/x-ui/util/common"
	"github.com/alireza0/x-ui/util/random"
	"github.com/alireza0/x-ui/web/service"
	"github.com/alireza0/x-ui/web/session"

	"github.com/gin-gonic/gin"
)

// LocationController exposes the multi-location API under /xui/API/locations.
type LocationController struct {
	locationService service.LocationService
}

func (a *LocationController) getLocations(c *gin.Context) {
	locType := c.Query("type")
	if locType != "" {
		locations, err := a.locationService.GetLocationsByType(locType)
		jsonObj(c, locations, err)
		return
	}
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

	locType := c.PostForm("type")
	if locType == "" {
		locType = "location"
	}
	loc := &model.Location{
		Type:    locType,
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

// generateReality creates one Reality inbound per selected master inbound,
// using the provided stream/sniffing template. Each gets a unique random port.
func (a *LocationController) generateReality(c *gin.Context) {
	streamSettings := c.PostForm("streamSettings")
	sniffing := c.PostForm("sniffing")
	remarkPrefix := c.PostForm("remarkPrefix")
	if remarkPrefix == "" {
		remarkPrefix = "reality"
	}

	// Get master inbound IDs
	raw := c.PostFormArray("masterIds")
	if len(raw) == 1 {
		raw = strings.Split(raw[0], ",")
	}
	var masterIds []int
	for _, r := range raw {
		if n, err := strconv.Atoi(strings.TrimSpace(r)); err == nil && n != 0 {
			masterIds = append(masterIds, n)
		}
	}
	if len(masterIds) == 0 {
		jsonMsg(c, "generate reality", fmt.Errorf("no master inbounds selected"))
		return
	}

	// Validate: stream must contain a non-empty privateKey
	var streamCheck map[string]interface{}
	json.Unmarshal([]byte(streamSettings), &streamCheck)
	if rs, ok := streamCheck["realitySettings"].(map[string]interface{}); ok {
		pk, _ := rs["privateKey"].(string)
		if pk == "" {
			jsonMsg(c, "generate reality", fmt.Errorf("privateKey is empty — paste your full Reality stream settings"))
			return
		}
	} else {
		jsonMsg(c, "generate reality", fmt.Errorf("realitySettings not found in stream settings JSON"))
		return
	}

	user := session.GetLoginUser(c)
	inboundSvc := service.InboundService{}
	var created []map[string]interface{}

	// Check which masters already have a reality mirror
	existing, _ := a.locationService.GetLocationsByType("reality")
	existingMasters := make(map[int]bool)
	for _, loc := range existing {
		existingMasters[loc.MasterInboundId] = true
	}

	for i, masterId := range masterIds {
		if existingMasters[masterId] {
			continue // skip — already has a reality mirror
		}

		master, err := inboundSvc.GetInbound(masterId)
		if err != nil {
			continue
		}

		// Generate unique port (random 10000-59999)
		port := 10000 + random.Num(50000)

		// Update external proxy ports to match this inbound's port
		finalStream := updateExternalProxyPorts(streamSettings, port)

		remark := fmt.Sprintf("%s-%d", remarkPrefix, i+1)
		tag := fmt.Sprintf("inbound-%d", port)

		inbound := &model.Inbound{
			UserId:         user.Id,
			Enable:         true,
			Remark:         remark + " - " + master.Remark,
			Listen:         "",
			Port:           port,
			Protocol:       "vless",
			Settings:       `{"clients":[],"decryption":"none","encryption":"none"}`,
			StreamSettings: finalStream,
			Sniffing:       sniffing,
			Tag:            tag,
		}

		loc := &model.Location{
			Type:            "reality",
			Remark:          remark,
			InboundId:       0,
			MasterInboundId: masterId,
			Enable:          true,
		}

		result, err := a.locationService.AddLocation(loc, inbound)
		if err != nil {
			created = append(created, map[string]interface{}{
				"masterId": masterId, "master": master.Remark, "error": err.Error(),
			})
			continue
		}
		created = append(created, map[string]interface{}{
			"masterId": masterId, "master": master.Remark, "port": port,
			"realityId": result.Id, "realityInboundId": result.InboundId,
		})

		// Add reality tag to the same routing rule as master
		if err := addTagToMasterRoutingRule(master.Tag, tag); err != nil {
			logger.Warning("generateReality: could not add routing rule for ", tag, ": ", err)
		}
	}

	jsonObj(c, created, nil)
}

// bulkRealityDest changes dest and serverNames on ALL reality inbounds at once.
func (a *LocationController) bulkRealityDest(c *gin.Context) {
	dest := c.PostForm("dest")
	sni := c.PostForm("sni")
	if dest == "" || sni == "" {
		jsonMsg(c, "bulk reality dest", fmt.Errorf("dest and sni are required"))
		return
	}

	realities, _ := a.locationService.GetLocationsByType("reality")
	inboundSvc := service.InboundService{}
	updated := 0

	for _, loc := range realities {
		inbound, err := inboundSvc.GetInbound(loc.InboundId)
		if err != nil {
			continue
		}
		var stream map[string]interface{}
		if err := json.Unmarshal([]byte(inbound.StreamSettings), &stream); err != nil {
			continue
		}
		rs, ok := stream["realitySettings"].(map[string]interface{})
		if !ok {
			continue
		}
		rs["dest"] = dest
		rs["serverNames"] = []string{sni}
		newStream, _ := json.MarshalIndent(stream, "", "  ")
		inbound.StreamSettings = string(newStream)

		db := database.GetDB()
		db.Model(&model.Inbound{}).Where("id = ?", inbound.Id).Update("stream_settings", inbound.StreamSettings)
		updated++
	}

	// Also update non-mirror reality inbounds (manual ones)
	var allInbounds []*model.Inbound
	db := database.GetDB()
	db.Find(&allInbounds)
	for _, inbound := range allInbounds {
		var stream map[string]interface{}
		if err := json.Unmarshal([]byte(inbound.StreamSettings), &stream); err != nil {
			continue
		}
		if stream["security"] != "reality" {
			continue
		}
		// Skip if already updated via locations
		alreadyDone := false
		for _, loc := range realities {
			if loc.InboundId == inbound.Id {
				alreadyDone = true
				break
			}
		}
		if alreadyDone {
			continue
		}
		rs, ok := stream["realitySettings"].(map[string]interface{})
		if !ok {
			continue
		}
		rs["dest"] = dest
		rs["serverNames"] = []string{sni}
		newStream, _ := json.MarshalIndent(stream, "", "  ")
		db.Model(&model.Inbound{}).Where("id = ?", inbound.Id).Update("stream_settings", string(newStream))
		updated++
	}

	jsonObj(c, updated, nil)
}

// addTagToMasterRoutingRule finds the routing rule that contains masterTag
// and adds newTag to the same rule, so the reality mirror gets the same outbound.
func addTagToMasterRoutingRule(masterTag, newTag string) error {
	settingSvc := service.SettingService{}
	template, err := settingSvc.GetXrayConfigTemplate()
	if err != nil {
		return err
	}
	var config map[string]json.RawMessage
	if err := json.Unmarshal([]byte(template), &config); err != nil {
		return err
	}
	routingRaw, ok := config["routing"]
	if !ok {
		return nil
	}
	var routing map[string]json.RawMessage
	if err := json.Unmarshal(routingRaw, &routing); err != nil {
		return err
	}
	rulesRaw, ok := routing["rules"]
	if !ok {
		return nil
	}
	var rules []map[string]interface{}
	if err := json.Unmarshal(rulesRaw, &rules); err != nil {
		return err
	}

	added := false
	for i, rule := range rules {
		tags, ok := rule["inboundTag"].([]interface{})
		if !ok {
			continue
		}
		for _, t := range tags {
			if t.(string) == masterTag {
				tags = append(tags, newTag)
				rules[i]["inboundTag"] = tags
				added = true
				break
			}
		}
		if added {
			break
		}
	}
	if !added {
		return nil
	}

	newRules, _ := json.Marshal(rules)
	routing["rules"] = newRules
	newRouting, _ := json.Marshal(routing)
	config["routing"] = newRouting
	newConfig, _ := json.MarshalIndent(config, "", "  ")

	xraySvc := service.XraySettingService{}
	return xraySvc.SaveXraySetting(string(newConfig))
}

func updateExternalProxyPorts(streamSettings string, port int) string {
	var stream map[string]interface{}
	if err := json.Unmarshal([]byte(streamSettings), &stream); err != nil {
		return streamSettings
	}
	eps, ok := stream["externalProxy"].([]interface{})
	if !ok {
		return streamSettings
	}
	for _, ep := range eps {
		if epMap, ok := ep.(map[string]interface{}); ok {
			epMap["port"] = float64(port)
		}
	}
	result, _ := json.Marshal(stream)
	return string(result)
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

func (a *LocationController) getMirrorTrafficMode(c *gin.Context) {
	jsonObj(c, (&service.SettingService{}).GetMirrorTrafficMode(), nil)
}

func (a *LocationController) setMirrorTrafficMode(c *gin.Context) {
	mode := c.PostForm("mode")
	if mode != "inline" && mode != "external" {
		jsonMsg(c, "set mirror traffic mode", common.NewError("mode must be 'inline' or 'external'"))
		return
	}
	settingSvc := service.SettingService{}
	err := settingSvc.SetMirrorTrafficMode(mode)
	jsonMsg(c, "set mirror traffic mode", err)
}
