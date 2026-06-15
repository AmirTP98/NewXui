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

	"gorm.io/gorm"
)

type LocationService struct {
	settingService SettingService
}

// ----- master inbound designation -----

func (s *LocationService) GetMasterInboundId() int {
	str, err := s.settingService.getString("locationMasterInboundId")
	if err != nil {
		return 0
	}
	id, _ := strconv.Atoi(str)
	return id
}

func (s *LocationService) SetMasterInboundId(id int) error {
	return s.settingService.saveSetting("locationMasterInboundId", strconv.Itoa(id))
}

// IsMasterInbound reports whether the given inbound id is the configured master.
func (s *LocationService) IsMasterInbound(inboundId int) bool {
	mid := s.GetMasterInboundId()
	return mid != 0 && mid == inboundId
}

func (s *LocationService) getMasterInbound() (*model.Inbound, error) {
	id := s.GetMasterInboundId()
	if id == 0 {
		return nil, nil
	}
	inbound, err := (&InboundService{}).GetInbound(id)
	if err != nil {
		if database.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return inbound, nil
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

// AddLocation creates the location's inbound from the given config, stores the
// location, and replicates the master inbound's current clients onto it.
func (s *LocationService) AddLocation(loc *model.Location, inbound *model.Inbound) (*model.Location, error) {
	if inbound == nil {
		return nil, common.NewError("missing inbound config")
	}
	inboundSvc := &InboundService{}

	// make the inbound's remark recognisable in the Inbounds page
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

	// replicate existing master clients onto the new location inbound
	if master, err := s.getMasterInbound(); err == nil && master != nil {
		if clients, err := inboundSvc.GetClients(master); err == nil && len(clients) > 0 {
			s.applyClientsToLocation(*loc, clients, false)
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

// ----- fan-out from master client operations (async, local) -----

func (s *LocationService) FanOutAddClients(clients []model.Client) {
	go func() {
		defer recoverLog("FanOutAddClients")
		locs, err := s.enabledLocations()
		if err != nil {
			logger.Warning("FanOutAddClients: ", err)
			return
		}
		for _, loc := range locs {
			s.applyClientsToLocation(loc, clients, false)
		}
	}()
}

func (s *LocationService) FanOutUpdateClient(oldClientId string, client model.Client) {
	go func() {
		defer recoverLog("FanOutUpdateClient")
		locs, err := s.enabledLocations()
		if err != nil {
			logger.Warning("FanOutUpdateClient: ", err)
			return
		}
		for _, loc := range locs {
			s.applyClientsToLocationUpdate(loc, oldClientId, client)
		}
	}()
}

func (s *LocationService) FanOutDelClient(clientId, baseEmail string) {
	go func() {
		defer recoverLog("FanOutDelClient")
		locs, err := s.enabledLocations()
		if err != nil {
			logger.Warning("FanOutDelClient: ", err)
			return
		}
		db := database.GetDB()
		inboundSvc := &InboundService{}
		for _, loc := range locs {
			if _, err := inboundSvc.DelInboundClient(loc.InboundId, clientId); err != nil {
				logger.Warning("FanOutDelClient: ", err)
			}
			if baseEmail != "" {
				db.Where("inbound_id = ? AND email = ?", loc.InboundId, locationClientEmail(baseEmail, loc)).
					Delete(&model.LocationTrafficSnapshot{})
			}
		}
	}()
}

func (s *LocationService) FanOutResetTraffic(baseEmail string) {
	if baseEmail == "" {
		return
	}
	go func() {
		defer recoverLog("FanOutResetTraffic")
		locs, err := s.enabledLocations()
		if err != nil {
			logger.Warning("FanOutResetTraffic: ", err)
			return
		}
		db := database.GetDB()
		inboundSvc := &InboundService{}
		for _, loc := range locs {
			email := locationClientEmail(baseEmail, loc)
			if _, err := inboundSvc.ResetClientTraffic(loc.InboundId, email); err != nil {
				logger.Warning("FanOutResetTraffic: ", err)
			}
			db.Where("inbound_id = ? AND email = ?", loc.InboundId, email).
				Delete(&model.LocationTrafficSnapshot{})
		}
	}()
}

func (s *LocationService) applyClientsToLocation(loc model.Location, clients []model.Client, update bool) {
	inboundSvc := &InboundService{}
	for _, base := range clients {
		sc := locationSuffixedClient(base, loc)
		settings, err := json.Marshal(map[string]interface{}{"clients": []model.Client{sc}})
		if err != nil {
			continue
		}
		data := &model.Inbound{Id: loc.InboundId, Settings: string(settings)}
		if update {
			if _, err := inboundSvc.UpdateInboundClient(data, base.ID); err != nil {
				logger.Warning("applyClientsToLocation update: ", err)
			}
		} else {
			if _, err := inboundSvc.AddInboundClient(data); err != nil {
				logger.Warning("applyClientsToLocation add: ", err)
			}
		}
	}
}

func (s *LocationService) applyClientsToLocationUpdate(loc model.Location, oldClientId string, client model.Client) {
	s.applyClientsToLocationOne(loc, oldClientId, client, true)
}

func (s *LocationService) applyClientsToLocationOne(loc model.Location, oldClientId string, base model.Client, update bool) {
	inboundSvc := &InboundService{}
	sc := locationSuffixedClient(base, loc)
	settings, err := json.Marshal(map[string]interface{}{"clients": []model.Client{sc}})
	if err != nil {
		return
	}
	data := &model.Inbound{Id: loc.InboundId, Settings: string(settings)}
	if update {
		if _, err := inboundSvc.UpdateInboundClient(data, oldClientId); err != nil {
			logger.Warning("applyClientsToLocationOne update: ", err)
		}
	} else {
		if _, err := inboundSvc.AddInboundClient(data); err != nil {
			logger.Warning("applyClientsToLocationOne add: ", err)
		}
	}
}

// ----- traffic aggregation -----

func (s *LocationService) GetTrafficSnapshot(inboundId int, email string) (*model.LocationTrafficSnapshot, error) {
	db := database.GetDB()
	var snap model.LocationTrafficSnapshot
	err := db.Where("inbound_id = ? AND email = ?", inboundId, email).First(&snap).Error
	if err != nil {
		if database.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return &snap, nil
}

func (s *LocationService) SaveTrafficSnapshot(inboundId int, email string, up, down int64) error {
	db := database.GetDB()
	existing, err := s.GetTrafficSnapshot(inboundId, email)
	if err != nil {
		return err
	}
	snap := model.LocationTrafficSnapshot{InboundId: inboundId, Email: email, Up: up, Down: down}
	if existing != nil {
		snap.Id = existing.Id
	}
	return db.Save(&snap).Error
}

// ApplyTrafficDelta adds deltaUp/deltaDown to the master client's traffic.
func (s *LocationService) ApplyTrafficDelta(email string, deltaUp, deltaDown int64) error {
	if deltaUp == 0 && deltaDown == 0 {
		return nil
	}
	db := database.GetDB()
	return db.Model(&xray.ClientTraffic{}).Where("email = ?", email).
		Updates(map[string]interface{}{
			"up":   gorm.Expr("up + ?", deltaUp),
			"down": gorm.Expr("down + ?", deltaDown),
		}).Error
}

// MasterClientEmails returns the emails of all clients on the master inbound.
func (s *LocationService) MasterClientEmails() ([]string, error) {
	master, err := s.getMasterInbound()
	if err != nil || master == nil {
		return nil, err
	}
	clients, err := (&InboundService{}).GetClients(master)
	if err != nil {
		return nil, err
	}
	emails := make([]string, 0, len(clients))
	for _, c := range clients {
		if c.Email != "" {
			emails = append(emails, c.Email)
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
