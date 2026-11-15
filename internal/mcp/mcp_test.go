package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"mostlydone/internal/api"
	"mostlydone/internal/store"
)

type env struct {
	s                  *store.Store
	ws                 *store.Workspace
	admin, member, gst *store.User
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	s, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	admin, err := s.Setup(ctx, "main", "ada@example.com", "password123", "Ada Admin")
	if err != nil {
		t.Fatal(err)
	}
	ws, err := s.Workspace(ctx, "main")
	if err != nil {
		t.Fatal(err)
	}
	member, err := s.CreateUser(ctx, ws.ID, "bob@example.com", "password123", "Bob Member", store.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	gst, err := s.CreateUser(ctx, ws.ID, "gus@example.com", "password123", "Gus Guest", store.RoleGuest)
	if err != nil {
		t.Fatal(err)
	}
	return &env{s, ws, admin, member, gst}
}

// connect returns a client session talking to a server that acts as u.
func (e *env) connect(t *testing.T, u *store.User) *sdk.ClientSession {
	t.Helper()
	ctx := context.Background()
	ws, role, err := e.s.UserWorkspace(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	srv := newServer(e.s, &caller{user: u, ws: ws, role: role}, sdk.NewSchemaCache())
	ct, st := sdk.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() })
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

// call runs a tool and returns its text and whether it was an error result.
func call(t *testing.T, cs *sdk.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: protocol error: %v", name, err)
	}
	var b strings.Builder
	for _, c := range res.Content {
		b.WriteString(c.(*sdk.TextContent).Text)
	}
	return b.String(), res.IsError
}

// ok runs a tool that must succeed and decodes its JSON into a generic value.
func ok(t *testing.T, cs *sdk.ClientSession, name string, args map[string]any) any {
	t.Helper()
	text, isErr := call(t, cs, name, args)
	if isErr {
		t.Fatalf("%s failed: %s", name, text)
	}
	var v any
	if err := json.Unmarshal([]byte(text), &v); err != nil {
		t.Fatalf("%s: bad JSON %q: %v", name, text, err)
	}
	return v
}

// fails runs a tool that must return a tool error containing every want.
func fails(t *testing.T, cs *sdk.ClientSession, name string, args map[string]any, want ...string) {
	t.Helper()
	text, isErr := call(t, cs, name, args)
	if !isErr {
		t.Fatalf("%s should fail, got %s", name, text)
	}
	for _, w := range want {
		if !strings.Contains(text, w) {
			t.Errorf("%s error %q lacks %q", name, text, w)
		}
	}
}

func obj(v any) map[string]any { return v.(map[string]any) }

func strs(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}

var allTools = []string{"add_comment", "create_cycle", "create_label", "create_module", "create_project", "create_work_item",
	"get_work_item", "list_cycles", "list_labels", "list_members", "list_modules", "list_projects", "list_states",
	"list_work_items", "update_work_item"}

func TestToolsList(t *testing.T) {
	e := newEnv(t)
	cs := e.connect(t, e.admin)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, tl := range res.Tools {
		got = append(got, tl.Name)
		if tl.Description == "" {
			t.Errorf("%s has no description", tl.Name)
		}
		if strings.HasPrefix(tl.Name, "list_") && (tl.Annotations == nil || !tl.Annotations.ReadOnlyHint) {
			t.Errorf("%s should be read-only", tl.Name)
		}
	}
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(allTools, ",") {
		t.Fatalf("tools = %v, want %v", got, allTools)
	}
	b, _ := json.Marshal(res)
	t.Logf("tools/list result: %d bytes", len(b))
}

