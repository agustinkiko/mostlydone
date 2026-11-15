package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"mostlydone/internal/store"
)

// client drives the handler in-process (the sandbox forbids real ports).
type client struct {
	t      *testing.T
	h      http.Handler
	cookie string
	key    string
	csrf   bool
}

func newHandler(t *testing.T) (http.Handler, *store.Store) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	a := New(s, "main", false)
	a.SetSetupToken(testSetupToken, "")
	return a.Handler(), s
}

const testSetupToken = "setup_test_token"

func (c *client) do(method, path string, body any) (int, any) {
	c.t.Helper()
	var rd *bytes.Reader
	switch b := body.(type) {
	case nil:
		rd = bytes.NewReader(nil)
	case string:
		rd = bytes.NewReader([]byte(b))
	default:
		raw, _ := json.Marshal(b)
		rd = bytes.NewReader(raw)
	}
	r := httptest.NewRequest(method, path, rd)
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	if c.cookie != "" {
		r.AddCookie(&http.Cookie{Name: cookieName, Value: c.cookie})
	}
	if c.key != "" {
		r.Header.Set("X-API-Key", c.key)
	}
	if c.csrf {
		r.Header.Set(csrfHeader, csrfValue)
	}
	w := httptest.NewRecorder()
	c.h.ServeHTTP(w, r)
	for _, ck := range w.Result().Cookies() {
		if ck.Name == cookieName {
			c.cookie = ck.Value
		}
	}
	var out any
	if w.Body.Len() > 0 {
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			c.t.Fatalf("%s %s: non-JSON body %q", method, path, w.Body.String())
		}
	}
	return w.Code, out
}

// setupAdmin completes first-run setup with the armed test setup token.
func (c *client) setupAdmin(email, password, displayName string) {
	c.t.Helper()
	c.must(201, "POST", "/auth/setup", map[string]any{"email": email, "password": password, "display_name": displayName, "setup_token": testSetupToken})
}

// must asserts the status and returns the body as an object.
func (c *client) must(status int, method, path string, body any) map[string]any {
	c.t.Helper()
	code, out := c.do(method, path, body)
	if code != status {
		c.t.Fatalf("%s %s: status %d, want %d, body %v", method, path, code, status, out)
	}
	m, _ := out.(map[string]any)
	return m
}

func (c *client) list(method, path string, body any) []any {
	c.t.Helper()
	code, out := c.do(method, path, body)
	if code != 200 {
		c.t.Fatalf("%s %s: status %d, body %v", method, path, code, out)
	}
	if m, ok := out.(map[string]any); ok {
		return m["results"].([]any)
	}
	return out.([]any)
}

func ids(list []any) []string {
	var out []string
	for _, x := range list {
		out = append(out, x.(map[string]any)["id"].(string))
	}
	sort.Strings(out)
	return out
}

func sorted(s ...string) []string { sort.Strings(s); return s }

func eq(t *testing.T, what string, got, want any) {
	t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("%s: got %v, want %v", what, got, want)
	}
}

// boot runs first-run setup and returns a session client and a token client.
func boot(t *testing.T) (http.Handler, *client, *client) {
	h, _ := newHandler(t)
	browser := &client{t: t, h: h}
	me := browser.must(200, "GET", "/auth/me", nil)
	eq(t, "setup_required", me["setup_required"], true)
	eq(t, "user before setup", me["user"], nil)

	browser.must(403, "POST", "/auth/setup", map[string]any{"email": "ada@example.com", "password": "password123", "display_name": "Ada L", "setup_token": testSetupToken})
	browser.csrf = true
	browser.must(400, "POST", "/auth/setup", map[string]any{"email": "ada@example.com", "password": "short", "setup_token": testSetupToken})
	browser.must(201, "POST", "/auth/setup", map[string]any{"email": "ada@example.com", "password": "password123", "display_name": "Ada L", "setup_token": testSetupToken})
	browser.must(403, "POST", "/auth/setup", map[string]any{"email": "b@example.com", "password": "password123", "setup_token": testSetupToken})

	browser.cookie = ""
	browser.must(401, "POST", "/auth/login", map[string]any{"email": "ada@example.com", "password": "wrong-password"})
	browser.must(200, "POST", "/auth/login", map[string]any{"email": "ada@example.com", "password": "password123"})
	me = browser.must(200, "GET", "/auth/me", nil)
	eq(t, "setup_required", me["setup_required"], false)
	eq(t, "workspace_slug", me["workspace_slug"], "main")
	eq(t, "role", me["role"], 20)

	tok := browser.must(201, "POST", "/auth/tokens", map[string]any{"label": "ci"})
	secret := tok["token"].(string)
	if len(browser.list("GET", "/auth/tokens", nil)) != 1 {
		t.Fatal("token not listed")
	}
	api := &client{t: t, h: h, key: secret}
	// Token management is session-only.
	api.must(403, "GET", "/auth/tokens", nil)
	return h, browser, api
}

