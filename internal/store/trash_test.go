package store

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// clock is an injectable, adjustable test clock.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func withClock(f *fixture) *clock {
	c := &clock{t: time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)}
	f.s.now = c.now
	return c
}

func count(t *testing.T, s *Store, q string, args ...any) int {
	t.Helper()
	var n int
	if err := s.r.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestTrashHidesProject(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	withClock(f)
	i := f.issue(t, IssueInput{Name: Some("one")})
	child := f.issue(t, IssueInput{Name: Some("two"), Parent: Some(&i.ID)})

	if err := f.s.DeleteProject(ctx, f.ws.ID, f.p.ID); err != nil {
		t.Fatal(err)
	}
	if kind(f.s.DeleteProject(ctx, f.ws.ID, f.p.ID)) != KindNotFound {
		t.Fatal("second delete should be not found")
	}
	if kind(f.s.DeleteProject(ctx, f.ws.ID, "nope")) != KindNotFound {
		t.Fatal("unknown delete should be not found")
	}
	if _, err := f.s.GetProject(ctx, f.ws.ID, f.p.ID); kind(err) != KindNotFound {
		t.Fatalf("GetProject: %v", err)
	}
	if l, _ := f.s.ListProjects(ctx, f.ws.ID, ""); len(l) != 0 {
		t.Fatal("ListProjects shows trashed project")
	}
	for _, ref := range []string{f.p.ID, "WEB", "web", "Web"} {
		if _, err := f.s.ProjectByRef(ctx, f.ws.ID, ref); kind(err) != KindNotFound {
			t.Fatalf("ProjectByRef(%s): %v", ref, err)
		}
	}
	if m, _ := f.s.ProjectsByIDs(ctx, []string{f.p.ID}); len(m) != 0 {
		t.Fatal("ProjectsByIDs shows trashed project")
	}
	if _, err := f.s.IssueByIdentifier(ctx, f.ws.ID, "WEB", 1); kind(err) != KindNotFound {
		t.Fatalf("IssueByIdentifier: %v", err)
	}
	if hits, _ := f.s.SearchIssues(ctx, f.ws.ID, "one", "", 10); len(hits) != 0 {
		t.Fatal("SearchIssues shows trashed items")
	}
	if m, _ := f.s.IssuesByIDs(ctx, []string{i.ID, child.ID}); len(m) != 0 {
		t.Fatal("IssuesByIDs shows trashed items")
	}
	if _, err := f.s.GetIssue(ctx, f.p.ID, i.ID); kind(err) != KindNotFound {
		t.Fatalf("GetIssue: %v", err)
	}
	if _, _, err := f.s.ListIssues(ctx, IssueFilter{ProjectID: f.p.ID}); err != nil {
		t.Fatal(err)
	}

	tr, err := f.s.ListTrashedProjects(ctx, f.ws.ID)
	if err != nil || len(tr) != 1 || tr[0].ID != f.p.ID || tr[0].DeletedAt == nil {
		t.Fatalf("trashed = %v, %v", tr, err)
	}
	if got, want := f.s.PurgeAt(*tr[0].DeletedAt), "2026-03-31T12:00:00.000000Z"; got != want {
		t.Fatalf("PurgeAt = %s, want %s", got, want)
	}
}

func TestTrashOrderNewestFirst(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	c := withClock(f)
	p2, err := f.s.CreateProject(ctx, f.ws.ID, f.admin.ID, ProjectInput{Name: Some("Api"), Identifier: Some("API")})
	if err != nil {
		t.Fatal(err)
	}
	f.s.DeleteProject(ctx, f.ws.ID, f.p.ID)
	c.add(time.Hour)
	f.s.DeleteProject(ctx, f.ws.ID, p2.ID)
	tr, _ := f.s.ListTrashedProjects(ctx, f.ws.ID)
	if len(tr) != 2 || tr[0].ID != p2.ID || tr[1].ID != f.p.ID {
		t.Fatalf("order = %v", tr)
	}
	// Another workspace's trash is not visible.
	if tr, _ := f.s.ListTrashedProjects(ctx, "other"); len(tr) != 0 {
		t.Fatal("cross-workspace trash")
	}
	if _, err := f.s.RestoreProject(ctx, "other", p2.ID); kind(err) != KindNotFound {
		t.Fatalf("cross-workspace restore: %v", err)
	}
	if kind(f.s.PurgeProject(ctx, "other", p2.ID)) != KindNotFound {
		t.Fatal("cross-workspace purge")
	}
}

func TestRestoreKeepsEverything(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	c := withClock(f)
	f.issue(t, IssueInput{Name: Some("one")})
	f.issue(t, IssueInput{Name: Some("two")})
	f.s.DeleteProject(ctx, f.ws.ID, f.p.ID)

	if _, err := f.s.RestoreProject(ctx, f.ws.ID, "nope"); kind(err) != KindNotFound {
		t.Fatalf("restore unknown: %v", err)
	}
	c.add(24 * time.Hour)
	p, err := f.s.RestoreProject(ctx, f.ws.ID, f.p.ID)
	if err != nil || p.DeletedAt != nil {
		t.Fatalf("restore = %v, %v", p, err)
	}
	if _, err := f.s.RestoreProject(ctx, f.ws.ID, f.p.ID); kind(err) != KindNotFound {
		t.Fatal("restoring a live project should be not found")
	}
	if _, err := f.s.IssueByIdentifier(ctx, f.ws.ID, "WEB", 2); err != nil {
		t.Fatal(err)
	}
	if hits, _ := f.s.SearchIssues(ctx, f.ws.ID, "one", "", 10); len(hits) != 1 {
		t.Fatalf("search after restore = %v", hits)
	}
	// Sequence counter continues where it left off.
	if n := f.issue(t, IssueInput{Name: Some("three")}); n.SequenceID != 3 {
		t.Fatalf("sequence after restore = %d", n.SequenceID)
	}
	if tr, _ := f.s.ListTrashedProjects(ctx, f.ws.ID); len(tr) != 0 {
		t.Fatal("restored project still in trash")
	}
}

func TestRestoreAfterRetention(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	c := withClock(f)
	f.s.DeleteProject(ctx, f.ws.ID, f.p.ID)
	c.add(30*24*time.Hour + time.Second)
	if _, err := f.s.RestoreProject(ctx, f.ws.ID, f.p.ID); kind(err) != KindNotFound {
		t.Fatalf("restore after retention: %v", err)
	}
	if tr, _ := f.s.ListTrashedProjects(ctx, f.ws.ID); len(tr) != 0 {
		t.Fatal("expired project still listed")
	}
}

func TestPurgeExpiredTiming(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	c := withClock(f)
	f.issue(t, IssueInput{Name: Some("one")})
	f.s.DeleteProject(ctx, f.ws.ID, f.p.ID)

	c.add(29*24*time.Hour + 23*time.Hour)
	if n, err := f.s.PurgeExpiredProjects(ctx); err != nil || n != 0 {
		t.Fatalf("purged %d, %v at 29d23h", n, err)
	}
	c.add(time.Hour + time.Second) // 30d + 1s
	if n, err := f.s.PurgeExpiredProjects(ctx); err != nil || n != 1 {
		t.Fatalf("purged %d, %v at 30d+1s", n, err)
	}
	if n, _ := f.s.PurgeExpiredProjects(ctx); n != 0 {
		t.Fatalf("second purge removed %d", n)
	}
	// A live project is never purged however old.
	c.add(365 * 24 * time.Hour)
	p2, _ := f.s.CreateProject(ctx, f.ws.ID, f.admin.ID, ProjectInput{Name: Some("Api"), Identifier: Some("API")})
	c.add(365 * 24 * time.Hour)
	if n, _ := f.s.PurgeExpiredProjects(ctx); n != 0 {
		t.Fatal("purged a live project")
	}
	if _, err := f.s.GetProject(ctx, f.ws.ID, p2.ID); err != nil {
		t.Fatal(err)
	}
}

func TestPurgeCascades(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	c := withClock(f)
	i := f.issue(t, IssueInput{Name: Some("one")})
	if _, err := f.s.CreateComment(ctx, i, f.admin.ID, CommentInput{CommentHTML: Some("<p>hi</p>")}); err != nil {
		t.Fatal(err)
	}
	pid := f.p.ID
	tables := map[string]string{
		"issues":     `SELECT COUNT(*) FROM issues WHERE project_id = ?`,
		"states":     `SELECT COUNT(*) FROM states WHERE project_id = ?`,
		"comments":   `SELECT COUNT(*) FROM comments WHERE project_id = ?`,
		"activities": `SELECT COUNT(*) FROM activities WHERE project_id = ?`,
	}
	for name, q := range tables {
		if count(t, f.s, q, pid) == 0 {
			t.Fatalf("setup: no %s rows", name)
		}
	}
	f.s.DeleteProject(ctx, f.ws.ID, pid)
	// Soft delete keeps rows.
	for name, q := range tables {
		if count(t, f.s, q, pid) == 0 {
			t.Fatalf("%s rows gone after trash", name)
		}
	}
	c.add(31 * 24 * time.Hour)
	if n, _ := f.s.PurgeExpiredProjects(ctx); n != 1 {
		t.Fatalf("purged %d", n)
	}
	for name, q := range tables {
		if n := count(t, f.s, q, pid); n != 0 {
			t.Fatalf("%s rows left after purge: %d", name, n)
		}
	}
}

func TestPurgeProjectImmediate(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	withClock(f)
	f.issue(t, IssueInput{Name: Some("one")})
	if kind(f.s.PurgeProject(ctx, f.ws.ID, f.p.ID)) != KindNotFound {
		t.Fatal("purging a live project must be refused")
	}
	f.s.DeleteProject(ctx, f.ws.ID, f.p.ID)
	if err := f.s.PurgeProject(ctx, f.ws.ID, f.p.ID); err != nil {
		t.Fatal(err)
	}
	if n := count(t, f.s, `SELECT COUNT(*) FROM projects`); n != 0 {
		t.Fatalf("projects left: %d", n)
	}
	if n := count(t, f.s, `SELECT COUNT(*) FROM issues`); n != 0 {
		t.Fatalf("issues left: %d", n)
	}
	if _, err := f.s.RestoreProject(ctx, f.ws.ID, f.p.ID); kind(err) != KindNotFound {
		t.Fatal("restore after purge")
	}
	// Name and identifier are free again.
	if _, err := f.s.CreateProject(ctx, f.ws.ID, f.admin.ID, ProjectInput{Name: Some("Web"), Identifier: Some("WEB")}); err != nil {
		t.Fatal(err)
	}
}

func TestTrashHoldsIdentifierAndName(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	withClock(f)
	f.s.DeleteProject(ctx, f.ws.ID, f.p.ID)

	const trashMsg = "is used by a project in the trash; restore it or delete it permanently"
	for _, in := range []ProjectInput{
		{Name: Some("Other"), Identifier: Some("WEB")},
		{Name: Some("Web"), Identifier: Some("OTHER")},
	} {
		_, err := f.s.CreateProject(ctx, f.ws.ID, f.admin.ID, in)
		e, _ := err.(*Error)
		if e == nil || e.Kind != KindConflict || !strings.Contains(e.Msg, trashMsg) {
			t.Fatalf("create %+v: %v", in, err)
		}
	}
	// Live collisions keep the old message.
	f.s.RestoreProject(ctx, f.ws.ID, f.p.ID)
	_, err := f.s.CreateProject(ctx, f.ws.ID, f.admin.ID, ProjectInput{Name: Some("Other"), Identifier: Some("WEB")})
	if e, _ := err.(*Error); e == nil || e.Msg != "The project identifier is already taken" {
		t.Fatalf("live collision: %v", err)
	}
}

func TestRestoreGuardsLiveCollision(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	withClock(f)
	// The unique index makes a live/trashed clash unreachable through the
	// public API, so exercise the guard RestoreProject uses directly: a
	// different project claiming the live project's identifier or name.
	for _, p := range []*Project{
		{ID: "x", WorkspaceID: f.ws.ID, Name: "Other", Identifier: f.p.Identifier},
		{ID: "x", WorkspaceID: f.ws.ID, Name: f.p.Name, Identifier: "OTHER"},
	} {
		if err := checkProjectTaken(ctx, f.s.r, p); kind(err) != KindConflict {
			t.Fatalf("guard %+v: %v", p, err)
		}
	}
	if err := checkProjectTaken(ctx, f.s.r, f.p); err != nil {
		t.Fatalf("a project does not collide with itself: %v", err)
	}
}

func TestRunTrashPurge(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	c := withClock(f)
	f.s.DeleteProject(ctx, f.ws.ID, f.p.ID)
	c.add(31 * 24 * time.Hour) // already expired at startup

	var mu sync.Mutex
	var logs []string
	logf := func(format string, args ...any) {
		mu.Lock()
		logs = append(logs, format)
		mu.Unlock()
	}
	rctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { f.s.RunTrashPurge(rctx, 5*time.Millisecond, logf); close(done) }()

	waitFor(t, func() bool { return count(t, f.s, `SELECT COUNT(*) FROM projects`) == 0 })
	mu.Lock()
	if len(logs) != 1 {
		t.Fatalf("logs = %v", logs)
	}
	mu.Unlock()

	// The loop keeps running: a project trashed and expired later is purged
	// by a tick, not just at startup.
	p2, _ := f.s.CreateProject(ctx, f.ws.ID, f.admin.ID, ProjectInput{Name: Some("Api"), Identifier: Some("API")})
	f.s.DeleteProject(ctx, f.ws.ID, p2.ID)
	c.add(31 * 24 * time.Hour)
	waitFor(t, func() bool { return count(t, f.s, `SELECT COUNT(*) FROM projects`) == 0 })

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RunTrashPurge did not stop on cancel")
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(2 * time.Millisecond)
	}
}
