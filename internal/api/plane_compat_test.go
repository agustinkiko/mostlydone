package api

// Plane v1 compatibility suite (MDQA-003).
//
// Table-driven checks against docs/plane-api-reference.md for the subset
// Mostlydone implements: paths, methods, status codes, key response fields,
// and the pagination envelope. It talks only to the in-process handler via
// httptest — no Plane source, no network, no official SDK.
//
// Known gaps are listed in TestPlaneV1CompatExpectedGaps so the suite stays
// green on main while documenting what is not implemented.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
)

const planeWS = "/api/v1/workspaces/main"

func envelopeKeys(t *testing.T, page map[string]any) {
	t.Helper()
	for _, k := range []string{
		"grouped_by", "sub_grouped_by", "extra_stats",
		"total_count", "total_results", "total_pages", "count",
		"next_cursor", "prev_cursor", "next_page_results", "prev_page_results",
		"results",
	} {
		if _, ok := page[k]; !ok {
			t.Fatalf("pagination envelope missing %q; keys=%v", k, keysOf(page))
		}
	}
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func hasFields(t *testing.T, obj map[string]any, want ...string) {
	t.Helper()
	var missing []string
	for _, k := range want {
		if _, ok := obj[k]; !ok {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("missing fields %v; have %v", missing, keysOf(obj))
	}
}

func asObject(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("want object, got %T %v", v, v)
	}
	return m
}

func asList(t *testing.T, v any) []any {
	t.Helper()
	s, ok := v.([]any)
	if !ok {
		t.Fatalf("want list, got %T %v", v, v)
	}
	return s
}

// TestPlaneV1Compat covers implemented Plane v1 endpoints.
func TestPlaneV1Compat(t *testing.T) {
	h, _, api := boot(t)

	t.Run("auth/X-API-Key and current user", func(t *testing.T) {
		me := api.must(200, "GET", "/api/v1/users/me/", nil)
		hasFields(t, me, "id", "first_name", "last_name", "email", "avatar", "avatar_url", "display_name")
		// Missing key is 401 with Plane-docs detail (source Plane may 403; we follow docs).
		anon := &client{t: t, h: h}
		got := anon.must(401, "GET", "/api/v1/users/me/", nil)
		eq(t, "missing key", got["detail"], "Authentication credentials were not provided.")
		bad := &client{t: t, h: h, key: "trk_not_a_real_token"}
		got = bad.must(401, "GET", planeWS+"/projects/", nil)
		eq(t, "bad key", got["detail"], "Given API token is not valid")
	})

	var pid, identifier string
	t.Run("projects CRUD", func(t *testing.T) {
		created := api.must(201, "POST", planeWS+"/projects/", map[string]any{
			"name": "Compat Project", "identifier": "CMP",
		})
		hasFields(t, created, "id", "name", "identifier", "workspace", "description",
			"cycle_view", "module_view", "intake_view", "created_at", "updated_at",
			"deleted_at", "is_member", "member_role")
		eq(t, "identifier", created["identifier"], "CMP")
		pid = created["id"].(string)
		identifier = created["identifier"].(string)

		listed := api.must(200, "GET", planeWS+"/projects/", nil)
		envelopeKeys(t, listed)
		if listed["total_count"].(float64) < 1 {
			t.Fatalf("expected at least one project, got %v", listed)
		}

		got := api.must(200, "GET", planeWS+"/projects/"+pid+"/", nil)
		eq(t, "get id", got["id"], pid)

		patched := api.must(200, "PATCH", planeWS+"/projects/"+pid+"/", map[string]any{
			"description": "updated",
		})
		eq(t, "patched description", patched["description"], "updated")
	})

	var stateID, labelID, issueID string
	var seq float64

	t.Run("states", func(t *testing.T) {
		page := api.must(200, "GET", planeWS+"/projects/"+pid+"/states/", nil)
		envelopeKeys(t, page)
		names := map[string]bool{}
		for _, row := range asList(t, page["results"]) {
			s := asObject(t, row)
			hasFields(t, s, "id", "name", "color", "group", "default", "sequence", "project", "workspace")
			names[s["name"].(string)] = true
		}
		for _, n := range []string{"Backlog", "Todo", "In Progress", "Done", "Cancelled"} {
			if !names[n] {
				t.Fatalf("default state %q missing; have %v", n, names)
			}
		}
		if names["Triage"] {
			t.Fatal("Triage should be hidden from the API list")
		}
		// Plane quirk: state create returns 200, not 201.
		created := api.must(200, "POST", planeWS+"/projects/"+pid+"/states/", map[string]any{
			"name": "Review", "color": "#123456", "group": "started",
		})
		hasFields(t, created, "id", "name", "color", "group", "default")
		eq(t, "state name", created["name"], "Review")
		stateID = created["id"].(string)
		got := api.must(200, "GET", planeWS+"/projects/"+pid+"/states/"+stateID+"/", nil)
		eq(t, "get state", got["id"], stateID)
		api.must(200, "PATCH", planeWS+"/projects/"+pid+"/states/"+stateID+"/", map[string]any{
			"name": "In Review",
		})
	})

	t.Run("labels", func(t *testing.T) {
		created := api.must(201, "POST", planeWS+"/projects/"+pid+"/labels/", map[string]any{
			"name": "bug", "color": "#ff0000",
		})
		hasFields(t, created, "id", "name", "color", "project", "workspace")
		labelID = created["id"].(string)
		page := api.must(200, "GET", planeWS+"/projects/"+pid+"/labels/", nil)
		envelopeKeys(t, page)
		got := api.must(200, "GET", planeWS+"/projects/"+pid+"/labels/"+labelID+"/", nil)
		eq(t, "get label", got["id"], labelID)
		api.must(200, "PATCH", planeWS+"/projects/"+pid+"/labels/"+labelID+"/", map[string]any{
			"name": "defect",
		})
	})

	t.Run("work items CRUD and issues alias", func(t *testing.T) {
		created := api.must(201, "POST", planeWS+"/projects/"+pid+"/work-items/", map[string]any{
			"name": "First item", "priority": "high", "labels": []string{labelID},
		})
		hasFields(t, created, "id", "name", "description_html", "priority", "state",
			"sequence_id", "assignees", "labels", "project", "workspace",
			"start_date", "target_date", "created_at", "deleted_at")
		eq(t, "name", created["name"], "First item")
		eq(t, "priority", created["priority"], "high")
		issueID = created["id"].(string)
		seq = created["sequence_id"].(float64)

		got := api.must(200, "GET", planeWS+"/projects/"+pid+"/work-items/"+issueID+"/", nil)
		eq(t, "get issue", got["id"], issueID)

		patched := api.must(200, "PATCH", planeWS+"/projects/"+pid+"/work-items/"+issueID+"/", map[string]any{
			"name": "First item (renamed)",
		})
		eq(t, "renamed", patched["name"], "First item (renamed)")

		// Deprecated /issues/ spelling must hit the same handlers.
		alias := api.must(200, "GET", planeWS+"/projects/"+pid+"/issues/"+issueID+"/", nil)
		eq(t, "issues alias", alias["id"], issueID)

		byIdent := api.must(200, "GET", planeWS+"/work-items/"+identifier+"-1/", nil)
		eq(t, "by identifier", byIdent["id"], issueID)
		eq(t, "seq", byIdent["sequence_id"], seq)

		search := api.must(200, "GET", planeWS+"/work-items/search/?search=First", nil)
		if _, ok := search["issues"]; !ok {
			t.Fatalf("search missing issues key: %v", search)
		}
	})

	t.Run("pagination envelope", func(t *testing.T) {
		for _, name := range []string{"Item B", "Item C", "Item D"} {
			api.must(201, "POST", planeWS+"/projects/"+pid+"/work-items/", map[string]any{"name": name})
		}
		page := api.must(200, "GET", planeWS+"/projects/"+pid+"/work-items/?per_page=2&order_by=sequence_id", nil)
		envelopeKeys(t, page)
		eq(t, "count", page["count"], 2.0)
		if page["total_count"].(float64) < 4 {
			t.Fatalf("total_count=%v, want >= 4", page["total_count"])
		}
		eq(t, "next_cursor", page["next_cursor"], "2:1:0")
		eq(t, "prev_cursor", page["prev_cursor"], "2:-1:1") // Plane source, not docs' empty string
		eq(t, "next_page_results", page["next_page_results"], true)
		eq(t, "prev_page_results", page["prev_page_results"], false)

		page2 := api.must(200, "GET", planeWS+"/projects/"+pid+"/work-items/?per_page=2&order_by=sequence_id&cursor=2:1:0", nil)
		envelopeKeys(t, page2)
		eq(t, "page2 prev_cursor", page2["prev_cursor"], "2:0:1")
		eq(t, "page2 prev_page_results", page2["prev_page_results"], true)

		bad := api.must(400, "GET", planeWS+"/projects/"+pid+"/work-items/?cursor=bad", nil)
		eq(t, "bad cursor", bad["detail"], "Invalid cursor parameter.")
		big := api.must(400, "GET", planeWS+"/projects/"+pid+"/work-items/?per_page=1001", nil)
		if !strings.Contains(fmtString(big["detail"]), "1000") {
			t.Fatalf("per_page 1001: %v", big)
		}
	})

	t.Run("comments, links, activities", func(t *testing.T) {
		base := planeWS + "/projects/" + pid + "/work-items/" + issueID
		c := api.must(201, "POST", base+"/comments/", map[string]any{
			"comment_html": "<p>hello</p>",
		})
		hasFields(t, c, "id", "comment_html", "actor", "issue", "project", "workspace")
		cid := c["id"].(string)
		api.must(200, "GET", base+"/comments/"+cid+"/", nil)
		page := api.must(200, "GET", base+"/comments/", nil)
		envelopeKeys(t, page)
		api.must(200, "PATCH", base+"/comments/"+cid+"/", map[string]any{
			"comment_html": "<p>hello edited</p>",
		})

		badLink := api.must(400, "POST", base+"/links/", map[string]any{"url": "javascript:alert(1)"})
		if _, ok := badLink["url"]; !ok {
			t.Fatalf("javascript: link should 400 on url, got %v", badLink)
		}
		lk := api.must(201, "POST", base+"/links/", map[string]any{
			"url": "https://example.com/spec", "title": "Spec",
		})
		hasFields(t, lk, "id", "url", "title", "issue", "project", "workspace")
		lid := lk["id"].(string)
		api.must(200, "GET", base+"/links/"+lid+"/", nil)
		envelopeKeys(t, api.must(200, "GET", base+"/links/", nil))

		acts := api.must(200, "GET", base+"/activities/", nil)
		envelopeKeys(t, acts)
		if acts["count"].(float64) < 1 {
			t.Fatalf("expected at least one activity, got %v", acts)
		}
	})

	var cycleID, moduleID string
	t.Run("cycles and modules", func(t *testing.T) {
		cy := api.must(201, "POST", planeWS+"/projects/"+pid+"/cycles/", map[string]any{"name": "Sprint 1"})
		hasFields(t, cy, "id", "name", "project", "workspace", "total_issues")
		cycleID = cy["id"].(string)
		api.must(200, "GET", planeWS+"/projects/"+pid+"/cycles/"+cycleID+"/", nil)
		envelopeKeys(t, api.must(200, "GET", planeWS+"/projects/"+pid+"/cycles/", nil))
		code, added := api.do("POST", planeWS+"/projects/"+pid+"/cycles/"+cycleID+"/cycle-issues/", map[string]any{
			"issues": []string{issueID},
		})
		eq(t, "add cycle issues status", code, 200)
		if _, ok := added.([]any); !ok {
			t.Fatalf("cycle-issues add should be a plain array of CycleIssue rows, got %T %v", added, added)
		}
		// List is paginated work-item objects (Plane).
		envelopeKeys(t, api.must(200, "GET", planeWS+"/projects/"+pid+"/cycles/"+cycleID+"/cycle-issues/", nil))

		mod := api.must(201, "POST", planeWS+"/projects/"+pid+"/modules/", map[string]any{"name": "Mod A"})
		hasFields(t, mod, "id", "name", "project", "workspace")
		moduleID = mod["id"].(string)
		api.must(200, "GET", planeWS+"/projects/"+pid+"/modules/"+moduleID+"/", nil)
		envelopeKeys(t, api.must(200, "GET", planeWS+"/projects/"+pid+"/modules/", nil))
		code, added = api.do("POST", planeWS+"/projects/"+pid+"/modules/"+moduleID+"/module-issues/", map[string]any{
			"issues": []string{issueID},
		})
		eq(t, "add module issues status", code, 200)
		if _, ok := added.([]any); !ok {
			t.Fatalf("module-issues add should be a plain array, got %T %v", added, added)
		}
		envelopeKeys(t, api.must(200, "GET", planeWS+"/projects/"+pid+"/modules/"+moduleID+"/module-issues/", nil))
	})

	t.Run("members", func(t *testing.T) {
		// Workspace members: Plane returns a plain array, not a pagination envelope.
		code, out := api.do("GET", planeWS+"/members/", nil)
		if code != 200 {
			t.Fatalf("workspace members: %d %v", code, out)
		}
		list := asList(t, out)
		if len(list) < 1 {
			t.Fatalf("expected at least the admin, got %v", out)
		}
		hasFields(t, asObject(t, list[0]), "id", "email", "display_name", "role")

		pm := api.must(200, "GET", planeWS+"/projects/"+pid+"/members/", nil)
		// Implementation returns a plain array (Plane: plain array of UserLite).
		// must() already decoded; do() for the raw shape.
		code, out = api.do("GET", planeWS+"/projects/"+pid+"/members/", nil)
		if code != 200 {
			t.Fatalf("project members: %d %v", code, out)
		}
		_ = pm
		if _, ok := out.([]any); !ok {
			t.Fatalf("project members should be a plain array, got %T", out)
		}
	})

	t.Run("error shapes", func(t *testing.T) {
		missing := api.must(404, "GET", planeWS+"/projects/"+pid+"/work-items/00000000-0000-0000-0000-000000000000/", nil)
		eq(t, "missing issue", missing["error"], "The requested resource does not exist.")
		ws := api.must(404, "GET", "/api/v1/workspaces/does-not-exist/projects/", nil)
		if ws["error"] == nil {
			t.Fatalf("unknown workspace: %v", ws)
		}
		req := api.must(400, "POST", planeWS+"/projects/", map[string]any{"identifier": "X"})
		if _, ok := req["name"]; !ok {
			t.Fatalf("missing name should be a field error, got %v", req)
		}
	})

	// Deletes last so earlier GETs still work. 204 with empty body.
	t.Run("deletes return 204", func(t *testing.T) {
		base := planeWS + "/projects/" + pid + "/work-items/" + issueID
		// link / comment already exist; delete the work item (admin or creator).
		code, _ := api.do("DELETE", planeWS+"/projects/"+pid+"/modules/"+moduleID+"/", nil)
		eq(t, "delete module", code, 204)
		code, _ = api.do("DELETE", planeWS+"/projects/"+pid+"/cycles/"+cycleID+"/", nil)
		eq(t, "delete cycle", code, 204)
		code, _ = api.do("DELETE", base+"/", nil)
		eq(t, "delete work item", code, 204)
		api.must(404, "GET", base+"/", nil)
		code, _ = api.do("DELETE", planeWS+"/projects/"+pid+"/labels/"+labelID+"/", nil)
		eq(t, "delete label", code, 204)
		code, _ = api.do("DELETE", planeWS+"/projects/"+pid+"/", nil)
		eq(t, "delete project", code, 204)
		api.must(404, "GET", planeWS+"/projects/"+pid+"/", nil)
		_ = stateID
	})
}

func fmtString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// TestPlaneV1CompatExpectedGaps documents Plane v1 routes and behaviors that
// docs/plane-api-reference.md describes but Mostlydone intentionally does not
// implement (see SPEC.md). Each case either expects a 404/405 from the mux or
// is skipped with a reason when there is no request we can fairly send.
func TestPlaneV1CompatExpectedGaps(t *testing.T) {
	h, _, api := boot(t)
	// A project so path-prefixed gaps are well-formed.
	p := api.must(201, "POST", planeWS+"/projects/", map[string]any{"name": "Gap", "identifier": "GAP"})
	pid := p["id"].(string)
	cy := api.must(201, "POST", planeWS+"/projects/"+pid+"/cycles/", map[string]any{"name": "C"})
	cid := cy["id"].(string)

	type gap struct {
		method, path, reason string
	}
	gaps := []gap{
		{"POST", planeWS + "/projects/" + pid + "/archive/", "project archive is out of scope (SPEC)"},
		{"DELETE", planeWS + "/projects/" + pid + "/archive/", "project unarchive is out of scope (SPEC)"},
		{"GET", planeWS + "/projects/" + pid + "/summary/", "project summary is out of scope (SPEC)"},
		{"GET", planeWS + "/projects/" + pid + "/archived-cycles/", "archived cycles are out of scope (SPEC)"},
		{"POST", planeWS + "/projects/" + pid + "/cycles/" + cid + "/transfer-issues/", "cycle transfer is out of scope (SPEC)"},
		{"POST", planeWS + "/projects/" + pid + "/cycles/" + cid + "/archive/", "cycle archive is out of scope (SPEC)"},
		{"GET", planeWS + "/projects/" + pid + "/archived-modules/", "archived modules are out of scope (SPEC)"},
		{"GET", planeWS + "/members-lite/", "members-lite is out of scope (SPEC)"},
		{"GET", planeWS + "/projects/" + pid + "/project-members-lite/", "project-members-lite is out of scope (SPEC)"},
	}

	t.Logf("expected Plane v1 gaps (%d):", len(gaps))
	for _, g := range gaps {
		t.Logf("  %s %s — %s", g.method, g.path, g.reason)
	}

	for _, g := range gaps {
		g := g
		t.Run(g.method+" "+g.path, func(t *testing.T) {
			var rd *strings.Reader
			if g.method == "GET" || g.method == "DELETE" {
				rd = strings.NewReader("")
			} else {
				rd = strings.NewReader(`{"issues":[],"new_cycle_id":"x"}`)
			}
			r := httptest.NewRequest(g.method, g.path, rd)
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-API-Key", api.key)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != http.StatusNotFound && w.Code != http.StatusMethodNotAllowed {
				t.Fatalf("%s %s: got %d, want 404 or 405 (gap: %s); body %q",
					g.method, g.path, w.Code, g.reason, w.Body.String())
			}
		})
	}

	// Behaviors that are not a missing route, so we Skip with a reason.
	t.Run("rate-limit headers", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/api/v1/users/me/", nil)
		r.Header.Set("X-API-Key", api.key)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Header().Get("X-RateLimit-Remaining") != "" || w.Header().Get("X-RateLimit-Reset") != "" {
			t.Fatalf("rate-limit headers unexpectedly present")
		}
		t.Skip("no API rate limit or X-RateLimit-* headers (SPEC; Plane docs: 60/min per key)")
	})
	t.Run("soft deletes", func(t *testing.T) {
		t.Skip("DELETE is a hard delete; Plane sets deleted_at (see MDQA-001 M2)")
	})
	t.Run("attachments", func(t *testing.T) {
		t.Skip("work-item attachments are out of scope (SPEC / reference not implemented)")
	})
	t.Run("oauth bearer", func(t *testing.T) {
		t.Skip("OAuth access tokens are out of scope; API keys and session cookies only")
	})
}

