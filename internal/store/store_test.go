package store

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"sync"
	"testing"
)

type fixture struct {
	s     *Store
	ws    *Workspace
	admin *User
	p     *Project
}

func setup(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	u, err := s.Setup(ctx, "main", "admin@example.com", "password123", "Ada Admin")
	if err != nil {
		t.Fatal(err)
	}
	ws, err := s.Workspace(ctx, "main")
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.CreateProject(ctx, ws.ID, u.ID, ProjectInput{Name: Some("Web"), Identifier: Some("web")})
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{s: s, ws: ws, admin: u, p: p}
}

func kind(err error) Kind {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind
	}
	return 0
}

func (f *fixture) issue(t *testing.T, in IssueInput) *Issue {
	t.Helper()
	i, err := f.s.CreateIssue(context.Background(), f.p, f.admin.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	return i
}

func (f *fixture) stateByGroup(t *testing.T, group string) *State {
	t.Helper()
	list, err := f.s.ListStates(context.Background(), f.p.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range list {
		if s.Group == group {
			return s
		}
	}
	t.Fatalf("no %s state", group)
	return nil
}

func TestMigrationsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.db")
	for range 2 {
		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		var n int
		if err := s.r.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil || n != 2 {
			t.Fatalf("schema_migrations rows = %d, %v", n, err)
		}
		var mode string
		s.r.QueryRow(`PRAGMA journal_mode`).Scan(&mode)
		if mode != "wal" {
			t.Fatalf("journal_mode = %q", mode)
		}
		s.Close()
	}
}

func TestDefaultStates(t *testing.T) {
	f := setup(t)
	list, err := f.s.ListStates(context.Background(), f.p.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Backlog", "Todo", "In Progress", "Done", "Cancelled"}
	if len(list) != len(want) {
		t.Fatalf("got %d states", len(list))
	}
	defaults := 0
	for k, s := range list {
		if s.Name != want[k] {
			t.Errorf("state %d = %s, want %s", k, s.Name, want[k])
		}
		if s.Default {
			defaults++
		}
	}
	if defaults != 1 || !list[0].Default {
		t.Errorf("want only Backlog default")
	}
	// A new default clears the old one.
	ctx := context.Background()
	st, err := f.s.CreateState(ctx, f.p.ID, f.admin.ID, StateInput{Name: Some("Review"), Color: Some("#fff"), Group: Some("started"), Default: Some(true)})
	if err != nil {
		t.Fatal(err)
	}
	if st.Sequence != 55000+15000 {
		t.Errorf("sequence = %v", st.Sequence)
	}
	i := f.issue(t, IssueInput{Name: Some("x")})
	if i.StateID != st.ID {
		t.Errorf("new issue did not get the new default state")
	}
	if err := f.s.DeleteState(ctx, f.p.ID, st.ID); kind(err) != KindInvalid {
		t.Errorf("deleting default state: %v", err)
	}
	if _, err := f.s.CreateState(ctx, f.p.ID, f.admin.ID, StateInput{Name: Some("Review"), Color: Some("#fff")}); kind(err) != KindConflict {
		t.Errorf("duplicate state name: %v", err)
	}
}

func TestSequenceIDsConcurrent(t *testing.T) {
	f := setup(t)
	const n = 40
	var wg sync.WaitGroup
	ids := make([]int, n)
	errs := make([]error, n)
	for k := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			i, err := f.s.CreateIssue(context.Background(), f.p, f.admin.ID, IssueInput{Name: Some("c")})
			errs[k] = err
			if err == nil {
				ids[k] = i.SequenceID
			}
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	sort.Ints(ids)
	for k, id := range ids {
		if id != k+1 {
			t.Fatalf("sequence ids not 1..%d: %v", n, ids)
		}
	}
}

func TestCompletedAt(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	done := f.stateByGroup(t, "completed")
	todo := f.stateByGroup(t, "unstarted")

	i := f.issue(t, IssueInput{Name: Some("a"), State: Some(&done.ID)})
	if i.CompletedAt == nil {
		t.Fatal("created in completed state without completed_at")
	}
	i, err := f.s.UpdateIssue(ctx, f.p, i.ID, f.admin.ID, IssueInput{State: Some(&todo.ID)})
	if err != nil || i.CompletedAt != nil {
		t.Fatalf("moving out of completed: %v %v", i.CompletedAt, err)
	}
	i, _ = f.s.UpdateIssue(ctx, f.p, i.ID, f.admin.ID, IssueInput{State: Some(&done.ID)})
	if i.CompletedAt == nil {
		t.Fatal("moving into completed did not set completed_at")
	}
	first := *i.CompletedAt
	i, _ = f.s.UpdateIssue(ctx, f.p, i.ID, f.admin.ID, IssueInput{Name: Some("renamed")})
	if i.CompletedAt == nil || *i.CompletedAt != first {
		t.Fatal("unrelated update touched completed_at")
	}
}

func TestCycleExclusive(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	c1, err := f.s.CreateCycle(ctx, f.p, f.admin.ID, CycleInput{Name: Some("C1")})
	if err != nil {
		t.Fatal(err)
	}
	c2, _ := f.s.CreateCycle(ctx, f.p, f.admin.ID, CycleInput{Name: Some("C2")})
	i := f.issue(t, IssueInput{Name: Some("a"), CycleID: Some(&c1.ID)})
	if i.CycleID == nil || *i.CycleID != c1.ID {
		t.Fatal("cycle_id not set on create")
	}
	rows, err := f.s.AddCycleIssues(ctx, f.p, c2.ID, f.admin.ID, []string{i.ID, "not-an-issue"})
	if err != nil || len(rows) != 1 {
		t.Fatalf("add to c2: %v rows=%d", err, len(rows))
	}
	got, _ := f.s.GetIssue(ctx, f.p.ID, i.ID)
	if *got.CycleID != c2.ID {
		t.Fatal("issue not moved to c2")
	}
	c1, _ = f.s.GetCycle(ctx, f.p.ID, c1.ID)
	c2, _ = f.s.GetCycle(ctx, f.p.ID, c2.ID)
	if c1.Counts.Total != 0 || c2.Counts.Total != 1 || c2.Counts.Backlog != 1 {
		t.Fatalf("counts c1=%+v c2=%+v", c1.Counts, c2.Counts)
	}
	if _, err := f.s.AddCycleIssues(ctx, f.p, c2.ID, f.admin.ID, nil); kind(err) != KindInvalid {
		t.Fatalf("empty add: %v", err)
	}
	_, err = f.s.CreateCycle(ctx, f.p, f.admin.ID, CycleInput{Name: Some("bad"), StartDate: Some(Ptr("2026-01-02"))})
	if kind(err) != KindInvalid {
		t.Fatalf("one date only: %v", err)
	}
	c3, err := f.s.CreateCycle(ctx, f.p, f.admin.ID, CycleInput{Name: Some("dt"), StartDate: Some(Ptr("2026-01-02T10:00:00Z")), EndDate: Some(Ptr("2026-01-09"))})
	if err != nil || *c3.StartDate != "2026-01-02" {
		t.Fatalf("datetime start: %v", err)
	}
	if _, err := f.s.AddCycleIssues(ctx, f.p, c3.ID, f.admin.ID, []string{i.ID}); kind(err) != KindInvalid {
		t.Fatalf("add to ended cycle: %v", err)
	}
	if err := f.s.RemoveCycleIssue(ctx, f.p, c2.ID, i.ID, f.admin.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = f.s.GetIssue(ctx, f.p.ID, i.ID)
	if got.CycleID != nil {
		t.Fatal("remove did not clear cycle")
	}
}

func TestModulesManyToMany(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	m1, err := f.s.CreateModule(ctx, f.p, f.admin.ID, ModuleInput{Name: Some("M1"), Members: Some([]string{f.admin.ID, "stranger"})})
	if err != nil {
		t.Fatal(err)
	}
	if len(m1.Members) != 1 {
		t.Fatalf("non-members not dropped: %v", m1.Members)
	}
	m2, _ := f.s.CreateModule(ctx, f.p, f.admin.ID, ModuleInput{Name: Some("M2")})
	if _, err := f.s.CreateModule(ctx, f.p, f.admin.ID, ModuleInput{Name: Some("M2")}); kind(err) != KindInvalid {
		t.Fatalf("dup module: %v", err)
	}
	i := f.issue(t, IssueInput{Name: Some("a"), ModuleIDs: Some([]string{m1.ID})})
	if _, err := f.s.AddModuleIssues(ctx, f.p, m2.ID, f.admin.ID, []string{i.ID}); err != nil {
		t.Fatal(err)
	}
	got, _ := f.s.GetIssue(ctx, f.p.ID, i.ID)
	if len(got.ModuleIDs) != 2 {
		t.Fatalf("module_ids = %v", got.ModuleIDs)
	}
	got, _ = f.s.UpdateIssue(ctx, f.p, i.ID, f.admin.ID, IssueInput{ModuleIDs: Some([]string{m2.ID})})
	if len(got.ModuleIDs) != 1 || got.ModuleIDs[0] != m2.ID {
		t.Fatalf("replace module_ids = %v", got.ModuleIDs)
	}
}

func TestCascadeDeletes(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	l, _ := f.s.CreateLabel(ctx, f.p.ID, f.admin.ID, LabelInput{Name: Some("bug")})
	parent := f.issue(t, IssueInput{Name: Some("parent"), Labels: Some([]string{l.ID}), Assignees: Some([]string{f.admin.ID})})
	child := f.issue(t, IssueInput{Name: Some("child"), Parent: Some(&parent.ID)})
	if _, err := f.s.CreateComment(ctx, parent, f.admin.ID, CommentInput{CommentHTML: Some("<p>hi</p>")}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.CreateLink(ctx, parent, f.admin.ID, LinkInput{URL: Some("https://example.com")}); err != nil {
		t.Fatal(err)
	}
	if err := f.s.DeleteLabel(ctx, f.p.ID, l.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := f.s.GetIssue(ctx, f.p.ID, parent.ID)
	if len(got.Labels) != 0 {
		t.Fatal("label delete did not cascade")
	}
	if err := f.s.DeleteIssue(ctx, f.p.ID, parent.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = f.s.GetIssue(ctx, f.p.ID, child.ID)
	if got.ParentID != nil {
		t.Fatal("child kept deleted parent")
	}
	for _, table := range []string{"comments", "issue_links", "issue_assignees", "issue_labels"} {
		var n int
		f.s.r.QueryRow(`SELECT COUNT(*) FROM `+table+` WHERE issue_id = ?`, parent.ID).Scan(&n)
		if n != 0 {
			t.Errorf("%s rows left: %d", table, n)
		}
	}
	// Projects go to the trash first; purging is what hard-deletes them.
	if err := f.s.DeleteProject(ctx, f.ws.ID, f.p.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.s.PurgeProject(ctx, f.ws.ID, f.p.ID); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"issues", "states", "labels", "activities"} {
		var n int
		f.s.r.QueryRow(`SELECT COUNT(*) FROM `+table+` WHERE project_id = ?`, f.p.ID).Scan(&n)
		if n != 0 {
			t.Errorf("%s rows left after project purge: %d", table, n)
		}
	}
}

func TestExternalIDConflict(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	src, id := Some(Ptr("plane")), Some(Ptr("abc"))
	a := f.issue(t, IssueInput{Name: Some("a"), ExternalSource: src, ExternalID: id})
	_, err := f.s.CreateIssue(ctx, f.p, f.admin.ID, IssueInput{Name: Some("b"), ExternalSource: src, ExternalID: id})
	var e *Error
	if !errors.As(err, &e) || e.Kind != KindConflict || e.ID != a.ID {
		t.Fatalf("want 409 with id, got %v", err)
	}
	b := f.issue(t, IssueInput{Name: Some("b")})
	if _, err := f.s.UpdateIssue(ctx, f.p, b.ID, f.admin.ID, IssueInput{ExternalSource: src, ExternalID: id}); kind(err) != KindConflict {
		t.Fatalf("patch to used pair: %v", err)
	}
	if _, err := f.s.UpdateIssue(ctx, f.p, a.ID, f.admin.ID, IssueInput{ExternalSource: src, ExternalID: id}); err != nil {
		t.Fatalf("patch to own pair: %v", err)
	}
	if _, err := f.s.CreateLabel(ctx, f.p.ID, f.admin.ID, LabelInput{Name: Some("x"), ExternalSource: src, ExternalID: id}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.CreateLabel(ctx, f.p.ID, f.admin.ID, LabelInput{Name: Some("y"), ExternalSource: src, ExternalID: id}); kind(err) != KindConflict {
		t.Fatalf("label dup pair: %v", err)
	}
	if _, err := f.s.CreateProject(ctx, f.ws.ID, f.admin.ID, ProjectInput{Name: Some("Other"), Identifier: Some("WEB")}); kind(err) != KindConflict {
		t.Fatalf("dup identifier: %v", err)
	}
}

func TestActivitiesWritten(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	i := f.issue(t, IssueInput{Name: Some("a")})
	done := f.stateByGroup(t, "completed")
	if _, err := f.s.UpdateIssue(ctx, f.p, i.ID, f.admin.ID, IssueInput{Name: Some("b"), State: Some(&done.ID), Priority: Some("high"),
		Assignees: Some([]string{f.admin.ID})}); err != nil {
		t.Fatal(err)
	}
	f.s.CreateComment(ctx, i, f.admin.ID, CommentInput{CommentHTML: Some("x")})
	list, err := f.s.ListActivities(ctx, i.ID, false, false)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, a := range list {
		got = append(got, a.Verb+":"+deref(a.Field))
	}
	want := []string{"created:", "updated:state", "updated:name", "updated:priority", "updated:assignees"}
	if len(got) != len(want) {
		t.Fatalf("activities = %v", got)
	}
	for k := range want {
		if got[k] != want[k] {
			t.Fatalf("activities = %v, want %v", got, want)
		}
	}
}

func TestIssueValidation(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	cases := map[string]IssueInput{
		"blank name":     {Name: Some("  ")},
		"bad priority":   {Name: Some("a"), Priority: Some("whenever")},
		"foreign state":  {Name: Some("a"), State: Some(Ptr("nope"))},
		"bad date":       {Name: Some("a"), StartDate: Some(Ptr("01/02/2026"))},
		"start > target": {Name: Some("a"), StartDate: Some(Ptr("2026-02-01")), TargetDate: Some(Ptr("2026-01-01"))},
		"bad assignee":   {Name: Some("a"), Assignees: Some([]string{"ghost"})},
		"bad label":      {Name: Some("a"), Labels: Some([]string{"ghost"})},
	}
	for name, in := range cases {
		if _, err := f.s.CreateIssue(ctx, f.p, f.admin.ID, in); kind(err) != KindInvalid {
			t.Errorf("%s: got %v", name, err)
		}
	}
	a := f.issue(t, IssueInput{Name: Some("a")})
	b := f.issue(t, IssueInput{Name: Some("b"), Parent: Some(&a.ID)})
	if _, err := f.s.UpdateIssue(ctx, f.p, a.ID, f.admin.ID, IssueInput{Parent: Some(&b.ID)}); kind(err) != KindInvalid {
		t.Errorf("parent loop accepted: %v", err)
	}
}

func TestListFilters(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	l, _ := f.s.CreateLabel(ctx, f.p.ID, f.admin.ID, LabelInput{Name: Some("bug")})
	f.issue(t, IssueInput{Name: Some("alpha"), Priority: Some("urgent"), Labels: Some([]string{l.ID})})
	f.issue(t, IssueInput{Name: Some("beta"), Priority: Some("low"), Assignees: Some([]string{f.admin.ID})})
	f.issue(t, IssueInput{Name: Some("gamma 100%")})
	count := func(fl IssueFilter) int {
		fl.ProjectID = f.p.ID
		list, total, err := f.s.ListIssues(ctx, fl)
		if err != nil {
			t.Fatal(err)
		}
		if total != len(list) {
			t.Fatalf("total %d != len %d", total, len(list))
		}
		return total
	}
	checks := []struct {
		name string
		f    IssueFilter
		want int
	}{
		{"all", IssueFilter{}, 3},
		{"priority", IssueFilter{Priorities: []string{"urgent", "low"}}, 2},
		{"label", IssueFilter{Labels: []string{l.ID}}, 1},
		{"assignee", IssueFilter{Assignees: []string{f.admin.ID}}, 1},
		{"group", IssueFilter{StateGroups: []string{"backlog"}}, 3},
		{"search", IssueFilter{Search: "alp"}, 1},
		{"search seq", IssueFilter{Search: "WEB-2"}, 1},
		{"search percent literal", IssueFilter{Search: "100%"}, 1},
	}
	for _, c := range checks {
		if got := count(c.f); got != c.want {
			t.Errorf("%s: got %d want %d", c.name, got, c.want)
		}
	}
	list, _, _ := f.s.ListIssues(ctx, IssueFilter{ProjectID: f.p.ID, OrderBy: "priority"})
	if list[0].Priority != "urgent" || list[2].Priority != "none" {
		t.Errorf("priority order wrong: %s %s %s", list[0].Priority, list[1].Priority, list[2].Priority)
	}
}

func TestAuthTokens(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	if _, err := f.s.Setup(ctx, "main", "b@example.com", "password123", ""); kind(err) != KindForbidden {
		t.Fatalf("second setup: %v", err)
	}
	if _, err := f.s.Authenticate(ctx, "ADMIN@example.com", "password123"); err != nil {
		t.Fatalf("login (case-insensitive email): %v", err)
	}
	if _, err := f.s.Authenticate(ctx, "admin@example.com", "wrong"); !errors.Is(err, ErrBadLogin) {
		t.Fatal("wrong password accepted")
	}
	tok, secret, err := f.s.CreateToken(ctx, f.admin.ID, "ci")
	if err != nil {
		t.Fatal(err)
	}
	if u, err := f.s.TokenUser(ctx, secret); err != nil || u.ID != f.admin.ID {
		t.Fatalf("token lookup: %v", err)
	}
	var stored []byte
	f.s.r.QueryRow(`SELECT token_hash FROM api_tokens WHERE id = ?`, tok.ID).Scan(&stored)
	if string(stored) == secret || len(stored) != 32 {
		t.Fatal("token not stored as sha-256")
	}
	f.s.DeleteToken(ctx, f.admin.ID, tok.ID)
	if _, err := f.s.TokenUser(ctx, secret); !errors.Is(err, ErrNotFound) {
		t.Fatal("revoked token still works")
	}
}

func TestUserWorkspace(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	ws, role, err := f.s.UserWorkspace(ctx, f.admin.ID)
	if err != nil || ws.ID != f.ws.ID || role != RoleAdmin {
		t.Fatalf("admin: ws=%v role=%d err=%v", ws, role, err)
	}
	if _, _, err := f.s.UserWorkspace(ctx, "no-such-user"); kind(err) != KindNotFound {
		t.Fatalf("unknown user: %v", err)
	}
}