const W = "/api/v1/workspaces/main"

func TestEndToEnd(t *testing.T) {
	h, browser, api := boot(t)
	me := api.must(200, "GET", "/api/v1/users/me/", nil)
	adaID := me["id"].(string)
	eq(t, "me email", me["email"], "ada@example.com")

	// Projects.
	api.must(400, "POST", W+"/projects/", map[string]any{"identifier": "X"})
	api.must(400, "POST", W+"/projects/", map[string]any{"name": "Bad.Name", "identifier": "BAD"})
	p := api.must(201, "POST", W+"/projects/", map[string]any{"name": "Web", "identifier": "web", "network": 0, "estimate_point": 3})
	eq(t, "identifier", p["identifier"], "WEB")
	eq(t, "cycle_view", p["cycle_view"], true)
	eq(t, "network ignored", p["network"], 2)
	dup := api.must(409, "POST", W+"/projects/", map[string]any{"name": "Web2", "identifier": "WEB"})
	eq(t, "dup identifier", dup["identifier"], "The project identifier is already taken")
	PJ := W + "/projects/" + p["id"].(string)
	api.must(200, "PATCH", PJ+"/", map[string]any{"description": "site"})
	eq(t, "projects", len(api.list("GET", W+"/projects/", nil)), 1)
	// No trailing slash is accepted too.
	api.must(200, "GET", PJ, nil)

	// States and labels.
	states := api.list("GET", PJ+"/states/", nil)
	eq(t, "default states", len(states), 5)
	stateID := map[string]string{}
	for _, s := range states {
		m := s.(map[string]any)
		stateID[m["group"].(string)] = m["id"].(string)
	}
	review := api.must(200, "POST", PJ+"/states/", map[string]any{"name": "Review", "color": "#123456", "group": "started"})
	eq(t, "state dup 409", api.must(409, "POST", PJ+"/states/", map[string]any{"name": "Review", "color": "#000000"})["id"], review["id"])
	api.must(400, "POST", PJ+"/states/", map[string]any{"name": "T", "color": "#000000", "group": "triage"})
	bug := api.must(201, "POST", PJ+"/labels/", map[string]any{"name": "bug", "color": "#ff0000"})
	feat := api.must(201, "POST", PJ+"/labels/", map[string]any{"name": "feature"})
	api.must(409, "POST", PJ+"/labels/", map[string]any{"name": "bug"})
	bugID, featID := bug["id"].(string), feat["id"].(string)

	// Work items.
	WI := PJ + "/work-items/"
	a := api.must(201, "POST", WI, map[string]any{"name": "Login page", "priority": "high", "labels": []string{bugID},
		"assignees": []string{adaID}, "description_html": `<p>hi<script>x()</script></p>`})
	eq(t, "sequence_id", a["sequence_id"], 1)
	eq(t, "sanitized", a["description_html"], "<p>hi</p>")
	eq(t, "default state", a["state"], stateID["backlog"])
	eq(t, "assignees", a["assignees"], []string{adaID})
	eq(t, "cycle_id", a["cycle_id"], nil)
	eq(t, "module_ids", a["module_ids"], []string{})
	aID := a["id"].(string)
	b := api.must(201, "POST", PJ+"/issues/", map[string]any{"name": "Signup page", "parent": aID, "labels": []string{featID},
		"state": stateID["started"], "external_source": "plane", "external_id": "77"})
	bID := b["id"].(string)
	conflict := api.must(409, "POST", WI, map[string]any{"name": "dup", "external_source": "plane", "external_id": "77"})
	eq(t, "409 id", conflict["id"], bID)
	c := api.must(201, "POST", WI, map[string]any{"name": "Docs", "priority": "low"})
	cID := c["id"].(string)

	// Validation and not-found shapes.
	bad := api.must(400, "POST", WI, map[string]any{"name": "x", "priority": "soon"})
	if _, ok := bad["priority"].([]any); !ok {
		t.Fatalf("field error shape: %v", bad)
	}
	bad = api.must(400, "POST", WI, map[string]any{"name": "x", "state": "00000000-0000-4000-8000-000000000000"})
	eq(t, "non_field_errors", bad["non_field_errors"], []string{"State is not valid please pass a valid state_id"})
	api.must(400, "POST", WI, map[string]any{"name": 5})
	eq(t, "body not object", api.must(400, "POST", WI, `[1]`)["detail"], "Request body must be a JSON object or array.")
	nf := api.must(404, "GET", WI+"00000000-0000-4000-8000-000000000000/", nil)
	eq(t, "404 body", nf["error"], "The requested resource does not exist.")
	api.must(404, "GET", W+"/projects/nope/states/", nil)
	api.must(404, "GET", "/api/v1/workspaces/other/projects/", nil)

	// PATCH: completed_at, sort_order, parent.
	done := api.must(200, "PATCH", WI+aID+"/", map[string]any{"state": stateID["completed"], "sort_order": 1.5})
	if done["completed_at"] == nil {
		t.Fatal("completed_at not set")
	}
	eq(t, "sort_order", done["sort_order"], 1.5)
	undone := api.must(200, "PATCH", WI+aID+"/", map[string]any{"state": stateID["unstarted"]})
	eq(t, "completed_at cleared", undone["completed_at"], nil)
	eq(t, "sub_issues_count", api.must(200, "GET", WI+aID+"/", nil)["sub_issues_count"], 1)

	// Cycles and cycle-issues.
	cy := api.must(201, "POST", PJ+"/cycles/", map[string]any{"name": "Sprint 1", "start_date": "2099-01-01", "end_date": "2099-01-14"})
	eq(t, "cycle start", cy["start_date"], "2099-01-01T00:00:00.000000Z")
	eq(t, "cycle end", cy["end_date"], "2099-01-14T23:59:59.000000Z")
	cyID := cy["id"].(string)
	api.must(400, "POST", PJ+"/cycles/", map[string]any{"name": "half", "start_date": "2099-01-01"})
	rows := api.list("POST", PJ+"/cycles/"+cyID+"/cycle-issues/", map[string]any{"issues": []string{aID, bID}})
	eq(t, "cycle rows", len(rows), 2)
	eq(t, "empty add", api.must(400, "POST", PJ+"/cycles/"+cyID+"/cycle-issues/", map[string]any{"issues": []string{}})["code"], "MISSING_WORK_ITEMS")
	api.must(200, "GET", PJ+"/cycles/"+cyID+"/cycle-issues/"+aID+"/", nil)
	eq(t, "cycle issues", ids(api.list("GET", PJ+"/cycles/"+cyID+"/cycle-issues/", nil)), sorted(aID, bID))
	eq(t, "cycle total", api.must(200, "GET", PJ+"/cycles/"+cyID+"/", nil)["total_issues"], 2)
	api.must(204, "DELETE", PJ+"/cycles/"+cyID+"/cycle-issues/"+bID+"/", nil)
	api.must(404, "DELETE", PJ+"/cycles/"+cyID+"/cycle-issues/"+bID+"/", nil)
	eq(t, "cycle_id via patch", api.must(200, "PATCH", WI+cID+"/", map[string]any{"cycle_id": cyID})["cycle_id"], cyID)
	eq(t, "upcoming", len(api.list("GET", PJ+"/cycles/?cycle_view=upcoming", nil)), 1)
	eq(t, "current is array", len(api.list("GET", PJ+"/cycles/?cycle_view=current", nil)), 0)

	// Modules and module-issues.
	m1 := api.must(201, "POST", PJ+"/modules/", map[string]any{"name": "Auth", "members": []string{adaID}})
	m2 := api.must(201, "POST", PJ+"/modules/", map[string]any{"name": "Docs"})
	eq(t, "module dup code", api.must(400, "POST", PJ+"/modules/", map[string]any{"name": "Auth"})["code"], "MODULE_NAME_ALREADY_EXISTS")
	m1ID, m2ID := m1["id"].(string), m2["id"].(string)
	api.list("POST", PJ+"/modules/"+m1ID+"/module-issues/", map[string]any{"issues": []string{aID}})
	api.list("POST", PJ+"/modules/"+m2ID+"/module-issues/", map[string]any{"issues": []string{aID, cID}})
	eq(t, "many modules", len(api.must(200, "GET", WI+aID+"/", nil)["module_ids"].([]any)), 2)
	eq(t, "module issues", ids(api.list("GET", PJ+"/modules/"+m2ID+"/module-issues/", nil)), sorted(aID, cID))
	eq(t, "module_ids patch", api.must(200, "PATCH", WI+bID+"/", map[string]any{"module_ids": []string{m1ID}})["module_ids"], []string{m1ID})
	api.must(204, "DELETE", PJ+"/modules/"+m2ID+"/module-issues/"+cID+"/", nil)
	eq(t, "module members", api.must(200, "GET", PJ+"/modules/"+m1ID+"/", nil)["members"], []string{adaID})

	// List filters (extension).
	filters := []struct {
		q    string
		want []string
	}{
		{"", sorted(aID, bID, cID)},
		{"?state=" + stateID["started"], sorted(bID)},
		{"?state_group=unstarted,started", sorted(aID, bID)},
		{"?priority=high,low", sorted(aID, cID)},
		{"?assignee=" + adaID, sorted(aID)},
		{"?label=" + featID, sorted(bID)},
		{"?label=" + bugID + "," + featID, sorted(aID, bID)},
		{"?cycle=" + cyID, sorted(aID, cID)},
		{"?module=" + m1ID, sorted(aID, bID)},
		{"?parent=" + aID, sorted(bID)},
		{"?search=page", sorted(aID, bID)},
		{"?search=WEB-3", sorted(cID)},
	}
	for _, f := range filters {
		eq(t, "filter "+f.q, ids(api.list("GET", WI+f.q, nil)), f.want)
	}
	ordered := api.list("GET", WI+"?order_by=priority", nil)
	eq(t, "order_by priority", ordered[0].(map[string]any)["id"], aID)
	ordered = api.list("GET", WI+"?order_by=-sequence_id", nil)
	eq(t, "order_by -sequence_id", ordered[0].(map[string]any)["id"], cID)
	eq(t, "external lookup", api.must(200, "GET", WI+"?external_source=plane&external_id=77", nil)["id"], bID)
	api.must(404, "GET", WI+"?external_source=plane&external_id=nope", nil)
	api.must(400, "GET", WI+"?pql=x", nil)

	// By identifier, both spellings, and search.
	eq(t, "by identifier", api.must(200, "GET", W+"/work-items/WEB-2/", nil)["id"], bID)
	eq(t, "by identifier old", api.must(200, "GET", W+"/issues/web-2/", nil)["id"], bID)
	eq(t, "bad identifier", api.must(404, "GET", W+"/work-items/WEB-x/", nil)["error"], "Work item not found")
	api.must(404, "GET", W+"/work-items/WEB-99/", nil)
	hits := api.must(200, "GET", W+"/work-items/search/?search=signup", nil)["issues"].([]any)
	eq(t, "search hits", len(hits), 1)
	hit := hits[0].(map[string]any)
	eq(t, "search shape", []any{hit["project__identifier"], hit["sequence_id"], hit["workspace__slug"]}, []any{"WEB", 2, "main"})
	eq(t, "empty search", len(api.must(200, "GET", W+"/work-items/search/", nil)["issues"].([]any)), 0)

	// Comments.
	CM := WI + aID + "/comments/"
	cm := api.must(201, "POST", CM, map[string]any{"comment_html": `<p onclick="x">ok</p>`})
	eq(t, "comment sanitized", cm["comment_html"], "<p>ok</p>")
	eq(t, "comment actor", cm["actor"], adaID)
	cmID := cm["id"].(string)
	edited := api.must(200, "PATCH", CM+cmID+"/", map[string]any{"comment_html": "<p>edited</p>"})
	if edited["edited_at"] == nil {
		t.Fatal("edited_at not set")
	}
	eq(t, "comments", len(api.list("GET", CM, nil)), 1)
	api.must(200, "GET", CM+cmID+"/", nil)

	// Links.
	LK := WI + aID + "/links/"
	api.must(400, "POST", LK, map[string]any{"url": "javascript:alert(1)"})
	api.must(400, "POST", LK, map[string]any{"url": "not a url"})
	lk := api.must(201, "POST", LK, map[string]any{"url": "https://example.com/spec", "title": "Spec"})
	eq(t, "dup link", api.must(400, "POST", LK, map[string]any{"url": "https://example.com/spec"})["error"], "URL already exists for this Issue")
	api.must(200, "PATCH", LK+lk["id"].(string)+"/", map[string]any{"title": "Spec v2", "metadata": map[string]any{"k": 1}})
	eq(t, "links", len(api.list("GET", LK, nil)), 1)
	api.must(204, "DELETE", LK+lk["id"].(string)+"/", nil)

	// Activities: comments hidden, ascending by default.
	acts := api.list("GET", WI+aID+"/activities/?per_page=250", nil)
	first := acts[0].(map[string]any)
	eq(t, "first activity", first["verb"], "created")
	fields := map[string]bool{}
	for _, x := range acts {
		if f, ok := x.(map[string]any)["field"].(string); ok {
			fields[f] = true
		}
	}
	for _, f := range []string{"state", "cycles", "modules", "link"} {
		if !fields[f] {
			t.Errorf("no %s activity in %v", f, fields)
		}
	}
	if fields["comment"] {
		t.Error("comment activities should be hidden")
	}
	api.must(200, "GET", WI+aID+"/activities/"+first["id"].(string)+"/", nil)
	eq(t, "activity 404", api.must(404, "GET", WI+aID+"/activities/nope/", nil)["code"], "NOT_FOUND")

	// Pagination envelope and cursors.
	pg := api.must(200, "GET", WI+"?per_page=2&order_by=sequence_id", nil)
	eq(t, "count", pg["count"], 2)
	eq(t, "total", []any{pg["total_results"], pg["total_count"], pg["total_pages"]}, []any{3, 3, 2})
	eq(t, "cursors", []any{pg["next_cursor"], pg["prev_cursor"], pg["next_page_results"], pg["prev_page_results"]}, []any{"2:1:0", "2:-1:1", true, false})
	for _, k := range []string{"grouped_by", "sub_grouped_by", "extra_stats"} {
		if v, ok := pg[k]; !ok || v != nil {
			t.Errorf("%s = %v, %v", k, v, ok)
		}
	}
	pg = api.must(200, "GET", WI+"?per_page=2&order_by=sequence_id&cursor=2:1:0", nil)
	eq(t, "page 2", []any{pg["count"], pg["next_page_results"], pg["prev_page_results"]}, []any{1, false, true})
	eq(t, "page 2 item", pg["results"].([]any)[0].(map[string]any)["id"], cID)
	eq(t, "bad cursor", api.must(400, "GET", WI+"?cursor=abc", nil)["detail"], "Invalid cursor parameter.")
	api.must(400, "GET", WI+"?cursor=2:-1:1", nil)
	api.must(400, "GET", WI+"?per_page=0", nil)
	eq(t, "per_page cap", api.must(400, "GET", WI+"?per_page=251", nil)["detail"], "Invalid per_page value. Cannot exceed 250.")
	api.must(200, "GET", WI+"?per_page=250", nil)
	api.must(400, "GET", WI+"?cursor=300:0:0", nil)
	api.must(200, "GET", WI+"?cursor=250:0:0", nil)
	eq(t, "default per_page", strings.HasPrefix(api.must(200, "GET", WI, nil)["next_cursor"].(string), "100:"), true)
	eq(t, "labels page", len(api.list("GET", PJ+"/labels/?per_page=1", nil)), 1)

	// fields and expand.
	x := api.must(200, "GET", WI+aID+"/?fields=id,name,bogus", nil)
	eq(t, "fields", len(x), 2)
	x = api.must(200, "GET", WI+aID+"/?expand=state,assignees,labels,project,created_by,nope", nil)
	eq(t, "expand state", x["state"].(map[string]any)["group"], "unstarted")
	eq(t, "expand assignees", x["assignees"].([]any)[0].(map[string]any)["email"], "ada@example.com")
	eq(t, "expand labels", x["labels"].([]any)[0].(map[string]any)["name"], "bug")
	eq(t, "expand project", x["project"].(map[string]any)["identifier"], "WEB")
	eq(t, "expand created_by", x["created_by"].(map[string]any)["id"], adaID)
	lx := api.list("GET", WI+"?expand=parent&fields=id,parent&order_by=sequence_id", nil)
	eq(t, "expand parent", lx[1].(map[string]any)["parent"].(map[string]any)["sequence_id"], 1)
	eq(t, "null parent stays null", lx[0].(map[string]any)["parent"], nil)
	cx := api.must(200, "GET", CM+cmID+"/?expand=actor", nil)
	eq(t, "expand actor", cx["actor"].(map[string]any)["display_name"], "Ada L")

	// Members.
	browser.must(201, "POST", "/auth/users", map[string]any{"email": "gus@example.com", "password": "password123", "display_name": "Gus", "role": 5})
	wsMembers := api.list("GET", W+"/members/", nil)
	eq(t, "workspace members", len(wsMembers), 2)
	pm := api.list("GET", PJ+"/members/", nil)
	eq(t, "project members", len(pm), 2)
	if _, hasRole := pm[0].(map[string]any)["role"]; hasRole {
		t.Error("project members should be UserLite without role")
	}
	api.must(200, "GET", PJ+"/project-members/"+adaID+"/", nil)

	// Guest: read and comment only.
	gus := &client{t: t, h: h, csrf: true}
	gus.must(200, "POST", "/auth/login", map[string]any{"email": "gus@example.com", "password": "password123"})
	gus.list("GET", WI, nil)
	gus.must(403, "POST", WI, map[string]any{"name": "nope"})
	gus.must(403, "PATCH", WI+aID+"/", map[string]any{"name": "nope"})
	gus.must(403, "POST", PJ+"/labels/", map[string]any{"name": "nope"})
	gus.must(403, "GET", W+"/members/", nil)
	gc := gus.must(201, "POST", CM, map[string]any{"comment_html": "<p>guest note</p>"})
	gus.must(403, "PATCH", CM+cmID+"/", map[string]any{"comment_html": "hijack"})
	gus.must(200, "PATCH", CM+gc["id"].(string)+"/", map[string]any{"comment_html": "<p>mine</p>"})
	gus.must(403, "POST", "/auth/users", map[string]any{"email": "z@example.com", "password": "password123"})

	// Deletes.
	api.must(400, "DELETE", PJ+"/states/"+stateID["backlog"]+"/", nil)
	api.must(400, "DELETE", PJ+"/states/"+stateID["started"]+"/", nil)
	api.must(204, "DELETE", PJ+"/states/"+review["id"].(string)+"/", nil)
	api.must(204, "DELETE", CM+cmID+"/", nil)
	api.must(204, "DELETE", PJ+"/modules/"+m2ID+"/", nil)
	api.must(204, "DELETE", PJ+"/cycles/"+cyID+"/", nil)
	api.must(204, "DELETE", WI+cID+"/", nil)
	api.must(404, "GET", WI+cID+"/", nil)
	api.must(204, "DELETE", PJ+"/labels/"+featID+"/", nil)
	api.must(204, "DELETE", PJ+"/", nil)
	api.must(404, "GET", PJ+"/", nil)
}

