package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

// Project is a container of work items.
type Project struct {
	ID, WorkspaceID, Name, Identifier, Description string
	Network                                        int
	ProjectLead, DefaultAssignee, DefaultState     *string
	Emoji, CoverImage                              *string
	IconProp                                       *string // raw JSON
	ModuleView, CycleView, IssueViewsView          bool
	PageView, IntakeView                           bool
	IsTimeTrackingEnabled, IsIssueTypeEnabled      bool
	GuestViewAllFeatures                           bool
	ArchiveIn, CloseIn                             int
	Timezone                                       string
	ExternalSource, ExternalID                     *string
	CreatedBy, UpdatedBy                           *string
	CreatedAt, UpdatedAt                           string
	TotalMembers, TotalCycles, TotalModules        int
}

// ProjectInput is the writable part of a project. Unset fields keep their
// current (or default) value.
type ProjectInput struct {
	Name                  Opt[string]          `json:"name"`
	Identifier            Opt[string]          `json:"identifier"`
	Description           Opt[string]          `json:"description"`
	ProjectLead           Opt[*string]         `json:"project_lead"`
	DefaultAssignee       Opt[*string]         `json:"default_assignee"`
	DefaultState          Opt[*string]         `json:"default_state"`
	Emoji                 Opt[*string]         `json:"emoji"`
	IconProp              Opt[json.RawMessage] `json:"icon_prop"`
	CoverImage            Opt[*string]         `json:"cover_image"`
	ModuleView            Opt[bool]            `json:"module_view"`
	CycleView             Opt[bool]            `json:"cycle_view"`
	IssueViewsView        Opt[bool]            `json:"issue_views_view"`
	PageView              Opt[bool]            `json:"page_view"`
	IntakeView            Opt[bool]            `json:"intake_view"`
	IsTimeTrackingEnabled Opt[bool]            `json:"is_time_tracking_enabled"`
	IsIssueTypeEnabled    Opt[bool]            `json:"is_issue_type_enabled"`
	GuestViewAllFeatures  Opt[bool]            `json:"guest_view_all_features"`
	ArchiveIn             Opt[int]             `json:"archive_in"`
	CloseIn               Opt[int]             `json:"close_in"`
	Timezone              Opt[string]          `json:"timezone"`
	ExternalSource        Opt[*string]         `json:"external_source"`
	ExternalID            Opt[*string]         `json:"external_id"`
}

const projectCols = `p.id, p.workspace_id, p.name, p.identifier, p.description, p.network, p.project_lead, p.default_assignee, p.default_state,
	p.emoji, p.cover_image, p.icon_prop, p.module_view, p.cycle_view, p.issue_views_view, p.page_view, p.intake_view,
	p.is_time_tracking_enabled, p.is_issue_type_enabled, p.guest_view_all_features, p.archive_in, p.close_in, p.timezone,
	p.external_source, p.external_id, p.created_by, p.updated_by, p.created_at, p.updated_at,
	(SELECT COUNT(*) FROM workspace_members m WHERE m.workspace_id = p.workspace_id),
	(SELECT COUNT(*) FROM cycles c WHERE c.project_id = p.id),
	(SELECT COUNT(*) FROM modules d WHERE d.project_id = p.id)`

func scanProject(sc interface{ Scan(...any) error }) (*Project, error) {
	p := &Project{}
	err := sc.Scan(&p.ID, &p.WorkspaceID, &p.Name, &p.Identifier, &p.Description, &p.Network, &p.ProjectLead, &p.DefaultAssignee, &p.DefaultState,
		&p.Emoji, &p.CoverImage, &p.IconProp, &p.ModuleView, &p.CycleView, &p.IssueViewsView, &p.PageView, &p.IntakeView,
		&p.IsTimeTrackingEnabled, &p.IsIssueTypeEnabled, &p.GuestViewAllFeatures, &p.ArchiveIn, &p.CloseIn, &p.Timezone,
		&p.ExternalSource, &p.ExternalID, &p.CreatedBy, &p.UpdatedBy, &p.CreatedAt, &p.UpdatedAt,
		&p.TotalMembers, &p.TotalCycles, &p.TotalModules)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return p, err
}

