package service

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/alireza0/x-ui/database"
	"github.com/alireza0/x-ui/database/model"
	"github.com/alireza0/x-ui/logger"
	"github.com/alireza0/x-ui/util/common"
	"github.com/alireza0/x-ui/xray"
)

type LocationService struct {
	settingService SettingService
}

// ----- master inbound designation (multiple masters supported) -----

func parseIntList(str string) []int {
	out := make([]int, 0)
	for _, part := range strings.Split(str, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if n, err := strconv.Atoi(part); err == nil && n != 0 {
			out = append(out, n)
		}
	}
	return out
}

// GetMasterInboundIds returns the set of inbound ids designated as masters.
// Falls back to the legacy single-master setting if the list is unset.
func (s *LocationService) GetMasterInboundIds() []int {
	str, _ := s.settingService.getString("locationMasterInboundIds")
	ids := parseIntList(str)
	if len(ids) == 0 {
		if old, err := s.settingService.getString("locationMasterInboundId"); err == nil {
			if n, _ := strconv.Atoi(old); n != 0 {
				ids = []int{n}
			}
		}
	}
	return ids
}

func (s *LocationService) SetMasterInboundIds(ids []int) error {
	seen := map[int]bool{}
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != 0 && !seen[id] {
			seen[id] = true
			parts = append(parts, strconv.Itoa(id))
		}
	}
	// Clear the legacy single-master key so it can't resurface as a fallback
	// once the multi-master list is authoritative (e.g. after deselecting all).
	_ = s.settingService.saveSetting("locationMasterInboundId", "0")
	return s.settingService.saveSetting("locationMasterInboundIds", strings.Join(parts, ","))
}

// IsMasterInbound reports whether the given inbound id is one of the masters.
func (s *LocationService) IsMasterInbound(inboundId int) bool {
	for _, id := range s.GetMasterInboundIds() {
		if id == inboundId {
			return true
		}
	}
	return false
}

// getMasterInbounds resolves all designated master inbounds (missing ones skipped).
func (s *LocationService) getMasterInbounds() ([]*model.Inbound, error) {
	ids := s.GetMasterInboundIds()
	inboundSvc := &InboundService{}
	out := make([]*model.Inbound, 0, len(ids))
	for _, id := range ids {
		inbound, err := inboundSvc.GetInbound(id)
		if err != nil {
			if database.IsNotFound(err) {
				continue
			}
			return nil, err
		}
		out = append(out, inbound)
	}
	return out, nil
}

func (s *LocationService) GetSyncInterval() int {
	str, err := s.settingService.getString("locationSyncIntervalSec")
	if err != nil {
		return 30
	}
	n, err := strconv.Atoi(str)
	if err != nil || n <= 0 {
		return 30
	}
	return n
}

func (s *LocationService) SetSyncInterval(seconds int) error {
	if seconds < 5 {
		seconds = 5
	}
	return s.settingService.saveSetting("locationSyncIntervalSec", strconv.Itoa(seconds))
}

// ----- per-location client naming -----

func locationSuffix(loc model.Location) string {
	r := strings.TrimSpace(loc.Remark)
	if r == "" {
		r = strings.TrimSpace(loc.Country)
	}
	r = strings.ReplaceAll(r, " ", "_")
	if r == "" {
		r = fmt.Sprintf("loc%d", loc.Id)
	}
	return r
}

func locationClientEmail(baseEmail string, loc model.Location) string {
	return baseEmail + "-" + locationSuffix(loc)
}

// LocationClientEmail is the exported per-location email helper (used by jobs).
func LocationClientEmail(baseEmail string, loc model.Location) string {
	return locationClientEmail(baseEmail, loc)
}

func locationSuffixedClient(client model.Client, loc model.Location) model.Client {
	c := client
	c.Email = locationClientEmail(client.Email, loc)
	return c
}

// ----- location CRUD -----

func (s *LocationService) GetAllLocations() ([]model.Location, error) {
	db := database.GetDB()
	var locations []model.Location
	err := db.Find(&locations).Error
	return locations, err
}