func TestAuthFailures(t *testing.T) {
	h, browser, api := boot(t)
	anon := &client{t: t, h: h}
	eq(t, "no creds", anon.must(401, "GET", W+"/projects/", nil)["detail"], "Authentication credentials were not provided.")
	badKey := &client{t: t, h: h, key: "trk_nope"}
	eq(t, "bad key", badKey.must(401, "GET", W+"/projects/", nil)["detail"], "Given API token is not valid")
	bearer := httptest.NewRequest("GET", "/api/v1/users/me/", nil)
	bearer.Header.Set("Authorization", "Bearer "+api.key)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, bearer)
	eq(t, "bearer token", w.Code, 200)

	// CSRF: cookie writes need the header; token writes do not.
	browser.csrf = false
	eq(t, "csrf", browser.must(403, "POST", W+"/projects/", map[string]any{"name": "A", "identifier": "A"})["detail"] != nil, true)
	browser.must(200, "GET", W+"/projects/", nil)
	browser.csrf = true
	browser.must(201, "POST", W+"/projects/", map[string]any{"name": "A", "identifier": "A"})
	api.must(201, "POST", W+"/projects/", map[string]any{"name": "B", "identifier": "B"})

	// Revoked token stops working; logout ends the session.
	toks := browser.list("GET", "/auth/tokens", nil)
	browser.must(204, "DELETE", "/auth/tokens/"+toks[0].(map[string]any)["id"].(string), nil)
	api.must(401, "GET", W+"/projects/", nil)
	old := browser.cookie
	browser.must(204, "POST", "/auth/logout", nil)
	browser.cookie = old
	eq(t, "after logout", browser.must(401, "GET", W+"/projects/", nil)["detail"], "Session expired or invalid.")
	me := anon.must(200, "GET", "/auth/me", nil)
	eq(t, "anon me", me["user"], nil)

	// Oversized body.
	browser.must(200, "POST", "/auth/login", map[string]any{"email": "ada@example.com", "password": "password123"})
	big := `{"name":"` + strings.Repeat("a", maxBody) + `"}`
	browser.must(413, "POST", W+"/projects/", big)
}

