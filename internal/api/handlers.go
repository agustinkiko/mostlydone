package api

import (
	"net/http"
	"strconv"
	"strings"

	"mostlydone/internal/store"
)

const (
	guest  = store.RoleGuest
	member = store.RoleMember
	admin  = store.RoleAdmin
)

func (a *API) routes(mux *http.ServeMux) {
	h := func(pattern string, role int, fn func(*rc)) { a.handle(mux, pattern, role, fn) }
	const ws = "/workspaces/{ws}"
	const pj = ws + "/projects/{pid}"

	h("GET /users/me/", 0, func(c *rc) { c.json(200, userLite(c.user)) })
	h("GET "+ws+"/members/", admin, getWorkspaceMembers)
	for _, m := range []string{"/members/", "/project-members/"} {
		h("GET "+pj+m, guest, getProjectMembers)
		h("GET "+pj+m+"{uid}/", guest, getProjectMember)
	}

	h("GET "+ws+"/projects/", guest, listProjects)
	h("POST "+ws+"/projects/", member, createProject)
	h("GET "+pj+"/", guest, func(c *rc) { c.one(200, projectJSON(c.p, c.role)) })
	h("PATCH "+pj+"/", admin, updateProject)
	h("DELETE "+pj+"/", admin, deleteProject)

	h("GET "+pj+"/states/", guest, listStates)
	h("POST "+pj+"/states/", admin, createState)
	h("GET "+pj+"/states/{id}/", guest, getState)
	h("PATCH "+pj+"/states/{id}/", admin, updateState)
	h("DELETE "+pj+"/states/{id}/", admin, deleteState)

	h("GET "+pj+"/labels/", guest, listLabels)
	h("POST "+pj+"/labels/", admin, createLabel)
	h("GET "+pj+"/labels/{id}/", guest, getLabel)
	h("PATCH "+pj+"/labels/{id}/", admin, updateLabel)
	h("DELETE "+pj+"/labels/{id}/", admin, deleteLabel)

	for _, sp := range []string{"/issues/", "/work-items/"} {
		h("GET "+ws+sp+"search/", guest, searchIssues)
		h("GET "+ws+sp+"{ident}/", guest, getIssueByIdentifier)
		it := pj + sp
		h("GET "+it, guest, listIssues)
		h("POST "+it, member, createIssue)
		h("GET "+it+"{id}/", guest, getIssue)
		h("PATCH "+it+"{id}/", member, updateIssue)
		h("DELETE "+it+"{id}/", member, deleteIssue)

		h("GET "+it+"{issue_id}/comments/", guest, listComments)
		h("POST "+it+"{issue_id}/comments/", guest, createComment)
		h("GET "+it+"{issue_id}/comments/{id}/", guest, getComment)
		h("PATCH "+it+"{issue_id}/comments/{id}/", guest, updateComment)
		h("DELETE "+it+"{issue_id}/comments/{id}/", guest, deleteComment)

		h("GET "+it+"{issue_id}/links/", guest, listLinks)
		h("POST "+it+"{issue_id}/links/", member, createLink)
		h("GET "+it+"{issue_id}/links/{id}/", guest, getLink)
		h("PATCH "+it+"{issue_id}/links/{id}/", member, updateLink)
		h("DELETE "+it+"{issue_id}/links/{id}/", member, deleteLink)

		h("GET "+it+"{issue_id}/activities/", guest, listActivities)
		h("GET "+it+"{issue_id}/activities/{id}/", guest, getActivity)
	}

	h("GET "+pj+"/cycles/", guest, listCycles)
	h("POST "+pj+"/cycles/", member, createCycle)
	h("GET "+pj+"/cycles/{id}/", guest, getCycle)
	h("PATCH "+pj+"/cycles/{id}/", member, updateCycle)
	h("DELETE "+pj+"/cycles/{id}/", member, deleteCycle)
	h("GET "+pj+"/cycles/{id}/cycle-issues/", guest, listCycleIssues)
	h("POST "+pj+"/cycles/{id}/cycle-issues/", member, addCycleIssues)
	h("GET "+pj+"/cycles/{id}/cycle-issues/{issue_id}/", guest, getCycleIssue)
	h("DELETE "+pj+"/cycles/{id}/cycle-issues/{issue_id}/", member, removeCycleIssue)

	h("GET "+pj+"/modules/", guest, listModules)
	h("POST "+pj+"/modules/", member, createModule)
	h("GET "+pj+"/modules/{id}/", guest, getModule)
	h("PATCH "+pj+"/modules/{id}/", member, updateModule)
	h("DELETE "+pj+"/modules/{id}/", member, deleteModule)
	h("GET "+pj+"/modules/{id}/module-issues/", guest, listModuleIssues)
	h("POST "+pj+"/modules/{id}/module-issues/", member, addModuleIssues)
	h("DELETE "+pj+"/modules/{id}/module-issues/{issue_id}/", member, removeModuleIssue)
}

