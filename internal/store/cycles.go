package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// GroupCounts counts linked work items per state group.
type GroupCounts struct {
	Total, Backlog, Unstarted, Started, Completed, Cancelled int
}

func (g *GroupCounts) add(group string, n int) {
	g.Total += n
	switch group {
	case "backlog":
		g.Backlog += n
	case "unstarted":
		g.Unstarted += n
	case "started":
		g.Started += n
	case "completed":
		g.Completed += n
	case "cancelled":
		g.Cancelled += n
	}
}

// loadCounts runs one grouped query for many cycles or modules. link is
// "cycle_issues"/"cycle_id" or "module_issues"/"module_id" (code constants).
func loadCounts(ctx context.Context, q querier, table, col string, ids []string) (map[string]*GroupCounts, error) {
	out := map[string]*GroupCounts{}
	for _, id := range ids {
		out[id] = &GroupCounts{}
	}
	if len(ids) == 0 {
		return out, nil
	}
	ph, args := in(ids)
	rows, err := q.QueryContext(ctx, `SELECT x.`+col+`, s.grp, COUNT(*) FROM `+table+` x JOIN issues i ON i.id = x.issue_id JOIN states s ON s.id = i.state_id
		WHERE x.`+col+` IN `+ph+` GROUP BY x.`+col+`, s.grp`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, grp string
		var n int
		if err := rows.Scan(&id, &grp, &n); err != nil {
			return nil, err
		}
		out[id].add(grp, n)
	}
	return out, rows.Err()
}

// Cycle is a time-boxed iteration. Dates are YYYY-MM-DD.
type Cycle struct {
	ID, ProjectID, Name, Description string
	StartDate, EndDate               *string
	OwnedBy                          *string
	Timezone                         string
	SortOrder                        float64
	ExternalSource, ExternalID       *string
	CreatedBy, UpdatedBy             *string
	CreatedAt, UpdatedAt             string
	Counts                           GroupCounts
}

// Ended reports whether the cycle's end date is before today (UTC).
func (c *Cycle) Ended() bool { return c.EndDate != nil && *c.EndDate < today() }

// CycleInput is the writable part of a cycle. Dates accept YYYY-MM-DD or an
// ISO datetime.
type CycleInput struct {
	Name           Opt[string]  `json:"name"`
	Description    Opt[string]  `json:"description"`
	StartDate      Opt[*string] `json:"start_date"`
	EndDate        Opt[*string] `json:"end_date"`
	OwnedBy        Opt[*string] `json:"owned_by"`
	Timezone       Opt[string]  `json:"timezone"`
	SortOrder      Opt[float64] `json:"sort_order"`
	ExternalSource Opt[*string] `json:"external_source"`
	ExternalID     Opt[*string] `json:"external_id"`
}

const cycleCols = `id, project_id, name, description, start_date, end_date, owned_by, timezone, sort_order, external_source, external_id,
	created_by, updated_by, created_at, updated_at`

