// Command plane-import loads a JSON export of an old Plane database
// (see deploy/plane-export.sql) into a MostlyDone database file.
//
// It is a one-off tool. It writes straight into the SQLite file, so the
// MostlyDone server must be stopped first. It keeps Plane's ids, work item
// numbers, dates, authors and ordering. Everything goes in one transaction:
// either all of it lands or none of it. Before writing, it saves a copy of the
// database next to it as <db>.before-plane-import-<time>.
//
//	plane-import -db /data/mostlydone.db -from plane-export.json
package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"mostlydone/internal/store"
)

type export struct {
	Users []struct {
		ID, Email string
	} `json:"users"`
	Projects []struct {
		ID, Name, Identifier, Description string
		Network                           int
		Emoji                             *string
		CycleView                         bool    `json:"cycle_view"`
		ModuleView                        bool    `json:"module_view"`
		ProjectLead                       *string `json:"project_lead_id"`
		DefaultAssignee                   *string `json:"default_assignee_id"`
		CreatedBy                         *string `json:"created_by_id"`
		CreatedAt                         string  `json:"created_at"`
		UpdatedAt                         string  `json:"updated_at"`
	} `json:"projects"`
	States []struct {
		ID, Name, Color, Group, Description string
		ProjectID                           string `json:"project_id"`
		IsDefault                           bool   `json:"is_default"`
		Sequence                            float64
		CreatedAt                           string `json:"created_at"`
	} `json:"states"`
	Labels []struct {
		ID, Name, Color, Description string
		ProjectID                    string  `json:"project_id"`
		Parent                       *string `json:"parent_id"`
		SortOrder                    float64 `json:"sort_order"`
		CreatedAt                    string  `json:"created_at"`
	} `json:"labels"`
	Issues []struct {
		ID, Name, Priority string
		ProjectID          string   `json:"project_id"`
		SequenceID         int      `json:"sequence_id"`
		DescriptionHTML    string   `json:"description_html"`
		StateID            string   `json:"state_id"`
		Parent             *string  `json:"parent_id"`
		StartDate          *string  `json:"start_date"`
		TargetDate         *string  `json:"target_date"`
		SortOrder          float64  `json:"sort_order"`
		CreatedBy          *string  `json:"created_by_id"`
		UpdatedBy          *string  `json:"updated_by_id"`
		CompletedAt        *string  `json:"completed_at"`
		CreatedAt          string   `json:"created_at"`
		UpdatedAt          string   `json:"updated_at"`
		LabelIDs           []string `json:"label_ids"`
		AssigneeIDs        []string `json:"assignee_ids"`
	} `json:"issues"`
	Cycles []struct {
		ID, Name, Description, Timezone string
		ProjectID                       string  `json:"project_id"`
		StartDate                       *string `json:"start_date"`
		EndDate                         *string `json:"end_date"`
		OwnedBy                         *string `json:"owned_by_id"`
		SortOrder                       float64 `json:"sort_order"`
		CreatedAt                       string  `json:"created_at"`
	} `json:"cycles"`
	CycleIssues []struct {
		CycleID   string `json:"cycle_id"`
		IssueID   string `json:"issue_id"`
		CreatedAt string `json:"created_at"`
	} `json:"cycle_issues"`
	Modules []struct {
		ID, Name, Description, Status string
		ProjectID                     string  `json:"project_id"`
		StartDate                     *string `json:"start_date"`
		TargetDate                    *string `json:"target_date"`
		Lead                          *string `json:"lead_id"`
		SortOrder                     float64 `json:"sort_order"`
		CreatedAt                     string  `json:"created_at"`
	} `json:"modules"`
	ModuleIssues []struct {
		ModuleID  string `json:"module_id"`
		IssueID   string `json:"issue_id"`
		CreatedAt string `json:"created_at"`
	} `json:"module_issues"`
	Comments []struct {
		ID, Access string
		IssueID    string  `json:"issue_id"`
		Actor      *string `json:"actor_id"`
		HTML       string  `json:"comment_html"`
		EditedAt   *string `json:"edited_at"`
		CreatedAt  string  `json:"created_at"`
		UpdatedAt  string  `json:"updated_at"`
	} `json:"comments"`
}