func listOf[T any](c *rc, list []T, f func(T) map[string]any) {
	objs := make([]map[string]any, len(list))
	for k, v := range list {
		objs[k] = f(v)
	}
	c.listAll(objs)
}

// --- members ---

func getWorkspaceMembers(c *rc) {
	list, err := c.a.s.ListMembers(c.r.Context(), c.ws.ID)
	if err != nil {
		c.fail(err)
		return
	}
	out := make([]map[string]any, len(list))
	for k, m := range list {
		out[k] = memberJSON(m)
	}
	c.json(200, out)
}

// Project members are the workspace members (no per-project membership).
func getProjectMembers(c *rc) {
	list, err := c.a.s.ListMembers(c.r.Context(), c.ws.ID)
	if err != nil {
		c.fail(err)
		return
	}
	out := make([]map[string]any, len(list))
	for k, m := range list {
		out[k] = userLite(&m.User)
	}
	c.json(200, out)
}

func getProjectMember(c *rc) {
	m, err := c.a.s.GetMember(c.r.Context(), c.ws.ID, c.r.PathValue("uid"))
	if err != nil {
		c.fail(err)
		return
	}
	c.json(200, userLite(&m.User))
}

// --- projects ---

func listProjects(c *rc) {
	list, err := c.a.s.ListProjects(c.r.Context(), c.ws.ID, c.r.URL.Query().Get("order_by"))
	if err != nil {
		c.fail(err)
		return
	}
	listOf(c, list, func(p *store.Project) map[string]any { return projectJSON(p, c.role) })
}

func createProject(c *rc) {
	var in store.ProjectInput
	if !c.decode(&in) {
		return
	}
	if !in.Name.Set {
		c.json(400, map[string]any{"name": []string{"This field is required."}})
		return
	}
	p, err := c.a.s.CreateProject(c.r.Context(), c.ws.ID, c.user.ID, in)
	if err != nil {
		c.fail(err)
		return
	}
	c.json(201, projectJSON(p, c.role))
}

func updateProject(c *rc) {
	var in store.ProjectInput
	if !c.decode(&in) {
		return
	}
	p, err := c.a.s.UpdateProject(c.r.Context(), c.ws.ID, c.p.ID, c.user.ID, in)
	if err != nil {
		c.fail(err)
		return
	}
	c.json(200, projectJSON(p, c.role))
}

func deleteProject(c *rc) {
	if err := c.a.s.DeleteProject(c.r.Context(), c.ws.ID, c.p.ID); err != nil {
		c.fail(err)
		return
	}
	c.noContent()
}

// --- states ---

func listStates(c *rc) {
	list, err := c.a.s.ListStates(c.r.Context(), c.p.ID)
	if err != nil {
		c.fail(err)
		return
	}
	listOf(c, list, func(s *store.State) map[string]any { return stateJSON(s, c.ws.ID) })
}

func getState(c *rc) {
	s, err := c.a.s.GetState(c.r.Context(), c.p.ID, c.r.PathValue("id"))
	if err != nil {
		c.fail(err)
		return
	}
	c.one(200, stateJSON(s, c.ws.ID))
}