func TestEndToEnd(t *testing.T) {
	e := newEnv(t)
	cs := e.connect(t, e.admin)

	p := obj(ok(t, cs, "create_project", map[string]any{"name": "Website Redesign", "identifier": "web"}))
	if p["identifier"] != "WEB" {
		t.Fatalf("project = %v", p)
	}
	if n := len(ok(t, cs, "list_projects", nil).([]any)); n != 1 {
		t.Fatalf("projects = %d", n)
	}
	states := ok(t, cs, "list_states", map[string]any{"project": "web"}).([]any)
	if len(states) != 5 || obj(states[0])["name"] != "Backlog" {
		t.Fatalf("states = %v", states)
	}

	ok(t, cs, "create_label", map[string]any{"project": "Website Redesign", "name": "Bug", "color": "#ff0000"})
	ok(t, cs, "create_label", map[string]any{"project": "WEB", "name": "UI"})
	if n := len(ok(t, cs, "list_labels", map[string]any{"project": "WEB"}).([]any)); n != 2 {
		t.Fatalf("labels = %d", n)
	}
	cy := obj(ok(t, cs, "create_cycle", map[string]any{"project": "WEB", "name": "Sprint 1", "start_date": "2026-01-05", "end_date": "2026-01-19"}))
	if cy["end_date"] != "2026-01-19" {
		t.Fatalf("cycle = %v", cy)
	}
	ok(t, cs, "create_module", map[string]any{"project": "WEB", "name": "Checkout", "lead": "bob@example.com", "members": []string{"me", "Bob Member"}})

	// create with state, labels and assignee by name; plain-text description.
	it := obj(ok(t, cs, "create_work_item", map[string]any{
		"project": "WEB", "name": "Fix login", "description": "Line one\nline two\n\nSecond paragraph",
		"state": "in progress", "priority": "HIGH", "labels": []string{"bug", "UI"}, "assignees": []string{"bob@example.com"},
		"target_date": "2026-02-01"}))
	if it["identifier"] != "WEB-1" || it["state"] != "In Progress" || it["state_group"] != "started" || it["priority"] != "high" {
		t.Fatalf("created = %v", it)
	}
	if got := strs(it["labels"]); len(got) != 2 || got[0] != "Bug" && got[1] != "Bug" {
		t.Fatalf("labels = %v", got)
	}
	if got := strs(it["assignees"]); len(got) != 1 || got[0] != "Bob Member" {
		t.Fatalf("assignees = %v", got)
	}
	if _, has := it["description"]; has {
		t.Fatal("create result should not echo the description")
	}
	// Defaults: second item lands in the default state with no noise fields.
	two := obj(ok(t, cs, "create_work_item", map[string]any{"project": "WEB", "name": "Write docs", "assignees": []string{"me"}}))
	if two["state"] != "Backlog" || two["priority"] != nil || two["labels"] != nil || two["cycle"] != nil {
		t.Fatalf("second = %v", two)
	}
	sub := obj(ok(t, cs, "create_work_item", map[string]any{"project": "WEB", "name": "Sub task", "parent": "web-1"}))
	if sub["parent"] != "WEB-1" {
		t.Fatalf("sub = %v", sub)
	}

	// list with filters
	list := func(args map[string]any) map[string]any {
		args["project"] = "WEB"
		return obj(ok(t, cs, "list_work_items", args))
	}
	if r := list(map[string]any{}); r["total"].(float64) != 3 || r["has_more"] != false {
		t.Fatalf("all = %v", r)
	}
	cases := []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"state": []string{"In Progress"}}, "WEB-1"},
		{map[string]any{"state_group": []string{"backlog"}, "assignee": []string{"me"}}, "WEB-2"},
		{map[string]any{"priority": []string{"high"}}, "WEB-1"},
		{map[string]any{"assignee": []string{"Bob Member"}}, "WEB-1"},
		{map[string]any{"label": []string{"ui"}}, "WEB-1"},
		{map[string]any{"parent": "WEB-1"}, "WEB-3"},
		{map[string]any{"search": "docs"}, "WEB-2"},
	}
	for _, c := range cases {
		r := list(c.args)
		items := r["items"].([]any)
		if len(items) != 1 || obj(items[0])["identifier"] != c.want {
			t.Errorf("filter %v: got %v, want only %s", c.args, items, c.want)
		}
	}
	// paging
	p1 := list(map[string]any{"limit": 2, "order_by": "sequence_id"})
	if p1["has_more"] != true || p1["next_cursor"] != "2" || len(p1["items"].([]any)) != 2 {
		t.Fatalf("page1 = %v", p1)
	}
	p2 := list(map[string]any{"limit": 2, "order_by": "sequence_id", "cursor": "2"})
	if p2["has_more"] != false || p2["next_cursor"] != nil || len(p2["items"].([]any)) != 1 || obj(p2["items"].([]any)[0])["identifier"] != "WEB-3" {
		t.Fatalf("page2 = %v", p2)
	}

	// update: cycle + modules + state; untouched fields stay.
	up := obj(ok(t, cs, "update_work_item", map[string]any{"item": "WEB-2", "cycle": "sprint 1", "modules": []string{"Checkout"}, "state": "Done"}))
	if up["cycle"] != "Sprint 1" || up["state_group"] != "completed" || strs(up["modules"])[0] != "Checkout" || strs(up["assignees"])[0] != "Ada Admin" {
		t.Fatalf("updated = %v", up)
	}
	if r := list(map[string]any{"cycle": []string{"Sprint 1"}, "module": []string{"checkout"}}); len(r["items"].([]any)) != 1 {
		t.Fatalf("cycle+module filter = %v", r)
	}
	// clearing: null cycle, [] modules and assignees, empty parent
	cl := obj(ok(t, cs, "update_work_item", map[string]any{"item": "WEB-2", "cycle": nil, "modules": []string{}, "assignees": []string{}}))
	if cl["cycle"] != nil || cl["modules"] != nil || cl["assignees"] != nil || cl["state"] != "Done" {
		t.Fatalf("cleared = %v", cl)
	}
	obj(ok(t, cs, "update_work_item", map[string]any{"item": "WEB-2", "cycle": "Sprint 1"}))
	if cl := obj(ok(t, cs, "update_work_item", map[string]any{"item": "WEB-2", "cycle": ""})); cl["cycle"] != nil {
		t.Fatalf("empty cycle did not clear: %v", cl)
	}
	if cl := obj(ok(t, cs, "update_work_item", map[string]any{"item": "WEB-3", "parent": ""})); cl["parent"] != nil {
		t.Fatalf("empty parent did not clear: %v", cl)
	}
	if re := obj(ok(t, cs, "update_work_item", map[string]any{"item": "WEB-3", "parent": "WEB-1"})); re["parent"] != "WEB-1" {
		t.Fatalf("parent not restored: %v", re)
	}
	// only given fields change
	ren := obj(ok(t, cs, "update_work_item", map[string]any{"item": "WEB-1", "name": "Fix login page"}))
	if ren["name"] != "Fix login page" || ren["state"] != "In Progress" || len(strs(ren["labels"])) != 2 || ren["target_date"] != "2026-02-01" {
		t.Fatalf("rename = %v", ren)
	}

	// comment, then get
	cm := obj(ok(t, cs, "add_comment", map[string]any{"item": "WEB-1", "comment": "Looks <b>good</b> & done"}))
	if cm["author"] != "Ada Admin" || !strings.HasPrefix(cm["body"].(string), "<p>") {
		t.Fatalf("comment = %v", cm)
	}
	ok(t, cs, "add_comment", map[string]any{"item": "WEB-1", "comment": "<p>second</p>"})
	got := obj(ok(t, cs, "get_work_item", map[string]any{"item": "web-1"}))
	cms := got["comments"].([]any)
	if len(cms) != 2 || obj(cms[1])["body"] != "<p>second</p>" {
		t.Fatalf("comments = %v", cms)
	}
	desc := got["description"].(string)
	if !strings.Contains(desc, "<p>Line one<br>line two</p>") || !strings.Contains(desc, "<p>Second paragraph</p>") {
		t.Fatalf("description = %q", desc)
	}
	kids := got["sub_items"].([]any)
	if len(kids) != 1 || obj(kids[0])["identifier"] != "WEB-3" || obj(kids[0])["state"] != "Backlog" || obj(kids[0])["id"] == "" {
		t.Fatalf("sub_items = %v", kids)
	}
	if got["id"] == "" || got["sub_item_count"].(float64) != 1 {
		t.Fatalf("detail fields = %v", got)
	}

	if ms := ok(t, cs, "list_members", nil).([]any); len(ms) != 3 || obj(ms[0])["role"] != "admin" {
		t.Fatalf("members = %v", ms)
	}
	if m := ok(t, cs, "list_modules", map[string]any{"project": "WEB"}).([]any); len(m) != 1 || obj(m[0])["name"] != "Checkout" {
		t.Fatalf("modules = %v", m)
	}
	if c := ok(t, cs, "list_cycles", map[string]any{"project": "WEB"}).([]any); len(c) != 1 || obj(c[0])["name"] != "Sprint 1" {
		t.Fatalf("cycles = %v", c)
	}
}