const src = "plane"

func main() {
	dbPath := flag.String("db", "", "MostlyDone SQLite file (server must be stopped)")
	from := flag.String("from", "", "JSON export of the old Plane data")
	slug := flag.String("workspace", "main", "slug of the MostlyDone workspace to import into")
	flag.Parse()
	if *dbPath == "" || *from == "" {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*dbPath, *from, *slug); err != nil {
		log.Fatalf("plane-import: %v", err)
	}
}

func run(dbPath, from, slug string) error {
	raw, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	var ex export
	if err := json.Unmarshal(raw, &ex); err != nil {
		return fmt.Errorf("read %s: %w", from, err)
	}

	// Open once through the store so a new file gets the schema.
	s, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	if err := s.Close(); err != nil {
		return err
	}
	db, err := sql.Open("sqlite", dbPath+"?_txlock=immediate&_journal_mode=WAL&_busy_timeout=10000&_foreign_keys=1")
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	backup := dbPath + ".before-plane-import-" + time.Now().UTC().Format("20060102-150405")
	if _, err := db.Exec(`VACUUM INTO ?`, backup); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	log.Printf("backup saved: %s", backup)

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	counts, err := load(tx, &ex, slug)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if _, err := db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		return err
	}
	for _, c := range counts {
		log.Printf("imported %-14s %d", c.name, c.n)
	}
	return nil
}

type count struct {
	name string
	n    int
}