func createState(c *rc) {
	var in store.StateInput
	if !c.decode(&in) {
		return
	}
	for field, set := range map[string]bool{"name": in.Name.Set, "color": in.Color.Set} {
		if !set {
			c.json(400, map[string]any{field: []string{"This field is required."}})
			return
		}
	}
	s, err := c.a.s.CreateState(c.r.Context(), c.p.ID, c.user.ID, in)
	if err != nil {
		c.fail(err)
		return
	}
	c.json(200, stateJSON(s, c.ws.ID))
}

func updateState(c *rc) {
	var in store.StateInput
	if !c.decode(&in) {
		return
	}
	s, err := c.a.s.UpdateState(c.r.Context(), c.p.ID, c.r.PathValue("id"), c.user.ID, in)
	if err != nil {
		c.fail(err)
		return
	}
	c.json(200, stateJSON(s, c.ws.ID))
}

func deleteState(c *rc) {
	if err := c.a.s.DeleteState(c.r.Context(), c.p.ID, c.r.PathValue("id")); err != nil {
		c.fail(err)
		return
	}
	c.noContent()
}

// --- labels ---

func listLabels(c *rc) {
	list, err := c.a.s.ListLabels(c.r.Context(), c.p.ID)
	if err != nil {
		c.fail(err)
		return
	}
	listOf(c, list, func(l *store.Label) map[string]any { return labelJSON(l, c.ws.ID) })
}

func getLabel(c *rc) {
	l, err := c.a.s.GetLabel(c.r.Context(), c.p.ID, c.r.PathValue("id"))
	if err != nil {
		c.fail(err)
		return
	}
	c.json(200, labelJSON(l, c.ws.ID)) // Plane ignores fields/expand here
}

func createLabel(c *rc) {
	var in store.LabelInput
	if !c.decode(&in) {
		return
	}
	if !in.Name.Set {
		c.json(400, map[string]any{"name": []string{"This field is required."}})
		return
	}
	l, err := c.a.s.CreateLabel(c.r.Context(), c.p.ID, c.user.ID, in)
	if err != nil {
		c.fail(err)
		return
	}
	c.json(201, labelJSON(l, c.ws.ID))
}

func updateLabel(c *rc) {
	var in store.LabelInput
	if !c.decode(&in) {
		return
	}
	l, err := c.a.s.UpdateLabel(c.r.Context(), c.p.ID, c.r.PathValue("id"), c.user.ID, in)
	if err != nil {
		c.fail(err)
		return
	}
	c.json(200, labelJSON(l, c.ws.ID))
}

func deleteLabel(c *rc) {
	if err := c.a.s.DeleteLabel(c.r.Context(), c.p.ID, c.r.PathValue("id")); err != nil {
		c.fail(err)
		return
	}
	c.noContent()
}

// --- work items ---

// issueFilter reads the list filters (an extension to Plane) from the query.
func (c *rc) issueFilter() store.IssueFilter {
	q := c.r.URL.Query()
	return store.IssueFilter{
		ProjectID:   c.p.ID,
		States:      csv(q.Get("state")),
		StateGroups: csv(q.Get("state_group")),
		Priorities:  csv(q.Get("priority")),
		Assignees:   csv(q.Get("assignee")),
		Labels:      csv(q.Get("label")),
		Cycles:      csv(q.Get("cycle")),
		Modules:     csv(q.Get("module")),
		Parents:     csv(q.Get("parent")),
		Search:      q.Get("search"),
		OrderBy:     q.Get("order_by"),
	}
}

func (c *rc) issuePage(f store.IssueFilter) {
	perPage, page, ok := c.page()
	if !ok {
		return
	}
	f.Page = store.Page{Limit: perPage, Offset: page * perPage}
	list, total, err := c.a.s.ListIssues(c.r.Context(), f)
	if err != nil {
		c.fail(err)
		return
	}
	objs, err := c.shape(issuesJSON(list))
	if err != nil {
		c.fail(err)
		return
	}
	c.envelope(objs, total, perPage, page)
}