func TestPlaneV1CompatTrailingSlash(t *testing.T) {
	_, _, api := boot(t)
	// Implementation accepts Plane paths without the trailing slash.
	api.must(200, "GET", "/api/v1/users/me", nil)
}

func exactKeys(t *testing.T, what string, obj map[string]any, want ...string) {
	t.Helper()
	have := keysOf(obj)
	sort.Strings(have)
	w := append([]string(nil), want...)
	sort.Strings(w)
	if strings.Join(have, ",") != strings.Join(w, ",") {
		t.Fatalf("%s keys = %v, want %v", what, have, w)
	}
}

func names(t *testing.T, page map[string]any) string {
	t.Helper()
	var out string
	for _, x := range asList(t, page["results"]) {
		out += asObject(t, x)["name"].(string)
	}
	return out
}

func TestPlaneV1CompatLite(t *testing.T) {
	h, _, api := boot(t)
	anon := &client{t: t, h: h}
	pa := api.must(201, "POST", planeWS+"/projects/", map[string]any{"name": "Alpha", "identifier": "ALP"})
	time.Sleep(5 * time.Millisecond)
	pb := api.must(201, "POST", planeWS+"/projects/", map[string]any{"name": "Beta", "identifier": "BET"})
	pid := pa["id"].(string)
	base := planeWS + "/projects/" + pid

	counts := []string{"total_issues", "completed_issues", "cancelled_issues", "started_issues", "unstarted_issues", "backlog_issues"}
	// liteKeys is the full object's key set minus the metric keys.
	liteKeys := func(full map[string]any) []string {
		var want []string
		for k := range full {
			if !slices.Contains(counts, k) {
				want = append(want, k)
			}
		}
		return want
	}
	noCounts := func(what string, o map[string]any) {
		t.Helper()
		for _, k := range counts {
			if _, ok := o[k]; ok {
				t.Fatalf("%s has metric key %q", what, k)
			}
		}
	}
	orders := func(kind string, want map[string]string) {
		t.Helper()
		for q, w := range want {
			eq(t, kind+" order_by="+q, names(t, api.must(200, "GET", base+"/"+kind+"-lite/?order_by="+q, nil)), w)
		}
	}

	t.Run("projects-lite", func(t *testing.T) {
		page := api.must(200, "GET", planeWS+"/projects-lite/?include_archived=true", nil)
		envelopeKeys(t, page)
		items := asList(t, page["results"])
		if len(items) != 2 {
			t.Fatalf("got %d projects", len(items))
		}
		for _, x := range items {
			exactKeys(t, "ProjectLite", asObject(t, x), "id", "identifier", "name", "cover_image", "icon_prop",
				"emoji", "description", "cover_image_url", "archived_at")
		}
		first := asObject(t, items[0])
		eq(t, "default order -created_at", first["id"], pb["id"])
		if first["archived_at"] != nil || first["cover_image"] != nil || first["cover_image_url"] != nil {
			t.Fatalf("null fields not null: %v", first)
		}
		// Alpha was created before Beta.
		for q, w := range map[string]string{"": "BetaAlpha", "name": "AlphaBeta", "-name": "BetaAlpha", "created_at": "AlphaBeta", "-created_at": "BetaAlpha"} {
			eq(t, "projects order_by="+q, names(t, api.must(200, "GET", planeWS+"/projects-lite/?order_by="+q, nil)), w)
		}
		pg := api.must(200, "GET", planeWS+"/projects-lite/?per_page=1", nil)
		eq(t, "next_page_results", pg["next_page_results"], true)
		eq(t, "count", pg["count"], float64(1))
		api.must(404, "GET", "/api/v1/workspaces/nope/projects-lite/", nil)
		anon.must(401, "GET", planeWS+"/projects-lite/", nil)
	})

	t.Run("cycles-lite", func(t *testing.T) {
		for _, n := range []string{"B", "A", "C"} {
			api.must(201, "POST", base+"/cycles/", map[string]any{"name": n})
			time.Sleep(5 * time.Millisecond)
		}
		full := asObject(t, asList(t, api.must(200, "GET", base+"/cycles/", nil)["results"])[0])
		page := api.must(200, "GET", base+"/cycles-lite/", nil)
		envelopeKeys(t, page)
		item := asObject(t, asList(t, page["results"])[0])
		noCounts("cycle-lite", item)
		exactKeys(t, "cycle-lite", item, liteKeys(full)...)
		eq(t, "default", names(t, page), "CAB")
		orders("cycles", map[string]string{"name": "ABC", "-name": "CBA", "created_at": "BAC", "bogus": "CAB"})
		eq(t, "next", api.must(200, "GET", base+"/cycles-lite/?per_page=1", nil)["next_page_results"], true)
		api.must(404, "GET", planeWS+"/projects/nope/cycles-lite/", nil)
		api.must(404, "GET", "/api/v1/workspaces/nope/projects/"+pid+"/cycles-lite/", nil)
		anon.must(401, "GET", base+"/cycles-lite/", nil)
	})

	t.Run("modules-lite", func(t *testing.T) {
		for _, n := range []string{"B", "A", "C"} {
			api.must(201, "POST", base+"/modules/", map[string]any{"name": n})
			time.Sleep(5 * time.Millisecond)
		}
		full := asObject(t, asList(t, api.must(200, "GET", base+"/modules/", nil)["results"])[0])
		page := api.must(200, "GET", base+"/modules-lite/", nil)
		envelopeKeys(t, page)
		item := asObject(t, asList(t, page["results"])[0])
		noCounts("module-lite", item)
		exactKeys(t, "module-lite", item, liteKeys(full)...)
		eq(t, "default", names(t, page), "CAB")
		orders("modules", map[string]string{"name": "ABC", "-name": "CBA", "created_at": "BAC", "bogus": "CAB"})
		eq(t, "next", api.must(200, "GET", base+"/modules-lite/?per_page=1", nil)["next_page_results"], true)
		api.must(404, "GET", planeWS+"/projects/nope/modules-lite/", nil)
		api.must(404, "GET", "/api/v1/workspaces/nope/projects/"+pid+"/modules-lite/", nil)
		anon.must(401, "GET", base+"/modules-lite/", nil)
	})
}

