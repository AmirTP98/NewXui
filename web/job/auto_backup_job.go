package job

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/alireza0/x-ui/config"
	"github.com/alireza0/x-ui/logger"
)

const maxBackups = 48

type AutoBackupJob struct{}

func NewAutoBackupJob() *AutoBackupJob {
	return new(AutoBackupJob)
}

func (j *AutoBackupJob) Run() {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("AutoBackupJob panic recovered: ", r)
		}
	}()

	dbPath := config.GetDBPath()
	if _, err := os.Stat(dbPath); err != nil {
		return
	}

	backupDir := filepath.Join(filepath.Dir(dbPath), "backups")
	os.MkdirAll(backupDir, 0755)

	backupName := fmt.Sprintf("x-ui-%s.db", time.Now().Format("2006-01-02_15-04"))
	backupPath := filepath.Join(backupDir, backupName)

	src, err := os.Open(dbPath)
	if err != nil {
		logger.Warning("AutoBackup: open failed:", err)
		return
	}
	defer src.Close()

	dst, err := os.Create(backupPath)
	if err != nil {
		logger.Warning("AutoBackup: create failed:", err)
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		logger.Warning("AutoBackup: copy failed:", err)
		return
	}

	// Rotate: keep only last maxBackups
	entries, _ := os.ReadDir(backupDir)
	var backups []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "x-ui-") && strings.HasSuffix(e.Name(), ".db") {
			backups = append(backups, filepath.Join(backupDir, e.Name()))
		}
	}
	sort.Strings(backups)
	if len(backups) > maxBackups {
		for _, old := range backups[:len(backups)-maxBackups] {
			os.Remove(old)
		}
	}
}