var projectOrder = map[string]string{"created_at": "p.created_at", "updated_at": "p.updated_at", "name": "p.name", "network": "p.network", "sort_order": "p.created_at"}

// ListProjects lists the workspace's projects.
func (s *Store) ListProjects(ctx context.Context, wsID, orderBy string) ([]*Project, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT `+projectCols+` FROM projects p WHERE p.workspace_id = ? ORDER BY `+
		orderClause(orderBy, projectOrder, "p.created_at ASC")+`, p.id`, wsID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Project{}
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetProject returns a project of the workspace.
func (s *Store) GetProject(ctx context.Context, wsID, id string) (*Project, error) {
	return getProject(ctx, s.r, wsID, id)
}

func getProject(ctx context.Context, q querier, wsID, id string) (*Project, error) {
	return scanProject(q.QueryRowContext(ctx, `SELECT `+projectCols+` FROM projects p WHERE p.workspace_id = ? AND p.id = ?`, wsID, id))
}

// ProjectByRef finds a project by id, identifier (case-insensitive) or name.
func (s *Store) ProjectByRef(ctx context.Context, wsID, ref string) (*Project, error) {
	return scanProject(s.r.QueryRowContext(ctx, `SELECT `+projectCols+` FROM projects p WHERE p.workspace_id = ?
		AND (p.id = ? OR p.identifier = upper(?) OR lower(p.name) = lower(?)) ORDER BY p.id = ? DESC LIMIT 1`, wsID, ref, ref, ref, ref))
}

// ProjectsByIDs batch-loads projects.
func (s *Store) ProjectsByIDs(ctx context.Context, ids []string) (map[string]*Project, error) {
	out := map[string]*Project{}
	if len(ids) == 0 {
		return out, nil
	}
	ph, args := in(dedupe(ids))
	rows, err := s.r.QueryContext(ctx, `SELECT `+projectCols+` FROM projects p WHERE p.id IN `+ph, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out[p.ID] = p
	}
	return out, rows.Err()
}

const forbiddenProjectChars = "&+,:;$^}{*=?@#|'<>.()%!-"

var defaultStates = []struct {
	name, group, color string
	seq                float64
}{
	{"Backlog", "backlog", "#60646C", 15000},
	{"Todo", "unstarted", "#60646C", 25000},
	{"In Progress", "started", "#F59E0B", 35000},
	{"Done", "completed", "#46A758", 45000},
	{"Cancelled", "cancelled", "#9AA4BC", 55000},
}

func (in *ProjectInput) apply(ctx context.Context, q querier, p *Project) error {
	if in.Name.Set {
		p.Name = strings.TrimSpace(in.Name.V)
	}
	if in.Identifier.Set {
		p.Identifier = strings.ToUpper(strings.TrimSpace(in.Identifier.V))
	}
	if in.Description.Set {
		p.Description = in.Description.V
	}
	set := func(dst **string, o Opt[*string]) {
		if o.Set {
			*dst = o.V
		}
	}
	set(&p.ProjectLead, in.ProjectLead)
	set(&p.DefaultAssignee, in.DefaultAssignee)
	set(&p.DefaultState, in.DefaultState)
	set(&p.Emoji, in.Emoji)
	set(&p.CoverImage, in.CoverImage)
	if in.IconProp.Set {
		if raw := string(in.IconProp.V); raw == "null" || raw == "" {
			p.IconProp = nil
		} else {
			p.IconProp = &raw
		}
	}
	for _, b := range []struct {
		dst *bool
		o   Opt[bool]
	}{{&p.ModuleView, in.ModuleView}, {&p.CycleView, in.CycleView}, {&p.IssueViewsView, in.IssueViewsView}, {&p.PageView, in.PageView},
		{&p.IntakeView, in.IntakeView}, {&p.IsTimeTrackingEnabled, in.IsTimeTrackingEnabled}, {&p.IsIssueTypeEnabled, in.IsIssueTypeEnabled},
		{&p.GuestViewAllFeatures, in.GuestViewAllFeatures}} {
		if b.o.Set {
			*b.dst = b.o.V
		}
	}
	if in.ArchiveIn.Set {
		p.ArchiveIn = in.ArchiveIn.V
	}
	if in.CloseIn.Set {
		p.CloseIn = in.CloseIn.V
	}
	if in.Timezone.Set {
		p.Timezone = in.Timezone.V
	}
	p.ExternalSource, p.ExternalID = mergeExt(p.ExternalSource, p.ExternalID, in.ExternalSource, in.ExternalID)

	switch {
	case p.Name == "":
		return invalid("name", "This field may not be blank.")
	case len(p.Name) > 255:
		return invalid("name", "Ensure this field has no more than 255 characters.")
	case strings.ContainsAny(p.Name, forbiddenProjectChars):
		return invalid("name", "Name cannot contain special characters")
	case len(p.Identifier) > 12:
		return invalid("identifier", "Ensure this field has no more than 12 characters.")
	case strings.ContainsAny(p.Identifier, forbiddenProjectChars) || strings.ContainsAny(p.Identifier, " \t"):
		return invalid("identifier", "Identifier cannot contain special characters")
	case p.ArchiveIn < 0 || p.ArchiveIn > 12:
		return invalid("archive_in", "Ensure this value is between 0 and 12.")
	case p.CloseIn < 0 || p.CloseIn > 12:
		return invalid("close_in", "Ensure this value is between 0 and 12.")
	}
	for field, id := range map[string]*string{"project_lead": p.ProjectLead, "default_assignee": p.DefaultAssignee} {
		if id == nil {
			continue
		}
		ok, err := activeMembers(ctx, q, p.WorkspaceID, []string{*id}, RoleGuest)
		if err != nil {
			return err
		}
		if !ok[*id] {
			return invalid(field, "The user is not a member of this workspace.")
		}
	}
	if p.DefaultState != nil && in.DefaultState.Set {
		var n int
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM states WHERE id = ? AND project_id = ?`, *p.DefaultState, p.ID).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return invalid("default_state", "Default state should be a state in the project.")
		}
	}
	// Blank or duplicate identifiers are 409 in Plane, with a string message.
	var other string
	err := q.QueryRowContext(ctx, `SELECT id FROM projects WHERE workspace_id = ? AND identifier = ? AND id != ?`, p.WorkspaceID, p.Identifier, p.ID).Scan(&other)
	if p.Identifier == "" || err == nil {
		return &Error{Kind: KindConflict, Field: "identifier", Msg: "The project identifier is already taken"}
	}
	err = q.QueryRowContext(ctx, `SELECT id FROM projects WHERE workspace_id = ? AND name = ? AND id != ?`, p.WorkspaceID, p.Name, p.ID).Scan(&other)
	if err == nil {
		return &Error{Kind: KindConflict, Field: "name", Msg: "The project name is already taken"}
	}
	return checkExternal(ctx, q, "projects", "workspace_id", p.WorkspaceID, p.ExternalSource, p.ExternalID, p.ID, "Project")
}

