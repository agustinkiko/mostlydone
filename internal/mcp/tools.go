package mcp

import (
	"context"
	"encoding/json"
	"html"
	"strconv"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"mostlydone/internal/store"
)

var readOnly = &sdk.ToolAnnotations{ReadOnlyHint: true}

func (t *tk) register(srv *sdk.Server) {
	sdk.AddTool(srv, &sdk.Tool{Name: "list_projects", Description: "List projects (identifier, name).", Annotations: readOnly}, t.listProjects)
	sdk.AddTool(srv, &sdk.Tool{Name: "create_project", Description: "Create a project with the default states."}, t.createProject)
	sdk.AddTool(srv, &sdk.Tool{Name: "list_work_items", Description: "List a project's work items, newest first. Filters combine with AND; list values mean any of.", Annotations: readOnly}, t.listWorkItems)
	sdk.AddTool(srv, &sdk.Tool{Name: "get_work_item", Description: "Get one work item with its description, comments and sub-items.", Annotations: readOnly}, t.getWorkItem)
	sdk.AddTool(srv, &sdk.Tool{Name: "create_work_item", Description: "Create a work item. Names are matched case-insensitively; ids also work."}, t.createWorkItem)
	sdk.AddTool(srv, &sdk.Tool{Name: "update_work_item", Description: "Change only the fields given. Clear with null or empty: description, parent, dates, cycle; [] for assignees, labels, modules."}, t.updateWorkItem)
	sdk.AddTool(srv, &sdk.Tool{Name: "add_comment", Description: "Comment on a work item as the current user."}, t.addComment)
	sdk.AddTool(srv, &sdk.Tool{Name: "list_states", Description: "List a project's states.", Annotations: readOnly}, t.listStates)
	sdk.AddTool(srv, &sdk.Tool{Name: "list_labels", Description: "List a project's labels.", Annotations: readOnly}, t.listLabels)
	sdk.AddTool(srv, &sdk.Tool{Name: "create_label", Description: "Create a label (admins only, as in the REST API)."}, t.createLabel)
	sdk.AddTool(srv, &sdk.Tool{Name: "list_cycles", Description: "List a project's cycles.", Annotations: readOnly}, t.listCycles)
	sdk.AddTool(srv, &sdk.Tool{Name: "create_cycle", Description: "Create a cycle. Give both dates or neither."}, t.createCycle)
	sdk.AddTool(srv, &sdk.Tool{Name: "list_modules", Description: "List a project's modules.", Annotations: readOnly}, t.listModules)
	sdk.AddTool(srv, &sdk.Tool{Name: "create_module", Description: "Create a module."}, t.createModule)
	sdk.AddTool(srv, &sdk.Tool{Name: "list_members", Description: "List workspace members (name, email, role).", Annotations: readOnly}, t.listMembers)
}

type projectIn struct {
	Project string `json:"project" jsonschema:"Project identifier or name"`
}

// ---- output shapes ----

type itemOut struct {
	ID          string       `json:"id,omitempty"`
	Identifier  string       `json:"identifier"`
	Name        string       `json:"name"`
	State       string       `json:"state"`
	StateGroup  string       `json:"state_group"`
	Priority    string       `json:"priority,omitempty"`
	Assignees   []string     `json:"assignees,omitempty"`
	Labels      []string     `json:"labels,omitempty"`
	Parent      string       `json:"parent,omitempty"`
	StartDate   string       `json:"start_date,omitempty"`
	TargetDate  string       `json:"target_date,omitempty"`
	Cycle       string       `json:"cycle,omitempty"`
	Modules     []string     `json:"modules,omitempty"`
	SubItems    int          `json:"sub_item_count,omitempty"`
	Description string       `json:"description,omitempty"`
	Comments    []commentOut `json:"comments,omitempty"`
	Children    []childOut   `json:"sub_items,omitempty"`
}

