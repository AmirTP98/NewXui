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

// MasterConfig describes the designated master inbound and the local inbounds
// that can be chosen as master.
type MasterConfig struct {
	MasterInboundId int             `json:"masterInboundId"`
	Master          *model.Inbound  `json:"master"`
	Inbounds        []model.Inbound `json:"inbounds"`
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
// inbound created (RemoteInboundId != 0) - the targets for client fan-out.
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

	node, _ := s.GetNode(id)
	if node != nil && node.LocalInboundId != 0 {
		if _, err := (&InboundService{}).DelInbound(node.LocalInboundId); err != nil {
			logger.Warning("DeleteNode: delete local mirror inbound: ", err)
		}
	}

	if err := db.Delete(&model.Node{}, id).Error; err != nil {
		return err
	}
	if err := db.Where("node_id = ?", id).Delete(&model.NodeClientTrafficSnapshot{}).Error; err != nil {
		return err
	}
	return s.recomputeBridges()
}

// provisionNode tests connectivity and, on success, ensures the node's inbounds
// exist (remote + local mirror) from the master template. Failures are recorded
// on the node, not fatal.
func (s *NodeService) provisionNode(node *model.Node) {
	if !node.Enable {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()

	testErr := s.TestConnection(ctx, node)
	s.UpdateNodeStatus(node.Id, testErr)
	if testErr != nil {
		return
	}
	if err := s.EnsureNodeInbounds(ctx, node.Id); err != nil {
		logger.Warning("provisionNode: ensure inbounds on node ", node.Id, ": ", err)
	}
}

// CreateNodeInbound is the manual (re)provision entry point used by the UI button.
func (s *NodeService) CreateNodeInbound(ctx context.Context, nodeId int) error {
	return s.EnsureNodeInbounds(ctx, nodeId)
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

// setNodeInboundError records the last inbound-provisioning error on a node.
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

// GetMasterConfig returns the designated master inbound and the list of local
// inbounds that can be chosen as master (mirror inbounds excluded).
func (s *NodeService) GetMasterConfig() (*MasterConfig, error) {
	db := database.GetDB()
	cfg := &MasterConfig{MasterInboundId: s.GetMasterInboundId()}

	master, err := s.getMasterInbound()
	if err != nil {
		return nil, err
	}
	cfg.Master = master

	var inbounds []model.Inbound
	if err := db.Where("tag NOT LIKE ?", "node-mirror-%").Find(&inbounds).Error; err != nil {
		return nil, err
	}
	cfg.Inbounds = inbounds
	return cfg, nil
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

// SetClientTrafficAbsolute overwrites a client's traffic counters (used to make
// a local mirror client mirror the node's absolute counters).
func (s *NodeService) SetClientTrafficAbsolute(email string, up, down int64) error {
	db := database.GetDB()
	return db.Model(&xray.ClientTraffic{}).Where("email = ?", email).
		Updates(map[string]interface{}{"up": up, "down": down}).Error
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