// CreateProject creates a project with the default states.
func (s *Store) CreateProject(ctx context.Context, wsID, actor string, in ProjectInput) (*Project, error) {
	var out *Project
	err := s.tx(ctx, func(tx *sql.Tx) error {
		ts := now()
		p := &Project{ID: newID(), WorkspaceID: wsID, Network: 2, ModuleView: true, CycleView: true, PageView: true, Timezone: "UTC",
			CreatedBy: &actor, CreatedAt: ts, UpdatedAt: ts}
		in.DefaultState = Opt[*string]{}
		if err := in.apply(ctx, tx, p); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO projects (id, workspace_id, name, identifier, description, network, project_lead, default_assignee,
			emoji, cover_image, icon_prop, module_view, cycle_view, issue_views_view, page_view, intake_view, is_time_tracking_enabled,
			is_issue_type_enabled, guest_view_all_features, archive_in, close_in, timezone, external_source, external_id, created_by, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			p.ID, p.WorkspaceID, p.Name, p.Identifier, p.Description, p.Network, p.ProjectLead, p.DefaultAssignee,
			p.Emoji, p.CoverImage, p.IconProp, p.ModuleView, p.CycleView, p.IssueViewsView, p.PageView, p.IntakeView, p.IsTimeTrackingEnabled,
			p.IsIssueTypeEnabled, p.GuestViewAllFeatures, p.ArchiveIn, p.CloseIn, p.Timezone, p.ExternalSource, p.ExternalID, actor, ts, ts)
		if err != nil {
			return err
		}
		for i, d := range defaultStates {
			_, err := tx.ExecContext(ctx, `INSERT INTO states (id, project_id, name, color, grp, sequence, is_default, slug, created_by, created_at, updated_at)
				VALUES (?,?,?,?,?,?,?,?,?,?,?)`, newID(), p.ID, d.name, d.color, d.group, d.seq, i == 0, slugify(d.name), actor, ts, ts)
			if err != nil {
				return err
			}
		}
		out, err = getProject(ctx, tx, wsID, p.ID)
		return err
	})
	return out, err
}

