package job

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/alireza0/x-ui/database/model"
	"github.com/alireza0/x-ui/logger"
	"github.com/alireza0/x-ui/web/service"
)

const maxNodeSyncConcurrency = 5

var (
	syncNodeTrafficMu      sync.Mutex
	syncNodeTrafficLastRun time.Time
)

// SyncNodeTrafficJob periodically pulls each node's traffic counters for the
// master inbound's clients (under their per-node suffixed emails), writes the
// absolute value onto the local mirror client, and adds the delta to the master
// client - so a client's subscription shows the sum of its usage across all
// nodes plus the main panel.
//
// It is registered on a short fixed cron tick, but only actually runs every
// nodeTrafficSyncIntervalSec (configurable at runtime).
type SyncNodeTrafficJob struct {
	nodeService service.NodeService
}

func NewSyncNodeTrafficJob() *SyncNodeTrafficJob {
	return new(SyncNodeTrafficJob)
}

func (j *SyncNodeTrafficJob) Run() {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("SyncNodeTrafficJob panic recovered: ", r)
		}
	}()

	interval, err := j.nodeService.GetTrafficSyncInterval()
	if err != nil {
		logger.Warning("SyncNodeTrafficJob: ", err)
		return
	}

	syncNodeTrafficMu.Lock()
	due := time.Since(syncNodeTrafficLastRun) >= time.Duration(interval)*time.Second
	if due {
		syncNodeTrafficLastRun = time.Now()
	}
	syncNodeTrafficMu.Unlock()
	if !due {
		return
	}

	clients, err := j.nodeService.GetMasterClients()
	if err != nil {
		logger.Warning("SyncNodeTrafficJob: ", err)
		return
	}
	if len(clients) == 0 {
		return
	}

	targets, err := j.nodeService.GetClientTargetNodes()
	if err != nil {
		logger.Warning("SyncNodeTrafficJob: ", err)
		return
	}
	if len(targets) == 0 {
		return
	}

	for _, client := range clients {
		if client.Email == "" {
			continue
		}
		j.syncClient(client.Email, targets)
	}
}

func (j *SyncNodeTrafficJob) syncClient(baseEmail string, targets []model.Node) {
	var sumUp, sumDown int64
	sem := make(chan struct{}, maxNodeSyncConcurrency)
	var wg sync.WaitGroup

	for _, n := range targets {
		wg.Add(1)
		sem <- struct{}{}
		go func(node model.Node) {
			defer wg.Done()
			defer func() { <-sem }()
			defer func() {
				if r := recover(); r != nil {
					logger.Warning("SyncNodeTrafficJob: panic recovered: ", r)
				}
			}()

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			deltaUp, deltaDown, err := j.syncNode(ctx, node, baseEmail)
			if err != nil {
				service.LogNodeError(node.Id, node.Remark, err)
				return
			}
			service.LogNodeOK(node.Id)
			j.nodeService.TouchNodeSync(node.Id)
			atomic.AddInt64(&sumUp, deltaUp)
			atomic.AddInt64(&sumDown, deltaDown)
		}(n)
	}

	wg.Wait()

	if sumUp != 0 || sumDown != 0 {
		if err := j.nodeService.ApplyTrafficDelta(baseEmail, sumUp, sumDown); err != nil {
			logger.Warning("SyncNodeTrafficJob: failed to apply traffic delta: ", err)
		}
	}
}

// syncNode fetches the remote traffic counters for the node-suffixed email,
// writes the absolute value onto the local mirror client, diffs against the
// stored snapshot, updates the snapshot, and returns the non-negative delta to
// apply to the master client. A remote-side reset (counters lower than the
// snapshot) clamps the delta to 0 and rebaselines instead of subtracting.
func (j *SyncNodeTrafficJob) syncNode(ctx context.Context, node model.Node, baseEmail string) (int64, int64, error) {
	email := j.nodeService.NodeClientEmail(baseEmail, node)

	c, err := service.NewNodeClient(&node)
	if err != nil {
		return 0, 0, err
	}

	traffic, err := c.GetClientTraffic(ctx, email)
	if err != nil {
		return 0, 0, err
	}

	// keep the local mirror client's counters identical to the node's
	if err := j.nodeService.SetClientTrafficAbsolute(email, traffic.Up, traffic.Down); err != nil {
		logger.Warning("SyncNodeTrafficJob: mirror traffic update: ", err)
	}

	snapshot, err := j.nodeService.GetTrafficSnapshot(node.Id, email)
	if err != nil {
		return 0, 0, err
	}

	if snapshot == nil {
		if err := j.nodeService.SaveTrafficSnapshot(node.Id, email, traffic.Up, traffic.Down); err != nil {
			return 0, 0, err
		}
		return 0, 0, nil
	}

	deltaUp := traffic.Up - snapshot.Up
	deltaDown := traffic.Down - snapshot.Down
	if deltaUp < 0 {
		deltaUp = 0
	}
	if deltaDown < 0 {
		deltaDown = 0
	}

	if err := j.nodeService.SaveTrafficSnapshot(node.Id, email, traffic.Up, traffic.Down); err != nil {
		return 0, 0, err
	}

	return deltaUp, deltaDown, nil
}