func listIssues(c *rc) {
	q := c.r.URL.Query()
	var unsupported []string
	for _, k := range []string{"pql", "filters"} {
		if q.Has(k) {
			unsupported = append(unsupported, k)
		}
	}
	if len(unsupported) > 0 {
		c.json(400, map[string]any{"pql": "Advanced filtering is not supported on this edition.", "unsupported_parameters": unsupported})
		return
	}
	f := c.issueFilter()
	if src, id := q.Get("external_source"), q.Get("external_id"); src != "" && id != "" {
		f.ExternalSource, f.ExternalID, f.Page = src, id, store.Page{Limit: 1}
		list, _, err := c.a.s.ListIssues(c.r.Context(), f)
		if err != nil {
			c.fail(err)
			return
		}
		if len(list) == 0 {
			c.fail(store.ErrNotFound)
			return
		}
		c.one(200, issueJSON(list[0]))
		return
	}
	c.issuePage(f)
}

func (c *rc) loadIssue(param string) (*store.Issue, bool) {
	i, err := c.a.s.GetIssue(c.r.Context(), c.p.ID, c.r.PathValue(param))
	if err != nil {
		c.fail(err)
		return nil, false
	}
	return i, true
}

func getIssue(c *rc) {
	if i, ok := c.loadIssue("id"); ok {
		c.one(200, issueJSON(i))
	}
}

func getIssueByIdentifier(c *rc) {
	ident := c.r.PathValue("ident")
	cut := strings.LastIndexByte(ident, '-')
	seq, err := strconv.Atoi(ident[cut+1:])
	if cut <= 0 || err != nil {
		c.json(404, map[string]any{"error": "Work item not found"})
		return
	}
	i, err := c.a.s.IssueByIdentifier(c.r.Context(), c.ws.ID, ident[:cut], seq)
	if err != nil {
		c.fail(err)
		return
	}
	c.one(200, issueJSON(i))
}

func searchIssues(c *rc) {
	q := c.r.URL.Query()
	limit := 10
	if n, err := strconv.Atoi(q.Get("limit")); err == nil && n > 0 {
		limit = min(n, 100)
	}
	projectID := ""
	if q.Get("workspace_search") != "true" {
		projectID = q.Get("project_id")
	}
	hits, err := c.a.s.SearchIssues(c.r.Context(), c.ws.ID, q.Get("search"), projectID, limit)
	if err != nil {
		c.fail(err)
		return
	}
	out := make([]map[string]any, len(hits))
	for k, h := range hits {
		out[k] = map[string]any{"name": h.Name, "id": h.ID, "sequence_id": h.SequenceID, "project__identifier": h.ProjectIdentifier,
			"project_id": h.ProjectID, "workspace__slug": h.WorkspaceSlug}
	}
	c.json(200, map[string]any{"issues": out})
}

func createIssue(c *rc) {
	var in store.IssueInput
	if !c.decode(&in) {
		return
	}
	if !in.Name.Set {
		c.json(400, map[string]any{"name": []string{"This field is required."}})
		return
	}
	i, err := c.a.s.CreateIssue(c.r.Context(), c.p, c.user.ID, in)
	if err != nil {
		c.fail(err)
		return
	}
	c.json(201, issueJSON(i))
}

func updateIssue(c *rc) {
	var in store.IssueInput
	if !c.decode(&in) {
		return
	}
	i, err := c.a.s.UpdateIssue(c.r.Context(), c.p, c.r.PathValue("id"), c.user.ID, in)
	if err != nil {
		c.fail(err)
		return
	}
	c.json(200, issueJSON(i))
}

func deleteIssue(c *rc) {
	i, ok := c.loadIssue("id")
	if !ok {
		return
	}
	if !c.isAdmin() && (i.CreatedBy == nil || *i.CreatedBy != c.user.ID) {
		c.json(403, map[string]any{"error": "Only admin or creator can delete the work item"})
		return
	}
	if err := c.a.s.DeleteIssue(c.r.Context(), c.p.ID, i.ID); err != nil {
		c.fail(err)
		return
	}
	c.noContent()
}

// --- comments ---

