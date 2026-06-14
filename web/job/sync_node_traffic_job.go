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
// shared client(s), diffs them against the last-seen snapshot, and applies the
// delta to the local client's traffic - so a client's subscription shows the
// sum of its usage across all nodes plus the main panel.
//
// It is registered on a short fixed cron tick, but only actually runs every
// nodeTrafficSyncIntervalSec (configurable at runtime without re-registering
// the cron job).
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

	clients, err := j.nodeService.GetAllSharedClients()
	if err != nil {
		logger.Warning("SyncNodeTrafficJob: ", err)
		return
	}

	for _, client := range clients {
		j.syncClient(client)
	}
}

func (j *SyncNodeTrafficJob) syncClient(client model.NodeSharedClient) {
	if client.Email == "" {
		return
	}

	maps, err := j.nodeService.GetSharedInboundMaps(client.SharedInboundId)
	if err != nil {
		logger.Warning("SyncNodeTrafficJob: ", err)
		return
	}

	targets, _, err := j.nodeService.ResolveSharedTargets(maps)
	if err != nil {
		logger.Warning("SyncNodeTrafficJob: ", err)
		return
	}
	if len(targets) == 0 {
		return
	}

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

			deltaUp, deltaDown, err := j.syncNode(ctx, node, client.Email)
			if err != nil {
				service.LogNodeError(node.Id, node.Remark, err)
				return
			}
			service.LogNodeOK(node.Id)
			atomic.AddInt64(&sumUp, deltaUp)
			atomic.AddInt64(&sumDown, deltaDown)
		}(n)
	}

	wg.Wait()

	if sumUp != 0 || sumDown != 0 {
		if err := j.nodeService.ApplyTrafficDelta(client.Email, sumUp, sumDown); err != nil {
			logger.Warning("SyncNodeTrafficJob: failed to apply traffic delta: ", err)
		}
	}
}

// syncNode fetches the remote traffic counters for email on node, diffs them
// against the stored snapshot, updates the snapshot, and returns the
// non-negative delta to apply. A remote-side reset (counters lower than the
// snapshot) clamps the delta to 0 and rebaselines instead of subtracting.
func (j *SyncNodeTrafficJob) syncNode(ctx context.Context, node model.Node, email string) (int64, int64, error) {
	c, err := service.NewNodeClient(&node)
	if err != nil {
		return 0, 0, err
	}

	traffic, err := c.GetClientTraffic(ctx, email)
	if err != nil {
		return 0, 0, err
	}

	snapshot, err := j.nodeService.GetTrafficSnapshot(node.Id, email)
	if err != nil {
		return 0, 0, err
	}

	if snapshot == nil {
		// First observation: establish a baseline without counting any
		// pre-existing historical usage on the remote node.
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