// UpdateProject applies a partial update.
func (s *Store) UpdateProject(ctx context.Context, wsID, id, actor string, in ProjectInput) (*Project, error) {
	var out *Project
	err := s.tx(ctx, func(tx *sql.Tx) error {
		p, err := getProject(ctx, tx, wsID, id)
		if err != nil {
			return err
		}
		if err := in.apply(ctx, tx, p); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE projects SET name=?, identifier=?, description=?, project_lead=?, default_assignee=?, default_state=?,
			emoji=?, cover_image=?, icon_prop=?, module_view=?, cycle_view=?, issue_views_view=?, page_view=?, intake_view=?, is_time_tracking_enabled=?,
			is_issue_type_enabled=?, guest_view_all_features=?, archive_in=?, close_in=?, timezone=?, external_source=?, external_id=?, updated_by=?, updated_at=?
			WHERE id=?`,
			p.Name, p.Identifier, p.Description, p.ProjectLead, p.DefaultAssignee, p.DefaultState,
			p.Emoji, p.CoverImage, p.IconProp, p.ModuleView, p.CycleView, p.IssueViewsView, p.PageView, p.IntakeView, p.IsTimeTrackingEnabled,
			p.IsIssueTypeEnabled, p.GuestViewAllFeatures, p.ArchiveIn, p.CloseIn, p.Timezone, p.ExternalSource, p.ExternalID, actor, now(), p.ID)
		if err != nil {
			return err
		}
		out, err = getProject(ctx, tx, wsID, id)
		return err
	})
	return out, err
}

// DeleteProject hard-deletes a project and everything in it.
func (s *Store) DeleteProject(ctx context.Context, wsID, id string) error {
	res, err := s.w.ExecContext(ctx, `DELETE FROM projects WHERE workspace_id = ? AND id = ?`, wsID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func slugify(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}

// State is a workflow state.
type State struct {
	ID, ProjectID, Name, Color, Group, Description string
	Sequence                                       float64
	Default                                        bool
	Slug                                           string
	ExternalSource, ExternalID                     *string
	CreatedBy, UpdatedBy                           *string
	CreatedAt, UpdatedAt                           string
}

// StateInput is the writable part of a state.
type StateInput struct {
	Name           Opt[string]  `json:"name"`
	Color          Opt[string]  `json:"color"`
	Group          Opt[string]  `json:"group"`
	Description    Opt[string]  `json:"description"`
	Default        Opt[bool]    `json:"default"`
	Sequence       Opt[float64] `json:"sequence"`
	ExternalSource Opt[*string] `json:"external_source"`
	ExternalID     Opt[*string] `json:"external_id"`
}

// StateGroups in board order.
var StateGroups = []string{"backlog", "unstarted", "started", "completed", "cancelled"}

const stateCols = `id, project_id, name, color, grp, description, sequence, is_default, slug, external_source, external_id, created_by, updated_by, created_at, updated_at`

func scanState(sc interface{ Scan(...any) error }) (*State, error) {
	st := &State{}
	err := sc.Scan(&st.ID, &st.ProjectID, &st.Name, &st.Color, &st.Group, &st.Description, &st.Sequence, &st.Default, &st.Slug,
		&st.ExternalSource, &st.ExternalID, &st.CreatedBy, &st.UpdatedBy, &st.CreatedAt, &st.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return st, err
}

func queryStates(ctx context.Context, q querier, where string, args ...any) ([]*State, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+stateCols+` FROM states WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*State{}
	for rows.Next() {
		st, err := scanState(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// ListStates lists a project's states by sequence.
func (s *Store) ListStates(ctx context.Context, projectID string) ([]*State, error) {
	return queryStates(ctx, s.r, `project_id = ? ORDER BY sequence, created_at`, projectID)
}

// GetState returns a state of the project.
func (s *Store) GetState(ctx context.Context, projectID, id string) (*State, error) {
	return scanState(s.r.QueryRowContext(ctx, `SELECT `+stateCols+` FROM states WHERE project_id = ? AND id = ?`, projectID, id))
}

// StatesByIDs batch-loads states.
func (s *Store) StatesByIDs(ctx context.Context, ids []string) (map[string]*State, error) {
	out := map[string]*State{}
	if len(ids) == 0 {
		return out, nil
	}
	ph, args := in(dedupe(ids))
	list, err := queryStates(ctx, s.r, `id IN `+ph, args...)
	for _, st := range list {
		out[st.ID] = st
	}
	return out, err
}

func validGroup(g string) bool {
	for _, x := range StateGroups {
		if g == x {
			return true
		}
	}
	return false
}

func (in *StateInput) apply(st *State) error {
	if in.Name.Set {
		st.Name = strings.TrimSpace(in.Name.V)
	}
	if in.Color.Set {
		st.Color = in.Color.V
	}
	if in.Group.Set {
		st.Group = in.Group.V
	}
	if in.Description.Set {
		st.Description = in.Description.V
	}
	if in.Default.Set {
		st.Default = in.Default.V
	}
	if in.Sequence.Set {
		st.Sequence = in.Sequence.V
	}
	st.ExternalSource, st.ExternalID = mergeExt(st.ExternalSource, st.ExternalID, in.ExternalSource, in.ExternalID)
	st.Slug = slugify(st.Name)
	switch {
	case st.Name == "":
		return invalid("name", "This field may not be blank.")
	case st.Color == "":
		return invalid("color", "This field may not be blank.")
	case st.Group == "triage":
		return invalid("non_field_errors", "Cannot create triage state")
	case !validGroup(st.Group):
		return invalid("group", `"`+st.Group+`" is not a valid choice.`)
	}
	return nil
}

// CreateState adds a state. A true default clears the flag on the others.
func (s *Store) CreateState(ctx context.Context, projectID, actor string, in StateInput) (*State, error) {
	var out *State
	err := s.tx(ctx, func(tx *sql.Tx) error {
		ts := now()
		st := &State{ID: newID(), ProjectID: projectID, Group: "backlog", CreatedBy: &actor, CreatedAt: ts, UpdatedAt: ts}
		if err := in.apply(st); err != nil {
			return err
		}
		var dup string
		if err := tx.QueryRowContext(ctx, `SELECT id FROM states WHERE project_id = ? AND name = ?`, projectID, st.Name).Scan(&dup); err == nil {
			return conflict("State with the same name already exists in the project", dup)
		}
		if err := checkExternal(ctx, tx, "states", "project_id", projectID, st.ExternalSource, st.ExternalID, st.ID, "State"); err != nil {
			return err
		}
		var maxSeq sql.NullFloat64
		if err := tx.QueryRowContext(ctx, `SELECT MAX(sequence) FROM states WHERE project_id = ?`, projectID).Scan(&maxSeq); err != nil {
			return err
		}
		st.Sequence = 65535
		if maxSeq.Valid {
			st.Sequence = maxSeq.Float64 + 15000
		}
		if st.Default {
			if _, err := tx.ExecContext(ctx, `UPDATE states SET is_default = 0 WHERE project_id = ?`, projectID); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO states (`+stateCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			st.ID, st.ProjectID, st.Name, st.Color, st.Group, st.Description, st.Sequence, st.Default, st.Slug,
			st.ExternalSource, st.ExternalID, st.CreatedBy, nil, st.CreatedAt, st.UpdatedAt)
		out = st
		return err
	})
	return out, err
}

// UpdateState applies a partial update.
func (s *Store) UpdateState(ctx context.Context, projectID, id, actor string, in StateInput) (*State, error) {
	var out *State
	err := s.tx(ctx, func(tx *sql.Tx) error {
		st, err := scanState(tx.QueryRowContext(ctx, `SELECT `+stateCols+` FROM states WHERE project_id = ? AND id = ?`, projectID, id))
		if err != nil {
			return err
		}
		if err := in.apply(st); err != nil {
			if e, ok := err.(*Error); ok && e.Msg == "Cannot create triage state" {
				return invalid("group", `"triage" is not a valid choice.`)
			}
			return err
		}
		if err := checkExternal(ctx, tx, "states", "project_id", projectID, st.ExternalSource, st.ExternalID, st.ID, "State"); err != nil {
			return err
		}
		if st.Default {
			if _, err := tx.ExecContext(ctx, `UPDATE states SET is_default = 0 WHERE project_id = ? AND id != ?`, projectID, id); err != nil {
				return err
			}
		}
		st.UpdatedBy, st.UpdatedAt = &actor, now()
		_, err = tx.ExecContext(ctx, `UPDATE states SET name=?, color=?, grp=?, description=?, sequence=?, is_default=?, slug=?,
			external_source=?, external_id=?, updated_by=?, updated_at=? WHERE id=?`,
			st.Name, st.Color, st.Group, st.Description, st.Sequence, st.Default, st.Slug, st.ExternalSource, st.ExternalID, actor, st.UpdatedAt, id)
		if isUnique(err) {
			return viewErr("The payload is not valid")
		}
		out = st
		return err
	})
	return out, err
}

// DeleteState deletes an empty, non-default state.
func (s *Store) DeleteState(ctx context.Context, projectID, id string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		st, err := scanState(tx.QueryRowContext(ctx, `SELECT `+stateCols+` FROM states WHERE project_id = ? AND id = ?`, projectID, id))
		if err != nil {
			return err
		}
		if st.Default {
			return viewErr("Default state cannot be deleted")
		}
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM issues WHERE state_id = ?`, id).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return viewErr("The state is not empty, only empty states can be deleted")
		}
		if _, err := tx.ExecContext(ctx, `UPDATE projects SET default_state = NULL WHERE default_state = ?`, id); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM states WHERE id = ?`, id)
		return err
	})
}

// Label tags work items.
type Label struct {
	ID, ProjectID, Name, Color, Description string
	Parent                                  *string
	SortOrder                               float64
	ExternalSource, ExternalID              *string
	CreatedBy, UpdatedBy                    *string
	CreatedAt, UpdatedAt                    string
}

// LabelInput is the writable part of a label.
type LabelInput struct {
	Name           Opt[string]  `json:"name"`
	Color          Opt[string]  `json:"color"`
	Description    Opt[string]  `json:"description"`
	Parent         Opt[*string] `json:"parent"`
	SortOrder      Opt[float64] `json:"sort_order"`
	ExternalSource Opt[*string] `json:"external_source"`
	ExternalID     Opt[*string] `json:"external_id"`
}

const labelCols = `id, project_id, name, color, description, parent_id, sort_order, external_source, external_id, created_by, updated_by, created_at, updated_at`

func scanLabel(sc interface{ Scan(...any) error }) (*Label, error) {
	l := &Label{}
	err := sc.Scan(&l.ID, &l.ProjectID, &l.Name, &l.Color, &l.Description, &l.Parent, &l.SortOrder, &l.ExternalSource, &l.ExternalID,
		&l.CreatedBy, &l.UpdatedBy, &l.CreatedAt, &l.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return l, err
}

func queryLabels(ctx context.Context, q querier, where string, args ...any) ([]*Label, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+labelCols+` FROM labels WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Label{}
	for rows.Next() {
		l, err := scanLabel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ListLabels lists a project's labels, newest first.
func (s *Store) ListLabels(ctx context.Context, projectID string) ([]*Label, error) {
	return queryLabels(ctx, s.r, `project_id = ? ORDER BY created_at DESC, id`, projectID)
}

// GetLabel returns a label of the project.
func (s *Store) GetLabel(ctx context.Context, projectID, id string) (*Label, error) {
	return scanLabel(s.r.QueryRowContext(ctx, `SELECT `+labelCols+` FROM labels WHERE project_id = ? AND id = ?`, projectID, id))
}

// LabelsByIDs batch-loads labels.
func (s *Store) LabelsByIDs(ctx context.Context, ids []string) (map[string]*Label, error) {
	out := map[string]*Label{}
	if len(ids) == 0 {
		return out, nil
	}
	ph, args := in(dedupe(ids))
	list, err := queryLabels(ctx, s.r, `id IN `+ph, args...)
	for _, l := range list {
		out[l.ID] = l
	}
	return out, err
}

func (in *LabelInput) apply(ctx context.Context, q querier, l *Label) error {
	if in.Name.Set {
		l.Name = strings.TrimSpace(in.Name.V)
	}
	if in.Color.Set {
		l.Color = in.Color.V
	}
	if in.Description.Set {
		l.Description = in.Description.V
	}
	if in.Parent.Set {
		l.Parent = in.Parent.V
	}
	if in.SortOrder.Set {
		l.SortOrder = in.SortOrder.V
	}
	l.ExternalSource, l.ExternalID = mergeExt(l.ExternalSource, l.ExternalID, in.ExternalSource, in.ExternalID)
	if l.Name == "" {
		return invalid("name", "This field may not be blank.")
	}
	if l.Parent != nil {
		var n int
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM labels WHERE id = ? AND project_id = ? AND id != ?`, *l.Parent, l.ProjectID, l.ID).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return invalid("parent", "Parent label must be another label in this project.")
		}
	}
	var dup string
	if err := q.QueryRowContext(ctx, `SELECT id FROM labels WHERE project_id = ? AND name = ? AND id != ?`, l.ProjectID, l.Name, l.ID).Scan(&dup); err == nil {
		return conflict("Label with the same name already exists in the project", dup)
	}
	return checkExternal(ctx, q, "labels", "project_id", l.ProjectID, l.ExternalSource, l.ExternalID, l.ID, "Label")
}

// CreateLabel adds a label.
func (s *Store) CreateLabel(ctx context.Context, projectID, actor string, in LabelInput) (*Label, error) {
	var out *Label
	err := s.tx(ctx, func(tx *sql.Tx) error {
		ts := now()
		l := &Label{ID: newID(), ProjectID: projectID, CreatedBy: &actor, CreatedAt: ts, UpdatedAt: ts}
		in.SortOrder = Opt[float64]{}
		if err := in.apply(ctx, tx, l); err != nil {
			return err
		}
		var maxSort sql.NullFloat64
		if err := tx.QueryRowContext(ctx, `SELECT MAX(sort_order) FROM labels WHERE project_id = ?`, projectID).Scan(&maxSort); err != nil {
			return err
		}
		l.SortOrder = 65535
		if maxSort.Valid {
			l.SortOrder = maxSort.Float64 + 10000
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO labels (`+labelCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			l.ID, l.ProjectID, l.Name, l.Color, l.Description, l.Parent, l.SortOrder, l.ExternalSource, l.ExternalID, actor, nil, ts, ts)
		out = l
		return err
	})
	return out, err
}

// UpdateLabel applies a partial update.
func (s *Store) UpdateLabel(ctx context.Context, projectID, id, actor string, in LabelInput) (*Label, error) {
	var out *Label
	err := s.tx(ctx, func(tx *sql.Tx) error {
		l, err := scanLabel(tx.QueryRowContext(ctx, `SELECT `+labelCols+` FROM labels WHERE project_id = ? AND id = ?`, projectID, id))
		if err != nil {
			return err
		}
		if err := in.apply(ctx, tx, l); err != nil {
			return err
		}
		l.UpdatedBy, l.UpdatedAt = &actor, now()
		_, err = tx.ExecContext(ctx, `UPDATE labels SET name=?, color=?, description=?, parent_id=?, sort_order=?, external_source=?, external_id=?,
			updated_by=?, updated_at=? WHERE id=?`, l.Name, l.Color, l.Description, l.Parent, l.SortOrder, l.ExternalSource, l.ExternalID, actor, l.UpdatedAt, id)
		out = l
		return err
	})
	return out, err
}

// DeleteLabel deletes a label; it is removed from all work items.
func (s *Store) DeleteLabel(ctx context.Context, projectID, id string) error {
	res, err := s.w.ExecContext(ctx, `DELETE FROM labels WHERE project_id = ? AND id = ?`, projectID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
