package service

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/alireza0/x-ui/database"
	"github.com/alireza0/x-ui/database/model"
	"github.com/alireza0/x-ui/logger"
	"github.com/alireza0/x-ui/util/common"
	"github.com/alireza0/x-ui/xray"

	"gorm.io/gorm"
)

const maxNodeConcurrency = 5

// NodeOpResult is the per-node outcome of a bulk operation.
type NodeOpResult struct {
	NodeId  int    `json:"nodeId"`
	Remark  string `json:"remark"`
	Success bool   `json:"success"`
	Error   string `json:"error"`
}

// SharedConfig describes the (at most one) shared inbound currently pushed to nodes.
type SharedConfig struct {
	Inbound *model.NodeSharedInbound     `json:"inbound"`
	Maps    []model.NodeSharedInboundMap `json:"maps"`
	Client  *model.NodeSharedClient      `json:"client"`
}

type NodeService struct {
	settingService SettingService
}

// nodeJobResult is the outcome of a single per-node job run by forEachNode.
type nodeJobResult struct {
	value interface{}
	err   error
}

// forEachNode runs fn for each node with bounded concurrency, a per-call
// timeout, and panic recovery, returning one result per node (same order/index).
func forEachNode(nodes []model.Node, timeout time.Duration, fn func(ctx context.Context, i int, node model.Node) (interface{}, error)) []nodeJobResult {
	results := make([]nodeJobResult, len(nodes))
	sem := make(chan struct{}, maxNodeConcurrency)
	var wg sync.WaitGroup

	for i := range nodes {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			defer func() {
				if r := recover(); r != nil {
					results[i] = nodeJobResult{err: common.NewErrorf("panic: %v", r)}
				}
			}()

			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			value, err := fn(ctx, i, nodes[i])
			results[i] = nodeJobResult{value: value, err: err}
		}(i)
	}

	wg.Wait()
	return results
}

func (s *NodeService) GetAllNodes() ([]model.Node, error) {
	db := database.GetDB()
	var nodes []model.Node
	err := db.Find(&nodes).Error
	return nodes, err
}

func (s *NodeService) GetNode(id int) (*model.Node, error) {
	db := database.GetDB()
	node := &model.Node{}
	err := db.First(node, id).Error
	if err != nil {
		return nil, err
	}
	return node, nil
}

func (s *NodeService) enabledNodes() ([]model.Node, error) {
	nodes, err := s.GetAllNodes()
	if err != nil {
		return nil, err
	}
	enabled := make([]model.Node, 0, len(nodes))
	for _, n := range nodes {
		if n.Enable {
			enabled = append(enabled, n)
		}
	}
	return enabled, nil
}

// recomputeBridges reconciles the local SOCKS5 bridge inbounds/routing rules
// against the current set of nodes using proxyMode "outbound".
func (s *NodeService) recomputeBridges() error {
	nodes, err := s.GetAllNodes()
	if err != nil {
		return err
	}
	tags := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if n.ProxyMode == model.ProxyModeOutbound && n.OutboundTag != "" {
			tags = append(tags, n.OutboundTag)
		}
	}
	return (&NodeBridgeService{}).EnsureBridges(tags)
}

func (s *NodeService) AddNode(node *model.Node) error {
	if node.Status == "" {
		node.Status = model.NodeStatusUnknown
	}
	db := database.GetDB()
	if err := db.Create(node).Error; err != nil {
		return err
	}
	return s.recomputeBridges()
}

func (s *NodeService) UpdateNode(node *model.Node) error {
	db := database.GetDB()
	if err := db.Save(node).Error; err != nil {
		return err
	}
	return s.recomputeBridges()
}

func (s *NodeService) DeleteNode(id int) error {
	db := database.GetDB()
	if err := db.Delete(&model.Node{}, id).Error; err != nil {
		return err
	}
	if err := db.Where("node_id = ?", id).Delete(&model.NodeSharedInboundMap{}).Error; err != nil {
		return err
	}
	if err := db.Where("node_id = ?", id).Delete(&model.NodeClientTrafficSnapshot{}).Error; err != nil {
		return err
	}
	return s.recomputeBridges()
}