func TestReferenceErrors(t *testing.T) {
	e := newEnv(t)
	cs := e.connect(t, e.admin)
	ok(t, cs, "create_project", map[string]any{"name": "Web", "identifier": "WEB"})
	ok(t, cs, "create_project", map[string]any{"name": "Other"}) // identifier derived from the name
	ok(t, cs, "create_label", map[string]any{"project": "WEB", "name": "Bug"})
	ok(t, cs, "create_work_item", map[string]any{"project": "WEB", "name": "One"})

	fails(t, cs, "list_states", map[string]any{"project": "NOPE"}, `unknown project "NOPE"`, "WEB (Web)", "OTHER (Other)")
	fails(t, cs, "create_work_item", map[string]any{"project": "WEB", "name": "x", "state": "Doing"},
		`unknown state "Doing"`, "Backlog", "Todo", "In Progress", "Done", "Cancelled")
	fails(t, cs, "create_work_item", map[string]any{"project": "WEB", "name": "x", "labels": []string{"Bug", "Nope"}}, `unknown label "Nope"`, "Bug")
	fails(t, cs, "create_work_item", map[string]any{"project": "WEB", "name": "x", "assignees": []string{"zed@example.com"}},
		`unknown person "zed@example.com"`, "ada@example.com", "bob@example.com")
	fails(t, cs, "update_work_item", map[string]any{"item": "WEB-1", "cycle": "Nope"}, `unknown cycle "Nope"`, "none yet")
	fails(t, cs, "update_work_item", map[string]any{"item": "WEB-1", "modules": []string{"Nope"}}, `unknown module "Nope"`)
	fails(t, cs, "update_work_item", map[string]any{"item": "WEB-1", "priority": "huge"}, "priority", "not a valid choice")
	fails(t, cs, "update_work_item", map[string]any{"item": "WEB-99", "name": "x"}, `"WEB-99" not found`)
	fails(t, cs, "get_work_item", map[string]any{"item": "garbage"}, "use PROJ-12")
	fails(t, cs, "list_work_items", map[string]any{"project": "WEB", "state_group": []string{"doing"}}, "valid: backlog")
	fails(t, cs, "list_work_items", map[string]any{"project": "WEB", "cursor": "abc"}, "bad cursor")
	fails(t, cs, "create_project", map[string]any{"name": "Web2", "identifier": "WEB"}, "identifier")
	fails(t, cs, "create_work_item", map[string]any{"project": "WEB", "name": "  "}, "name")
	// Missing required input is a tool error from schema validation, not a protocol error.
	fails(t, cs, "create_work_item", map[string]any{"project": "WEB"}, "name")

	// ambiguous state names (case-insensitive duplicates) must not guess.
	p, _ := e.s.ProjectByRef(context.Background(), e.ws.ID, "WEB")
	if _, err := e.s.CreateState(context.Background(), p.ID, e.admin.ID, store.StateInput{Name: store.Some("done"), Group: store.Some("completed"), Color: store.Some("#000000")}); err != nil {
		t.Fatal(err)
	}
	fails(t, cs, "create_work_item", map[string]any{"project": "WEB", "name": "x", "state": "DONE"}, "ambiguous")
}

