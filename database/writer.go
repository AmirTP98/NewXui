package database

import "sync"

var writeMu sync.Mutex

// WithWrite serializes all database write operations through a single
// mutex. SQLite supports only one writer at a time — concurrent writes
// cause SQLITE_BUSY which can corrupt the database if busy_timeout
// expires mid-transaction. Reads are not affected (WAL allows concurrent reads).
func WithWrite(fn func() error) error {
	writeMu.Lock()
	defer writeMu.Unlock()
	return fn()
}