func listComments(c *rc) {
	i, ok := c.loadIssue("issue_id")
	if !ok {
		return
	}
	list, err := c.a.s.ListComments(c.r.Context(), i.ID)
	if err != nil {
		c.fail(err)
		return
	}
	listOf(c, list, func(x *store.Comment) map[string]any { return commentJSON(x, c.ws.ID) })
}

func getComment(c *rc) {
	i, ok := c.loadIssue("issue_id")
	if !ok {
		return
	}
	x, err := c.a.s.GetComment(c.r.Context(), i.ID, c.r.PathValue("id"))
	if err != nil {
		c.fail(err)
		return
	}
	c.one(200, commentJSON(x, c.ws.ID))
}

func createComment(c *rc) {
	i, ok := c.loadIssue("issue_id")
	if !ok {
		return
	}
	var in store.CommentInput
	if !c.decode(&in) {
		return
	}
	x, err := c.a.s.CreateComment(c.r.Context(), i, c.user.ID, in)
	if err != nil {
		c.fail(err)
		return
	}
	c.json(201, commentJSON(x, c.ws.ID))
}

// ownComment loads a comment the caller may change (author or admin).
func (c *rc) ownComment(verb string) (*store.Comment, bool) {
	i, ok := c.loadIssue("issue_id")
	if !ok {
		return nil, false
	}
	x, err := c.a.s.GetComment(c.r.Context(), i.ID, c.r.PathValue("id"))
	if err != nil {
		c.fail(err)
		return nil, false
	}
	if !c.isAdmin() && (x.ActorID == nil || *x.ActorID != c.user.ID) {
		c.json(403, map[string]any{"error": "You are not allowed to " + verb + " this comment"})
		return nil, false
	}
	return x, true
}

func updateComment(c *rc) {
	x, ok := c.ownComment("edit")
	if !ok {
		return
	}
	var in store.CommentInput
	if !c.decode(&in) {
		return
	}
	x, err := c.a.s.UpdateComment(c.r.Context(), x.IssueID, x.ID, c.user.ID, in)
	if err != nil {
		c.fail(err)
		return
	}
	c.json(200, commentJSON(x, c.ws.ID))
}

func deleteComment(c *rc) {
	x, ok := c.ownComment("delete")
	if !ok {
		return
	}
	if err := c.a.s.DeleteComment(c.r.Context(), x.IssueID, x.ID, c.user.ID); err != nil {
		c.fail(err)
		return
	}
	c.noContent()
}

// --- links ---

func listLinks(c *rc) {
	i, ok := c.loadIssue("issue_id")
	if !ok {
		return
	}
	list, err := c.a.s.ListLinks(c.r.Context(), i.ID)
	if err != nil {
		c.fail(err)
		return
	}
	listOf(c, list, func(l *store.Link) map[string]any { return linkJSON(l, c.ws.ID) })
}

func getLink(c *rc) {
	i, ok := c.loadIssue("issue_id")
	if !ok {
		return
	}
	l, err := c.a.s.GetLink(c.r.Context(), i.ID, c.r.PathValue("id"))
	if err != nil {
		c.fail(err)
		return
	}
	c.one(200, linkJSON(l, c.ws.ID))
}

func createLink(c *rc) {
	i, ok := c.loadIssue("issue_id")
	if !ok {
		return
	}
	var in store.LinkInput
	if !c.decode(&in) {
		return
	}
	l, err := c.a.s.CreateLink(c.r.Context(), i, c.user.ID, in)
	if err != nil {
		c.fail(err)
		return
	}
	c.json(201, linkJSON(l, c.ws.ID))
}

func updateLink(c *rc) {
	i, ok := c.loadIssue("issue_id")
	if !ok {
		return
	}
	var in store.LinkInput
	if !c.decode(&in) {
		return
	}
	l, err := c.a.s.UpdateLink(c.r.Context(), i.ID, c.r.PathValue("id"), c.user.ID, in)
	if err != nil {
		c.fail(err)
		return
	}
	c.json(200, linkJSON(l, c.ws.ID))
}

