package job

import (
	"context"
	"time"

	"github.com/alireza0/x-ui/logger"
	"github.com/alireza0/x-ui/web/service"
)

// CheckNodeStatusJob periodically tests connectivity to every enabled node and
// records the result. Each node is checked on its own goroutine with a bounded
// timeout and panic recovery, so a single hung/misbehaving node can never block
// or crash the cron loop.
type CheckNodeStatusJob struct {
	nodeService service.NodeService
}

func NewCheckNodeStatusJob() *CheckNodeStatusJob {
	return new(CheckNodeStatusJob)
}

func (j *CheckNodeStatusJob) Run() {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("CheckNodeStatusJob panic recovered: ", r)
		}
	}()

	nodes, err := j.nodeService.GetAllNodes()
	if err != nil {
		logger.Warning("CheckNodeStatusJob: ", err)
		return
	}

	for _, n := range nodes {
		if !n.Enable {
			continue
		}
		node := n
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Warning("CheckNodeStatusJob: panic recovered: ", r)
				}
			}()

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			err := j.nodeService.TestConnection(ctx, &node)
			j.nodeService.UpdateNodeStatus(node.Id, err)
		}()
	}
}