func TestLoginThrottle(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	a := New(s, "main", false)
	a.SetSetupToken(testSetupToken, "")
	a.ClientIPHeader = "CF-Connecting-IP"
	now := time.Now()
	a.logins.now = func() time.Time { return now }
	h := a.Handler()

	login := func(ip, password string) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(map[string]any{"email": "ada@example.com", "password": password})
		r := httptest.NewRequest("POST", "/auth/login", bytes.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set(csrfHeader, csrfValue)
		if ip != "" {
			r.Header.Set("CF-Connecting-IP", ip)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	c := &client{t: t, h: h, csrf: true}
	c.must(201, "POST", "/auth/setup", map[string]any{"email": "ada@example.com", "password": "password123", "setup_token": testSetupToken})

	// A success clears earlier failures.
	for i := 0; i < maxLoginFailures-1; i++ {
		eq(t, "bad login", login("203.0.113.9", "wrong-password").Code, 401)
	}
	eq(t, "good login", login("203.0.113.9", "password123").Code, 200)

	for i := 0; i < maxLoginFailures; i++ {
		eq(t, "bad login", login("203.0.113.9", "wrong-password").Code, 401)
	}
	// Blocked now, even with the right password.
	w := login("203.0.113.9", "password123")
	eq(t, "blocked", w.Code, 429)
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("no Retry-After header")
	}
	// Other addresses are not affected: by header, and by socket address.
	eq(t, "other ip", login("203.0.113.10", "password123").Code, 200)
	eq(t, "no header", login("", "password123").Code, 200)

	// The block ends with the window.
	now = now.Add(loginWindow + time.Second)
	eq(t, "after window", login("203.0.113.9", "password123").Code, 200)
}