func deleteLink(c *rc) {
	i, ok := c.loadIssue("issue_id")
	if !ok {
		return
	}
	if err := c.a.s.DeleteLink(c.r.Context(), i.ID, c.r.PathValue("id"), c.user.ID); err != nil {
		c.fail(err)
		return
	}
	c.noContent()
}

// --- activities ---

func listActivities(c *rc) {
	i, ok := c.loadIssue("issue_id")
	if !ok {
		return
	}
	ob := c.r.URL.Query().Get("order_by")
	field := strings.TrimPrefix(ob, "-")
	if field != "created_at" && field != "updated_at" {
		ob, field = "created_at", "created_at"
	}
	list, err := c.a.s.ListActivities(c.r.Context(), i.ID, field == "updated_at", strings.HasPrefix(ob, "-"))
	if err != nil {
		c.fail(err)
		return
	}
	listOf(c, list, func(a *store.Activity) map[string]any { return activityJSON(a, c.ws.ID) })
}

func getActivity(c *rc) {
	i, ok := c.loadIssue("issue_id")
	if !ok {
		return
	}
	a, err := c.a.s.GetActivity(c.r.Context(), i.ID, c.r.PathValue("id"))
	if err != nil {
		c.json(404, map[string]any{"message": "Activity not found.", "code": "NOT_FOUND"})
		return
	}
	c.one(200, activityJSON(a, c.ws.ID))
}

// --- cycles ---

func listCycles(c *rc) {
	view := c.r.URL.Query().Get("cycle_view")
	list, err := c.a.s.ListCycles(c.r.Context(), c.p.ID, view)
	if err != nil {
		c.fail(err)
		return
	}
	objs := make([]map[string]any, len(list))
	for k, x := range list {
		objs[k] = cycleJSON(x, c.ws.ID)
	}
	if view == "current" { // Plane returns a plain array here
		out, err := c.shape(objs)
		if err != nil {
			c.fail(err)
			return
		}
		c.json(200, out)
		return
	}
	c.listAll(objs)
}

func getCycle(c *rc) {
	x, err := c.a.s.GetCycle(c.r.Context(), c.p.ID, c.r.PathValue("id"))
	if err != nil {
		c.fail(err)
		return
	}
	c.one(200, cycleJSON(x, c.ws.ID))
}

func createCycle(c *rc) {
	var in store.CycleInput
	if !c.decode(&in) {
		return
	}
	if !in.Name.Set {
		c.json(400, map[string]any{"name": []string{"This field is required."}})
		return
	}
	x, err := c.a.s.CreateCycle(c.r.Context(), c.p, c.user.ID, in)
	if err != nil {
		c.fail(err)
		return
	}
	c.json(201, cycleJSON(x, c.ws.ID))
}

func updateCycle(c *rc) {
	var in store.CycleInput
	if !c.decode(&in) {
		return
	}
	x, err := c.a.s.UpdateCycle(c.r.Context(), c.p, c.r.PathValue("id"), c.user.ID, in)
	if err != nil {
		c.fail(err)
		return
	}
	c.json(200, cycleJSON(x, c.ws.ID))
}

func deleteCycle(c *rc) {
	x, err := c.a.s.GetCycle(c.r.Context(), c.p.ID, c.r.PathValue("id"))
	if err != nil {
		c.fail(err)
		return
	}
	if !c.isAdmin() && (x.CreatedBy == nil || *x.CreatedBy != c.user.ID) {
		c.json(403, map[string]any{"error": "Only admin or creator can delete the cycle"})
		return
	}
	if err := c.a.s.DeleteCycle(c.r.Context(), c.p.ID, x.ID); err != nil {
		c.fail(err)
		return
	}
	c.noContent()
}

func listCycleIssues(c *rc) {
	x, err := c.a.s.GetCycle(c.r.Context(), c.p.ID, c.r.PathValue("id"))
	if err != nil {
		c.fail(err)
		return
	}
	f := c.issueFilter()
	f.Cycles = []string{x.ID}
	c.issuePage(f)
}

type issueIDs struct {
	Issues []string `json:"issues"`
}

func linkRows(c *rc, rows []*store.LinkRow, key string) {
	out := make([]map[string]any, len(rows))
	for k, r := range rows {
		out[k] = linkRowJSON(r, key, c.ws.ID)
	}
	c.json(200, out)
}