// Activity epoch must be an integer on the wire (the official Python SDK
// validates it as int), though the reference lists float.
func TestPlaneV1CompatActivityEpochInt(t *testing.T) {
	h, _, api := boot(t)
	p := api.must(201, "POST", planeWS+"/projects/", map[string]any{"name": "Ep", "identifier": "EPC"})
	pid := p["id"].(string)
	iss := api.must(201, "POST", planeWS+"/projects/"+pid+"/work-items/", map[string]any{"name": "x"})
	url := planeWS + "/projects/" + pid + "/work-items/" + iss["id"].(string) + "/activities/"

	get := func(u string) []byte {
		t.Helper()
		r := httptest.NewRequest("GET", u, nil)
		r.Header.Set("X-API-Key", api.key)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", u, w.Code, w.Body)
		}
		return w.Body.Bytes()
	}
	epochOK := func(a map[string]any) {
		t.Helper()
		n, ok := a["epoch"].(json.Number)
		if !ok {
			t.Fatalf("epoch is %T, want number", a["epoch"])
		}
		if strings.ContainsAny(n.String(), ".eE") {
			t.Fatalf("epoch %q is not an integer", n)
		}
	}
	decode := func(raw []byte, v any) {
		t.Helper()
		d := json.NewDecoder(bytes.NewReader(raw))
		d.UseNumber()
		if err := d.Decode(v); err != nil {
			t.Fatal(err)
		}
	}

	var page struct {
		Results []map[string]any `json:"results"`
	}
	decode(get(url), &page)
	if len(page.Results) == 0 {
		t.Fatal("no activities")
	}
	for _, a := range page.Results {
		epochOK(a)
	}
	var one map[string]any
	decode(get(url+page.Results[0]["id"].(string)+"/"), &one)
	epochOK(one)
}