func scanCycle(sc interface{ Scan(...any) error }) (*Cycle, error) {
	c := &Cycle{}
	err := sc.Scan(&c.ID, &c.ProjectID, &c.Name, &c.Description, &c.StartDate, &c.EndDate, &c.OwnedBy, &c.Timezone, &c.SortOrder,
		&c.ExternalSource, &c.ExternalID, &c.CreatedBy, &c.UpdatedBy, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return c, err
}

func queryCycles(ctx context.Context, q querier, where string, args ...any) ([]*Cycle, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+cycleCols+` FROM cycles WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Cycle{}
	var ids []string
	for rows.Next() {
		c, err := scanCycle(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
		ids = append(ids, c.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	counts, err := loadCounts(ctx, q, "cycle_issues", "cycle_id", ids)
	if err != nil {
		return nil, err
	}
	for _, c := range out {
		c.Counts = *counts[c.ID]
	}
	return out, nil
}

func getCycle(ctx context.Context, q querier, projectID, id string) (*Cycle, error) {
	list, err := queryCycles(ctx, q, `project_id = ? AND id = ?`, projectID, id)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, ErrNotFound
	}
	return list[0], nil
}

// GetCycle returns a cycle of the project.
func (s *Store) GetCycle(ctx context.Context, projectID, id string) (*Cycle, error) {
	return getCycle(ctx, s.r, projectID, id)
}

var cycleViews = map[string]string{
	"current":    ` AND start_date <= ? AND end_date >= ?`,
	"upcoming":   ` AND start_date > ?`,
	"completed":  ` AND end_date < ?`,
	"draft":      ` AND start_date IS NULL AND end_date IS NULL`,
	"incomplete": ` AND (end_date >= ? OR end_date IS NULL)`,
}

// ListCycles lists cycles, filtered by view (all, current, upcoming,
// completed, draft, incomplete; unknown = all).
func (s *Store) ListCycles(ctx context.Context, projectID, view string) ([]*Cycle, error) {
	return s.ListCyclesBy(ctx, projectID, view, "")
}

var cycleOrder = map[string]string{"created_at": "created_at", "updated_at": "updated_at", "name": "name",
	"start_date": "start_date", "end_date": "end_date", "sort_order": "sort_order"}

// ListCyclesBy is ListCycles with an allow-listed order_by (unknown or empty
// keeps the default, newest first).
func (s *Store) ListCyclesBy(ctx context.Context, projectID, view, orderBy string) ([]*Cycle, error) {
	cond := cycleViews[view]
	args := []any{projectID}
	for range strings.Count(cond, "?") {
		args = append(args, today())
	}
	return queryCycles(ctx, s.r, `project_id = ?`+cond+` ORDER BY `+orderClause(orderBy, cycleOrder, "created_at DESC")+`, id`, args...)
}

// CycleByRef finds a cycle by id or name (case-insensitive).
func (s *Store) CycleByRef(ctx context.Context, projectID, ref string) (*Cycle, error) {
	list, err := queryCycles(ctx, s.r, `project_id = ? AND (id = ? OR lower(name) = lower(?)) ORDER BY id = ? DESC LIMIT 1`, projectID, ref, ref, ref)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, ErrNotFound
	}
	return list[0], nil
}

// parseDay accepts YYYY-MM-DD or an ISO datetime and returns the date part.
func parseDay(field string, v *string) (*string, error) {
	if v == nil {
		return nil, nil
	}
	if _, err := time.Parse(time.DateOnly, *v); err == nil {
		return v, nil
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999", "2006-01-02T15:04:05", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, *v); err == nil {
			d := t.Format(time.DateOnly)
			return &d, nil
		}
	}
	return nil, invalid(field, "Datetime has wrong format. Use one of these formats instead: YYYY-MM-DD, YYYY-MM-DDThh:mm[:ss[.uuuuuu]][+HH:MM|-HH:MM|Z].")
}

func (in *CycleInput) apply(ctx context.Context, q querier, wsID string, c *Cycle) error {
	if in.Name.Set {
		c.Name = strings.TrimSpace(in.Name.V)
	}
	if in.Description.Set {
		c.Description = in.Description.V
	}
	var err error
	if in.StartDate.Set {
		if c.StartDate, err = parseDay("start_date", in.StartDate.V); err != nil {
			return err
		}
	}
	if in.EndDate.Set {
		if c.EndDate, err = parseDay("end_date", in.EndDate.V); err != nil {
			return err
		}
	}
	if in.OwnedBy.Set && in.OwnedBy.V != nil {
		ok, err := activeMembers(ctx, q, wsID, []string{*in.OwnedBy.V}, RoleGuest)
		if err != nil {
			return err
		}
		if !ok[*in.OwnedBy.V] {
			return invalid("owned_by", "The user is not a member of this workspace.")
		}
		c.OwnedBy = in.OwnedBy.V
	}
	if in.Timezone.Set {
		c.Timezone = in.Timezone.V
	}
	if in.SortOrder.Set {
		c.SortOrder = in.SortOrder.V
	}
	c.ExternalSource, c.ExternalID = mergeExt(c.ExternalSource, c.ExternalID, in.ExternalSource, in.ExternalID)
	switch {
	case c.Name == "":
		return invalid("name", "This field may not be blank.")
	case (c.StartDate == nil) != (c.EndDate == nil):
		return viewErr("Both start date and end date are either required or are to be null")
	case c.StartDate != nil && *c.StartDate > *c.EndDate:
		return viewErr("Start date cannot exceed end date")
	}
	return checkExternal(ctx, q, "cycles", "project_id", c.ProjectID, c.ExternalSource, c.ExternalID, c.ID, "Cycle")
}

// CreateCycle adds a cycle; the project must have cycles enabled.
func (s *Store) CreateCycle(ctx context.Context, p *Project, actor string, in CycleInput) (*Cycle, error) {
	if !p.CycleView {
		return nil, invalid("non_field_errors", "Cycles are not enabled for this project")
	}
	var out *Cycle
	err := s.tx(ctx, func(tx *sql.Tx) error {
		ts := now()
		c := &Cycle{ID: newID(), ProjectID: p.ID, OwnedBy: &actor, Timezone: "UTC", CreatedBy: &actor, CreatedAt: ts, UpdatedAt: ts}
		in.SortOrder = Opt[float64]{}
		if err := in.apply(ctx, tx, p.WorkspaceID, c); err != nil {
			return err
		}
		var minSort sql.NullFloat64
		if err := tx.QueryRowContext(ctx, `SELECT MIN(sort_order) FROM cycles WHERE project_id = ?`, p.ID).Scan(&minSort); err != nil {
			return err
		}
		c.SortOrder = 65535
		if minSort.Valid {
			c.SortOrder = minSort.Float64 - 10000
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO cycles (`+cycleCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			c.ID, c.ProjectID, c.Name, c.Description, c.StartDate, c.EndDate, c.OwnedBy, c.Timezone, c.SortOrder,
			c.ExternalSource, c.ExternalID, actor, nil, ts, ts)
		out = c
		return err
	})
	return out, err
}

