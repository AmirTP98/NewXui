package service

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/alireza0/x-ui/config"
	"github.com/alireza0/x-ui/logger"
)

const (
	nodeErrorLogMaxSize   = 5 * 1024 * 1024 // 5MB, plus at most one rotated copy
	nodeErrorLogHeartbeat = 30 * time.Minute
)

type nodeLogState struct {
	lastStatus   string
	lastLoggedAt time.Time
}

var (
	nodeLogMu     sync.Mutex
	nodeLogStates = map[int]*nodeLogState{}
)

func nodeErrorLogPath() string {
	return filepath.Join(config.GetDBFolderPath(), "node_errors.log")
}

// LogNodeError writes a rate-limited line to node_errors.log: only on the
// first error for a node, on an online->offline transition, or every 30
// minutes while the node stays down (heartbeat) - never on every check, so
// a permanently-down node can't spam or fill the disk.
func LogNodeError(nodeId int, remark string, err error) {
	nodeLogMu.Lock()
	state, ok := nodeLogStates[nodeId]
	if !ok {
		state = &nodeLogState{}
		nodeLogStates[nodeId] = state
	}

	shouldLog := state.lastStatus != "offline" || time.Since(state.lastLoggedAt) >= nodeErrorLogHeartbeat
	state.lastStatus = "offline"
	if shouldLog {
		state.lastLoggedAt = time.Now()
	}
	nodeLogMu.Unlock()

	if !shouldLog {
		return
	}

	writeNodeLogLine(fmt.Sprintf("[%s] node #%d (%s): %v", time.Now().Format(time.RFC3339), nodeId, remark, err))
}

// LogNodeOK marks a node as healthy so the next failure is logged immediately
// as a fresh online->offline transition.
func LogNodeOK(nodeId int) {
	nodeLogMu.Lock()
	defer nodeLogMu.Unlock()
	state, ok := nodeLogStates[nodeId]
	if !ok {
		state = &nodeLogState{}
		nodeLogStates[nodeId] = state
	}
	state.lastStatus = "online"
}

func writeNodeLogLine(line string) {
	defer func() {
		if r := recover(); r != nil {
			logger.Warning("node_logger panic recovered: ", r)
		}
	}()

	path := nodeErrorLogPath()
	if info, err := os.Stat(path); err == nil && info.Size() > nodeErrorLogMaxSize {
		rotated := path + ".1"
		_ = os.Remove(rotated)
		if err := os.Rename(path, rotated); err != nil {
			logger.Warning("node_logger: failed to rotate log: ", err)
		}
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		logger.Warning("node_logger: failed to open log file: ", err)
		return
	}
	defer f.Close()

	if _, err := f.WriteString(line + "\n"); err != nil {
		logger.Warning("node_logger: failed to write log line: ", err)
	}
}