type commentOut struct {
	Author string `json:"author"`
	At     string `json:"at"`
	Body   string `json:"body"`
}

type childOut struct {
	ID         string `json:"id"`
	Identifier string `json:"identifier"`
	Name       string `json:"name"`
	State      string `json:"state"`
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// names resolves ids through m, skipping unknown ones.
func names(ids []string, m map[string]string) []string {
	var out []string
	for _, id := range ids {
		if n, ok := m[id]; ok {
			out = append(out, n)
		}
	}
	return out
}

// render turns issues of project p into compact output, resolving ids to names
// with one query per kind. detail adds the id and description.
func (t *tk) render(ctx context.Context, p *store.Project, issues []*store.Issue, detail bool) ([]itemOut, error) {
	states, err := t.s.ListStates(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	labels, err := t.s.ListLabels(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	cycles, err := t.s.ListCycles(ctx, p.ID, "all")
	if err != nil {
		return nil, err
	}
	modules, err := t.s.ListModules(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	members, err := t.members(ctx)
	if err != nil {
		return nil, err
	}
	var parentIDs []string
	for _, i := range issues {
		if i.ParentID != nil {
			parentIDs = append(parentIDs, *i.ParentID)
		}
	}
	parents, err := t.s.IssuesByIDs(ctx, parentIDs)
	if err != nil {
		return nil, err
	}
	stateByID := map[string]*store.State{}
	for _, s := range states {
		stateByID[s.ID] = s
	}
	labelName, cycleName, moduleName, userByID := map[string]string{}, map[string]string{}, map[string]string{}, map[string]string{}
	for _, l := range labels {
		labelName[l.ID] = l.Name
	}
	for _, c := range cycles {
		cycleName[c.ID] = c.Name
	}
	for _, m := range modules {
		moduleName[m.ID] = m.Name
	}
	for _, m := range members {
		userByID[m.ID] = userName(&m.User)
	}

	out := make([]itemOut, len(issues))
	for k, i := range issues {
		st := stateByID[i.StateID]
		o := itemOut{
			Identifier: p.Identifier + "-" + strconv.Itoa(i.SequenceID), Name: i.Name,
			State: st.Name, StateGroup: st.Group,
			Assignees: names(i.Assignees, userByID), Labels: names(i.Labels, labelName), Modules: names(i.ModuleIDs, moduleName),
			StartDate: str(i.StartDate), TargetDate: str(i.TargetDate), SubItems: i.SubIssuesCount,
		}
		if i.Priority != "none" {
			o.Priority = i.Priority
		}
		if i.CycleID != nil {
			o.Cycle = cycleName[*i.CycleID]
		}
		if i.ParentID != nil {
			if par, ok := parents[*i.ParentID]; ok {
				o.Parent = par.ProjectIdentifier + "-" + strconv.Itoa(par.SequenceID)
			}
		}
		if detail {
			o.ID, o.Description = i.ID, i.DescriptionHTML
		}
		out[k] = o
	}
	return out, nil
}

// toHTML wraps plain text in <p> (blank line = new paragraph, newline = <br>).
// Text that already starts with a tag is passed through; the store sanitizes it.
func toHTML(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
	if s == "" || strings.HasPrefix(s, "<") {
		return s
	}
	paras := strings.Split(s, "\n\n")
	for i, p := range paras {
		paras[i] = "<p>" + strings.ReplaceAll(html.EscapeString(strings.TrimSpace(p)), "\n", "<br>") + "</p>"
	}
	return strings.Join(paras, "")
}

// ---- projects ----

func (t *tk) listProjects(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, any, error) {
	list, err := t.s.ListProjects(ctx, t.ws.ID, "")
	if err != nil {
		return fail(err)
	}
	type row struct {
		Identifier  string `json:"identifier"`
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
	}
	out := make([]row, len(list))
	for i, p := range list {
		out[i] = row{p.Identifier, p.Name, p.Description}
	}
	return reply(out)
}

type createProjectIn struct {
	Name        string `json:"name" jsonschema:"Project name (no special characters)"`
	Identifier  string `json:"identifier,omitempty" jsonschema:"Short unique prefix like WEB, max 12 chars; default derived from the name"`
	Description string `json:"description,omitempty" jsonschema:"Plain text"`
}

func (t *tk) createProject(ctx context.Context, _ *sdk.CallToolRequest, in createProjectIn) (*sdk.CallToolResult, any, error) {
	if err := t.needWrite(); err != nil {
		return fail(err)
	}
	if in.Identifier == "" {
		var b strings.Builder
		for _, r := range strings.ToUpper(in.Name) {
			if (r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') && b.Len() < 5 {
				b.WriteRune(r)
			}
		}
		in.Identifier = b.String()
	}
	p, err := t.s.CreateProject(ctx, t.ws.ID, t.user.ID, store.ProjectInput{
		Name: store.Some(in.Name), Identifier: store.Some(in.Identifier), Description: store.Some(in.Description)})
	if err != nil {
		return fail(err)
	}
	return reply(map[string]string{"identifier": p.Identifier, "name": p.Name})
}

// ---- work items ----

type listItemsIn struct {
	Project    string   `json:"project" jsonschema:"Project identifier or name"`
	State      []string `json:"state,omitempty" jsonschema:"State names or ids"`
	StateGroup []string `json:"state_group,omitempty" jsonschema:"backlog, unstarted, started, completed, cancelled"`
	Priority   []string `json:"priority,omitempty" jsonschema:"urgent, high, medium, low, none"`
	Assignee   []string `json:"assignee,omitempty" jsonschema:"Emails, names, ids or me"`
	Label      []string `json:"label,omitempty" jsonschema:"Label names or ids"`
	Cycle      []string `json:"cycle,omitempty" jsonschema:"Cycle names or ids"`
	Module     []string `json:"module,omitempty" jsonschema:"Module names or ids"`
	Parent     string   `json:"parent,omitempty" jsonschema:"Parent work item like PROJ-3: its sub-items only"`
	Search     string   `json:"search,omitempty" jsonschema:"Text in the name, or a sequence number"`
	OrderBy    string   `json:"order_by,omitempty" jsonschema:"created_at, updated_at, priority, target_date, start_date, sequence_id, state__group or state__name; prefix - for descending. Default -created_at"`
	Limit      int      `json:"limit,omitempty" jsonschema:"Rows per page, default 50, max 200"`
	Cursor     string   `json:"cursor,omitempty" jsonschema:"next_cursor from the previous page"`
}

func (t *tk) listWorkItems(ctx context.Context, _ *sdk.CallToolRequest, in listItemsIn) (*sdk.CallToolResult, any, error) {
	p, err := t.project(ctx, in.Project)
	if err != nil {
		return fail(err)
	}
	r, err := t.refs(ctx, p)
	if err != nil {
		return fail(err)
	}
	f := store.IssueFilter{ProjectID: p.ID, Search: in.Search, OrderBy: in.OrderBy}
	for _, step := range []func() error{
		func() (err error) {
			f.States, err = pickAll("state", "project "+p.Identifier, in.State, r.states)
			return
		},
		func() (err error) {
			f.Labels, err = pickAll("label", "project "+p.Identifier, in.Label, r.labels)
			return
		},
		func() (err error) {
			f.Cycles, err = pickAll("cycle", "project "+p.Identifier, in.Cycle, r.cycles)
			return
		},
		func() (err error) {
			f.Modules, err = pickAll("module", "project "+p.Identifier, in.Module, r.modules)
			return
		},
		func() (err error) { f.Assignees, err = t.userIDs(ctx, in.Assignee); return },
		func() (err error) {
			f.Priorities, err = oneOf("priority", in.Priority, store.Priorities)
			return
		},
		func() (err error) {
			f.StateGroups, err = oneOf("state_group", in.StateGroup, store.StateGroups)
			return
		},
	} {
		if err := step(); err != nil {
			return fail(err)
		}
	}
	if in.Parent != "" {
		par, err := t.item(ctx, in.Parent)
		if err != nil {
			return fail(err)
		}
		if par.ProjectID != p.ID {
			return fail(errf("parent %s is not in project %s", in.Parent, p.Identifier))
		}
		f.Parents = []string{par.ID}
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 50
	}
	limit = min(limit, 200)
	offset := 0
	if in.Cursor != "" {
		if offset, err = strconv.Atoi(in.Cursor); err != nil || offset < 0 {
			return fail(errf("bad cursor %q: pass next_cursor from the previous page", in.Cursor))
		}
	}
	f.Page = store.Page{Limit: limit, Offset: offset}
	issues, total, err := t.s.ListIssues(ctx, f)
	if err != nil {
		return fail(err)
	}
	items, err := t.render(ctx, p, issues, false)
	if err != nil {
		return fail(err)
	}
	more := offset+len(issues) < total
	res := struct {
		Total      int       `json:"total"`
		HasMore    bool      `json:"has_more"`
		NextCursor string    `json:"next_cursor,omitempty"`
		Items      []itemOut `json:"items"`
	}{Total: total, HasMore: more, Items: items}
	if more {
		res.NextCursor = strconv.Itoa(offset + len(issues))
	}
	return reply(res)
}

// oneOf checks each value (case-insensitively) against the allowed list.
func oneOf(kind string, vals, allowed []string) ([]string, error) {
	var out []string
	for _, v := range vals {
		v = strings.ToLower(strings.TrimSpace(v))
		ok := false
		for _, a := range allowed {
			ok = ok || a == v
		}
		if !ok {
			return nil, errf("unknown %s %q; valid: %s", kind, v, strings.Join(allowed, ", "))
		}
		out = append(out, v)
	}
	return out, nil
}

// projRefs are a project's states, labels, cycles and modules as named pairs.
type projRefs struct{ states, labels, cycles, modules []named }

func (t *tk) refs(ctx context.Context, p *store.Project) (*projRefs, error) {
	r := &projRefs{}
	states, err := t.s.ListStates(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	for _, x := range states {
		r.states = append(r.states, named{x.ID, x.Name})
	}
	labels, err := t.s.ListLabels(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	for _, x := range labels {
		r.labels = append(r.labels, named{x.ID, x.Name})
	}
	cycles, err := t.s.ListCycles(ctx, p.ID, "all")
	if err != nil {
		return nil, err
	}
	for _, x := range cycles {
		r.cycles = append(r.cycles, named{x.ID, x.Name})
	}
	modules, err := t.s.ListModules(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	for _, x := range modules {
		r.modules = append(r.modules, named{x.ID, x.Name})
	}
	return r, nil
}

type itemRefIn struct {
	Item string `json:"item" jsonschema:"Work item like PROJ-12"`
}

func (t *tk) getWorkItem(ctx context.Context, _ *sdk.CallToolRequest, in itemRefIn) (*sdk.CallToolResult, any, error) {
	is, err := t.item(ctx, in.Item)
	if err != nil {
		return fail(err)
	}
	p, err := t.s.GetProject(ctx, t.ws.ID, is.ProjectID)
	if err != nil {
		return fail(err)
	}
	items, err := t.render(ctx, p, []*store.Issue{is}, true)
	if err != nil {
		return fail(err)
	}
	out := items[0]

	comments, err := t.s.ListComments(ctx, is.ID)
	if err != nil {
		return fail(err)
	}
	members, err := t.members(ctx)
	if err != nil {
		return fail(err)
	}
	who := map[string]string{}
	for _, m := range members {
		who[m.ID] = userName(&m.User)
	}
	for k := len(comments) - 1; k >= 0; k-- { // oldest first
		c := comments[k]
		a := who[str(c.ActorID)]
		if a == "" {
			a = "(former member)"
		}
		out.Comments = append(out.Comments, commentOut{Author: a, At: c.CreatedAt, Body: c.CommentHTML})
	}

	if is.SubIssuesCount > 0 {
		kids, _, err := t.s.ListIssues(ctx, store.IssueFilter{ProjectID: p.ID, Parents: []string{is.ID}, OrderBy: "sequence_id", Page: store.Page{Limit: 200}})
		if err != nil {
			return fail(err)
		}
		rendered, err := t.render(ctx, p, kids, false)
		if err != nil {
			return fail(err)
		}
		for k, kid := range kids {
			out.Children = append(out.Children, childOut{ID: kid.ID, Identifier: rendered[k].Identifier, Name: kid.Name, State: rendered[k].State})
		}
	}
	return reply(out)
}

// itemFields are the writable fields shared by create and update.
type itemFields struct {
	Description *string  `json:"description,omitempty" jsonschema:"HTML or plain text"`
	State       string   `json:"state,omitempty" jsonschema:"State name or id"`
	Priority    string   `json:"priority,omitempty" jsonschema:"urgent, high, medium, low or none"`
	Assignees   []string `json:"assignees,omitempty" jsonschema:"Emails, names, ids or me; replaces the set"`
	Labels      []string `json:"labels,omitempty" jsonschema:"Label names or ids; replaces the set"`
	Parent      *string  `json:"parent,omitempty" jsonschema:"Parent work item like PROJ-3"`
	StartDate   *string  `json:"start_date,omitempty" jsonschema:"YYYY-MM-DD"`
	TargetDate  *string  `json:"target_date,omitempty" jsonschema:"YYYY-MM-DD"`
	Cycle       *string  `json:"cycle,omitempty" jsonschema:"Cycle name or id"`
	Modules     []string `json:"modules,omitempty" jsonschema:"Module names or ids; replaces the set"`
}

type createItemIn struct {
	Project string `json:"project" jsonschema:"Project identifier or name"`
	Name    string `json:"name" jsonschema:"Title"`
	itemFields
}

type updateItemIn struct {
	Item string `json:"item" jsonschema:"Work item like PROJ-12"`
	Name string `json:"name,omitempty" jsonschema:"New title"`
	itemFields
}

// build turns f into a store input. Only fields present in the request (has)
// are set; a null or empty value clears where the store allows it.
func (t *tk) build(ctx context.Context, p *store.Project, f itemFields, has func(string) bool) (store.IssueInput, error) {
	var in store.IssueInput
	r, err := t.refs(ctx, p)
	if err != nil {
		return in, err
	}
	scope := "project " + p.Identifier
	if f.Description != nil {
		in.DescriptionHTML = store.Some(toHTML(*f.Description))
	} else if has("description") {
		in.DescriptionHTML = store.Some("")
	}
	if f.State != "" {
		id, err := pick("state", scope, f.State, r.states)
		if err != nil {
			return in, err
		}
		in.State = store.Some(&id)
	}
	if f.Priority != "" {
		in.Priority = store.Some(strings.ToLower(strings.TrimSpace(f.Priority)))
	}
	if has("assignees") {
		ids, err := t.userIDs(ctx, f.Assignees)
		if err != nil {
			return in, err
		}
		in.Assignees = store.Some(append([]string{}, ids...))
	}
	if has("labels") {
		ids, err := pickAll("label", scope, f.Labels, r.labels)
		if err != nil {
			return in, err
		}
		in.Labels = store.Some(append([]string{}, ids...))
	}
	if has("modules") {
		ids, err := pickAll("module", scope, f.Modules, r.modules)
		if err != nil {
			return in, err
		}
		in.ModuleIDs = store.Some(append([]string{}, ids...))
	}
	if has("parent") {
		in.Parent = store.Some[*string](nil)
		if f.Parent != nil && strings.TrimSpace(*f.Parent) != "" {
			par, err := t.item(ctx, *f.Parent)
			if err != nil {
				return in, err
			}
			in.Parent = store.Some(&par.ID)
		}
	}
	if has("cycle") {
		in.CycleID = store.Some[*string](nil)
		if f.Cycle != nil && strings.TrimSpace(*f.Cycle) != "" {
			id, err := pick("cycle", scope, *f.Cycle, r.cycles)
			if err != nil {
				return in, err
			}
			in.CycleID = store.Some(&id)
		}
	}
	date := func(key string, v *string) store.Opt[*string] {
		if !has(key) {
			return store.Opt[*string]{}
		}
		if v == nil || strings.TrimSpace(*v) == "" {
			return store.Some[*string](nil)
		}
		return store.Some(v)
	}
	in.StartDate = date("start_date", f.StartDate)
	in.TargetDate = date("target_date", f.TargetDate)
	return in, nil
}

// present reports which top-level argument keys the caller sent, so null and
// empty values can mean "clear" while absent means "leave alone".
func present(req *sdk.CallToolRequest) func(string) bool {
	var m map[string]json.RawMessage
	if req != nil && req.Params != nil {
		json.Unmarshal(req.Params.Arguments, &m)
	}
	return func(k string) bool { _, ok := m[k]; return ok }
}

func (t *tk) createWorkItem(ctx context.Context, req *sdk.CallToolRequest, in createItemIn) (*sdk.CallToolResult, any, error) {
	if err := t.needWrite(); err != nil {
		return fail(err)
	}
	p, err := t.project(ctx, in.Project)
	if err != nil {
		return fail(err)
	}
	si, err := t.build(ctx, p, in.itemFields, present(req))
	if err != nil {
		return fail(err)
	}
	si.Name = store.Some(in.Name)
	is, err := t.s.CreateIssue(ctx, p, t.user.ID, si)
	if err != nil {
		return fail(err)
	}
	return t.itemReply(ctx, p, is)
}

func (t *tk) updateWorkItem(ctx context.Context, req *sdk.CallToolRequest, in updateItemIn) (*sdk.CallToolResult, any, error) {
	if err := t.needWrite(); err != nil {
		return fail(err)
	}
	cur, err := t.item(ctx, in.Item)
	if err != nil {
		return fail(err)
	}
	p, err := t.s.GetProject(ctx, t.ws.ID, cur.ProjectID)
	if err != nil {
		return fail(err)
	}
	si, err := t.build(ctx, p, in.itemFields, present(req))
	if err != nil {
		return fail(err)
	}
	if in.Name != "" {
		si.Name = store.Some(in.Name)
	}
	is, err := t.s.UpdateIssue(ctx, p, cur.ID, t.user.ID, si)
	if err != nil {
		return fail(err)
	}
	return t.itemReply(ctx, p, is)
}

func (t *tk) itemReply(ctx context.Context, p *store.Project, is *store.Issue) (*sdk.CallToolResult, any, error) {
	items, err := t.render(ctx, p, []*store.Issue{is}, false)
	if err != nil {
		return fail(err)
	}
	return reply(items[0])
}

type addCommentIn struct {
	Item    string `json:"item" jsonschema:"Work item like PROJ-12"`
	Comment string `json:"comment" jsonschema:"HTML or plain text"`
}

func (t *tk) addComment(ctx context.Context, _ *sdk.CallToolRequest, in addCommentIn) (*sdk.CallToolResult, any, error) {
	is, err := t.item(ctx, in.Item) // any role may comment
	if err != nil {
		return fail(err)
	}
	if strings.TrimSpace(in.Comment) == "" {
		return fail(errf("comment is empty"))
	}
	c, err := t.s.CreateComment(ctx, is, t.user.ID, store.CommentInput{CommentHTML: store.Some(toHTML(in.Comment))})
	if err != nil {
		return fail(err)
	}
	return reply(commentOut{Author: userName(t.user), At: c.CreatedAt, Body: c.CommentHTML})
}

// ---- project lookups and small creates ----

func (t *tk) listStates(ctx context.Context, _ *sdk.CallToolRequest, in projectIn) (*sdk.CallToolResult, any, error) {
	p, err := t.project(ctx, in.Project)
	if err != nil {
		return fail(err)
	}
	list, err := t.s.ListStates(ctx, p.ID)
	if err != nil {
		return fail(err)
	}
	type row struct {
		Name    string `json:"name"`
		Group   string `json:"group"`
		Default bool   `json:"default,omitempty"`
	}
	out := make([]row, len(list))
	for i, s := range list {
		out[i] = row{s.Name, s.Group, s.Default}
	}
	return reply(out)
}

type labelOut struct {
	Name        string `json:"name"`
	Color       string `json:"color,omitempty"`
	Description string `json:"description,omitempty"`
}

func (t *tk) listLabels(ctx context.Context, _ *sdk.CallToolRequest, in projectIn) (*sdk.CallToolResult, any, error) {
	p, err := t.project(ctx, in.Project)
	if err != nil {
		return fail(err)
	}
	list, err := t.s.ListLabels(ctx, p.ID)
	if err != nil {
		return fail(err)
	}
	out := make([]labelOut, len(list))
	for i, l := range list {
		out[i] = labelOut{l.Name, l.Color, l.Description}
	}
	return reply(out)
}

type createLabelIn struct {
	Project     string `json:"project" jsonschema:"Project identifier or name"`
	Name        string `json:"name" jsonschema:"Label name, unique in the project"`
	Color       string `json:"color,omitempty" jsonschema:"Hex color like #ff8800"`
	Description string `json:"description,omitempty"`
}

func (t *tk) createLabel(ctx context.Context, _ *sdk.CallToolRequest, in createLabelIn) (*sdk.CallToolResult, any, error) {
	if t.role < store.RoleAdmin {
		return fail(errf("creating labels needs an admin account"))
	}
	p, err := t.project(ctx, in.Project)
	if err != nil {
		return fail(err)
	}
	li := store.LabelInput{Name: store.Some(in.Name), Description: store.Some(in.Description)}
	if in.Color != "" {
		li.Color = store.Some(in.Color)
	}
	l, err := t.s.CreateLabel(ctx, p.ID, t.user.ID, li)
	if err != nil {
		return fail(err)
	}
	return reply(labelOut{l.Name, l.Color, l.Description})
}

func (t *tk) listCycles(ctx context.Context, _ *sdk.CallToolRequest, in projectIn) (*sdk.CallToolResult, any, error) {
	p, err := t.project(ctx, in.Project)
	if err != nil {
		return fail(err)
	}
	list, err := t.s.ListCycles(ctx, p.ID, "all")
	if err != nil {
		return fail(err)
	}
	type row struct {
		Name        string `json:"name"`
		StartDate   string `json:"start_date,omitempty"`
		EndDate     string `json:"end_date,omitempty"`
		Description string `json:"description,omitempty"`
		Items       int    `json:"items,omitempty"`
	}
	out := make([]row, len(list))
	for i, c := range list {
		out[i] = row{c.Name, str(c.StartDate), str(c.EndDate), c.Description, c.Counts.Total}
	}
	return reply(out)
}

type createCycleIn struct {
	Project     string `json:"project" jsonschema:"Project identifier or name"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	StartDate   string `json:"start_date,omitempty" jsonschema:"YYYY-MM-DD; needs end_date"`
	EndDate     string `json:"end_date,omitempty" jsonschema:"YYYY-MM-DD; needs start_date"`
}

func optStr(s string) store.Opt[*string] {
	if s == "" {
		return store.Opt[*string]{}
	}
	return store.Some(&s)
}

func (t *tk) createCycle(ctx context.Context, _ *sdk.CallToolRequest, in createCycleIn) (*sdk.CallToolResult, any, error) {
	if err := t.needWrite(); err != nil {
		return fail(err)
	}
	p, err := t.project(ctx, in.Project)
	if err != nil {
		return fail(err)
	}
	c, err := t.s.CreateCycle(ctx, p, t.user.ID, store.CycleInput{Name: store.Some(in.Name), Description: store.Some(in.Description),
		StartDate: optStr(in.StartDate), EndDate: optStr(in.EndDate)})
	if err != nil {
		return fail(err)
	}
	return reply(map[string]string{"name": c.Name, "start_date": str(c.StartDate), "end_date": str(c.EndDate)})
}

func (t *tk) listModules(ctx context.Context, _ *sdk.CallToolRequest, in projectIn) (*sdk.CallToolResult, any, error) {
	p, err := t.project(ctx, in.Project)
	if err != nil {
		return fail(err)
	}
	list, err := t.s.ListModules(ctx, p.ID)
	if err != nil {
		return fail(err)
	}
	type row struct {
		Name        string `json:"name"`
		Status      string `json:"status"`
		StartDate   string `json:"start_date,omitempty"`
		TargetDate  string `json:"target_date,omitempty"`
		Description string `json:"description,omitempty"`
		Items       int    `json:"items,omitempty"`
	}
	out := make([]row, len(list))
	for i, m := range list {
		out[i] = row{m.Name, m.Status, str(m.StartDate), str(m.TargetDate), m.Description, m.Counts.Total}
	}
	return reply(out)
}

type createModuleIn struct {
	Project     string   `json:"project" jsonschema:"Project identifier or name"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Status      string   `json:"status,omitempty" jsonschema:"backlog, planned, in-progress, paused, completed or cancelled; default backlog"`
	StartDate   string   `json:"start_date,omitempty" jsonschema:"YYYY-MM-DD"`
	TargetDate  string   `json:"target_date,omitempty" jsonschema:"YYYY-MM-DD"`
	Lead        string   `json:"lead,omitempty" jsonschema:"Email, name, id or me"`
	Members     []string `json:"members,omitempty" jsonschema:"Emails, names, ids or me"`
}

func (t *tk) createModule(ctx context.Context, _ *sdk.CallToolRequest, in createModuleIn) (*sdk.CallToolResult, any, error) {
	if err := t.needWrite(); err != nil {
		return fail(err)
	}
	p, err := t.project(ctx, in.Project)
	if err != nil {
		return fail(err)
	}
	mi := store.ModuleInput{Name: store.Some(in.Name), Description: store.Some(in.Description),
		StartDate: optStr(in.StartDate), TargetDate: optStr(in.TargetDate)}
	if in.Status != "" {
		mi.Status = store.Some(in.Status)
	}
	if in.Lead != "" {
		ids, err := t.userIDs(ctx, []string{in.Lead})
		if err != nil {
			return fail(err)
		}
		mi.Lead = store.Some(&ids[0])
	}
	if in.Members != nil {
		ids, err := t.userIDs(ctx, in.Members)
		if err != nil {
			return fail(err)
		}
		mi.Members = store.Some(ids)
	}
	m, err := t.s.CreateModule(ctx, p, t.user.ID, mi)
	if err != nil {
		return fail(err)
	}
	return reply(map[string]string{"name": m.Name, "status": m.Status})
}

func (t *tk) listMembers(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, any, error) {
	list, err := t.members(ctx)
	if err != nil {
		return fail(err)
	}
	type row struct {
		Name  string `json:"name"`
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	out := make([]row, len(list))
	for i, m := range list {
		role := "guest"
		switch {
		case m.Role >= store.RoleAdmin:
			role = "admin"
		case m.Role >= store.RoleMember:
			role = "member"
		}
		out[i] = row{userName(&m.User), m.Email, role}
	}
	return reply(out)
}
