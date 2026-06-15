package service

import (
	"context"
	"fmt"
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

// SharedConfig describes the universal inbound template and the tracked shared client.
type SharedConfig struct {
	Inbound *model.NodeSharedInbound `json:"inbound"`
	Client  *model.NodeSharedClient  `json:"client"`
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

// clientTargetNodes returns the enabled nodes that already have a default
// inbound created (RemoteInboundId != 0) - the targets for client add/edit.
func (s *NodeService) clientTargetNodes() ([]model.Node, error) {
	nodes, err := s.enabledNodes()
	if err != nil {
		return nil, err
	}
	targets := make([]model.Node, 0, len(nodes))
	for _, n := range nodes {
		if n.RemoteInboundId != 0 {
			targets = append(targets, n)
		}
	}
	return targets, nil
}

// GetClientTargetNodes is the exported view of clientTargetNodes for jobs.
func (s *NodeService) GetClientTargetNodes() ([]model.Node, error) {
	return s.clientTargetNodes()
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
	if err := s.recomputeBridges(); err != nil {
		logger.Warning("AddNode: recomputeBridges: ", err)
	}
	// Test the connection and, if OK, create this node's default inbound from
	// the universal template. Failures are recorded on the node, not fatal.
	s.provisionNode(node)
	return nil
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
	if err := db.Where("node_id = ?", id).Delete(&model.NodeClientTrafficSnapshot{}).Error; err != nil {
		return err
	}
	return s.recomputeBridges()
}

// provisionNode tests connectivity and, on success, ensures the node's default
// inbound exists. It persists status/inbound results onto the node row.
func (s *NodeService) provisionNode(node *model.Node) {
	if !node.Enable {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	testErr := s.TestConnection(ctx, node)
	s.UpdateNodeStatus(node.Id, testErr)
	if testErr != nil {
		return
	}
	if err := s.CreateNodeInbound(ctx, node.Id); err != nil {
		logger.Warning("provisionNode: create inbound on node ", node.Id, ": ", err)
	}
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

// GetTemplate returns the single universal inbound template, or nil if unset.
func (s *NodeService) GetTemplate() (*model.NodeSharedInbound, error) {
	db := database.GetDB()
	var inbound model.NodeSharedInbound
	err := db.Order("id desc").First(&inbound).Error
	if err != nil {
		if database.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return &inbound, nil
}

// SaveTemplate stores the universal inbound template (a single row, replaced on
// each save) and then best-effort provisions the default inbound on any enabled
// node that does not have one yet.
func (s *NodeService) SaveTemplate(t *model.NodeSharedInbound) ([]NodeOpResult, error) {
	db := database.GetDB()

	existing, err := s.GetTemplate()
	if err != nil {
		return nil, err
	}
	if existing != nil {
		t.Id = existing.Id
	} else {
		t.Id = 0
	}
	if err := db.Save(t).Error; err != nil {
		return nil, err
	}

	// Provision the inbound on enabled nodes that don't already have one.
	nodes, err := s.enabledNodes()
	if err != nil {
		return nil, err
	}
	pending := make([]model.Node, 0, len(nodes))
	for _, n := range nodes {
		if n.RemoteInboundId == 0 && n.Port > 0 {
			pending = append(pending, n)
		}
	}

	jobResults := forEachNode(pending, 20*time.Second, func(ctx context.Context, i int, node model.Node) (interface{}, error) {
		return nil, s.createNodeInboundWithTemplate(ctx, &node, t)
	})

	results := make([]NodeOpResult, len(pending))
	for i, node := range pending {
		results[i] = NodeOpResult{NodeId: node.Id, Remark: node.Remark}
		if jobResults[i].err != nil {
			results[i].Error = jobResults[i].err.Error()
			continue
		}
		results[i].Success = true
	}
	return results, nil
}

// buildInboundFromTemplate constructs a model.Inbound from the template using
// the given port and a remark.
func buildInboundFromTemplate(t *model.NodeSharedInbound, port int, remark string) *model.Inbound {
	if remark == "" {
		remark = t.Remark
	}
	return &model.Inbound{
		Remark:         remark,
		Enable:         true,
		Listen:         "",
		Port:           port,
		Protocol:       model.Protocol(t.Protocol),
		Settings:       t.Settings,
		StreamSettings: t.StreamSettings,
		Sniffing:       t.Sniffing,
		Tag:            fmt.Sprintf("inbound-%v", port),
	}
}

// CreateNodeInbound (re)creates the node's default inbound from the current
// universal template on the node's configured port, storing the resulting
// remote inbound id (or error) on the node.
func (s *NodeService) CreateNodeInbound(ctx context.Context, nodeId int) error {
	node, err := s.GetNode(nodeId)
	if err != nil {
		return err
	}
	t, err := s.GetTemplate()
	if err != nil {
		return err
	}
	if t == nil {
		s.setNodeInboundError(node.Id, "no universal inbound template defined")
		return common.NewError("no universal inbound template defined")
	}
	return s.createNodeInboundWithTemplate(ctx, node, t)
}

func (s *NodeService) createNodeInboundWithTemplate(ctx context.Context, node *model.Node, t *model.NodeSharedInbound) error {
	if node.Port <= 0 {
		s.setNodeInboundError(node.Id, "node has no inbound port set")
		return common.NewError("node has no inbound port set")
	}
	client, err := NewNodeClient(node)
	if err != nil {
		s.setNodeInboundError(node.Id, err.Error())
		return err
	}
	inbound := buildInboundFromTemplate(t, node.Port, node.Remark)
	created, err := client.AddInbound(ctx, inbound)
	if err != nil {
		s.setNodeInboundError(node.Id, err.Error())
		return err
	}
	db := database.GetDB()
	return db.Model(&model.Node{}).Where("id = ?", node.Id).Updates(map[string]interface{}{
		"remote_inbound_id": created.Id,
		"inbound_error":     "",
	}).Error
}

func (s *NodeService) setNodeInboundError(nodeId int, msg string) {
	db := database.GetDB()
	if err := db.Model(&model.Node{}).Where("id = ?", nodeId).
		Update("inbound_error", msg).Error; err != nil {
		logger.Warning("setNodeInboundError: ", err)
	}
}

// TouchNodeSync records the last successful traffic-sync time for a node.
func (s *NodeService) TouchNodeSync(nodeId int) {
	db := database.GetDB()
	if err := db.Model(&model.Node{}).Where("id = ?", nodeId).
		Update("last_sync", time.Now().UnixMilli()).Error; err != nil {
		logger.Warning("TouchNodeSync: ", err)
	}
}

// GetSharedConfig returns the universal inbound template (if any) and the
// tracked shared client.
func (s *NodeService) GetSharedConfig() (*SharedConfig, error) {
	db := database.GetDB()
	config := &SharedConfig{}

	t, err := s.GetTemplate()
	if err != nil {
		return nil, err
	}
	config.Inbound = t

	var client model.NodeSharedClient
	err = db.Order("id desc").First(&client).Error
	if err == nil {
		config.Client = &client
	} else if !database.IsNotFound(err) {
		return nil, err
	}

	return config, nil
}

// GetAllSharedClients returns every tracked shared client.
func (s *NodeService) GetAllSharedClients() ([]model.NodeSharedClient, error) {
	db := database.GetDB()
	var clients []model.NodeSharedClient
	err := db.Find(&clients).Error
	return clients, err
}

// CreateSharedClient adds the given client to the default inbound on every
// enabled node that has one, and records it as the panel-wide shared client.
func (s *NodeService) CreateSharedClient(client model.Client) ([]NodeOpResult, error) {
	db := database.GetDB()

	targets, err := s.clientTargetNodes()
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return nil, common.NewError("no nodes with a default inbound; add nodes (and define the universal inbound) first")
	}

	jobResults := forEachNode(targets, 20*time.Second, func(ctx context.Context, i int, node model.Node) (interface{}, error) {
		c, err := NewNodeClient(&node)
		if err != nil {
			return nil, err
		}
		return nil, c.AddClient(ctx, node.RemoteInboundId, client)
	})

	results := s.collectResults(targets, jobResults)

	var existing model.NodeSharedClient
	sharedClient := model.NodeSharedClient{
		ClientId:   client.ID,
		Email:      client.Email,
		TotalGB:    client.TotalGB,
		ExpiryTime: client.ExpiryTime,
		Enable:     client.Enable,
	}
	if err := db.Order("id desc").First(&existing).Error; err == nil {
		sharedClient.Id = existing.Id
	}
	if err := db.Save(&sharedClient).Error; err != nil {
		return results, err
	}

	return results, nil
}

// UpdateSharedClient updates the shared client on every enabled node that has a
// default inbound, and updates the panel-wide record. If the email changes,
// stale per-node traffic snapshots for the old email are removed.
func (s *NodeService) UpdateSharedClient(client model.Client) ([]NodeOpResult, error) {
	db := database.GetDB()

	var shared model.NodeSharedClient
	if err := db.Order("id desc").First(&shared).Error; err != nil {
		if database.IsNotFound(err) {
			return nil, common.NewError("no shared client configured")
		}
		return nil, err
	}
	oldEmail := shared.Email
	oldClientId := shared.ClientId

	targets, err := s.clientTargetNodes()
	if err != nil {
		return nil, err
	}

	jobResults := forEachNode(targets, 20*time.Second, func(ctx context.Context, i int, node model.Node) (interface{}, error) {
		c, err := NewNodeClient(&node)
		if err != nil {
			return nil, err
		}
		return nil, c.UpdateClient(ctx, oldClientId, node.RemoteInboundId, client)
	})

	results := s.collectResults(targets, jobResults)

	if client.Email != oldEmail {
		if err := db.Where("email = ?", oldEmail).
			Delete(&model.NodeClientTrafficSnapshot{}).Error; err != nil {
			logger.Warning("UpdateSharedClient: failed to clean up old traffic snapshots: ", err)
		}
	}

	shared.ClientId = client.ID
	shared.Email = client.Email
	shared.TotalGB = client.TotalGB
	shared.ExpiryTime = client.ExpiryTime
	shared.Enable = client.Enable
	if err := db.Save(&shared).Error; err != nil {
		return results, err
	}

	return results, nil
}

func (s *NodeService) collectResults(targets []model.Node, jobResults []nodeJobResult) []NodeOpResult {
	results := make([]NodeOpResult, len(targets))
	for i, node := range targets {
		results[i] = NodeOpResult{NodeId: node.Id, Remark: node.Remark}
		if jobResults[i].err != nil {
			results[i].Error = jobResults[i].err.Error()
			continue
		}
		results[i].Success = true
	}
	return results
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