// UpdateCycle applies a partial update. An ended cycle only accepts updates
// that include sort_order.
func (s *Store) UpdateCycle(ctx context.Context, p *Project, id, actor string, in CycleInput) (*Cycle, error) {
	var out *Cycle
	err := s.tx(ctx, func(tx *sql.Tx) error {
		c, err := getCycle(ctx, tx, p.ID, id)
		if err != nil {
			return err
		}
		if c.Ended() && !in.SortOrder.Set {
			return viewErr("The Cycle has already been completed so it cannot be edited")
		}
		if err := in.apply(ctx, tx, p.WorkspaceID, c); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE cycles SET name=?, description=?, start_date=?, end_date=?, owned_by=?, timezone=?, sort_order=?,
			external_source=?, external_id=?, updated_by=?, updated_at=? WHERE id=?`,
			c.Name, c.Description, c.StartDate, c.EndDate, c.OwnedBy, c.Timezone, c.SortOrder, c.ExternalSource, c.ExternalID, actor, now(), id)
		if err != nil {
			return err
		}
		out, err = getCycle(ctx, tx, p.ID, id)
		return err
	})
	return out, err
}

// DeleteCycle deletes a cycle; its work items stay, unlinked.
func (s *Store) DeleteCycle(ctx context.Context, projectID, id string) error {
	res, err := s.w.ExecContext(ctx, `DELETE FROM cycles WHERE project_id = ? AND id = ?`, projectID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// LinkRow is a cycle_issues or module_issues row. Parent is the cycle or
// module id.
type LinkRow struct {
	ID, ParentID, IssueID, ProjectID string
	CreatedBy, UpdatedBy             *string
	CreatedAt, UpdatedAt             string
	SubIssuesCount                   int
}

// listLinks reads link rows; table/col are code constants.
func listLinks(ctx context.Context, q querier, table, col, where string, args ...any) ([]*LinkRow, error) {
	rows, err := q.QueryContext(ctx, `SELECT x.id, x.`+col+`, x.issue_id, x.project_id, x.created_by, x.updated_by, x.created_at, x.updated_at,
		(SELECT COUNT(*) FROM issues c WHERE c.parent_id = x.issue_id) FROM `+table+` x WHERE `+where+` ORDER BY x.created_at, x.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*LinkRow{}
	for rows.Next() {
		r := &LinkRow{}
		if err := rows.Scan(&r.ID, &r.ParentID, &r.IssueID, &r.ProjectID, &r.CreatedBy, &r.UpdatedBy, &r.CreatedAt, &r.UpdatedAt, &r.SubIssuesCount); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AddCycleIssues moves the given work items into the cycle (out of any other
// cycle). Unknown ids are skipped. Returns all rows of the cycle.
func (s *Store) AddCycleIssues(ctx context.Context, p *Project, cycleID, actor string, issueIDs []string) ([]*LinkRow, error) {
	if len(issueIDs) == 0 {
		return nil, &Error{Kind: KindInvalid, Body: map[string]any{"error": "Work items are required", "code": "MISSING_WORK_ITEMS"}}
	}
	var out []*LinkRow
	err := s.tx(ctx, func(tx *sql.Tx) error {
		c, err := getCycle(ctx, tx, p.ID, cycleID)
		if err != nil {
			return err
		}
		if c.Ended() {
			return &Error{Kind: KindInvalid, Body: map[string]any{"code": "CYCLE_COMPLETED", "message": "The Cycle has already been completed so no new issues can be added"}}
		}
		ph, args := in(dedupe(issueIDs))
		issues, err := queryIssues(ctx, tx, ` WHERE i.project_id = ? AND i.id IN `+ph, append([]any{p.ID}, args...)...)
		if err != nil {
			return err
		}
		for _, i := range issues {
			if err := setIssueCycle(ctx, tx, i, &cycleID, actor); err != nil {
				return err
			}
		}
		out, err = listLinks(ctx, tx, "cycle_issues", "cycle_id", `x.cycle_id = ?`, cycleID)
		return err
	})
	return out, err
}

// CycleIssue returns the link row of one work item in the cycle.
func (s *Store) CycleIssue(ctx context.Context, cycleID, issueID string) (*LinkRow, error) {
	list, err := listLinks(ctx, s.r, "cycle_issues", "cycle_id", `x.cycle_id = ? AND x.issue_id = ?`, cycleID, issueID)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, ErrNotFound
	}
	return list[0], nil
}

// RemoveCycleIssue unlinks a work item from the cycle.
func (s *Store) RemoveCycleIssue(ctx context.Context, p *Project, cycleID, issueID, actor string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		i, err := getIssue(ctx, tx, p.ID, issueID)
		if err != nil {
			return err
		}
		if deref(i.CycleID) != cycleID {
			return ErrNotFound
		}
		return setIssueCycle(ctx, tx, i, nil, actor)
	})
}

// Module groups work items; an item may be in many modules.
type Module struct {
	ID, ProjectID, Name, Description string
	StartDate, TargetDate            *string
	Status                           string
	Lead                             *string
	Members                          []string
	SortOrder                        float64
	ExternalSource, ExternalID       *string
	CreatedBy, UpdatedBy             *string
	CreatedAt, UpdatedAt             string
	Counts                           GroupCounts
}

// ModuleInput is the writable part of a module. Members replaces the set.
type ModuleInput struct {
	Name           Opt[string]   `json:"name"`
	Description    Opt[string]   `json:"description"`
	StartDate      Opt[*string]  `json:"start_date"`
	TargetDate     Opt[*string]  `json:"target_date"`
	Status         Opt[string]   `json:"status"`
	Lead           Opt[*string]  `json:"lead"`
	Members        Opt[[]string] `json:"members"`
	ExternalSource Opt[*string]  `json:"external_source"`
	ExternalID     Opt[*string]  `json:"external_id"`
}

// ModuleStatuses are the allowed module statuses.
var ModuleStatuses = []string{"backlog", "planned", "in-progress", "paused", "completed", "cancelled"}

const moduleCols = `id, project_id, name, description, start_date, target_date, status, lead_id, sort_order, external_source, external_id,
	created_by, updated_by, created_at, updated_at`

func queryModules(ctx context.Context, q querier, where string, args ...any) ([]*Module, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+moduleCols+` FROM modules WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Module{}
	byID := map[string]*Module{}
	var ids []string
	for rows.Next() {
		m := &Module{Members: []string{}}
		if err := rows.Scan(&m.ID, &m.ProjectID, &m.Name, &m.Description, &m.StartDate, &m.TargetDate, &m.Status, &m.Lead, &m.SortOrder,
			&m.ExternalSource, &m.ExternalID, &m.CreatedBy, &m.UpdatedBy, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
		byID[m.ID] = m
		ids = append(ids, m.ID)
	}
	if err := rows.Err(); err != nil || len(ids) == 0 {
		return out, err
	}
	counts, err := loadCounts(ctx, q, "module_issues", "module_id", ids)
	if err != nil {
		return nil, err
	}
	ph, args2 := in(ids)
	mrows, err := q.QueryContext(ctx, `SELECT module_id, user_id FROM module_members WHERE module_id IN `+ph+` ORDER BY user_id`, args2...)
	if err != nil {
		return nil, err
	}
	defer mrows.Close()
	for mrows.Next() {
		var mid, uid string
		if err := mrows.Scan(&mid, &uid); err != nil {
			return nil, err
		}
		byID[mid].Members = append(byID[mid].Members, uid)
	}
	for _, m := range out {
		m.Counts = *counts[m.ID]
	}
	return out, mrows.Err()
}

func getModule(ctx context.Context, q querier, projectID, id string) (*Module, error) {
	list, err := queryModules(ctx, q, `project_id = ? AND id = ?`, projectID, id)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, ErrNotFound
	}
	return list[0], nil
}

// GetModule returns a module of the project.
func (s *Store) GetModule(ctx context.Context, projectID, id string) (*Module, error) {
	return getModule(ctx, s.r, projectID, id)
}

// ListModules lists a project's modules, newest first.
func (s *Store) ListModules(ctx context.Context, projectID string) ([]*Module, error) {
	return s.ListModulesBy(ctx, projectID, "")
}

var moduleOrder = map[string]string{"created_at": "created_at", "updated_at": "updated_at", "name": "name",
	"start_date": "start_date", "target_date": "target_date", "status": "status", "sort_order": "sort_order"}

// ListModulesBy is ListModules with an allow-listed order_by (unknown or
// empty keeps the default, newest first).
func (s *Store) ListModulesBy(ctx context.Context, projectID, orderBy string) ([]*Module, error) {
	return queryModules(ctx, s.r, `project_id = ? ORDER BY `+orderClause(orderBy, moduleOrder, "created_at DESC")+`, id`, projectID)
}

// ModuleByRef finds a module by id or name (case-insensitive).
func (s *Store) ModuleByRef(ctx context.Context, projectID, ref string) (*Module, error) {
	list, err := queryModules(ctx, s.r, `project_id = ? AND (id = ? OR lower(name) = lower(?)) ORDER BY id = ? DESC LIMIT 1`, projectID, ref, ref, ref)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, ErrNotFound
	}
	return list[0], nil
}

func (in *ModuleInput) apply(ctx context.Context, q querier, wsID string, m *Module, creating bool) ([]string, error) {
	if in.Name.Set {
		m.Name = strings.TrimSpace(in.Name.V)
	}
	if in.Description.Set {
		m.Description = in.Description.V
	}
	if in.StartDate.Set {
		m.StartDate = in.StartDate.V
	}
	if in.TargetDate.Set {
		m.TargetDate = in.TargetDate.V
	}
	if in.Status.Set {
		m.Status = in.Status.V
	}
	if in.Lead.Set {
		m.Lead = in.Lead.V
	}
	m.ExternalSource, m.ExternalID = mergeExt(m.ExternalSource, m.ExternalID, in.ExternalSource, in.ExternalID)
	if m.Name == "" {
		return nil, invalid("name", "This field may not be blank.")
	}
	if err := validDate("start_date", m.StartDate); err != nil {
		return nil, err
	}
	if err := validDate("target_date", m.TargetDate); err != nil {
		return nil, err
	}
	if m.StartDate != nil && m.TargetDate != nil && *m.StartDate > *m.TargetDate {
		return nil, invalid("non_field_errors", "Start date cannot exceed target date")
	}
	okStatus := false
	for _, st := range ModuleStatuses {
		okStatus = okStatus || st == m.Status
	}
	if !okStatus {
		return nil, invalid("status", `"`+m.Status+`" is not a valid choice.`)
	}
	if m.Lead != nil && in.Lead.Set {
		ok, err := activeMembers(ctx, q, wsID, []string{*m.Lead}, RoleGuest)
		if err != nil {
			return nil, err
		}
		if !ok[*m.Lead] {
			return nil, invalid("lead", "The user is not a member of this workspace.")
		}
	}
	var dup string
	if err := q.QueryRowContext(ctx, `SELECT id FROM modules WHERE project_id = ? AND name = ? AND id != ?`, m.ProjectID, m.Name, m.ID).Scan(&dup); err == nil {
		if creating {
			msg := "Module with this name already exists"
			return nil, &Error{Kind: KindInvalid, Body: map[string]any{"id": dup, "code": "MODULE_NAME_ALREADY_EXISTS", "error": msg, "message": msg}}
		}
		return nil, viewErr("Module with this name already exists")
	}
	if err := checkExternal(ctx, q, "modules", "project_id", m.ProjectID, m.ExternalSource, m.ExternalID, m.ID, "Module"); err != nil {
		return nil, err
	}
	if !in.Members.Set {
		return nil, nil
	}
	members := dedupe(in.Members.V)
	ok, err := activeMembers(ctx, q, wsID, members, RoleGuest)
	if err != nil {
		return nil, err
	}
	kept := []string{}
	for _, id := range members {
		if ok[id] {
			kept = append(kept, id)
		}
	}
	return kept, nil
}

func writeModuleMembers(ctx context.Context, tx *sql.Tx, moduleID string, members []string) error {
	if members == nil {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM module_members WHERE module_id = ?`, moduleID); err != nil {
		return err
	}
	for _, u := range members {
		if _, err := tx.ExecContext(ctx, `INSERT INTO module_members (module_id, user_id) VALUES (?, ?)`, moduleID, u); err != nil {
			return err
		}
	}
	return nil
}

// CreateModule adds a module; the project must have modules enabled.
func (s *Store) CreateModule(ctx context.Context, p *Project, actor string, in ModuleInput) (*Module, error) {
	if !p.ModuleView {
		return nil, invalid("non_field_errors", "Modules are not enabled for this project")
	}
	var out *Module
	err := s.tx(ctx, func(tx *sql.Tx) error {
		ts := now()
		m := &Module{ID: newID(), ProjectID: p.ID, Status: "planned", CreatedBy: &actor, CreatedAt: ts, UpdatedAt: ts}
		members, err := in.apply(ctx, tx, p.WorkspaceID, m, true)
		if err != nil {
			return err
		}
		var minSort sql.NullFloat64
		if err := tx.QueryRowContext(ctx, `SELECT MIN(sort_order) FROM modules WHERE project_id = ?`, p.ID).Scan(&minSort); err != nil {
			return err
		}
		m.SortOrder = 65535
		if minSort.Valid {
			m.SortOrder = minSort.Float64 - 10000
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO modules (`+moduleCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			m.ID, m.ProjectID, m.Name, m.Description, m.StartDate, m.TargetDate, m.Status, m.Lead, m.SortOrder,
			m.ExternalSource, m.ExternalID, actor, nil, ts, ts)
		if err != nil {
			return err
		}
		if err := writeModuleMembers(ctx, tx, m.ID, members); err != nil {
			return err
		}
		out, err = getModule(ctx, tx, p.ID, m.ID)
		return err
	})
	return out, err
}

// UpdateModule applies a partial update.
func (s *Store) UpdateModule(ctx context.Context, p *Project, id, actor string, in ModuleInput) (*Module, error) {
	var out *Module
	err := s.tx(ctx, func(tx *sql.Tx) error {
		m, err := getModule(ctx, tx, p.ID, id)
		if err != nil {
			return err
		}
		members, err := in.apply(ctx, tx, p.WorkspaceID, m, false)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE modules SET name=?, description=?, start_date=?, target_date=?, status=?, lead_id=?,
			external_source=?, external_id=?, updated_by=?, updated_at=? WHERE id=?`,
			m.Name, m.Description, m.StartDate, m.TargetDate, m.Status, m.Lead, m.ExternalSource, m.ExternalID, actor, now(), id)
		if err != nil {
			return err
		}
		if err := writeModuleMembers(ctx, tx, id, members); err != nil {
			return err
		}
		out, err = getModule(ctx, tx, p.ID, id)
		return err
	})
	return out, err
}

// DeleteModule deletes a module; its work items stay.
func (s *Store) DeleteModule(ctx context.Context, projectID, id string) error {
	res, err := s.w.ExecContext(ctx, `DELETE FROM modules WHERE project_id = ? AND id = ?`, projectID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// AddModuleIssues adds work items to the module (never removing them from
// other modules). Unknown ids are skipped. Returns all rows of the module.
func (s *Store) AddModuleIssues(ctx context.Context, p *Project, moduleID, actor string, issueIDs []string) ([]*LinkRow, error) {
	if len(issueIDs) == 0 {
		return nil, viewErr("Issues are required")
	}
	var out []*LinkRow
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := getModule(ctx, tx, p.ID, moduleID); err != nil {
			return err
		}
		ph, args := in(dedupe(issueIDs))
		issues, err := queryIssues(ctx, tx, ` WHERE i.project_id = ? AND i.id IN `+ph, append([]any{p.ID}, args...)...)
		if err != nil {
			return err
		}
		for _, i := range issues {
			if err := addIssueModule(ctx, tx, i, moduleID, actor); err != nil {
				return err
			}
		}
		out, err = listLinks(ctx, tx, "module_issues", "module_id", `x.module_id = ?`, moduleID)
		return err
	})
	return out, err
}

// RemoveModuleIssue unlinks a work item from the module.
func (s *Store) RemoveModuleIssue(ctx context.Context, p *Project, moduleID, issueID, actor string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		i, err := getIssue(ctx, tx, p.ID, issueID)
		if err != nil {
			return err
		}
		return removeIssueModule(ctx, tx, i, moduleID, actor)
	})
}