// TestConnection logs into the node and fetches its inbounds to verify connectivity.
func (s *NodeService) TestConnection(ctx context.Context, node *model.Node) error {
	client, err := NewNodeClient(node)
	if err != nil {
		return err
	}
	if err := client.Login(ctx); err != nil {
		return err
	}
	if _, err := client.GetInbounds(ctx); err != nil {
		return err
	}
	return nil
}

// UpdateNodeStatus persists the result of a health check and reports it
// through the rate-limited node error logger.
func (s *NodeService) UpdateNodeStatus(id int, checkErr error) {
	db := database.GetDB()

	node := &model.Node{}
	if err := db.First(node, id).Error; err != nil {
		logger.Warning("UpdateNodeStatus: node not found: ", err)
		return
	}

	node.LastCheck = time.Now().UnixMilli()
	if checkErr != nil {
		node.Status = model.NodeStatusOffline
		node.LastError = checkErr.Error()
	} else {
		node.Status = model.NodeStatusOnline
		node.LastError = ""
	}

	if err := db.Save(node).Error; err != nil {
		logger.Warning("UpdateNodeStatus: failed to save node: ", err)
		return
	}

	if checkErr != nil {
		LogNodeError(id, node.Remark, checkErr)
	} else {
		LogNodeOK(id)
	}
}

// GetTrafficSyncInterval returns the configured traffic-sync interval in
// seconds, defaulting to 60 if unset or invalid.
func (s *NodeService) GetTrafficSyncInterval() (int, error) {
	str, err := s.settingService.getString("nodeTrafficSyncIntervalSec")
	if err != nil {
		return 60, nil
	}
	n, err := strconv.Atoi(str)
	if err != nil || n <= 0 {
		return 60, nil
	}
	return n, nil
}

func (s *NodeService) SetTrafficSyncInterval(seconds int) error {
	if seconds < 5 {
		seconds = 5
	}
	return s.settingService.saveSetting("nodeTrafficSyncIntervalSec", strconv.Itoa(seconds))
}

// GetSharedConfig returns the most recently created shared inbound (if any),
// the per-node mappings it was pushed to, and its shared client.
func (s *NodeService) GetSharedConfig() (*SharedConfig, error) {
	db := database.GetDB()
	config := &SharedConfig{}

	var inbound model.NodeSharedInbound
	err := db.Order("id desc").First(&inbound).Error
	if err != nil {
		if database.IsNotFound(err) {
			return config, nil
		}
		return nil, err
	}
	config.Inbound = &inbound

	var maps []model.NodeSharedInboundMap
	if err := db.Where("shared_inbound_id = ?", inbound.Id).Find(&maps).Error; err != nil {
		return nil, err
	}
	config.Maps = maps

	var client model.NodeSharedClient
	err = db.Where("shared_inbound_id = ?", inbound.Id).First(&client).Error
	if err == nil {
		config.Client = &client
	} else if !database.IsNotFound(err) {
		return nil, err
	}

	return config, nil
}

