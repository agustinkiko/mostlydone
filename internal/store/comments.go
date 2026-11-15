package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/url"
	"strings"
)

// Activity is one recorded change to a work item.
type Activity struct {
	ID, IssueID, ProjectID       string
	ActorID                      *string
	Verb                         string
	Field                        *string
	OldValue, NewValue           *string
	Comment                      string
	OldIdentifier, NewIdentifier *string
	IssueCommentID               *string
	Epoch                        float64
	CreatedAt, UpdatedAt         string
}

// logActivity writes an activity row inside the caller's transaction.
func logActivity(ctx context.Context, tx *sql.Tx, a *Activity) error {
	a.ID, a.CreatedAt, a.Epoch = newID(), now(), epoch()
	_, err := tx.ExecContext(ctx, `INSERT INTO activities (id, issue_id, project_id, actor_id, verb, field, old_value, new_value, comment,
		old_identifier, new_identifier, issue_comment_id, epoch, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		a.ID, a.IssueID, a.ProjectID, a.ActorID, a.Verb, a.Field, a.OldValue, a.NewValue, a.Comment,
		a.OldIdentifier, a.NewIdentifier, a.IssueCommentID, a.Epoch, a.CreatedAt, a.CreatedAt)
	return err
}

const activityCols = `id, issue_id, project_id, actor_id, verb, field, old_value, new_value, comment, old_identifier, new_identifier,
	issue_comment_id, epoch, created_at, updated_at`

// ListActivities lists an issue's activities, hiding comment rows (as Plane
// does). desc reverses the order; byUpdated orders by updated_at.
func (s *Store) ListActivities(ctx context.Context, issueID string, byUpdated, desc bool) ([]*Activity, error) {
	order := "created_at"
	if byUpdated {
		order = "updated_at"
	}
	dir := " ASC"
	if desc {
		dir = " DESC"
	}
	return s.queryActivities(ctx, `issue_id = ? AND (field IS NULL OR field != 'comment') ORDER BY `+order+dir+`, rowid`+dir, issueID)
}

// GetActivity returns one activity of the issue.
func (s *Store) GetActivity(ctx context.Context, issueID, id string) (*Activity, error) {
	list, err := s.queryActivities(ctx, `issue_id = ? AND id = ?`, issueID, id)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, ErrNotFound
	}
	return list[0], nil
}

func (s *Store) queryActivities(ctx context.Context, where string, args ...any) ([]*Activity, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT `+activityCols+` FROM activities WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Activity{}
	for rows.Next() {
		a := &Activity{}
		if err := rows.Scan(&a.ID, &a.IssueID, &a.ProjectID, &a.ActorID, &a.Verb, &a.Field, &a.OldValue, &a.NewValue, &a.Comment,
			&a.OldIdentifier, &a.NewIdentifier, &a.IssueCommentID, &a.Epoch, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Comment is a comment on a work item.
type Comment struct {
	ID, IssueID, ProjectID     string
	ActorID                    *string
	CommentHTML, Access        string
	EditedAt                   *string
	ExternalSource, ExternalID *string
	CreatedBy, UpdatedBy       *string
	CreatedAt, UpdatedAt       string
}

// CommentInput is the writable part of a comment.
type CommentInput struct {
	CommentHTML    Opt[string]  `json:"comment_html"`
	Access         Opt[string]  `json:"access"`
	ExternalSource Opt[*string] `json:"external_source"`
	ExternalID     Opt[*string] `json:"external_id"`
}

const commentCols = `id, issue_id, project_id, actor_id, comment_html, access, edited_at, external_source, external_id, created_by, updated_by, created_at, updated_at`

func queryComments(ctx context.Context, q querier, where string, args ...any) ([]*Comment, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+commentCols+` FROM comments WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Comment{}
	for rows.Next() {
		c := &Comment{}
		if err := rows.Scan(&c.ID, &c.IssueID, &c.ProjectID, &c.ActorID, &c.CommentHTML, &c.Access, &c.EditedAt, &c.ExternalSource, &c.ExternalID,
			&c.CreatedBy, &c.UpdatedBy, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func getComment(ctx context.Context, q querier, issueID, id string) (*Comment, error) {
	list, err := queryComments(ctx, q, `issue_id = ? AND id = ?`, issueID, id)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, ErrNotFound
	}
	return list[0], nil
}

// ListComments lists an issue's comments, newest first.
func (s *Store) ListComments(ctx context.Context, issueID string) ([]*Comment, error) {
	return queryComments(ctx, s.r, `issue_id = ? ORDER BY created_at DESC, rowid DESC`, issueID)
}

// GetComment returns one comment of the issue.
func (s *Store) GetComment(ctx context.Context, issueID, id string) (*Comment, error) {
	return getComment(ctx, s.r, issueID, id)
}

func (in *CommentInput) apply(ctx context.Context, q querier, c *Comment) error {
	if in.CommentHTML.Set {
		c.CommentHTML = SanitizeHTML(in.CommentHTML.V)
	}
	if in.Access.Set {
		c.Access = in.Access.V
	}
	if c.Access != "INTERNAL" && c.Access != "EXTERNAL" {
		return invalid("access", `"`+c.Access+`" is not a valid choice.`)
	}
	c.ExternalSource, c.ExternalID = mergeExt(c.ExternalSource, c.ExternalID, in.ExternalSource, in.ExternalID)
	return checkExternal(ctx, q, "comments", "project_id", c.ProjectID, c.ExternalSource, c.ExternalID, c.ID, "Work item comment")
}

func commentActivity(ctx context.Context, tx *sql.Tx, c *Comment, actor, verb string) error {
	field := "comment"
	a := &Activity{IssueID: c.IssueID, ProjectID: c.ProjectID, ActorID: &actor, Verb: verb, Field: &field, Comment: verb + " a comment"}
	if verb != "deleted" {
		a.NewValue, a.IssueCommentID = &c.CommentHTML, &c.ID
	}
	return logActivity(ctx, tx, a)
}

// CreateComment adds a comment by actor.
func (s *Store) CreateComment(ctx context.Context, issue *Issue, actor string, in CommentInput) (*Comment, error) {
	var out *Comment
	err := s.tx(ctx, func(tx *sql.Tx) error {
		ts := now()
		c := &Comment{ID: newID(), IssueID: issue.ID, ProjectID: issue.ProjectID, ActorID: &actor, CommentHTML: "<p></p>", Access: "INTERNAL",
			CreatedBy: &actor, CreatedAt: ts, UpdatedAt: ts}
		if err := in.apply(ctx, tx, c); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO comments (`+commentCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			c.ID, c.IssueID, c.ProjectID, c.ActorID, c.CommentHTML, c.Access, nil, c.ExternalSource, c.ExternalID, actor, nil, ts, ts)
		if err != nil {
			return err
		}
		out = c
		return commentActivity(ctx, tx, c, actor, "created")
	})
	return out, err
}

// UpdateComment edits a comment and sets edited_at.
func (s *Store) UpdateComment(ctx context.Context, issueID, id, actor string, in CommentInput) (*Comment, error) {
	var out *Comment
	err := s.tx(ctx, func(tx *sql.Tx) error {
		c, err := getComment(ctx, tx, issueID, id)
		if err != nil {
			return err
		}
		if err := in.apply(ctx, tx, c); err != nil {
			return err
		}
		ts := now()
		c.EditedAt, c.UpdatedBy, c.UpdatedAt = &ts, &actor, ts
		_, err = tx.ExecContext(ctx, `UPDATE comments SET comment_html=?, access=?, edited_at=?, external_source=?, external_id=?, updated_by=?, updated_at=? WHERE id=?`,
			c.CommentHTML, c.Access, ts, c.ExternalSource, c.ExternalID, actor, ts, id)
		if err != nil {
			return err
		}
		out = c
		return commentActivity(ctx, tx, c, actor, "updated")
	})
	return out, err
}

// DeleteComment deletes a comment.
func (s *Store) DeleteComment(ctx context.Context, issueID, id, actor string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		c, err := getComment(ctx, tx, issueID, id)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM comments WHERE id = ?`, id); err != nil {
			return err
		}
		return commentActivity(ctx, tx, c, actor, "deleted")
	})
}

// Link is a URL attached to a work item.
type Link struct {
	ID, IssueID, ProjectID string
	Title                  *string
	URL                    string
	Metadata               string // raw JSON object
	CreatedBy, UpdatedBy   *string
	CreatedAt, UpdatedAt   string
}

// LinkInput is the writable part of a link. Metadata is ignored on create.
type LinkInput struct {
	Title    Opt[*string]         `json:"title"`
	URL      Opt[string]          `json:"url"`
	Metadata Opt[json.RawMessage] `json:"metadata"`
}

const linkCols = `id, issue_id, project_id, title, url, metadata, created_by, updated_by, created_at, updated_at`

func queryLinks(ctx context.Context, q querier, where string, args ...any) ([]*Link, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+linkCols+` FROM issue_links WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Link{}
	for rows.Next() {
		l := &Link{}
		if err := rows.Scan(&l.ID, &l.IssueID, &l.ProjectID, &l.Title, &l.URL, &l.Metadata, &l.CreatedBy, &l.UpdatedBy, &l.CreatedAt, &l.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func getLink(ctx context.Context, q querier, issueID, id string) (*Link, error) {
	list, err := queryLinks(ctx, q, `issue_id = ? AND id = ?`, issueID, id)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, ErrNotFound
	}
	return list[0], nil
}

// ListLinks lists an issue's links, newest first.
func (s *Store) ListLinks(ctx context.Context, issueID string) ([]*Link, error) {
	return queryLinks(ctx, s.r, `issue_id = ? ORDER BY created_at DESC, rowid DESC`, issueID)
}

// GetLink returns one link of the issue.
func (s *Store) GetLink(ctx context.Context, issueID, id string) (*Link, error) {
	return getLink(ctx, s.r, issueID, id)
}

func (in *LinkInput) apply(l *Link, creating bool) error {
	if in.URL.Set {
		l.URL = strings.TrimSpace(in.URL.V)
	}
	if in.Title.Set {
		l.Title = in.Title.V
	}
	if in.Metadata.Set && !creating {
		var obj map[string]any
		if json.Unmarshal(in.Metadata.V, &obj) != nil || obj == nil {
			return invalid("metadata", "Expected a JSON object.")
		}
		l.Metadata = string(in.Metadata.V)
	}
	if l.URL == "" {
		return invalid("url", "This field is required.")
	}
	u, err := url.Parse(l.URL)
	if err != nil || u.Host == "" {
		return invalid("url", "Invalid URL format.")
	}
	// Plane skips the scheme check on PATCH; we keep it so no javascript: URL
	// can ever be stored and rendered as a link.
	if u.Scheme != "http" && u.Scheme != "https" {
		return invalid("url", "Invalid URL scheme.")
	}
	return nil
}

func linkActivity(ctx context.Context, tx *sql.Tx, l *Link, actor, verb string) error {
	field := "link"
	a := &Activity{IssueID: l.IssueID, ProjectID: l.ProjectID, ActorID: &actor, Verb: verb, Field: &field, Comment: verb + " a link"}
	if verb == "deleted" {
		a.OldValue = &l.URL
	} else {
		a.NewValue, a.NewIdentifier = &l.URL, &l.ID
	}
	return logActivity(ctx, tx, a)
}

// CreateLink attaches a URL to the issue. The same URL twice is refused.
func (s *Store) CreateLink(ctx context.Context, issue *Issue, actor string, in LinkInput) (*Link, error) {
	var out *Link
	err := s.tx(ctx, func(tx *sql.Tx) error {
		ts := now()
		l := &Link{ID: newID(), IssueID: issue.ID, ProjectID: issue.ProjectID, Metadata: "{}", CreatedBy: &actor, CreatedAt: ts, UpdatedAt: ts}
		if err := in.apply(l, true); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO issue_links (`+linkCols+`) VALUES (?,?,?,?,?,?,?,?,?,?)`,
			l.ID, l.IssueID, l.ProjectID, l.Title, l.URL, l.Metadata, actor, nil, ts, ts)
		if isUnique(err) {
			return viewErr("URL already exists for this Issue")
		}
		if err != nil {
			return err
		}
		out = l
		return linkActivity(ctx, tx, l, actor, "created")
	})
	return out, err
}

// UpdateLink applies a partial update.
func (s *Store) UpdateLink(ctx context.Context, issueID, id, actor string, in LinkInput) (*Link, error) {
	var out *Link
	err := s.tx(ctx, func(tx *sql.Tx) error {
		l, err := getLink(ctx, tx, issueID, id)
		if err != nil {
			return err
		}
		if err := in.apply(l, false); err != nil {
			return err
		}
		ts := now()
		l.UpdatedBy, l.UpdatedAt = &actor, ts
		_, err = tx.ExecContext(ctx, `UPDATE issue_links SET title=?, url=?, metadata=?, updated_by=?, updated_at=? WHERE id=?`, l.Title, l.URL, l.Metadata, actor, ts, id)
		if isUnique(err) {
			return viewErr("URL already exists for this Issue")
		}
		if err != nil {
			return err
		}
		out = l
		return linkActivity(ctx, tx, l, actor, "updated")
	})
	return out, err
}

// DeleteLink removes a link.
func (s *Store) DeleteLink(ctx context.Context, issueID, id, actor string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		l, err := getLink(ctx, tx, issueID, id)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM issue_links WHERE id = ?`, id); err != nil {
			return err
		}
		return linkActivity(ctx, tx, l, actor, "deleted")
	})
}
