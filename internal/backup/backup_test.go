package backup

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"mostlydone/internal/dblock"
	"mostlydone/internal/store"

	_ "modernc.org/sqlite"
)

var ctx = context.Background()

// makeDB builds a store DB with one project and n work items.
func makeDB(t *testing.T, path string, n int) {
	t.Helper()
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	u, err := s.Setup(ctx, "main", "a@example.com", "password123", "A")
	if err != nil {
		t.Fatal(err)
	}
	ws, err := s.Workspace(ctx, "main")
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.CreateProject(ctx, ws.ID, u.ID, store.ProjectInput{Name: store.Some("Web"), Identifier: store.Some("web")})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		if _, err := s.CreateIssue(ctx, p, u.ID, store.IssueInput{Name: store.Some(fmt.Sprintf("item %d", i))}); err != nil {
			t.Fatal(err)
		}
	}
}

func count(t *testing.T, path, table string) int {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestRoundTripConcurrent(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.db")
	s, err := store.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := s.Setup(ctx, "main", "a@example.com", "password123", "A")
	ws, _ := s.Workspace(ctx, "main")
	p, err := s.CreateProject(ctx, ws.ID, u.ID, store.ProjectInput{Name: store.Some("Web"), Identifier: store.Some("web")})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		if _, err := s.CreateIssue(ctx, p, u.ID, store.IssueInput{Name: store.Some("seed")}); err != nil {
			t.Fatal(err)
		}
	}

	// An open read transaction on a second connection.
	rdb, err := sql.Open("sqlite", src)
	if err != nil {
		t.Fatal(err)
	}
	defer rdb.Close()
	tx, err := rdb.Begin()
	if err != nil {
		t.Fatal(err)
	}
	var seen int
	if err := tx.QueryRow(`SELECT count(*) FROM issues`).Scan(&seen); err != nil {
		t.Fatal(err)
	}

	// A writer inserting while the backup runs.
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				s.CreateIssue(ctx, p, u.ID, store.IssueInput{Name: store.Some("live")})
			}
		}
	}()

	dest := filepath.Join(dir, "backups", "b.db")
	os.Mkdir(filepath.Dir(dest), 0o755)
	info, err := Backup(ctx, src, dest, false)
	close(stop)
	wg.Wait()
	tx.Rollback()
	if err != nil {
		t.Fatal(err)
	}
	if info.SchemaVersion != store.MaxSchemaVersion() || info.Size == 0 {
		t.Fatalf("bad info %+v", info)
	}
	if ents, _ := os.ReadDir(filepath.Dir(dest)); len(ents) != 1 {
		t.Fatalf("temp files left: %v", ents)
	}
	s.Close()

	// Backup file is a consistent snapshot with at least the seeded rows.
	want := count(t, dest, "issues")
	if want < 20 {
		t.Fatalf("backup has %d issues", want)
	}

	// Restore into a new path and open with the real store.
	target := filepath.Join(dir, "new", "restored.db")
	os.Mkdir(filepath.Dir(target), 0o755)
	rinfo, err := Restore(ctx, dest, target, false)
	if err != nil {
		t.Fatal(err)
	}
	if rinfo.Aside != "" {
		t.Fatalf("unexpected aside %q", rinfo.Aside)
	}
	rs, err := store.Open(target)
	if err != nil {
		t.Fatal(err)
	}
	projects, err := rs.ListProjects(ctx, ws.ID, "")
	rs.Close()
	if err != nil || len(projects) != 1 {
		t.Fatalf("projects: %v %v", projects, err)
	}
	if got := count(t, target, "issues"); got != want {
		t.Fatalf("issues: got %d want %d", got, want)
	}
}