func (s *LocationService) GetLocationsByType(locType string) ([]model.Location, error) {
	db := database.GetDB()
	var locations []model.Location
	err := db.Where("type = ?", locType).Find(&locations).Error
	return locations, err
}

func (s *LocationService) GetLocation(id int) (*model.Location, error) {
	db := database.GetDB()
	loc := &model.Location{}
	if err := db.First(loc, id).Error; err != nil {
		return nil, err
	}
	return loc, nil
}

func (s *LocationService) enabledLocations() ([]model.Location, error) {
	all, err := s.GetAllLocations()
	if err != nil {
		return nil, err
	}
	enabled := make([]model.Location, 0, len(all))
	for _, l := range all {
		if l.Enable && l.InboundId != 0 {
			enabled = append(enabled, l)
		}
	}
	return enabled, nil
}

// AddLocation creates the location's inbound from the given config and stores
// the location. Clients are NOT copied — they are injected at runtime via
// InjectLocationClients (Xray config) and HotAddClientsToLocations (live Xray).
func (s *LocationService) AddLocation(loc *model.Location, inbound *model.Inbound) (*model.Location, error) {
	if inbound == nil {
		return nil, common.NewError("missing inbound config")
	}
	inboundSvc := &InboundService{}

	if inbound.Remark == "" {
		inbound.Remark = strings.TrimSpace(loc.Flag + " " + loc.Remark)
	}
	created, _, err := inboundSvc.AddInbound(inbound)
	if err != nil {
		return nil, err
	}

	loc.Id = 0
	loc.InboundId = created.Id
	db := database.GetDB()
	if err := db.Create(loc).Error; err != nil {
		return nil, err
	}

	// Hot-add existing master clients to the new location inbound in Xray
	if masters, err := s.getMasterInbounds(); err == nil {
		for _, master := range masters {
			if clients, err := inboundSvc.GetClients(master); err == nil && len(clients) > 0 {
				s.HotAddClientsToLocations(master, clients)
			}
		}
	}
	return loc, nil
}

func (s *LocationService) UpdateLocation(loc *model.Location) error {
	db := database.GetDB()
	return db.Save(loc).Error
}

// DeleteLocation removes the location and its inbound (and snapshot rows).
func (s *LocationService) DeleteLocation(id int) error {
	loc, err := s.GetLocation(id)
	if err != nil {
		return err
	}
	db := database.GetDB()
	if loc.InboundId != 0 {
		if _, err := (&InboundService{}).DelInbound(loc.InboundId); err != nil {
			logger.Warning("DeleteLocation: del inbound: ", err)
		}
		db.Where("inbound_id = ?", loc.InboundId).Delete(&model.LocationTrafficSnapshot{})
	}
	return db.Delete(&model.Location{}, id).Error
}

// ----- hot-reload: Xray API only, zero DB writes -----

// HotAddClientsToLocations adds clients to every location inbound in the
// running Xray instance via gRPC API. No database writes.
func (s *LocationService) HotAddClientsToLocations(masterInbound *model.Inbound, clients []model.Client) {
	go func() {
		defer recoverLog("HotAddClientsToLocations")
		locs, err := s.enabledLocations()
		if err != nil || len(locs) == 0 {
			return
		}
		inboundSvc := &InboundService{}
		if p == nil {
			return
		}
		if err := inboundSvc.xrayApi.Init(p.GetAPIPort()); err != nil {
			return
		}
		defer inboundSvc.xrayApi.Close()

		for _, loc := range locs {
			inbound, err := inboundSvc.GetInbound(loc.InboundId)
			if err != nil || !inbound.Enable {
				continue
			}
			isReality := loc.Type == "reality"
			for _, client := range clients {
				sc := locationSuffixedClient(client, loc)
				if !sc.Enable {
					continue
				}
				if isReality && sc.Flow == "" {
					sc.Flow = "xtls-rprx-vision"
				}
				var settings map[string]interface{}
				json.Unmarshal([]byte(inbound.Settings), &settings)
				cipher := ""
				if inbound.Protocol == "shadowsocks" {
					if m, ok := settings["method"].(string); ok {
						cipher = m
					}
				}
				inboundSvc.xrayApi.AddUser(string(inbound.Protocol), inbound.Tag, map[string]interface{}{
					"email": sc.Email, "id": sc.ID, "flow": sc.Flow,
					"password": sc.Password, "cipher": cipher,
				})
			}
		}
	}()
}

