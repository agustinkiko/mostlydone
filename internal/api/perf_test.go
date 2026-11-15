package api

import (
	"context"
	"fmt"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"mostlydone/internal/store"
)

// TestIssueList10k checks the budget line "issue list, 10k issues, 50 per
// page, under 20 ms server time". It asserts a loose 200 ms so it never
// flakes, and logs the measured median.
func TestIssueList10k(t *testing.T) {
	if testing.Short() {
		t.Skip("seeds 10k issues")
	}
	h, s := newHandler(t)
	ctx := context.Background()
	admin, err := s.Setup(ctx, "main", "ada@example.com", "password123", "Ada")
	if err != nil {
		t.Fatal(err)
	}
	ws, _ := s.Workspace(ctx, "main")
	var users []string
	users = append(users, admin.ID)
	for k := range 4 {
		u, err := s.CreateUser(ctx, ws.ID, fmt.Sprintf("u%d@example.com", k), "password123", "", store.RoleMember)
		if err != nil {
			t.Fatal(err)
		}
		users = append(users, u.ID)
	}
	p, err := s.CreateProject(ctx, ws.ID, admin.ID, store.ProjectInput{Name: store.Some("Big"), Identifier: store.Some("BIG")})
	if err != nil {
		t.Fatal(err)
	}
	var labels []string
	for k := range 8 {
		l, err := s.CreateLabel(ctx, p.ID, admin.ID, store.LabelInput{Name: store.Some(fmt.Sprintf("l%d", k))})
		if err != nil {
			t.Fatal(err)
		}
		labels = append(labels, l.ID)
	}
	start := time.Now()
	priorities := store.Priorities
	for k := range 10000 {
		_, err := s.CreateIssue(ctx, p, admin.ID, store.IssueInput{
			Name:      store.Some(fmt.Sprintf("Issue number %d", k)),
			Priority:  store.Some(priorities[k%len(priorities)]),
			Assignees: store.Some([]string{users[k%len(users)], users[(k+1)%len(users)]}),
			Labels:    store.Some([]string{labels[k%len(labels)], labels[(k+3)%len(labels)]}),
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("seeded 10000 issues in %v", time.Since(start).Round(time.Millisecond))

	_, secret, err := s.CreateToken(ctx, admin.ID, "perf")
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/workspaces/main/projects/" + p.ID + "/work-items/?per_page=50"
	run := func(query string) time.Duration {
		var times []time.Duration
		for range 21 {
			r := httptest.NewRequest("GET", path+query, nil)
			r.Header.Set("X-API-Key", secret)
			w := httptest.NewRecorder()
			t0 := time.Now()
			h.ServeHTTP(w, r)
			times = append(times, time.Since(t0))
			if w.Code != 200 {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
		}
		sort.Slice(times, func(a, b int) bool { return times[a] < times[b] })
		return times[len(times)/2]
	}
	for _, q := range []string{"", "&cursor=50:150:0", "&order_by=priority", "&priority=urgent&label=" + labels[0], "&expand=assignees,labels,state"} {
		med := run(q)
		t.Logf("list 10k issues, 50/page %-40q median %v", q, med.Round(10*time.Microsecond))
		fmt.Printf("PERF list 10k issues, 50/page %q: median %v\n", q, med.Round(10*time.Microsecond))
		if med > 200*time.Millisecond {
			t.Errorf("median %v exceeds loose 200ms bound", med)
		}
	}
}
