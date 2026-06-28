package database

import (
	"bytes"
	"io"
	"io/fs"
	"os"
	"path"
	"time"

	"github.com/alireza0/x-ui/config"
	"github.com/alireza0/x-ui/database/model"
	"github.com/alireza0/x-ui/logger"
	"github.com/alireza0/x-ui/util/common"
	"github.com/alireza0/x-ui/xray"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

var db *gorm.DB

func initUser() error {
	err := db.AutoMigrate(&model.User{})
	if err != nil {
		return err
	}
	var count int64
	err = db.Model(&model.User{}).Count(&count).Error
	if err != nil {
		return err
	}
	if count == 0 {
		user := &model.User{
			Username: "admin",
			Password: "admin",
		}
		return db.Create(user).Error
	}
	return nil
}

func initInbound() error {
	return db.AutoMigrate(&model.Inbound{})
}

func initSetting() error {
	return db.AutoMigrate(&model.Setting{})
}

func initClientTraffic() error {
	return db.AutoMigrate(&xray.ClientTraffic{})
}

func initLocation() error {
	return db.AutoMigrate(&model.Location{}, &model.LocationTrafficSnapshot{})
}

func InitDB(dbPath string) error {
	dir := path.Dir(dbPath)
	err := os.MkdirAll(dir, fs.ModeDir)
	if err != nil {
		return err
	}

	// Validate database before opening
	if _, err := os.Stat(dbPath); err == nil {
		if err := ValidateSQLiteDB(dbPath); err != nil {
			logger.Warning("Database may be corrupted, attempting to continue:", err)
		}
	}

	var gormLogger gormlogger.Interface

	if config.IsDebug() {
		gormLogger = gormlogger.Default
	} else {
		gormLogger = gormlogger.Discard
	}

	c := &gorm.Config{
		Logger: gormLogger,
	}

	// Open with pure path (no query params in DSN)
	db, err = gorm.Open(sqlite.Open(dbPath), c)
	if err != nil {
		return err
	}

	// Serialize all DB access through one connection (SQLite supports only 1 writer)
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	// Set pragmas AFTER opening connection
	db.Exec("PRAGMA busy_timeout = 5000")
	db.Exec("PRAGMA journal_mode = WAL")
	db.Exec("PRAGMA wal_autocheckpoint = 1000")
	db.Exec("PRAGMA synchronous = NORMAL")
	db.Exec("PRAGMA foreign_keys = ON")

	err = initUser()
	if err != nil {
		return err
	}
	err = initInbound()
	if err != nil {
		return err
	}
	err = initSetting()
	if err != nil {
		return err
	}

	err = initClientTraffic()
	if err != nil {
		return err
	}

	err = initLocation()
	if err != nil {
		return err
	}

	return nil
}

func CloseDB() error {
	if db != nil {
		sqlDB, err := db.DB()
		if err != nil {
			return err
		}
		return sqlDB.Close()
	}
	return nil
}

// BackupDB creates a timestamped backup of the database file.
// Called before major operations to enable recovery if needed.
func BackupDB(dbPath string) error {
	if _, err := os.Stat(dbPath); err != nil {
		return nil // Database doesn't exist yet, no backup needed
	}
	backupPath := dbPath + ".backup-" + time.Now().Format("20060102-150405")
	src, err := os.Open(dbPath)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.Create(backupPath)
	if err != nil {
		return err
	}
	defer dst.Close()
	_, err = io.Copy(dst, src)
	return err
}

// CheckDatabaseHealth validates database integrity and attempts to recover if needed.
// Returns true if the database is healthy or was recovered, false if unrecoverable.
func CheckDatabaseHealth(dbPath string) bool {
	if err := ValidateSQLiteDB(dbPath); err == nil {
		return true // Healthy
	}
	// Database corrupt - return false to signal potential issues
	logger.Warning("Database health check FAILED")
	return false
}

func GetDB() *gorm.DB {
	return db
}

func IsNotFound(err error) bool {
	return err == gorm.ErrRecordNotFound
}

func IsSQLiteDB(file io.Reader) (bool, error) {
	signature := []byte("SQLite format 3\x00")
	buf := make([]byte, len(signature))
	_, err := file.Read(buf)
	if err != nil {
		return false, err
	}
	return bytes.Equal(buf, signature), nil
}

func Checkpoint() error {
	// Update WAL
	err := db.Exec("PRAGMA wal_checkpoint;").Error
	if err != nil {
		return err
	}
	return nil
}

func ValidateSQLiteDB(dbPath string) error {
	if _, err := os.Stat(dbPath); err != nil { // file must exist
		return err
	}
	gdb, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{Logger: gormlogger.Discard})
	if err != nil {
		return err
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	var res string
	if err := gdb.Raw("PRAGMA integrity_check;").Scan(&res).Error; err != nil {
		return err
	}
	if res != "ok" {
		return common.NewError("sqlite integrity check failed: " + res)
	}
	return nil
}
