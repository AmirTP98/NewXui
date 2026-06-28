package job

import (
	"github.com/alireza0/x-ui/database"
	"github.com/alireza0/x-ui/logger"
	"os"
	"path"
)

type CheckDatabaseHealthJob struct{}

func NewCheckDatabaseHealthJob() *CheckDatabaseHealthJob {
	return new(CheckDatabaseHealthJob)
}

func (j *CheckDatabaseHealthJob) Run() {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("CheckDatabaseHealthJob panic recovered: ", r)
		}
	}()

	// Get database path from config
	dir, _ := os.Getwd()
	dbPath := path.Join(dir, "x-ui.db")
	if _, err := os.Stat(dbPath); err != nil {
		// Try /etc/x-ui/x-ui.db
		dbPath = "/etc/x-ui/x-ui.db"
	}

	if !database.CheckDatabaseHealth(dbPath) {
		logger.Warning("Database health check FAILED for: ", dbPath)
		// Backup corrupted database so we can investigate
		if err := database.BackupDB(dbPath); err != nil {
			logger.Error("Failed to backup corrupted database: ", err)
		}
	}
}
