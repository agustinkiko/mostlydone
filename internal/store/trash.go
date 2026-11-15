package store

import (
	"context"
	"database/sql"
	"time"
)

// DefaultTrashRetention is how long a deleted project stays in the trash.
const DefaultTrashRetention = 30 * 24 * time.Hour

func (s *Store) clock() time.Time { return s.now().UTC() }

// PurgeAt returns when a project trashed at deletedAt (TimeLayout) is purged.
func (s *Store) PurgeAt(deletedAt string) string {
	t, err := time.Parse(TimeLayout, deletedAt)
	if err != nil {
		return deletedAt
	}
	return t.Add(s.TrashRetention).Format(TimeLayout)
}

func (s *Store) cutoff() string {
	return s.clock().Add(-s.TrashRetention).Format(TimeLayout)
}

// DeleteProject moves a project to the trash. Its identifier and name stay
// reserved until it is purged. Deleting a trashed or unknown project is
// ErrNotFound.
func (s *Store) DeleteProject(ctx context.Context, wsID, id string) error {
	res, err := s.w.ExecContext(ctx, `UPDATE projects SET deleted_at = ? WHERE workspace_id = ? AND id = ? AND deleted_at IS NULL`,
		s.clock().Format(TimeLayout), wsID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListTrashedProjects lists the workspace's trashed projects, newest deleted
// first. Projects past retention but not yet purged are not listed.
func (s *Store) ListTrashedProjects(ctx context.Context, wsID string) ([]*Project, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT `+projectCols+` FROM projects p WHERE p.workspace_id = ? AND p.deleted_at IS NOT NULL
		AND p.deleted_at >= ? ORDER BY p.deleted_at DESC, p.id`, wsID, s.cutoff())
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

// RestoreProject takes a project out of the trash. It is ErrNotFound unless
// the project is trashed and within retention.
func (s *Store) RestoreProject(ctx context.Context, wsID, id string) (*Project, error) {
	var out *Project
	err := s.tx(ctx, func(tx *sql.Tx) error {
		p, err := scanProject(tx.QueryRowContext(ctx, `SELECT `+projectCols+` FROM projects p WHERE p.workspace_id = ? AND p.id = ?
			AND p.deleted_at IS NOT NULL AND p.deleted_at >= ?`, wsID, id, s.cutoff()))
		if err != nil {
			return err
		}
		if err := checkProjectTaken(ctx, tx, p); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE projects SET deleted_at = NULL WHERE id = ?`, id); err != nil {
			return err
		}
		out, err = getProject(ctx, tx, wsID, id)
		return err
	})
	return out, err
}

// PurgeProject permanently deletes a trashed project and everything in it.
func (s *Store) PurgeProject(ctx context.Context, wsID, id string) error {
	res, err := s.w.ExecContext(ctx, `DELETE FROM projects WHERE workspace_id = ? AND id = ? AND deleted_at IS NOT NULL`, wsID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// PurgeExpiredProjects permanently deletes every trashed project, in all
// workspaces, whose retention has passed. It returns how many were purged.
func (s *Store) PurgeExpiredProjects(ctx context.Context) (int, error) {
	res, err := s.w.ExecContext(ctx, `DELETE FROM projects WHERE deleted_at IS NOT NULL AND deleted_at < ?`, s.cutoff())
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// RunTrashPurge purges expired projects once, then every interval, until ctx
// is cancelled. logf, if set, is called with the count when it is > 0 and
// with errors.
func (s *Store) RunTrashPurge(ctx context.Context, interval time.Duration, logf func(format string, args ...any)) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	purge := func() {
		n, err := s.PurgeExpiredProjects(ctx)
		switch {
		case ctx.Err() != nil:
		case err != nil:
			logf("trash purge: %v", err)
		case n > 0:
			logf("trash purge: permanently deleted %d expired project(s)", n)
		}
	}
	purge()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			purge()
		}
	}
}