func addCycleIssues(c *rc) {
	var in issueIDs
	if !c.decode(&in) {
		return
	}
	rows, err := c.a.s.AddCycleIssues(c.r.Context(), c.p, c.r.PathValue("id"), c.user.ID, in.Issues)
	if err != nil {
		c.fail(err)
		return
	}
	linkRows(c, rows, "cycle")
}

func getCycleIssue(c *rc) {
	r, err := c.a.s.CycleIssue(c.r.Context(), c.r.PathValue("id"), c.r.PathValue("issue_id"))
	if err != nil || r.ProjectID != c.p.ID {
		c.fail(store.ErrNotFound)
		return
	}
	c.json(200, linkRowJSON(r, "cycle", c.ws.ID))
}

func removeCycleIssue(c *rc) {
	if err := c.a.s.RemoveCycleIssue(c.r.Context(), c.p, c.r.PathValue("id"), c.r.PathValue("issue_id"), c.user.ID); err != nil {
		c.fail(err)
		return
	}
	c.noContent()
}

// --- modules ---

func listModules(c *rc) {
	list, err := c.a.s.ListModules(c.r.Context(), c.p.ID)
	if err != nil {
		c.fail(err)
		return
	}
	listOf(c, list, func(m *store.Module) map[string]any { return moduleJSON(m, c.ws.ID) })
}

func getModule(c *rc) {
	m, err := c.a.s.GetModule(c.r.Context(), c.p.ID, c.r.PathValue("id"))
	if err != nil {
		c.fail(err)
		return
	}
	c.one(200, moduleJSON(m, c.ws.ID))
}

func createModule(c *rc) {
	var in store.ModuleInput
	if !c.decode(&in) {
		return
	}
	if !in.Name.Set {
		c.json(400, map[string]any{"name": []string{"This field is required."}})
		return
	}
	m, err := c.a.s.CreateModule(c.r.Context(), c.p, c.user.ID, in)
	if err != nil {
		c.fail(err)
		return
	}
	c.json(201, moduleJSON(m, c.ws.ID))
}

func updateModule(c *rc) {
	var in store.ModuleInput
	if !c.decode(&in) {
		return
	}
	m, err := c.a.s.UpdateModule(c.r.Context(), c.p, c.r.PathValue("id"), c.user.ID, in)
	if err != nil {
		c.fail(err)
		return
	}
	c.json(200, moduleJSON(m, c.ws.ID))
}

func deleteModule(c *rc) {
	m, err := c.a.s.GetModule(c.r.Context(), c.p.ID, c.r.PathValue("id"))
	if err != nil {
		c.fail(err)
		return
	}
	if !c.isAdmin() && (m.CreatedBy == nil || *m.CreatedBy != c.user.ID) {
		c.json(403, map[string]any{"error": "Only admin or creator can delete the module"})
		return
	}
	if err := c.a.s.DeleteModule(c.r.Context(), c.p.ID, m.ID); err != nil {
		c.fail(err)
		return
	}
	c.noContent()
}

func listModuleIssues(c *rc) {
	m, err := c.a.s.GetModule(c.r.Context(), c.p.ID, c.r.PathValue("id"))
	if err != nil {
		c.fail(err)
		return
	}
	f := c.issueFilter()
	f.Modules = []string{m.ID}
	c.issuePage(f)
}

func addModuleIssues(c *rc) {
	var in issueIDs
	if !c.decode(&in) {
		return
	}
	rows, err := c.a.s.AddModuleIssues(c.r.Context(), c.p, c.r.PathValue("id"), c.user.ID, in.Issues)
	if err != nil {
		c.fail(err)
		return
	}
	linkRows(c, rows, "module")
}

func removeModuleIssue(c *rc) {
	if err := c.a.s.RemoveModuleIssue(c.r.Context(), c.p, c.r.PathValue("id"), c.r.PathValue("issue_id"), c.user.ID); err != nil {
		c.fail(err)
		return
	}
	c.noContent()
}