func load(tx *sql.Tx, ex *export, slug string) ([]count, error) {
	var wsID string
	if err := tx.QueryRow(`SELECT id FROM workspaces WHERE slug = ?`, slug).Scan(&wsID); err != nil {
		return nil, fmt.Errorf("workspace %q: %w", slug, err)
	}

	// Plane users are matched to existing MostlyDone users by email. This tool
	// never creates a user (it would have no password). Plane's own bot user is
	// not in the export; ids that are not matched become NULL (author unknown).
	user := map[string]string{}
	for _, u := range ex.Users {
		var id string
		err := tx.QueryRow(`SELECT id FROM users WHERE email = ?`, u.Email).Scan(&id)
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("Plane user %s has no MostlyDone account; create it first", u.Email)
		}
		if err != nil {
			return nil, err
		}
		user[u.ID] = id
	}
	who := func(p *string) *string {
		if p == nil {
			return nil
		}
		if id, ok := user[*p]; ok {
			return &id
		}
		return nil
	}

	// Refuse to mix with existing work.
	var existing int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM projects WHERE workspace_id = ?`, wsID).Scan(&existing); err != nil {
		return nil, err
	}
	if existing > 0 {
		return nil, fmt.Errorf("workspace %q already has %d project(s); refusing to import on top of them", slug, existing)
	}

	exec := func(q string, args ...any) error {
		_, err := tx.Exec(q, args...)
		return err
	}
	ext := func(id string) (string, string) { return src, id }

	maxSeq := map[string]int{}
	for _, i := range ex.Issues {
		if i.SequenceID > maxSeq[i.ProjectID] {
			maxSeq[i.ProjectID] = i.SequenceID
		}
	}
	defaultState := map[string]string{}
	for _, st := range ex.States {
		if st.IsDefault {
			defaultState[st.ProjectID] = st.ID
		}
	}

	for _, p := range ex.Projects {
		es, ei := ext(p.ID)
		err := exec(`INSERT INTO projects (id, workspace_id, name, identifier, description, network, project_lead, default_assignee,
			default_state, emoji, module_view, cycle_view, external_source, external_id, next_sequence, created_by, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			p.ID, wsID, p.Name, p.Identifier, p.Description, p.Network, who(p.ProjectLead), who(p.DefaultAssignee),
			nullable(defaultState[p.ID]), p.Emoji, p.ModuleView, p.CycleView, es, ei, maxSeq[p.ID]+1, who(p.CreatedBy), p.CreatedAt, p.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("project %s: %w", p.Identifier, err)
		}
	}

	group := map[string]string{}
	for _, st := range ex.States {
		group[st.ID] = st.Group
		es, ei := ext(st.ID)
		err := exec(`INSERT INTO states (id, project_id, name, color, grp, description, sequence, is_default, slug, external_source, external_id, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			st.ID, st.ProjectID, st.Name, st.Color, st.Group, st.Description, st.Sequence, st.IsDefault, slugify(st.Name), es, ei, st.CreatedAt, st.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("state %s: %w", st.Name, err)
		}
	}

	for _, l := range ex.Labels {
		es, ei := ext(l.ID)
		err := exec(`INSERT INTO labels (id, project_id, name, color, description, sort_order, external_source, external_id, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?)`,
			l.ID, l.ProjectID, l.Name, l.Color, l.Description, l.SortOrder, es, ei, l.CreatedAt, l.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("label %s: %w", l.Name, err)
		}
	}
	for _, l := range ex.Labels { // parents after all labels exist
		if l.Parent != nil {
			if err := exec(`UPDATE labels SET parent_id = ? WHERE id = ?`, *l.Parent, l.ID); err != nil {
				return nil, fmt.Errorf("label parent %s: %w", l.Name, err)
			}
		}
	}

	nLabelLinks, nAssignees := 0, 0
	for _, i := range ex.Issues {
		desc := store.SanitizeHTML(i.DescriptionHTML)
		if strings.TrimSpace(desc) == "" {
			desc = "<p></p>"
		}
		// Same rule as the app: completed_at only for work in a completed state.
		completed := i.CompletedAt
		if group[i.StateID] == "completed" {
			if completed == nil {
				completed = &i.UpdatedAt
			}
		} else {
			completed = nil
		}
		priority := i.Priority
		if priority == "" {
			priority = "none"
		}
		es, ei := ext(i.ID)
		err := exec(`INSERT INTO issues (id, project_id, sequence_id, name, description_html, state_id, priority, start_date, target_date,
			sort_order, completed_at, external_source, external_id, created_by, updated_by, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			i.ID, i.ProjectID, i.SequenceID, i.Name, desc, i.StateID, priority, i.StartDate, i.TargetDate,
			i.SortOrder, completed, es, ei, who(i.CreatedBy), who(i.UpdatedBy), i.CreatedAt, i.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("work item %d in project %s: %w", i.SequenceID, i.ProjectID, err)
		}
		for _, l := range i.LabelIDs {
			if err := exec(`INSERT INTO issue_labels (issue_id, label_id) VALUES (?,?)`, i.ID, l); err != nil {
				return nil, fmt.Errorf("label link on work item %d: %w", i.SequenceID, err)
			}
			nLabelLinks++
		}
		for _, a := range i.AssigneeIDs {
			if id := who(&a); id != nil {
				if err := exec(`INSERT OR IGNORE INTO issue_assignees (issue_id, user_id) VALUES (?,?)`, i.ID, *id); err != nil {
					return nil, fmt.Errorf("assignee on work item %d: %w", i.SequenceID, err)
				}
				nAssignees++
			}
		}
		// One "created" line so the activity list is not empty.
		err = exec(`INSERT INTO activities (id, issue_id, project_id, actor_id, verb, comment, epoch, created_at, updated_at)
			VALUES (lower(hex(randomblob(16))), ?, ?, ?, 'created', 'created the work item', ?, ?, ?)`,
			i.ID, i.ProjectID, who(i.CreatedBy), epoch(i.CreatedAt), i.CreatedAt, i.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("activity on work item %d: %w", i.SequenceID, err)
		}
	}
	for _, i := range ex.Issues { // parents after all work items exist
		if i.Parent != nil {
			if err := exec(`UPDATE issues SET parent_id = ? WHERE id = ?`, *i.Parent, i.ID); err != nil {
				return nil, fmt.Errorf("parent of work item %d: %w", i.SequenceID, err)
			}
		}
	}

	for _, c := range ex.Cycles {
		es, ei := ext(c.ID)
		tz := c.Timezone
		if tz == "" {
			tz = "UTC"
		}
		err := exec(`INSERT INTO cycles (id, project_id, name, description, start_date, end_date, owned_by, timezone, sort_order, external_source, external_id, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			c.ID, c.ProjectID, c.Name, c.Description, c.StartDate, c.EndDate, who(c.OwnedBy), tz, c.SortOrder, es, ei, c.CreatedAt, c.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("cycle %s: %w", c.Name, err)
		}
	}
	projectOf := map[string]string{}
	for _, i := range ex.Issues {
		projectOf[i.ID] = i.ProjectID
	}
	for _, ci := range ex.CycleIssues {
		pid, ok := projectOf[ci.IssueID]
		if !ok {
			continue // work item was a draft or archived
		}
		err := exec(`INSERT INTO cycle_issues (id, cycle_id, issue_id, project_id, created_at, updated_at) VALUES (lower(hex(randomblob(16))),?,?,?,?,?)`,
			ci.CycleID, ci.IssueID, pid, ci.CreatedAt, ci.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("cycle link: %w", err)
		}
	}

	for _, m := range ex.Modules {
		es, ei := ext(m.ID)
		status := m.Status
		if status == "" {
			status = "planned"
		}
		err := exec(`INSERT INTO modules (id, project_id, name, description, start_date, target_date, status, lead_id, sort_order, external_source, external_id, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			m.ID, m.ProjectID, m.Name, m.Description, m.StartDate, m.TargetDate, status, who(m.Lead), m.SortOrder, es, ei, m.CreatedAt, m.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("module %s: %w", m.Name, err)
		}
	}
	for _, mi := range ex.ModuleIssues {
		pid, ok := projectOf[mi.IssueID]
		if !ok {
			continue
		}
		err := exec(`INSERT OR IGNORE INTO module_issues (id, module_id, issue_id, project_id, created_at, updated_at) VALUES (lower(hex(randomblob(16))),?,?,?,?,?)`,
			mi.ModuleID, mi.IssueID, pid, mi.CreatedAt, mi.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("module link: %w", err)
		}
	}

	nComments := 0
	for _, c := range ex.Comments {
		pid, ok := projectOf[c.IssueID]
		if !ok {
			continue
		}
		access := c.Access
		if access == "" {
			access = "INTERNAL"
		}
		es, ei := ext(c.ID)
		err := exec(`INSERT INTO comments (id, issue_id, project_id, actor_id, comment_html, access, edited_at, external_source, external_id, created_by, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
			c.ID, c.IssueID, pid, who(c.Actor), store.SanitizeHTML(c.HTML), access, c.EditedAt, es, ei, who(c.Actor), c.CreatedAt, c.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("comment %s: %w", c.ID, err)
		}
		nComments++
	}

	rows, err := tx.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		return nil, err
	}
	bad := rows.Next()
	rows.Close()
	if bad {
		return nil, fmt.Errorf("foreign key check failed after import")
	}

	return []count{
		{"projects", len(ex.Projects)}, {"states", len(ex.States)}, {"labels", len(ex.Labels)},
		{"work items", len(ex.Issues)}, {"label links", nLabelLinks}, {"assignees", nAssignees},
		{"cycles", len(ex.Cycles)}, {"cycle links", countKept(ex.CycleIssues, projectOf, func(i int) string { return ex.CycleIssues[i].IssueID })},
		{"modules", len(ex.Modules)}, {"module links", countKept(ex.ModuleIssues, projectOf, func(i int) string { return ex.ModuleIssues[i].IssueID })},
		{"comments", nComments},
	}, nil
}

func countKept[T any](list []T, projectOf map[string]string, issueID func(int) string) int {
	n := 0
	for i := range list {
		if _, ok := projectOf[issueID(i)]; ok {
			n++
		}
	}
	return n
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// epoch is a work item's creation time as seconds since 1970, as the app's
// activity rows use.
func epoch(ts string) float64 {
	t, err := time.Parse(store.TimeLayout, ts)
	if err != nil {
		return 0
	}
	return float64(t.UnixMicro()) / 1e6
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
