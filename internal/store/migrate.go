package store

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

type migration struct {
	name    string
	version int
}

func listMigrations(fsys fs.FS) ([]migration, error) {
	names, err := fs.Glob(fsys, "migrations/*.sql")
	if err != nil {
		return nil, err
	}
	out := make([]migration, 0, len(names))
	for _, name := range names {
		base := strings.TrimPrefix(name, "migrations/")
		v, err := strconv.Atoi(strings.SplitN(base, "_", 2)[0])
		if err != nil {
			return nil, fmt.Errorf("migration %s: bad version prefix", name)
		}
		out = append(out, migration{name: name, version: v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

// migrateFS applies the migrations in fsys (under migrations/) that the
// database at path has not seen. It refuses a database whose schema is newer
// than fsys, and snapshots an existing database before upgrading it.
func migrateFS(db *sql.DB, fsys fs.FS, path string) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return err
	}
	var cur int
	if err := db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&cur); err != nil {
		return err
	}
	migs, err := listMigrations(fsys)
	if err != nil {
		return err
	}
	maxV := 0
	if len(migs) > 0 {
		maxV = migs[len(migs)-1].version
	}
	if cur > maxV {
		return fmt.Errorf("database schema v%d is newer than this binary (max v%d); upgrade MostlyDone or restore a backup", cur, maxV)
	}
	var pending []migration
	for _, m := range migs {
		if m.version > cur {
			pending = append(pending, m)
		}
	}
	if len(pending) == 0 {
		return nil
	}
	if cur > 0 {
		if err := snapshot(db, path, pending[0].version); err != nil {
			return err
		}
	}
	for _, m := range pending {
		body, err := fs.ReadFile(fsys, m.name)
		if err != nil {
			return err
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", m.name, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`, m.version, now()); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// snapshot copies the database to <path>.pre-v<N>.bak before migration N runs.
// An existing backup is never overwritten; a UTC timestamp is appended instead.
// In-memory databases are skipped.
func snapshot(db *sql.DB, path string, firstPending int) error {
	if path == "" || path == ":memory:" || strings.HasPrefix(path, "file::memory:") || strings.Contains(path, "mode=memory") {
		return nil
	}
	dst := fmt.Sprintf("%s.pre-v%d.bak", path, firstPending)
	if _, err := os.Stat(dst); err == nil {
		dst += "." + time.Now().UTC().Format("20060102T150405Z")
		if _, err := os.Stat(dst); err == nil {
			return fmt.Errorf("pre-migration snapshot: %s already exists", dst)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("pre-migration snapshot: %w", err)
	}
	if _, err := db.Exec(`VACUUM INTO ?`, dst); err != nil {
		return fmt.Errorf("pre-migration snapshot to %s: %w", dst, err)
	}
	return nil
}