func TestRoles(t *testing.T) {
	e := newEnv(t)
	admin := e.connect(t, e.admin)
	ok(t, admin, "create_project", map[string]any{"name": "Web", "identifier": "WEB"})
	ok(t, admin, "create_work_item", map[string]any{"project": "WEB", "name": "One"})

	guest := e.connect(t, e.gst)
	// guests read and comment
	ok(t, guest, "list_projects", nil)
	ok(t, guest, "get_work_item", map[string]any{"item": "WEB-1"})
	ok(t, guest, "list_work_items", map[string]any{"project": "WEB"})
	cm := obj(ok(t, guest, "add_comment", map[string]any{"item": "WEB-1", "comment": "from a guest"}))
	if cm["author"] != "Gus Guest" {
		t.Fatalf("comment = %v", cm)
	}
	// but cannot write
	denied := "guest"
	fails(t, guest, "create_project", map[string]any{"name": "Nope"}, denied)
	fails(t, guest, "create_work_item", map[string]any{"project": "WEB", "name": "x"}, denied)
	fails(t, guest, "update_work_item", map[string]any{"item": "WEB-1", "name": "x"}, denied)
	fails(t, guest, "create_cycle", map[string]any{"project": "WEB", "name": "x"}, denied)
	fails(t, guest, "create_module", map[string]any{"project": "WEB", "name": "x"}, denied)
	fails(t, guest, "create_label", map[string]any{"project": "WEB", "name": "x"}, "admin")

	// members write, like REST; labels need an admin, like REST.
	member := e.connect(t, e.member)
	ok(t, member, "create_work_item", map[string]any{"project": "WEB", "name": "Two"})
	ok(t, member, "create_cycle", map[string]any{"project": "WEB", "name": "C"})
	fails(t, member, "create_label", map[string]any{"project": "WEB", "name": "x"}, "admin")

	got := obj(ok(t, admin, "get_work_item", map[string]any{"item": "WEB-1"}))
	if n := len(got["comments"].([]any)); n != 1 {
		t.Fatalf("comments = %d", n)
	}
}

