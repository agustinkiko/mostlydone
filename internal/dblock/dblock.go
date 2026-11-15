// Package dblock provides an advisory lock on "<db>.lock" so that only one
// process (server or offline restore) uses a database file at a time.
package dblock

import (
	"errors"
	"fmt"
	"os"
)

// ErrLocked means another process holds the lock.
var ErrLocked = errors.New("database is in use by another MostlyDone process")

// Lock is a held lock. Close releases it.
type Lock struct {
	f *os.File
}

// Path returns the lock file path for a database path.
func Path(dbPath string) string { return dbPath + ".lock" }

// Acquire takes the lock non-blocking, creating the lock file next to the
// database. It returns ErrLocked if another holder exists.
func Acquire(dbPath string) (*Lock, error) {
	f, err := os.OpenFile(Path(dbPath), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	if err := lockFile(f); err != nil {
		f.Close()
		return nil, err
	}
	return &Lock{f: f}, nil
}

// Close releases the lock. The lock file itself is left in place (removing it
// would race with another acquirer). Safe to call more than once.
func (l *Lock) Close() error {
	if l == nil || l.f == nil {
		return nil
	}
	f := l.f
	l.f = nil
	unlockFile(f)
	return f.Close()
}
