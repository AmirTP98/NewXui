package database

import (
	"path"
	"sync"

	"github.com/alireza0/x-ui/config"
	"github.com/alireza0/x-ui/logger"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// MirrorTraffic holds the cumulative traffic attributed from mirror/location
// inbounds back to the master client. Stored in mirrors.db, separate from the
// main x-ui.db, so the two writes never contend.
type MirrorTraffic struct {
	Email string `gorm:"primaryKey"`
	Up    int64  `gorm:"default:0"`
	Down  int64  `gorm:"default:0"`
}

var (
	mirrorsDB     *gorm.DB
	mirrorsDBOnce sync.Once
	mirrorsDBMu   sync.RWMutex
)

func GetMirrorsDBPath() string {
	return path.Join(config.GetDBFolderPath(), "mirrors.db")
}

// InitMirrorsDB opens (or creates) mirrors.db.
// WAL mode is intentionally NOT enabled — it caused SQLITE_IOERR (522) on
// this server's filesystem. busy_timeout + the in-process RWMutex below are
// what keep concurrent reads/writes safe instead.
// Idempotent — safe to call multiple times.
func InitMirrorsDB() error {
	var initErr error
	mirrorsDBOnce.Do(func() {
		dbPath := GetMirrorsDBPath()
		gdb, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
			Logger: gormlogger.Discard,
		})
		if err != nil {
			initErr = err
			return
		}
		gdb.Exec("PRAGMA busy_timeout = 5000")
		// Force rollback-journal format — see comment in database/db.go InitDB.
		// mirrors.db was also briefly created/used while WAL mode was active.
		if res := gdb.Exec("PRAGMA journal_mode=DELETE;"); res.Error != nil {
			logger.Warning("Failed to force journal_mode=DELETE on mirrors.db:", res.Error)
		}
		if err := gdb.AutoMigrate(&MirrorTraffic{}); err != nil {
			initErr = err
			return
		}
		mirrorsDB = gdb
		logger.Info("mirrors.db initialised at", dbPath)
	})
	return initErr
}

func CloseMirrorsDB() {
	mirrorsDBMu.Lock()
	defer mirrorsDBMu.Unlock()
	if mirrorsDB != nil {
		if sqlDB, err := mirrorsDB.DB(); err == nil {
			sqlDB.Close()
		}
		mirrorsDB = nil
	}
}

// BatchUpsertMirrorTraffic atomically adds up/down deltas for every email in
// the map. Uses a single transaction; INSERT OR REPLACE is not used because
// we want to accumulate, not overwrite.
func BatchUpsertMirrorTraffic(deltas map[string][2]int64) error {
	if err := InitMirrorsDB(); err != nil {
		return err
	}
	if len(deltas) == 0 {
		return nil
	}
	mirrorsDBMu.Lock()
	defer mirrorsDBMu.Unlock()
	return mirrorsDB.Transaction(func(tx *gorm.DB) error {
		for email, d := range deltas {
			if err := tx.Exec(
				`INSERT INTO mirror_traffics (email, up, down)
				 VALUES (?, ?, ?)
				 ON CONFLICT(email) DO UPDATE SET
				   up   = up   + excluded.up,
				   down = down + excluded.down`,
				email, d[0], d[1],
			).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// GetMirrorTraffic returns accumulated mirror up/down for a single email.
// Returns (0, 0) if the email has no mirror traffic recorded.
func GetMirrorTraffic(email string) (int64, int64) {
	if err := InitMirrorsDB(); err != nil {
		return 0, 0
	}
	mirrorsDBMu.RLock()
	defer mirrorsDBMu.RUnlock()
	var row MirrorTraffic
	mirrorsDB.Where("email = ?", email).First(&row)
	return row.Up, row.Down
}

// GetAllMirrorTraffic returns a snapshot of all mirror traffic rows.
// Used by disableInvalidClients to check quotas for all clients in one shot.
func GetAllMirrorTraffic() map[string][2]int64 {
	if err := InitMirrorsDB(); err != nil {
		return nil
	}
	mirrorsDBMu.RLock()
	defer mirrorsDBMu.RUnlock()
	var rows []MirrorTraffic
	mirrorsDB.Find(&rows)
	result := make(map[string][2]int64, len(rows))
	for _, r := range rows {
		result[r.Email] = [2]int64{r.Up, r.Down}
	}
	return result
}

// ResetMirrorTraffic zeroes the mirror traffic counters for a single email.
// Call when the user resets their traffic quota.
func ResetMirrorTraffic(email string) error {
	if err := InitMirrorsDB(); err != nil {
		return err
	}
	mirrorsDBMu.Lock()
	defer mirrorsDBMu.Unlock()
	return mirrorsDB.Model(&MirrorTraffic{}).
		Where("email = ?", email).
		Updates(map[string]interface{}{"up": 0, "down": 0}).Error
}

// DeleteMirrorTraffic removes the mirror traffic record for a single email.
// Call when the client is permanently deleted.
func DeleteMirrorTraffic(email string) error {
	if err := InitMirrorsDB(); err != nil {
		return err
	}
	mirrorsDBMu.Lock()
	defer mirrorsDBMu.Unlock()
	return mirrorsDB.Where("email = ?", email).Delete(&MirrorTraffic{}).Error
}

// ResetAllMirrorTraffic zeroes all mirror traffic counters.
// Call when the user triggers a global traffic reset.
func ResetAllMirrorTraffic() error {
	if err := InitMirrorsDB(); err != nil {
		return err
	}
	mirrorsDBMu.Lock()
	defer mirrorsDBMu.Unlock()
	return mirrorsDB.Exec("UPDATE mirror_traffics SET up = 0, down = 0").Error
}
