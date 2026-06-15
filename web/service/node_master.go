package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/alireza0/x-ui/database"
	"github.com/alireza0/x-ui/database/model"
	"github.com/alireza0/x-ui/logger"
	"github.com/alireza0/x-ui/util/common"
)

// ----- master inbound designation -----

func (s *NodeService) GetMasterInboundId() int {
	str, err := s.settingService.getString("nodeMasterInboundId")
	if err != nil {
		return 0
	}
	id, _ := strconv.Atoi(str)
	return id
}

func (s *NodeService) SetMasterInboundId(id int) error {
	return s.settingService.saveSetting("nodeMasterInboundId", strconv.Itoa(id))
}

func (s *NodeService) getMasterInbound() (*model.Inbound, error) {
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

// IsMasterInbound reports whether the given inbound id is the configured master.
func (s *NodeService) IsMasterInbound(inboundId int) bool {
	mid := s.GetMasterInboundId()
	return mid != 0 && mid == inboundId
}

// ----- per-node naming -----

// nodeSuffix is the per-node email/remark suffix (the node remark, sanitised).
func nodeSuffix(node model.Node) string {
	r := strings.TrimSpace(node.Remark)
	r = strings.ReplaceAll(r, " ", "_")
	if r == "" {
		r = fmt.Sprintf("node%d", node.Id)
	}
	return r
}

func nodeClientEmail(baseEmail string, node model.Node) string {
	return baseEmail + "-" + nodeSuffix(node)
}

// NodeClientEmail is the exported per-node suffixed email used by jobs.
func (s *NodeService) NodeClientEmail(baseEmail string, node model.Node) string {
	return nodeClientEmail(baseEmail, node)
}

// MasterClientEmail returns the base email of the master client matching the
// given clientId (uuid/password/email), or "" if not found.
func (s *NodeService) MasterClientEmail(clientId string) string {
	clients, err := s.GetMasterClients()
	if err != nil {
		return ""
	}
	for _, c := range clients {
		if c.ID == clientId || c.Password == clientId || c.Email == clientId {
			return c.Email
		}
	}
	return ""
}

// GetMasterClients returns the clients of the designated master inbound, or nil
// if no master is configured.
func (s *NodeService) GetMasterClients() ([]model.Client, error) {
	master, err := s.getMasterInbound()
	if err != nil || master == nil {
		return nil, err
	}
	return (&InboundService{}).GetClients(master)
}

// suffixedClient returns a copy of client with its email suffixed for the node.
// The id/password/limits are preserved so the same uuid works everywhere.
func suffixedClient(client model.Client, node model.Node) model.Client {
	c := client
	c.Email = nodeClientEmail(client.Email, node)
	return c
}

// stripClients returns settings JSON with an empty clients array (other keys kept).
func stripClients(settings string) string {
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(settings), &m); err != nil {
		return settings
	}
	if _, ok := m["clients"]; ok {
		m["clients"] = []interface{}{}
	}
	b, err := json.Marshal(m)
	if err != nil {
		return settings
	}
	return string(b)
}

// buildNodeInbound clones the master inbound config onto a fresh inbound with
// the given port/remark/tag/enable and no clients.
func buildNodeInbound(master *model.Inbound, port int, remark, tag string, enable bool) *model.Inbound {
	return &model.Inbound{
		Enable:         enable,
		Remark:         remark,
		Listen:         "",
		Port:           port,
		Protocol:       master.Protocol,
		Settings:       stripClients(master.Settings),
		StreamSettings: master.StreamSettings,
		Sniffing:       master.Sniffing,
		Tag:            tag,
	}
}

// ----- node inbound provisioning -----

// EnsureNodeInbounds creates (if missing) this node's remote inbound and its
// local mirror inbound from the master template, then replicates the master's
// current clients to the node. Results are recorded on the node row.
func (s *NodeService) EnsureNodeInbounds(ctx context.Context, nodeId int) error {
	node, err := s.GetNode(nodeId)
	if err != nil {
		return err
	}
	master, err := s.getMasterInbound()
	if err != nil {
		return err
	}
	if master == nil {
		s.setNodeInboundError(node.Id, "no master inbound selected")
		return common.NewError("no master inbound selected")
	}
	if node.Port <= 0 {
		s.setNodeInboundError(node.Id, "node has no inbound port set")
		return common.NewError("node has no inbound port set")
	}

	// remote inbound on the node
	client, err := NewNodeClient(node)
	if err != nil {
		s.setNodeInboundError(node.Id, err.Error())
		return err
	}
	remoteInbound := buildNodeInbound(master, node.Port, node.Remark, fmt.Sprintf("inbound-%d", node.Port), true)
	created, err := client.AddInbound(ctx, remoteInbound)
	if err != nil {
		s.setNodeInboundError(node.Id, "remote inbound: "+err.Error())
		return err
	}
	node.RemoteInboundId = created.Id

	// local mirror inbound (disabled - it is a tracking shell, not a live listener)
	if node.LocalInboundId == 0 {
		mirror := buildNodeInbound(master, node.Port, node.Remark, fmt.Sprintf("node-mirror-%d", node.Id), false)
		localCreated, _, err := (&InboundService{}).AddInbound(mirror)
		if err != nil {
			logger.Warning("EnsureNodeInbounds: create local mirror: ", err)
		} else {
			node.LocalInboundId = localCreated.Id
		}
	}

	db := database.GetDB()
	if err := db.Model(&model.Node{}).Where("id = ?", node.Id).Updates(map[string]interface{}{
		"remote_inbound_id": node.RemoteInboundId,
		"local_inbound_id":  node.LocalInboundId,
		"inbound_error":     "",
	}).Error; err != nil {
		return err
	}

	// replicate the master's existing clients onto this freshly provisioned node
	clients, err := (&InboundService{}).GetClients(master)
	if err == nil && len(clients) > 0 {
		s.applyClientsToNode(ctx, *node, clients, false)
	}
	return nil
}