// HotUpdateClientInLocations removes old user and adds updated one on every
// location inbound via Xray API.
func (s *LocationService) HotUpdateClientInLocations(oldClient, newClient model.Client) {
	go func() {
		defer recoverLog("HotUpdateClientInLocations")
		locs, err := s.enabledLocations()
		if err != nil || len(locs) == 0 {
			return
		}
		inboundSvc := &InboundService{}
		if p == nil {
			return
		}
		if err := inboundSvc.xrayApi.Init(p.GetAPIPort()); err != nil {
			return
		}
		defer inboundSvc.xrayApi.Close()

		for _, loc := range locs {
			inbound, err := inboundSvc.GetInbound(loc.InboundId)
			if err != nil || !inbound.Enable {
				continue
			}
			isReality := loc.Type == "reality"
			oldSc := locationSuffixedClient(oldClient, loc)
			inboundSvc.xrayApi.RemoveUser(inbound.Tag, oldSc.Email)

			newSc := locationSuffixedClient(newClient, loc)
			if !newSc.Enable {
				continue
			}
			if isReality && newSc.Flow == "" {
				newSc.Flow = "xtls-rprx-vision"
			}
			var settings map[string]interface{}
			json.Unmarshal([]byte(inbound.Settings), &settings)
			cipher := ""
			if inbound.Protocol == "shadowsocks" {
				if m, ok := settings["method"].(string); ok {
					cipher = m
				}
			}
			inboundSvc.xrayApi.AddUser(string(inbound.Protocol), inbound.Tag, map[string]interface{}{
				"email": newSc.Email, "id": newSc.ID, "flow": newSc.Flow,
				"password": newSc.Password, "cipher": cipher,
			})
		}
	}()
}

// HotDelClientFromLocations removes a client from every location inbound
// via Xray API. No database writes.
func (s *LocationService) HotDelClientFromLocations(client model.Client) {
	go func() {
		defer recoverLog("HotDelClientFromLocations")
		locs, err := s.enabledLocations()
		if err != nil || len(locs) == 0 {
			return
		}
		inboundSvc := &InboundService{}
		if p == nil {
			return
		}
		if err := inboundSvc.xrayApi.Init(p.GetAPIPort()); err != nil {
			return
		}
		defer inboundSvc.xrayApi.Close()

		for _, loc := range locs {
			inbound, err := inboundSvc.GetInbound(loc.InboundId)
			if err != nil {
				continue
			}
			sc := locationSuffixedClient(client, loc)
			inboundSvc.xrayApi.RemoveUser(inbound.Tag, sc.Email)
		}
	}()
}

// HotDisableClientInLocations removes a client from every location inbound
// in the running Xray when the master client exceeds its quota.
func (s *LocationService) HotDisableClientInLocations(client model.Client) {
	s.HotDelClientFromLocations(client)
}

// ----- traffic: location suffix helpers (used by addClientTraffic) -----

// LocationSuffixes returns the "-suffix" strings for all enabled locations.
// Used by addClientTraffic to detect location-suffixed emails and attribute
// their traffic to the master client.
func (s *LocationService) LocationSuffixes() []string {
	locs, err := s.enabledLocations()
	if err != nil {
		return nil
	}
	suffixes := make([]string, 0, len(locs))
	for _, loc := range locs {
		suffixes = append(suffixes, "-"+locationSuffix(loc))
	}
	return suffixes
}

