package job

import (
	"sync"
	"time"

	"github.com/alireza0/x-ui/database/model"
	"github.com/alireza0/x-ui/logger"
	"github.com/alireza0/x-ui/web/service"
)

var (
	syncLocationMu      sync.Mutex
	syncLocationLastRun time.Time
)

// SyncLocationTrafficJob periodically reads each location inbound's per-client
// traffic, diffs it against the last snapshot, and adds the delta to the master
// client's traffic - so the master client shows the sum across all locations.
type SyncLocationTrafficJob struct {
	locationService service.LocationService
	inboundService  service.InboundService
}

func NewSyncLocationTrafficJob() *SyncLocationTrafficJob {
	return new(SyncLocationTrafficJob)
}

func (j *SyncLocationTrafficJob) Run() {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("SyncLocationTrafficJob panic recovered: ", r)
		}
	}()

	interval := j.locationService.GetSyncInterval()
	syncLocationMu.Lock()
	due := time.Since(syncLocationLastRun) >= time.Duration(interval)*time.Second
	if due {
		syncLocationLastRun = time.Now()
	}
	syncLocationMu.Unlock()
	if !due {
		return
	}

	emails, err := j.locationService.MasterClientEmails()
	if err != nil || len(emails) == 0 {
		return
	}
	locations, err := j.locationService.LocationInboundIds()
	if err != nil || len(locations) == 0 {
		return
	}

	for _, baseEmail := range emails {
		var sumUp, sumDown int64
		for _, loc := range locations {
			dUp, dDown := j.syncOne(loc, baseEmail)
			sumUp += dUp
			sumDown += dDown
		}
		if sumUp != 0 || sumDown != 0 {
			if err := j.locationService.ApplyTrafficDelta(baseEmail, sumUp, sumDown); err != nil {
				logger.Warning("SyncLocationTrafficJob: apply delta: ", err)
			}
		}
	}
}

// syncOne reads the location client's traffic, diffs the snapshot, updates it,
// and returns the non-negative delta. A counter reset (location lower than the
// snapshot) clamps the delta to 0 and rebaselines instead of subtracting.
func (j *SyncLocationTrafficJob) syncOne(loc model.Location, baseEmail string) (int64, int64) {
	inboundId := loc.InboundId
	email := service.LocationClientEmail(baseEmail, loc)
	traffic, err := j.inboundService.GetClientTrafficByEmail(email)
	if err != nil || traffic == nil {
		return 0, 0
	}

	snapshot, err := j.locationService.GetTrafficSnapshot(inboundId, email)
	if err != nil {
		return 0, 0
	}
	if snapshot == nil {
		j.locationService.SaveTrafficSnapshot(inboundId, email, traffic.Up, traffic.Down)
		return 0, 0
	}

	dUp := traffic.Up - snapshot.Up
	dDown := traffic.Down - snapshot.Down
	if dUp < 0 {
		dUp = 0
	}
	if dDown < 0 {
		dDown = 0
	}
	j.locationService.SaveTrafficSnapshot(inboundId, email, traffic.Up, traffic.Down)
	return dUp, dDown
}