// CreateSharedInbound creates a new inbound on every enabled node from the
// given template and records the result in NodeSharedInbound/NodeSharedInboundMap.
func (s *NodeService) CreateSharedInbound(template *model.NodeSharedInbound) ([]NodeOpResult, error) {
	nodes, err := s.enabledNodes()
	if err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		return nil, common.NewError("no enabled nodes configured")
	}

	db := database.GetDB()
	record := *template
	record.Id = 0
	if err := db.Create(&record).Error; err != nil {
		return nil, err
	}

	jobResults := forEachNode(nodes, 20*time.Second, func(ctx context.Context, i int, node model.Node) (interface{}, error) {
		client, err := NewNodeClient(&node)
		if err != nil {
			return nil, err
		}
		inbound := &model.Inbound{
			Remark:         record.Remark,
			Enable:         true,
			Port:           record.Port,
			Protocol:       model.Protocol(record.Protocol),
			Settings:       record.Settings,
			StreamSettings: record.StreamSettings,
			Sniffing:       record.Sniffing,
		}
		return client.AddInbound(ctx, inbound)
	})

	results := make([]NodeOpResult, len(nodes))
	for i, node := range nodes {
		results[i] = NodeOpResult{NodeId: node.Id, Remark: node.Remark}
		jr := jobResults[i]
		if jr.err != nil {
			results[i].Error = jr.err.Error()
			continue
		}
		created, ok := jr.value.(*model.Inbound)
		if !ok || created == nil {
			results[i].Error = "no inbound returned"
			continue
		}
		mapRow := model.NodeSharedInboundMap{
			SharedInboundId: record.Id,
			NodeId:          node.Id,
			RemoteInboundId: created.Id,
		}
		if err := db.Create(&mapRow).Error; err != nil {
			results[i].Error = err.Error()
			continue
		}
		results[i].Success = true
	}

	return results, nil
}

// GetAllSharedClients returns every tracked shared client (one per shared
// inbound that has had a client created on it).
func (s *NodeService) GetAllSharedClients() ([]model.NodeSharedClient, error) {
	db := database.GetDB()
	var clients []model.NodeSharedClient
	err := db.Find(&clients).Error
	return clients, err
}

// GetSharedInboundMaps returns the per-node mappings for a shared inbound.
func (s *NodeService) GetSharedInboundMaps(sharedInboundId int) ([]model.NodeSharedInboundMap, error) {
	db := database.GetDB()
	var maps []model.NodeSharedInboundMap
	err := db.Where("shared_inbound_id = ?", sharedInboundId).Find(&maps).Error
	return maps, err
}

// ResolveSharedTargets resolves the enabled nodes (and their remote inbound
// ids) that currently host the given shared inbound's mappings.
func (s *NodeService) ResolveSharedTargets(maps []model.NodeSharedInboundMap) ([]model.Node, []int, error) {
	nodes, err := s.GetAllNodes()
	if err != nil {
		return nil, nil, err
	}
	nodeById := make(map[int]model.Node, len(nodes))
	for _, n := range nodes {
		nodeById[n.Id] = n
	}

	targets := make([]model.Node, 0, len(maps))
	remoteInboundIds := make([]int, 0, len(maps))
	for _, m := range maps {
		node, ok := nodeById[m.NodeId]
		if !ok || !node.Enable {
			continue
		}
		targets = append(targets, node)
		remoteInboundIds = append(remoteInboundIds, m.RemoteInboundId)
	}
	return targets, remoteInboundIds, nil
}

// CreateSharedClient adds the given client to the shared inbound on every node
// it was pushed to, and records it as the panel-wide shared client.
func (s *NodeService) CreateSharedClient(client model.Client) ([]NodeOpResult, error) {
	db := database.GetDB()

	shared, err := s.GetSharedConfig()
	if err != nil {
		return nil, err
	}
	if shared.Inbound == nil || len(shared.Maps) == 0 {
		return nil, common.NewError("no shared inbound configured")
	}

	targets, remoteInboundIds, err := s.ResolveSharedTargets(shared.Maps)
	if err != nil {
		return nil, err
	}

	jobResults := forEachNode(targets, 20*time.Second, func(ctx context.Context, i int, node model.Node) (interface{}, error) {
		c, err := NewNodeClient(&node)
		if err != nil {
			return nil, err
		}
		return nil, c.AddClient(ctx, remoteInboundIds[i], client)
	})

	results := make([]NodeOpResult, len(targets))
	for i, node := range targets {
		results[i] = NodeOpResult{NodeId: node.Id, Remark: node.Remark}
		if jobResults[i].err != nil {
			results[i].Error = jobResults[i].err.Error()
			continue
		}
		results[i].Success = true
	}

	sharedClient := model.NodeSharedClient{
		SharedInboundId: shared.Inbound.Id,
		ClientId:        client.ID,
		Email:           client.Email,
		TotalGB:         client.TotalGB,
		ExpiryTime:      client.ExpiryTime,
		Enable:          client.Enable,
	}
	if shared.Client != nil {
		sharedClient.Id = shared.Client.Id
	}
	if err := db.Save(&sharedClient).Error; err != nil {
		return results, err
	}

	return results, nil
}

