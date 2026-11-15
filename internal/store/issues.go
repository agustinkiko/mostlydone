package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Issue is a work item.
type Issue struct {
	ID, ProjectID, WorkspaceID, ProjectIdentifier string
	SequenceID                                    int
	Name, DescriptionHTML                         string
	StateID                                       string
	Priority                                      string
	ParentID                                      *string
	StartDate, TargetDate                         *string
	SortOrder                                     float64
	CompletedAt                                   *string
	ExternalSource, ExternalID                    *string
	CreatedBy, UpdatedBy                          *string
	CreatedAt, UpdatedAt                          string
	CycleID                                       *string
	SubIssuesCount                                int
	Assignees, Labels, ModuleIDs                  []string
}

// IssueInput is the writable part of a work item. CycleID and ModuleIDs are
// extensions to Plane's shape; ModuleIDs replaces the whole set.
type IssueInput struct {
	Name            Opt[string]   `json:"name"`
	DescriptionHTML Opt[string]   `json:"description_html"`
	State           Opt[*string]  `json:"state"`
	Priority        Opt[string]   `json:"priority"`
	Parent          Opt[*string]  `json:"parent"`
	StartDate       Opt[*string]  `json:"start_date"`
	TargetDate      Opt[*string]  `json:"target_date"`
	SortOrder       Opt[float64]  `json:"sort_order"`
	Assignees       Opt[[]string] `json:"assignees"`
	Labels          Opt[[]string] `json:"labels"`
	CycleID         Opt[*string]  `json:"cycle_id"`
	ModuleIDs       Opt[[]string] `json:"module_ids"`
	ExternalSource  Opt[*string]  `json:"external_source"`
	ExternalID      Opt[*string]  `json:"external_id"`
}

// Priorities in sort order.
var Priorities = []string{"urgent", "high", "medium", "low", "none"}

const issueCols = `i.id, i.project_id, p.workspace_id, p.identifier, i.sequence_id, i.name, i.description_html, i.state_id, i.priority, i.parent_id,
	i.start_date, i.target_date, i.sort_order, i.completed_at, i.external_source, i.external_id, i.created_by, i.updated_by, i.created_at, i.updated_at,
	(SELECT ci.cycle_id FROM cycle_issues ci WHERE ci.issue_id = i.id),
	(SELECT COUNT(*) FROM issues c WHERE c.parent_id = i.id)`

const issueFrom = ` FROM issues i JOIN projects p ON p.id = i.project_id AND p.deleted_at IS NULL JOIN states s ON s.id = i.state_id`

func scanIssue(sc interface{ Scan(...any) error }) (*Issue, error) {
	i := &Issue{}
	err := sc.Scan(&i.ID, &i.ProjectID, &i.WorkspaceID, &i.ProjectIdentifier, &i.SequenceID, &i.Name, &i.DescriptionHTML, &i.StateID, &i.Priority, &i.ParentID,
		&i.StartDate, &i.TargetDate, &i.SortOrder, &i.CompletedAt, &i.ExternalSource, &i.ExternalID, &i.CreatedBy, &i.UpdatedBy, &i.CreatedAt, &i.UpdatedAt,
		&i.CycleID, &i.SubIssuesCount)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return i, err
}