// MasterClientEmails returns the unique emails of all clients across every
// master inbound.
func (s *LocationService) MasterClientEmails() ([]string, error) {
	masters, err := s.getMasterInbounds()
	if err != nil {
		return nil, err
	}
	inboundSvc := &InboundService{}
	seen := map[string]bool{}
	emails := make([]string, 0)
	for _, master := range masters {
		clients, err := inboundSvc.GetClients(master)
		if err != nil {
			continue
		}
		for _, c := range clients {
			if c.Email != "" && !seen[c.Email] {
				seen[c.Email] = true
				emails = append(emails, c.Email)
			}
		}
	}
	return emails, nil
}

// LocationInboundIds returns the inbound ids of all enabled locations.
func (s *LocationService) LocationInboundIds() ([]model.Location, error) {
	return s.enabledLocations()
}

func recoverLog(where string) {
	if r := recover(); r != nil {
		logger.Warning(where, " panic recovered: ", r)
	}
}

// InjectLocationClients populates every location inbound's settings with the
// master inbound clients (suffixed emails). This is an in-memory operation —
// nothing is written back to the DB. Called from GetXrayConfig so Xray sees
// real clients on every location inbound even though the DB stores them empty.
func (s *LocationService) InjectLocationClients(inbounds []*model.Inbound) {
	locs, err := s.enabledLocations()
	if err != nil {
		logger.Warning("InjectLocationClients: failed to get locations:", err)
		return
	}
	if len(locs) == 0 {
		return
	}
	locMap := make(map[int]model.Location, len(locs))
	for _, l := range locs {
		locMap[l.InboundId] = l
	}

	masterIds := s.GetMasterInboundIds()
	masterIdSet := make(map[int]bool, len(masterIds))
	for _, id := range masterIds {
		masterIdSet[id] = true
	}

	// Build inbound-by-id map for quick lookups
	inboundSvc := &InboundService{}
	inboundById := make(map[int]*model.Inbound, len(inbounds))
	for _, ib := range inbounds {
		inboundById[ib.Id] = ib
	}

	// Collect master clients: shared masters (for locations) + per-record (for reality)
	sharedMasterClients := make([]model.Client, 0)
	for _, inbound := range inbounds {
		if masterIdSet[inbound.Id] {
			if clients, err := inboundSvc.GetClients(inbound); err == nil {
				sharedMasterClients = append(sharedMasterClients, clients...)
			}
		}
	}

	// Inject into each location/reality inbound
	injected := 0
	for _, inbound := range inbounds {
		loc, isLocation := locMap[inbound.Id]
		if !isLocation {
			continue
		}

		// Determine which master clients to use
		var sourceClients []model.Client
		if loc.Type == "reality" && loc.MasterInboundId > 0 {
			if master, ok := inboundById[loc.MasterInboundId]; ok {
				if clients, err := inboundSvc.GetClients(master); err == nil {
					sourceClients = clients
				}
			}
		} else {
			sourceClients = sharedMasterClients
		}
		if len(sourceClients) == 0 {
			continue
		}

		var settings map[string]interface{}
		json.Unmarshal([]byte(inbound.Settings), &settings)
		if settings == nil {
			settings = map[string]interface{}{}
		}
		isReality := loc.Type == "reality"
		locClients := make([]model.Client, 0, len(sourceClients))
		for _, mc := range sourceClients {
			sc := locationSuffixedClient(mc, loc)
			if isReality && sc.Flow == "" {
				sc.Flow = "xtls-rprx-vision"
			}
			locClients = append(locClients, sc)
		}
		settings["clients"] = locClients
		newSettings, err := json.Marshal(settings)
		if err != nil {
			continue
		}
		inbound.Settings = string(newSettings)
		injected += len(locClients)
	}
	if injected > 0 {
		logger.Infof("InjectLocationClients: %d clients across %d locations/reality mirrors", injected, len(locs))
	} else {
		logger.Warning("InjectLocationClients: 0 clients injected — check master inbound settings")
	}
}

