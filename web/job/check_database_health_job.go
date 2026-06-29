package job

import (
	"github.com/alireza0/x-ui/database"
	"github.com/alireza0/x-ui/logger"
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

	db := database.GetDB()
	var result string
	if err := db.Raw("PRAGMA integrity_check;").Scan(&result).Error; err != nil {
		logger.Warning("DB health check error: ", err)
		return
	}
	if result == "ok" {
		return
	}
	logger.Warning("DB integrity issue detected: ", result)
	db.Exec("REINDEX")
	logger.Info("REINDEX completed — auto-repair applied")
}