func TestBackupRefusesExistingAndLeavesNoPartial(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.db")
	makeDB(t, src, 1)
	dest := filepath.Join(dir, "b.db")
	if _, err := Backup(ctx, src, dest, false); err != nil {
		t.Fatal(err)
	}
	if _, err := Backup(ctx, src, dest, false); err == nil || !strings.Contains(err.Error(), "exists") {
		t.Fatalf("want exists error, got %v", err)
	}
	if _, err := Backup(ctx, src, dest, true); err != nil {
		t.Fatalf("force: %v", err)
	}

	// Bad source: nothing appears, source is not created.
	bad := filepath.Join(dir, "missing.db")
	dest2 := filepath.Join(dir, "b2.db")
	if _, err := Backup(ctx, bad, dest2, false); err == nil {
		t.Fatal("want error")
	}
	if _, err := os.Stat(bad); err == nil {
		t.Fatal("source was created")
	}
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if e.Name() != "src.db" && e.Name() != "b.db" && !strings.HasPrefix(e.Name(), "src.db-") {
			t.Fatalf("stray file %s", e.Name())
		}
	}

	// Non-SQLite source.
	junk := filepath.Join(dir, "junk.db")
	os.WriteFile(junk, []byte(strings.Repeat("not sqlite ", 100)), 0o644)
	if _, err := Backup(ctx, junk, dest2, false); err == nil {
		t.Fatal("want error")
	}
	if _, err := os.Stat(dest2); err == nil {
		t.Fatal("partial backup under dest")
	}
}

func TestRestoreRefusals(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.db")
	makeDB(t, src, 3)
	good := filepath.Join(dir, "good.bak")
	if _, err := Backup(ctx, src, good, false); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "live.db")

	// Lock held.
	l, err := dblock.Acquire(target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(ctx, good, target, true); err == nil || !strings.Contains(err.Error(), "in use") {
		t.Fatalf("want in-use error, got %v", err)
	}
	l.Close()

	// Corrupt / non-SQLite.
	junk := filepath.Join(dir, "junk.bak")
	os.WriteFile(junk, []byte(strings.Repeat("garbage ", 500)), 0o644)
	if _, err := Restore(ctx, junk, target, true); err == nil {
		t.Fatal("want error for junk")
	}
	if _, err := os.Stat(target); err == nil {
		t.Fatal("target created by failed restore")
	}

	// Too-new schema.
	newer := filepath.Join(dir, "newer.bak")
	data, _ := os.ReadFile(good)
	os.WriteFile(newer, data, 0o644)
	db, err := sql.Open("sqlite", newer)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO schema_migrations (version, applied_at) VALUES (?, 'x')`, store.MaxSchemaVersion()+1); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, err := Restore(ctx, newer, target, true); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("want schema error, got %v", err)
	}

	// No schema_migrations.
	plain := filepath.Join(dir, "plain.bak")
	db, _ = sql.Open("sqlite", plain)
	db.Exec(`CREATE TABLE t (a)`)
	db.Close()
	if _, err := Restore(ctx, plain, target, true); err == nil {
		t.Fatal("want error for non-mostlydone db")
	}

	// Existing target without force.
	makeDB(t, target, 5)
	if _, err := Restore(ctx, good, target, false); err == nil || !strings.Contains(err.Error(), "-force") {
		t.Fatalf("want force error, got %v", err)
	}
	if got := count(t, target, "issues"); got != 5 {
		t.Fatalf("target modified: %d", got)
	}

	// With force: old DB moved aside, intact and valid.
	info, err := Restore(ctx, good, target, true)
	if err != nil {
		t.Fatal(err)
	}
	if info.Aside == "" || !strings.Contains(info.Aside, ".pre-restore-") || !strings.HasSuffix(info.Aside, ".bak") {
		t.Fatalf("aside %q", info.Aside)
	}
	if got := count(t, info.Aside, "issues"); got != 5 {
		t.Fatalf("aside has %d issues, want 5", got)
	}
	if got := count(t, target, "issues"); got != 3 {
		t.Fatalf("restored has %d issues, want 3", got)
	}
	for _, suf := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(target + suf); err == nil {
			t.Fatalf("stale %s remains", suf)
		}
	}
}