// ----- fan-out from master client operations -----

// FanOutAddClients pushes newly added master clients to every provisioned node.
func (s *NodeService) FanOutAddClients(clients []model.Client) {
	nodes, err := s.clientTargetNodes()
	if err != nil {
		logger.Warning("FanOutAddClients: ", err)
		return
	}
	for _, node := range nodes {
		n := node
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		s.applyClientsToNode(ctx, n, clients, false)
		cancel()
	}
}

// applyClientsToNode adds (or, when update=true, updates) the suffixed copies of
// the given clients on a single node's remote inbound and local mirror inbound.
func (s *NodeService) applyClientsToNode(ctx context.Context, node model.Node, clients []model.Client, update bool) {
	defer func() {
		if r := recover(); r != nil {
			logger.Warning("applyClientsToNode panic: ", r)
		}
	}()
	if node.RemoteInboundId == 0 {
		return
	}
	rc, err := NewNodeClient(&node)
	if err != nil {
		LogNodeError(node.Id, node.Remark, err)
		return
	}
	inboundSvc := &InboundService{}
	for _, base := range clients {
		sc := suffixedClient(base, node)
		var rerr error
		if update {
			rerr = rc.UpdateClient(ctx, base.ID, node.RemoteInboundId, sc)
		} else {
			rerr = rc.AddClient(ctx, node.RemoteInboundId, sc)
		}
		if rerr != nil {
			LogNodeError(node.Id, node.Remark, rerr)
		} else {
			LogNodeOK(node.Id)
		}
		// local mirror
		if node.LocalInboundId != 0 {
			s.writeMirrorClient(inboundSvc, node.LocalInboundId, base.ID, sc, update)
		}
	}
}

func (s *NodeService) writeMirrorClient(inboundSvc *InboundService, localInboundId int, oldClientId string, sc model.Client, update bool) {
	settings, err := json.Marshal(map[string]interface{}{"clients": []model.Client{sc}})
	if err != nil {
		return
	}
	data := &model.Inbound{Id: localInboundId, Settings: string(settings)}
	if update {
		if _, err := inboundSvc.UpdateInboundClient(data, oldClientId); err != nil {
			logger.Warning("writeMirrorClient update: ", err)
		}
	} else {
		if _, err := inboundSvc.AddInboundClient(data); err != nil {
			logger.Warning("writeMirrorClient add: ", err)
		}
	}
}

// FanOutUpdateClient propagates an edit of the master client (identified by its
// previous clientId) to every node.
func (s *NodeService) FanOutUpdateClient(oldClientId string, client model.Client) {
	nodes, err := s.clientTargetNodes()
	if err != nil {
		logger.Warning("FanOutUpdateClient: ", err)
		return
	}
	for _, node := range nodes {
		n := node
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		s.applyClientsToNode(ctx, n, []model.Client{client}, true)
		cancel()
	}
}

// FanOutDelClient removes the per-node copies of a deleted master client.
// clientId is the uuid/password (matches the node copy); baseEmail is the
// master email used to derive the suffixed email for snapshot cleanup.
func (s *NodeService) FanOutDelClient(clientId, baseEmail string) {
	nodes, err := s.clientTargetNodes()
	if err != nil {
		logger.Warning("FanOutDelClient: ", err)
		return
	}
	db := database.GetDB()
	inboundSvc := &InboundService{}
	for _, node := range nodes {
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Warning("FanOutDelClient panic: ", r)
				}
			}()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			rc, err := NewNodeClient(&node)
			if err != nil {
				LogNodeError(node.Id, node.Remark, err)
				return
			}
			if err := rc.DelClient(ctx, node.RemoteInboundId, clientId); err != nil {
				LogNodeError(node.Id, node.Remark, err)
			}
			if node.LocalInboundId != 0 {
				if _, err := inboundSvc.DelInboundClient(node.LocalInboundId, clientId); err != nil {
					logger.Warning("FanOutDelClient mirror: ", err)
				}
			}
			if baseEmail != "" {
				db.Where("node_id = ? AND email = ?", node.Id, nodeClientEmail(baseEmail, node)).
					Delete(&model.NodeClientTrafficSnapshot{})
			}
		}()
	}
}

// FanOutResetTraffic zeroes the per-node traffic for a master client (by email).
func (s *NodeService) FanOutResetTraffic(baseEmail string) {
	if baseEmail == "" {
		return
	}
	nodes, err := s.clientTargetNodes()
	if err != nil {
		logger.Warning("FanOutResetTraffic: ", err)
		return
	}
	db := database.GetDB()
	inboundSvc := &InboundService{}
	for _, node := range nodes {
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Warning("FanOutResetTraffic panic: ", r)
				}
			}()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			email := nodeClientEmail(baseEmail, node)
			rc, err := NewNodeClient(&node)
			if err != nil {
				LogNodeError(node.Id, node.Remark, err)
				return
			}
			if err := rc.ResetClientTraffic(ctx, node.RemoteInboundId, email); err != nil {
				LogNodeError(node.Id, node.Remark, err)
			}
			if node.LocalInboundId != 0 {
				inboundSvc.ResetClientTraffic(node.LocalInboundId, email)
			}
			db.Where("node_id = ? AND email = ?", node.Id, email).
				Delete(&model.NodeClientTrafficSnapshot{})
		}()
	}
}