// VerifyAndRepairRunningXray checks that all location/reality inbounds have
// their virtual clients in the running Xray instance. If any are missing
// (e.g., after a crash or failed injection), re-adds them via gRPC API.
// Called from a delayed goroutine after Xray starts.
func (s *LocationService) VerifyAndRepairRunningXray() {
	locs, err := s.enabledLocations()
	if err != nil || len(locs) == 0 {
		return
	}

	inboundSvc := &InboundService{}
	masterIds := s.GetMasterInboundIds()
	masterIdSet := make(map[int]bool, len(masterIds))
	for _, id := range masterIds {
		masterIdSet[id] = true
	}

	if p == nil {
		return
	}
	if err := inboundSvc.xrayApi.Init(p.GetAPIPort()); err != nil {
		return
	}
	defer inboundSvc.xrayApi.Close()

	repaired := 0
	for _, loc := range locs {
		inbound, err := inboundSvc.GetInbound(loc.InboundId)
		if err != nil || !inbound.Enable {
			continue
		}

		var sourceClients []model.Client
		if loc.Type == "reality" && loc.MasterInboundId > 0 {
			master, err := inboundSvc.GetInbound(loc.MasterInboundId)
			if err != nil {
				continue
			}
			sourceClients, _ = inboundSvc.GetClients(master)
		} else {
			for _, mid := range masterIds {
				master, err := inboundSvc.GetInbound(mid)
				if err != nil {
					continue
				}
				clients, _ := inboundSvc.GetClients(master)
				sourceClients = append(sourceClients, clients...)
			}
		}

		isReality := loc.Type == "reality"
		var settings map[string]interface{}
		json.Unmarshal([]byte(inbound.Settings), &settings)
		cipher := ""
		if inbound.Protocol == "shadowsocks" {
			if m, ok := settings["method"].(string); ok {
				cipher = m
			}
		}

		for _, mc := range sourceClients {
			sc := locationSuffixedClient(mc, loc)
			if !sc.Enable {
				continue
			}
			if isReality && sc.Flow == "" {
				sc.Flow = "xtls-rprx-vision"
			}
			inboundSvc.xrayApi.AddUser(string(inbound.Protocol), inbound.Tag, map[string]interface{}{
				"email": sc.Email, "id": sc.ID, "flow": sc.Flow,
				"password": sc.Password, "cipher": cipher,
			})
			repaired++
		}
	}
	if repaired > 0 {
		logger.Infof("VerifyAndRepairRunningXray: ensured %d clients across %d locations", repaired, len(locs))
	}
}

// MigrateLocationClientsToVirtual strips clients from location inbound settings
// and removes orphaned location client_traffics rows. Run once at startup.
func (s *LocationService) MigrateLocationClientsToVirtual() {
	db := database.GetDB()

	// Check if already migrated
	var done string
	db.Model(&model.Setting{}).Where("`key` = ?", "locationVirtualMigrated").Select("value").Scan(&done)
	if done == "true" {
		return
	}

	locs, err := s.enabledLocations()
	if err != nil || len(locs) == 0 {
		db.Create(&model.Setting{Key: "locationVirtualMigrated", Value: "true"})
		return
	}

	// Set type="location" for any existing locations without a type
	db.Model(&model.Location{}).Where("type IS NULL OR type = ''").Update("type", "location")

	suffixes := s.LocationSuffixes()

	// Strip clients from location inbound settings
	for _, loc := range locs {
		var inbound model.Inbound
		if db.First(&inbound, loc.InboundId).Error != nil {
			continue
		}
		var settings map[string]interface{}
		json.Unmarshal([]byte(inbound.Settings), &settings)
		if settings == nil {
			continue
		}
		settings["clients"] = []interface{}{}
		newSettings, _ := json.Marshal(settings)
		db.Model(&model.Inbound{}).Where("id = ?", loc.InboundId).Update("settings", string(newSettings))
	}

	// Delete location-suffixed client_traffics rows
	for _, suf := range suffixes {
		db.Where("email LIKE ?", "%"+suf).Delete(&xray.ClientTraffic{})
	}

	// Delete all location_traffic_snapshots
	db.Exec("DELETE FROM location_traffic_snapshots")

	db.Create(&model.Setting{Key: "locationVirtualMigrated", Value: "true"})
	logger.Info("MigrateLocationClientsToVirtual: completed")
}
