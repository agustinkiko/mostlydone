// Package backup implements online backup (VACUUM INTO) and offline restore
// of the MostlyDone SQLite file.
package backup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"mostlydone/internal/dblock"
	"mostlydone/internal/store"

	_ "modernc.org/sqlite"
)

// Info describes a finished backup or restore.
type Info struct {
	Path          string        // file written (backup) or database restored into
	Size          int64         // bytes
	SchemaVersion int           // MAX(version) in schema_migrations
	Duration      time.Duration // wall time
	Aside         string        // restore: where the previous DB was moved, if any
}

// openRO opens path read-only without ever creating it. immutable=1 skips
// locking and WAL files; use it only on files no one is writing.
func openRO(path string, immutable bool) (*sql.DB, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(abs); err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("mode", "ro")
	if immutable {
		q.Set("immutable", "1")
	}
	u := url.URL{Scheme: "file", Path: abs, RawQuery: q.Encode()}
	dsn := u.String()
	if !immutable {
		dsn += "&_pragma=busy_timeout(10000)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

// check runs integrity_check and reads the schema version. The file is
// opened read-only and is not modified.
func check(ctx context.Context, path string) (int, error) {
	db, err := openRO(path, true)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var res string
	if err := db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&res); err != nil {
		return 0, fmt.Errorf("%s: not a valid SQLite database: %w", path, err)
	}
	if res != "ok" {
		return 0, fmt.Errorf("%s: integrity_check failed: %s", path, res)
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name='schema_migrations'`).Scan(&n); err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, fmt.Errorf("%s: no schema_migrations table; not a MostlyDone database", path)
	}
	var v sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&v); err != nil {
		return 0, err
	}
	if !v.Valid {
		return 0, fmt.Errorf("%s: schema_migrations is empty", path)
	}
	return int(v.Int64), nil
}

// Backup writes a consistent snapshot of the database at srcPath to dest
// while the server may be running. It never migrates or modifies the source.
// The snapshot is built in a temp file next to dest, verified, then moved
// into place, so a partial backup never appears under dest.
func Backup(ctx context.Context, srcPath, dest string, force bool) (Info, error) {
	start := time.Now()
	if !force {
		if _, err := os.Lstat(dest); err == nil {
			return Info{}, fmt.Errorf("%s already exists (use -force to overwrite)", dest)
		}
	}
	src, err := openRO(srcPath, false)
	if err != nil {
		return Info{}, fmt.Errorf("open source: %w", err)
	}
	defer src.Close()
	if err := src.PingContext(ctx); err != nil {
		return Info{}, fmt.Errorf("open source: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return Info{}, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), "."+filepath.Base(dest)+".tmp-*")
	if err != nil {
		return Info{}, err
	}
	tmpName := tmp.Name()
	tmp.Close()
	ok := false
	defer func() {
		if !ok {
			os.Remove(tmpName)
		}
	}()

	if _, err := src.ExecContext(ctx, `VACUUM INTO ?`, tmpName); err != nil {
		return Info{}, fmt.Errorf("snapshot: %w", err)
	}
	ver, err := check(ctx, tmpName)
	if err != nil {
		return Info{}, fmt.Errorf("verify backup: %w", err)
	}
	// Backups hold the whole database: owner-only, set explicitly rather than
	// relying on CreateTemp. Done before the move so a failure leaves nothing
	// under dest (the deferred cleanup removes the temp file).
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return Info{}, fmt.Errorf("set backup mode: %w", err)
	}
	if err := syncFile(tmpName); err != nil {
		return Info{}, err
	}
	if force {
		err = os.Rename(tmpName, dest)
	} else {
		// Link fails if dest appeared since the check above.
		if err = os.Link(tmpName, dest); err == nil {
			os.Remove(tmpName)
		} else if errors.Is(err, os.ErrExist) {
			return Info{}, fmt.Errorf("%s already exists (use -force to overwrite)", dest)
		} else {
			err = os.Rename(tmpName, dest) // filesystem without hard links
		}
	}
	if err != nil {
		return Info{}, err
	}
	ok = true
	st, err := os.Stat(dest)
	if err != nil {
		return Info{}, err
	}
	return Info{Path: dest, Size: st.Size(), SchemaVersion: ver, Duration: time.Since(start)}, nil
}

func syncFile(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

// AsidePath is where Restore moves an existing database.
func AsidePath(dbPath string, t time.Time) string {
	return dbPath + ".pre-restore-" + t.UTC().Format("20060102T150405Z") + ".bak"
}

// Restore replaces the database at dbPath with the backup at srcBackup. It
// is offline only: it takes the database lock and refuses if the server (or
// anything else) holds it. An existing database is moved aside first and
// requires force.
func Restore(ctx context.Context, srcBackup, dbPath string, force bool) (Info, error) {
	start := time.Now()
	ver, err := check(ctx, srcBackup)
	if err != nil {
		return Info{}, err
	}
	if max := store.MaxSchemaVersion(); ver > max {
		return Info{}, fmt.Errorf("%s has schema version %d, newer than this binary supports (%d); use a newer mostlydone", srcBackup, ver, max)
	}
	if si, err := os.Stat(srcBackup); err == nil {
		if di, err := os.Stat(dbPath); err == nil && os.SameFile(si, di) {
			return Info{}, errors.New("backup and target database are the same file")
		}
	}

	lock, err := dblock.Acquire(dbPath)
	if err != nil {
		return Info{}, err
	}
	defer lock.Close()

	var aside string
	_, statErr := os.Stat(dbPath)
	exists := statErr == nil
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return Info{}, statErr
	}
	if exists {
		aside = AsidePath(dbPath, time.Now())
		if !force {
			return Info{}, fmt.Errorf("%s exists; use -force to replace it (current DB would be moved to %s)", dbPath, aside)
		}
		if _, err := os.Lstat(aside); err == nil {
			return Info{}, fmt.Errorf("%s already exists; retry in a second", aside)
		}
		if err := checkpoint(ctx, dbPath); err != nil {
			return Info{}, fmt.Errorf("checkpoint current database: %w", err)
		}
		if err := os.Rename(dbPath, aside); err != nil {
			return Info{}, err
		}
		// The aside file keeps the live file's mode; make it owner-only. The
		// restore has not completed yet, so undo like any other later failure.
		if err := os.Chmod(aside, 0o600); err != nil {
			undo(dbPath, aside)
			return Info{}, fmt.Errorf("set mode on %s: %w", aside, err)
		}
	}
	// Stale WAL/SHM belong to the old file and would corrupt the new one.
	for _, suf := range []string{"-wal", "-shm"} {
		if err := os.Remove(dbPath + suf); err != nil && !errors.Is(err, os.ErrNotExist) {
			undo(dbPath, aside)
			return Info{}, err
		}
	}
	if err := copyInto(srcBackup, dbPath); err != nil {
		undo(dbPath, aside)
		return Info{}, err
	}
	st, err := os.Stat(dbPath)
	if err != nil {
		return Info{}, err
	}
	return Info{Path: dbPath, Size: st.Size(), SchemaVersion: ver, Duration: time.Since(start), Aside: aside}, nil
}

// undo puts the aside copy back after a failed restore.
func undo(dbPath, aside string) {
	if aside != "" {
		os.Rename(aside, dbPath)
	}
}

// checkpoint folds the WAL into the main file so the aside copy is complete.
func checkpoint(ctx context.Context, path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	u := url.URL{Scheme: "file", Path: abs, RawQuery: "mode=rw&_pragma=busy_timeout(10000)"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	var busy, logf, ckpt int
	if err := db.QueryRowContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &logf, &ckpt); err != nil {
		return err
	}
	if busy != 0 {
		return errors.New("checkpoint blocked by another connection")
	}
	return nil
}

// copyInto copies src to a temp file beside dst and renames it into place.
func copyInto(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".restore-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	fail := func(err error) error {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if _, err := io.Copy(tmp, in); err != nil {
		return fail(err)
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, dst); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}