// UpdateSharedClient updates the shared client on every node it was pushed to,
// and updates the panel-wide record. If the email changes, stale per-node
// traffic snapshots for the old email are removed so the new email starts
// with a fresh baseline.
func (s *NodeService) UpdateSharedClient(client model.Client) ([]NodeOpResult, error) {
	db := database.GetDB()

	shared, err := s.GetSharedConfig()
	if err != nil {
		return nil, err
	}
	if shared.Inbound == nil || shared.Client == nil {
		return nil, common.NewError("no shared client configured")
	}

	oldEmail := shared.Client.Email
	oldClientId := shared.Client.ClientId

	targets, remoteInboundIds, err := s.ResolveSharedTargets(shared.Maps)
	if err != nil {
		return nil, err
	}

	jobResults := forEachNode(targets, 20*time.Second, func(ctx context.Context, i int, node model.Node) (interface{}, error) {
		c, err := NewNodeClient(&node)
		if err != nil {
			return nil, err
		}
		return nil, c.UpdateClient(ctx, oldClientId, remoteInboundIds[i], client)
	})

	results := make([]NodeOpResult, len(targets))
	for i, node := range targets {
		results[i] = NodeOpResult{NodeId: node.Id, Remark: node.Remark}
		if jobResults[i].err != nil {
			results[i].Error = jobResults[i].err.Error()
			continue
		}
		results[i].Success = true
	}

	if client.Email != oldEmail {
		nodeIds := make([]int, 0, len(shared.Maps))
		for _, m := range shared.Maps {
			nodeIds = append(nodeIds, m.NodeId)
		}
		if err := db.Where("email = ? AND node_id IN ?", oldEmail, nodeIds).
			Delete(&model.NodeClientTrafficSnapshot{}).Error; err != nil {
			logger.Warning("UpdateSharedClient: failed to clean up old traffic snapshots: ", err)
		}
	}

	shared.Client.ClientId = client.ID
	shared.Client.Email = client.Email
	shared.Client.TotalGB = client.TotalGB
	shared.Client.ExpiryTime = client.ExpiryTime
	shared.Client.Enable = client.Enable
	if err := db.Save(shared.Client).Error; err != nil {
		return results, err
	}

	return results, nil
}

// ApplyTrafficDelta adds deltaUp/deltaDown to the local client's traffic
// counters directly, without touching the online-clients list maintained by
// the local Xray traffic job.
func (s *NodeService) ApplyTrafficDelta(email string, deltaUp, deltaDown int64) error {
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

// GetTrafficSnapshot returns the last-seen traffic counters for (nodeId, email),
// or nil if there is no snapshot yet.
func (s *NodeService) GetTrafficSnapshot(nodeId int, email string) (*model.NodeClientTrafficSnapshot, error) {
	db := database.GetDB()
	var snap model.NodeClientTrafficSnapshot
	err := db.Where("node_id = ? AND email = ?", nodeId, email).First(&snap).Error
	if err != nil {
		if database.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return &snap, nil
}

// SaveTrafficSnapshot creates or updates the (nodeId, email) snapshot row.
func (s *NodeService) SaveTrafficSnapshot(nodeId int, email string, up, down int64) error {
	db := database.GetDB()
	existing, err := s.GetTrafficSnapshot(nodeId, email)
	if err != nil {
		return err
	}
	snap := model.NodeClientTrafficSnapshot{NodeId: nodeId, Email: email, Up: up, Down: down}
	if existing != nil {
		snap.Id = existing.Id
	}
	return db.Save(&snap).Error
}
