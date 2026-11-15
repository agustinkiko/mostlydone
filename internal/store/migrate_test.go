package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"
)

func rawDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

func mfs(versions ...string) fstest.MapFS {
	m := fstest.MapFS{}
	for _, v := range versions {
		m["migrations/"+v+"_x.sql"] = &fstest.MapFile{Data: []byte(`CREATE TABLE t` + v + ` (id INTEGER);`)}
	}
	return m
}

func TestSchemaNewerThanBinary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.db")
	db := rawDB(t, path)
	if err := migrateFS(db, mfs("001", "002"), path); err != nil {
		t.Fatal(err)
	}
	err := migrateFS(db, mfs("001"), path)
	if err == nil || !strings.Contains(err.Error(), "database schema v2 is newer than this binary (max v1)") {
		t.Fatalf("got %v", err)
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&n)
	if n != 2 {
		t.Fatalf("schema_migrations rows = %d, want 2", n)
	}
	if m, _ := filepath.Glob(path + ".pre-*"); len(m) != 0 {
		t.Fatalf("unexpected snapshots %v", m)
	}
}

func TestPreMigrationSnapshot(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t.db")
	db := rawDB(t, path)

	if err := migrateFS(db, mfs("001"), path); err != nil {
		t.Fatal(err)
	}
	if m, _ := filepath.Glob(path + ".pre-*"); len(m) != 0 {
		t.Fatalf("fresh DB must not be snapshotted: %v", m)
	}

	if err := migrateFS(db, mfs("001", "002"), path); err != nil {
		t.Fatal(err)
	}
	bak := path + ".pre-v2.bak"
	if _, err := os.Stat(bak); err != nil {
		t.Fatalf("snapshot missing: %v", err)
	}
	// The snapshot holds the pre-upgrade schema.
	b := rawDB(t, bak)
	var n int
	if err := b.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("backup version %d err %v", n, err)
	}
}

func TestSnapshotDoesNotOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.db")
	db := rawDB(t, path)
	if err := migrateFS(db, mfs("001"), path); err != nil {
		t.Fatal(err)
	}
	bak := path + ".pre-v2.bak"
	if err := os.WriteFile(bak, []byte("precious"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := migrateFS(db, mfs("001", "002"), path); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(bak); string(got) != "precious" {
		t.Fatalf("existing backup overwritten: %q", got)
	}
	m, _ := filepath.Glob(bak + ".*")
	if len(m) != 1 {
		t.Fatalf("want one timestamped backup, got %v", m)
	}
}

func TestSnapshotsAreOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no unix file modes")
	}
	defer setPermissiveUmask()()
	upgrade := func(name string, preexisting bool) []string {
		path := filepath.Join(t.TempDir(), name)
		db := rawDB(t, path)
		if err := migrateFS(db, mfs("001"), path); err != nil {
			t.Fatal(err)
		}
		if preexisting {
			// Forces the timestamped name.
			if err := os.WriteFile(path+".pre-v2.bak", []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if err := migrateFS(db, mfs("001", "002"), path); err != nil {
			t.Fatal(err)
		}
		pat := path + ".pre-v2.bak"
		if preexisting {
			pat += ".*"
		}
		m, _ := filepath.Glob(pat)
		if len(m) != 1 {
			t.Fatalf("want one snapshot matching %s, got %v", pat, m)
		}
		return m
	}
	for _, p := range append(upgrade("a.db", false), upgrade("b.db", true)...) {
		st, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if got := st.Mode().Perm(); got != 0o600 {
			t.Errorf("%s mode %o, want 600", filepath.Base(p), got)
		}
	}
}