// post sends one JSON-RPC request to the handler as a stateless client would.
func post(h http.Handler, token, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestHTTPAuth(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	h := Handler(e.s, api.TokenAuth(e.s), nil)
	const list = `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`

	for name, tok := range map[string]string{"no token": "", "bad token": "nope"} {
		w := post(h, tok, list)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s: status %d", name, w.Code)
		}
		if got := w.Header().Get("WWW-Authenticate"); got != "Bearer" {
			t.Errorf("%s: WWW-Authenticate = %q", name, got)
		}
		var body map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body["detail"] == "" {
			t.Errorf("%s: body %q", name, w.Body.String())
		}
	}

	_, adminTok, err := e.s.CreateToken(ctx, e.admin.ID, "t")
	if err != nil {
		t.Fatal(err)
	}
	_, bobTok, err := e.s.CreateToken(ctx, e.member.ID, "t")
	if err != nil {
		t.Fatal(err)
	}
	w := post(h, adminTok, list)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"create_work_item"`) {
		t.Fatalf("tools/list: %d %s", w.Code, w.Body.String())
	}

	// The token's owner is the actor of the tool call, per request.
	p, err := e.s.CreateProject(ctx, e.ws.ID, e.admin.ID, store.ProjectInput{Name: store.Some("Web"), Identifier: store.Some("WEB")})
	if err != nil {
		t.Fatal(err)
	}
	is, err := e.s.CreateIssue(ctx, p, e.admin.ID, store.IssueInput{Name: store.Some("One")})
	if err != nil {
		t.Fatal(err)
	}
	callBody := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"add_comment","arguments":{"item":"WEB-1","comment":"hi"}}}`
	for _, tok := range []string{bobTok, adminTok} {
		if w := post(h, tok, callBody); w.Code != http.StatusOK || strings.Contains(w.Body.String(), `"isError":true`) {
			t.Fatalf("add_comment: %d %s", w.Code, w.Body.String())
		}
	}
	cms, err := e.s.ListComments(ctx, is.ID)
	if err != nil || len(cms) != 2 {
		t.Fatalf("comments = %v, %v", cms, err)
	}
	actors := []string{*cms[0].ActorID, *cms[1].ActorID}
	sort.Strings(actors)
	want := []string{e.admin.ID, e.member.ID}
	sort.Strings(want)
	if actors[0] != want[0] || actors[1] != want[1] {
		t.Fatalf("actors = %v, want %v", actors, want)
	}

	// Stateless: no session header, GET is refused.
	if w.Header().Get("Mcp-Session-Id") != "" {
		t.Error("stateless server set a session id")
	}
	r := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	r.Header.Set("Authorization", "Bearer "+adminTok)
	r.Header.Set("Accept", "text/event-stream")
	gw := httptest.NewRecorder()
	h.ServeHTTP(gw, r)
	if gw.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET status %d", gw.Code)
	}
}