func queryIssues(ctx context.Context, q querier, rest string, args ...any) ([]*Issue, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+issueCols+issueFrom+rest, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Issue{}
	for rows.Next() {
		i, err := scanIssue(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, loadRelations(ctx, q, out)
}

// loadRelations fills assignees, labels and modules for a page of issues with
// one query each.
func loadRelations(ctx context.Context, q querier, issues []*Issue) error {
	if len(issues) == 0 {
		return nil
	}
	byID := make(map[string]*Issue, len(issues))
	ids := make([]string, len(issues))
	for k, i := range issues {
		byID[i.ID] = i
		ids[k] = i.ID
		i.Assignees, i.Labels, i.ModuleIDs = []string{}, []string{}, []string{}
	}
	ph, args := in(ids)
	for _, rel := range []struct {
		sql string
		dst func(*Issue, string)
	}{
		{`SELECT issue_id, user_id FROM issue_assignees WHERE issue_id IN ` + ph + ` ORDER BY user_id`, func(i *Issue, v string) { i.Assignees = append(i.Assignees, v) }},
		{`SELECT issue_id, label_id FROM issue_labels WHERE issue_id IN ` + ph + ` ORDER BY label_id`, func(i *Issue, v string) { i.Labels = append(i.Labels, v) }},
		{`SELECT issue_id, module_id FROM module_issues WHERE issue_id IN ` + ph + ` ORDER BY created_at`, func(i *Issue, v string) { i.ModuleIDs = append(i.ModuleIDs, v) }},
	} {
		rows, err := q.QueryContext(ctx, rel.sql, args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var iid, v string
			if err := rows.Scan(&iid, &v); err != nil {
				rows.Close()
				return err
			}
			rel.dst(byID[iid], v)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
	}
	return nil
}

func getIssue(ctx context.Context, q querier, projectID, id string) (*Issue, error) {
	list, err := queryIssues(ctx, q, ` WHERE i.project_id = ? AND i.id = ?`, projectID, id)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, ErrNotFound
	}
	return list[0], nil
}

// GetIssue returns a work item of the project.
func (s *Store) GetIssue(ctx context.Context, projectID, id string) (*Issue, error) {
	return getIssue(ctx, s.r, projectID, id)
}

// IssueByIdentifier finds a work item by project identifier and sequence id.
func (s *Store) IssueByIdentifier(ctx context.Context, wsID, identifier string, seq int) (*Issue, error) {
	list, err := queryIssues(ctx, s.r, ` WHERE p.workspace_id = ? AND p.identifier = ? AND i.sequence_id = ?`, wsID, strings.ToUpper(identifier), seq)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, ErrNotFound
	}
	return list[0], nil
}

// IssuesByIDs batch-loads work items (with relations).
func (s *Store) IssuesByIDs(ctx context.Context, ids []string) (map[string]*Issue, error) {
	out := map[string]*Issue{}
	if len(ids) == 0 {
		return out, nil
	}
	ph, args := in(dedupe(ids))
	list, err := queryIssues(ctx, s.r, ` WHERE i.id IN `+ph, args...)
	for _, i := range list {
		out[i.ID] = i
	}
	return out, err
}

// IssueFilter selects work items. Slice fields mean "any of".
type IssueFilter struct {
	ProjectID                  string
	States, StateGroups        []string
	Priorities                 []string
	Assignees, Labels          []string
	Cycles, Modules, Parents   []string
	Search                     string
	ExternalSource, ExternalID string
	OrderBy                    string
	Page                       Page
}

var issueOrder = map[string]string{
	"created_at":                 "i.created_at",
	"updated_at":                 "i.updated_at",
	"sequence_id":                "i.sequence_id",
	"sort_order":                 "i.sort_order",
	"target_date":                "i.target_date",
	"start_date":                 "i.start_date",
	"completed_at":               "i.completed_at",
	"archived_at":                "NULL",
	"priority":                   "CASE i.priority WHEN 'urgent' THEN 0 WHEN 'high' THEN 1 WHEN 'medium' THEN 2 WHEN 'low' THEN 3 ELSE 4 END",
	"state__name":                "s.name",
	"state__group":               "CASE s.grp WHEN 'backlog' THEN 0 WHEN 'unstarted' THEN 1 WHEN 'started' THEN 2 WHEN 'completed' THEN 3 ELSE 4 END",
	"assignees__first_name":      "(SELECT MIN(u.first_name) FROM issue_assignees a JOIN users u ON u.id = a.user_id WHERE a.issue_id = i.id)",
	"labels__name":               "(SELECT MIN(l.name) FROM issue_labels il JOIN labels l ON l.id = il.label_id WHERE il.issue_id = i.id)",
	"issue_module__module__name": "(SELECT MIN(m.name) FROM module_issues mi JOIN modules m ON m.id = mi.module_id WHERE mi.issue_id = i.id)",
}

func (f IssueFilter) where() (string, []any) {
	var b strings.Builder
	args := []any{f.ProjectID}
	b.WriteString(` WHERE i.project_id = ?`)
	add := func(vals []string, tmpl string) {
		if len(vals) == 0 {
			return
		}
		ph, a := in(vals)
		b.WriteString(" AND " + strings.ReplaceAll(tmpl, "$IN", ph))
		args = append(args, a...)
	}
	add(f.States, `i.state_id IN $IN`)
	add(f.StateGroups, `s.grp IN $IN`)
	add(f.Priorities, `i.priority IN $IN`)
	add(f.Parents, `i.parent_id IN $IN`)
	add(f.Assignees, `EXISTS (SELECT 1 FROM issue_assignees x WHERE x.issue_id = i.id AND x.user_id IN $IN)`)
	add(f.Labels, `EXISTS (SELECT 1 FROM issue_labels x WHERE x.issue_id = i.id AND x.label_id IN $IN)`)
	add(f.Cycles, `EXISTS (SELECT 1 FROM cycle_issues x WHERE x.issue_id = i.id AND x.cycle_id IN $IN)`)
	add(f.Modules, `EXISTS (SELECT 1 FROM module_issues x WHERE x.issue_id = i.id AND x.module_id IN $IN)`)
	if f.ExternalSource != "" || f.ExternalID != "" {
		b.WriteString(` AND i.external_source = ? AND i.external_id = ?`)
		args = append(args, f.ExternalSource, f.ExternalID)
	}
	if q := strings.TrimSpace(f.Search); q != "" {
		b.WriteString(` AND (i.name LIKE ? ESCAPE '\'`)
		args = append(args, "%"+escapeLike(q)+"%")
		for _, n := range intTokens(q) {
			b.WriteString(` OR i.sequence_id = ?`)
			args = append(args, n)
		}
		b.WriteString(`)`)
	}
	return b.String(), args
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// intTokens returns the standalone integers in s ("PROJ-12 x 7" -> 12, 7).
func intTokens(s string) []int {
	var out []int
	for _, tok := range strings.FieldsFunc(s, func(r rune) bool { return r < '0' || r > '9' }) {
		if n, err := strconv.Atoi(tok); err == nil {
			out = append(out, n)
		}
	}
	return out
}

// ListIssues returns one page of matching work items and the total count.
func (s *Store) ListIssues(ctx context.Context, f IssueFilter) ([]*Issue, int, error) {
	where, args := f.where()
	var total int
	if err := s.r.QueryRowContext(ctx, `SELECT COUNT(*)`+issueFrom+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	// Pick the page's ids first so the per-row subqueries in issueCols run
	// for one page only, not for every matching row before the sort.
	order := orderClause(f.OrderBy, issueOrder, "i.created_at DESC")
	rows, err := s.r.QueryContext(ctx, `SELECT i.id`+issueFrom+where+` ORDER BY `+order+`, i.created_at DESC, i.id`+f.Page.clause(), args...)
	if err != nil {
		return nil, 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil || len(ids) == 0 {
		return []*Issue{}, total, err
	}
	ph, idArgs := in(ids)
	list, err := queryIssues(ctx, s.r, ` WHERE i.id IN `+ph, idArgs...)
	if err != nil {
		return nil, 0, err
	}
	pos := make(map[string]int, len(ids))
	for k, id := range ids {
		pos[id] = k
	}
	sort.Slice(list, func(a, b int) bool { return pos[list[a].ID] < pos[list[b].ID] })
	return list, total, nil
}

// SearchHit is one result of SearchIssues.
type SearchHit struct {
	ID, Name, ProjectID, ProjectIdentifier, WorkspaceSlug string
	SequenceID                                            int
}

// SearchIssues matches name or project identifier (substring), or a sequence
// id equal to any integer in the query.
func (s *Store) SearchIssues(ctx context.Context, wsID, query, projectID string, limit int) ([]SearchHit, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return []SearchHit{}, nil
	}
	like := "%" + escapeLike(q) + "%"
	cond := `(i.name LIKE ? ESCAPE '\' OR p.identifier LIKE ? ESCAPE '\'`
	args := []any{wsID, like, like}
	for _, n := range intTokens(q) {
		cond += ` OR i.sequence_id = ?`
		args = append(args, n)
	}
	cond += `)`
	if projectID != "" {
		cond += ` AND i.project_id = ?`
		args = append(args, projectID)
	}
	args = append(args, limit)
	rows, err := s.r.QueryContext(ctx, `SELECT i.id, i.name, i.project_id, p.identifier, w.slug, i.sequence_id
		FROM issues i JOIN projects p ON p.id = i.project_id JOIN workspaces w ON w.id = p.workspace_id
		WHERE p.deleted_at IS NULL AND p.workspace_id = ? AND `+cond+` ORDER BY i.created_at DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SearchHit{}
	for rows.Next() {
		var h SearchHit
		if err := rows.Scan(&h.ID, &h.Name, &h.ProjectID, &h.ProjectIdentifier, &h.WorkspaceSlug, &h.SequenceID); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// issueChange carries what apply decided, for writing rows and activities.
type issueChange struct {
	assignees, labels, modules []string
	setAssignees, setLabels    bool
	setModules, setCycle       bool
	cycle                      *string
}

func validPriority(p string) bool {
	for _, x := range Priorities {
		if p == x {
			return true
		}
	}
	return false
}

// apply validates in against project p and merges it into i.
func (in *IssueInput) apply(ctx context.Context, q querier, p *Project, i *Issue, creating bool) (*issueChange, error) {
	ch := &issueChange{}
	if in.Name.Set {
		i.Name = strings.TrimSpace(in.Name.V)
	}
	if i.Name == "" {
		return nil, invalid("name", "This field may not be blank.")
	}
	if len(i.Name) > 255 {
		return nil, invalid("name", "Ensure this field has no more than 255 characters.")
	}
	if in.DescriptionHTML.Set {
		i.DescriptionHTML = SanitizeHTML(in.DescriptionHTML.V)
	}
	if in.Priority.Set {
		i.Priority = in.Priority.V
	}
	if !validPriority(i.Priority) {
		return nil, invalid("priority", `"`+i.Priority+`" is not a valid choice.`)
	}
	if in.State.Set && in.State.V != nil {
		var n int
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM states WHERE id = ? AND project_id = ?`, *in.State.V, p.ID).Scan(&n); err != nil {
			return nil, err
		}
		if n == 0 {
			return nil, invalid("non_field_errors", "State is not valid please pass a valid state_id")
		}
		i.StateID = *in.State.V
	} else if i.StateID == "" || (in.State.Set && in.State.V == nil) {
		err := q.QueryRowContext(ctx, `SELECT id FROM states WHERE project_id = ? ORDER BY is_default DESC, sequence LIMIT 1`, p.ID).Scan(&i.StateID)
		if err != nil {
			return nil, err
		}
	}
	if in.Parent.Set {
		i.ParentID = in.Parent.V
	}
	if i.ParentID != nil && in.Parent.Set {
		// The new parent must be in the project and must not be this item or
		// one of its descendants.
		var n, loop int
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM issues WHERE id = ? AND project_id = ?`, *i.ParentID, p.ID).Scan(&n); err != nil {
			return nil, err
		}
		if err := q.QueryRowContext(ctx, `WITH RECURSIVE anc(id) AS (SELECT ? UNION SELECT x.parent_id FROM issues x JOIN anc ON x.id = anc.id WHERE x.parent_id IS NOT NULL)
			SELECT COUNT(*) FROM anc WHERE id = ?`, *i.ParentID, i.ID).Scan(&loop); err != nil {
			return nil, err
		}
		if n == 0 || loop > 0 {
			return nil, invalid("parent", "Parent must be another work item in this project.")
		}
	}
	if in.StartDate.Set {
		i.StartDate = in.StartDate.V
	}
	if in.TargetDate.Set {
		i.TargetDate = in.TargetDate.V
	}
	if err := validDate("start_date", i.StartDate); err != nil {
		return nil, err
	}
	if err := validDate("target_date", i.TargetDate); err != nil {
		return nil, err
	}
	if i.StartDate != nil && i.TargetDate != nil && *i.StartDate > *i.TargetDate {
		return nil, invalid("non_field_errors", "Start date cannot exceed target date")
	}
	if in.SortOrder.Set {
		i.SortOrder = in.SortOrder.V
	}
	if in.Assignees.Set {
		ch.setAssignees, ch.assignees = true, dedupe(in.Assignees.V)
	} else if creating && p.DefaultAssignee != nil {
		ch.setAssignees, ch.assignees = true, []string{*p.DefaultAssignee}
	}
	if ch.setAssignees {
		ok, err := activeMembers(ctx, q, p.WorkspaceID, ch.assignees, RoleMember)
		if err != nil {
			return nil, err
		}
		var bad []string
		for _, a := range ch.assignees {
			if !ok[a] {
				bad = append(bad, a)
			}
		}
		if len(bad) > 0 {
			if !in.Assignees.Set { // stale default assignee: just skip it
				ch.assignees = nil
			} else {
				return nil, invalid("non_field_errors", fmt.Sprintf("Assignees %v are not active members of this project", bad))
			}
		}
	}
	if in.Labels.Set {
		ch.setLabels, ch.labels = true, dedupe(in.Labels.V)
		if bad, err := notInProject(ctx, q, "labels", p.ID, ch.labels); err != nil {
			return nil, err
		} else if len(bad) > 0 {
			return nil, invalid("non_field_errors", fmt.Sprintf("Labels %v do not belong to this project", bad))
		}
	}
	if in.CycleID.Set {
		ch.setCycle, ch.cycle = true, in.CycleID.V
		if ch.cycle != nil {
			if bad, err := notInProject(ctx, q, "cycles", p.ID, []string{*ch.cycle}); err != nil {
				return nil, err
			} else if len(bad) > 0 {
				return nil, invalid("cycle_id", "Cycle does not belong to this project.")
			}
		}
	}
	if in.ModuleIDs.Set {
		ch.setModules, ch.modules = true, dedupe(in.ModuleIDs.V)
		if bad, err := notInProject(ctx, q, "modules", p.ID, ch.modules); err != nil {
			return nil, err
		} else if len(bad) > 0 {
			return nil, invalid("module_ids", fmt.Sprintf("Modules %v do not belong to this project", bad))
		}
	}
	i.ExternalSource, i.ExternalID = mergeExt(i.ExternalSource, i.ExternalID, in.ExternalSource, in.ExternalID)
	if err := checkExternal(ctx, q, "issues", "project_id", p.ID, i.ExternalSource, i.ExternalID, i.ID, "Issue"); err != nil {
		return nil, err
	}
	return ch, nil
}

// notInProject returns the ids that are not rows of table in the project.
// table is a code constant.
func notInProject(ctx context.Context, q querier, table, projectID string, ids []string) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	ph, args := in(ids)
	rows, err := q.QueryContext(ctx, `SELECT id FROM `+table+` WHERE project_id = ? AND id IN `+ph, append([]any{projectID}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	found := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		found[id] = true
	}
	var bad []string
	for _, id := range ids {
		if !found[id] {
			bad = append(bad, id)
		}
	}
	return bad, rows.Err()
}

func stateGroup(ctx context.Context, q querier, stateID string) (string, string, error) {
	var grp, name string
	err := q.QueryRowContext(ctx, `SELECT grp, name FROM states WHERE id = ?`, stateID).Scan(&grp, &name)
	return grp, name, err
}

// CreateIssue creates a work item. The sequence id comes from the project's
// counter in the same transaction.
func (s *Store) CreateIssue(ctx context.Context, p *Project, actor string, in IssueInput) (*Issue, error) {
	var out *Issue
	err := s.tx(ctx, func(tx *sql.Tx) error {
		ts := now()
		i := &Issue{ID: newID(), ProjectID: p.ID, DescriptionHTML: "<p></p>", Priority: "none", CreatedBy: &actor, CreatedAt: ts, UpdatedAt: ts}
		ch, err := in.apply(ctx, tx, p, i, true)
		if err != nil {
			return err
		}
		if !in.SortOrder.Set {
			var maxSort sql.NullFloat64
			if err := tx.QueryRowContext(ctx, `SELECT MAX(sort_order) FROM issues WHERE project_id = ? AND state_id = ?`, p.ID, i.StateID).Scan(&maxSort); err != nil {
				return err
			}
			i.SortOrder = 65535
			if maxSort.Valid {
				i.SortOrder = maxSort.Float64 + 10000
			}
		}
		if err := tx.QueryRowContext(ctx, `UPDATE projects SET next_sequence = next_sequence + 1 WHERE id = ? RETURNING next_sequence - 1`, p.ID).Scan(&i.SequenceID); err != nil {
			return err
		}
		if grp, _, err := stateGroup(ctx, tx, i.StateID); err != nil {
			return err
		} else if grp == "completed" {
			i.CompletedAt = &ts
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO issues (id, project_id, sequence_id, name, description_html, state_id, priority, parent_id,
			start_date, target_date, sort_order, completed_at, external_source, external_id, created_by, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			i.ID, i.ProjectID, i.SequenceID, i.Name, i.DescriptionHTML, i.StateID, i.Priority, i.ParentID,
			i.StartDate, i.TargetDate, i.SortOrder, i.CompletedAt, i.ExternalSource, i.ExternalID, actor, ts, ts)
		if err != nil {
			return err
		}
		if err := logActivity(ctx, tx, &Activity{IssueID: i.ID, ProjectID: p.ID, ActorID: &actor, Verb: "created", Comment: "created the work item"}); err != nil {
			return err
		}
		if err := writeIssueRelations(ctx, tx, p, i, ch, actor, false); err != nil {
			return err
		}
		out, err = getIssue(ctx, tx, p.ID, i.ID)
		return err
	})
	return out, err
}

// UpdateIssue applies a partial update and records one activity per change.
func (s *Store) UpdateIssue(ctx context.Context, p *Project, id, actor string, in IssueInput) (*Issue, error) {
	var out *Issue
	err := s.tx(ctx, func(tx *sql.Tx) error {
		old, err := getIssue(ctx, tx, p.ID, id)
		if err != nil {
			return err
		}
		i := *old
		ch, err := in.apply(ctx, tx, p, &i, false)
		if err != nil {
			return err
		}
		ts := now()
		act := func(field string, oldV, newV, oldID, newID *string) error {
			return logActivity(ctx, tx, &Activity{IssueID: id, ProjectID: p.ID, ActorID: &actor, Verb: "updated", Field: &field,
				OldValue: oldV, NewValue: newV, OldIdentifier: oldID, NewIdentifier: newID, Comment: "updated the " + strings.ReplaceAll(field, "_", " ")})
		}
		if i.StateID != old.StateID {
			oldGrp, oldName, err := stateGroup(ctx, tx, old.StateID)
			if err != nil {
				return err
			}
			grp, name, err := stateGroup(ctx, tx, i.StateID)
			if err != nil {
				return err
			}
			if grp == "completed" && oldGrp != "completed" {
				i.CompletedAt = &ts
			} else if grp != "completed" {
				i.CompletedAt = nil
			}
			if err := act("state", &oldName, &name, &old.StateID, &i.StateID); err != nil {
				return err
			}
		}
		for _, c := range []struct {
			field    string
			old, new *string
		}{
			{"name", &old.Name, &i.Name},
			{"description", &old.DescriptionHTML, &i.DescriptionHTML},
			{"priority", &old.Priority, &i.Priority},
			{"start_date", old.StartDate, i.StartDate},
			{"target_date", old.TargetDate, i.TargetDate},
		} {
			if deref(c.old) != deref(c.new) {
				if err := act(c.field, c.old, c.new, nil, nil); err != nil {
					return err
				}
			}
		}
		if deref(old.ParentID) != deref(i.ParentID) {
			if err := act("parent", issueRef(ctx, tx, old.ParentID), issueRef(ctx, tx, i.ParentID), old.ParentID, i.ParentID); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, `UPDATE issues SET name=?, description_html=?, state_id=?, priority=?, parent_id=?, start_date=?, target_date=?,
			sort_order=?, completed_at=?, external_source=?, external_id=?, updated_by=?, updated_at=? WHERE id=?`,
			i.Name, i.DescriptionHTML, i.StateID, i.Priority, i.ParentID, i.StartDate, i.TargetDate,
			i.SortOrder, i.CompletedAt, i.ExternalSource, i.ExternalID, actor, ts, id)
		if err != nil {
			return err
		}
		i.Assignees, i.Labels, i.ModuleIDs = old.Assignees, old.Labels, old.ModuleIDs
		if err := writeIssueRelations(ctx, tx, p, &i, ch, actor, true); err != nil {
			return err
		}
		out, err = getIssue(ctx, tx, p.ID, id)
		return err
	})
	return out, err
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func issueRef(ctx context.Context, q querier, id *string) *string {
	if id == nil {
		return nil
	}
	var ref string
	if q.QueryRowContext(ctx, `SELECT p.identifier || '-' || i.sequence_id FROM issues i JOIN projects p ON p.id = i.project_id AND p.deleted_at IS NULL WHERE i.id = ?`, *id).Scan(&ref) != nil {
		return nil
	}
	return &ref
}

// diff returns the elements added to and removed from old to get new.
func diff(old, new []string) (added, removed []string) {
	o, n := map[string]bool{}, map[string]bool{}
	for _, v := range old {
		o[v] = true
	}
	for _, v := range new {
		n[v] = true
		if !o[v] {
			added = append(added, v)
		}
	}
	for _, v := range old {
		if !n[v] {
			removed = append(removed, v)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	return
}

// writeIssueRelations replaces the sets named in ch and logs activities when
// logChanges is set (on create only "created" is logged).
func writeIssueRelations(ctx context.Context, tx *sql.Tx, p *Project, i *Issue, ch *issueChange, actor string, logChanges bool) error {
	type rel struct {
		set            bool
		cur, next      []string
		field, nameSQL string
		insert, delete string
	}
	rels := []rel{
		{ch.setAssignees, i.Assignees, ch.assignees, "assignees", `SELECT display_name FROM users WHERE id = ?`,
			`INSERT OR IGNORE INTO issue_assignees (issue_id, user_id) VALUES (?, ?)`, `DELETE FROM issue_assignees WHERE issue_id = ? AND user_id = ?`},
		{ch.setLabels, i.Labels, ch.labels, "labels", `SELECT name FROM labels WHERE id = ?`,
			`INSERT OR IGNORE INTO issue_labels (issue_id, label_id) VALUES (?, ?)`, `DELETE FROM issue_labels WHERE issue_id = ? AND label_id = ?`},
	}
	for _, r := range rels {
		if !r.set {
			continue
		}
		added, removed := diff(r.cur, r.next)
		for _, v := range added {
			if _, err := tx.ExecContext(ctx, r.insert, i.ID, v); err != nil {
				return err
			}
			if logChanges {
				if err := logSetChange(ctx, tx, i, actor, r.field, r.nameSQL, v, true); err != nil {
					return err
				}
			}
		}
		for _, v := range removed {
			if _, err := tx.ExecContext(ctx, r.delete, i.ID, v); err != nil {
				return err
			}
			if logChanges {
				if err := logSetChange(ctx, tx, i, actor, r.field, r.nameSQL, v, false); err != nil {
					return err
				}
			}
		}
	}
	if ch.setCycle {
		if err := setIssueCycle(ctx, tx, i, ch.cycle, actor); err != nil {
			return err
		}
	}
	if ch.setModules {
		added, removed := diff(i.ModuleIDs, ch.modules)
		for _, m := range added {
			if err := addIssueModule(ctx, tx, i, m, actor); err != nil {
				return err
			}
		}
		for _, m := range removed {
			if err := removeIssueModule(ctx, tx, i, m, actor); err != nil {
				return err
			}
		}
	}
	return nil
}

func logSetChange(ctx context.Context, tx *sql.Tx, i *Issue, actor, field, nameSQL, id string, added bool) error {
	var name string
	tx.QueryRowContext(ctx, nameSQL, id).Scan(&name)
	a := &Activity{IssueID: i.ID, ProjectID: i.ProjectID, ActorID: &actor, Verb: "updated", Field: &field}
	if added {
		a.NewValue, a.NewIdentifier, a.Comment = &name, &id, "added "+field+" "+name
	} else {
		a.OldValue, a.OldIdentifier, a.Comment = &name, &id, "removed "+field+" "+name
	}
	return logActivity(ctx, tx, a)
}

// setIssueCycle moves the issue into cycle (nil removes it from any cycle).
func setIssueCycle(ctx context.Context, tx *sql.Tx, i *Issue, cycle *string, actor string) error {
	var cur *string
	if err := tx.QueryRowContext(ctx, `SELECT cycle_id FROM cycle_issues WHERE issue_id = ?`, i.ID).Scan(&cur); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if deref(cur) == deref(cycle) {
		return nil
	}
	field := "cycles"
	a := &Activity{IssueID: i.ID, ProjectID: i.ProjectID, ActorID: &actor, Field: &field, OldIdentifier: cur, NewIdentifier: cycle}
	a.OldValue = nameOf(ctx, tx, `SELECT name FROM cycles WHERE id = ?`, cur)
	a.NewValue = nameOf(ctx, tx, `SELECT name FROM cycles WHERE id = ?`, cycle)
	if cycle == nil {
		if _, err := tx.ExecContext(ctx, `DELETE FROM cycle_issues WHERE issue_id = ?`, i.ID); err != nil {
			return err
		}
		a.Verb, a.Comment = "deleted", "removed the work item from cycle "+deref(a.OldValue)
		return logActivity(ctx, tx, a)
	}
	ts := now()
	_, err := tx.ExecContext(ctx, `INSERT INTO cycle_issues (id, cycle_id, issue_id, project_id, created_by, created_at, updated_at) VALUES (?,?,?,?,?,?,?)
		ON CONFLICT (issue_id) DO UPDATE SET cycle_id = excluded.cycle_id, updated_by = excluded.created_by, updated_at = excluded.updated_at`,
		newID(), *cycle, i.ID, i.ProjectID, actor, ts, ts)
	if err != nil {
		return err
	}
	a.Verb, a.Comment = "updated", "added the work item to cycle "+deref(a.NewValue)
	return logActivity(ctx, tx, a)
}

func addIssueModule(ctx context.Context, tx *sql.Tx, i *Issue, module, actor string) error {
	ts := now()
	res, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO module_issues (id, module_id, issue_id, project_id, created_by, created_at, updated_at) VALUES (?,?,?,?,?,?,?)`,
		newID(), module, i.ID, i.ProjectID, actor, ts, ts)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil
	}
	field := "modules"
	name := nameOf(ctx, tx, `SELECT name FROM modules WHERE id = ?`, &module)
	return logActivity(ctx, tx, &Activity{IssueID: i.ID, ProjectID: i.ProjectID, ActorID: &actor, Verb: "updated", Field: &field,
		NewValue: name, NewIdentifier: &module, Comment: "added the work item to module " + deref(name)})
}

func removeIssueModule(ctx context.Context, tx *sql.Tx, i *Issue, module, actor string) error {
	res, err := tx.ExecContext(ctx, `DELETE FROM module_issues WHERE module_id = ? AND issue_id = ?`, module, i.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	field := "modules"
	name := nameOf(ctx, tx, `SELECT name FROM modules WHERE id = ?`, &module)
	return logActivity(ctx, tx, &Activity{IssueID: i.ID, ProjectID: i.ProjectID, ActorID: &actor, Verb: "deleted", Field: &field,
		OldValue: name, OldIdentifier: &module, Comment: "removed the work item from module " + deref(name)})
}

func nameOf(ctx context.Context, q querier, query string, id *string) *string {
	if id == nil {
		return nil
	}
	var name string
	if q.QueryRowContext(ctx, query, *id).Scan(&name) != nil {
		return nil
	}
	return &name
}

// DeleteIssue hard-deletes a work item; its comments, links, activities and
// memberships go with it, sub-items lose their parent.
func (s *Store) DeleteIssue(ctx context.Context, projectID, id string) error {
	res, err := s.w.ExecContext(ctx, `DELETE FROM issues WHERE project_id = ? AND id = ?`, projectID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
