package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// SessionTTL is how long a login session lasts.
const SessionTTL = 30 * 24 * time.Hour

// User is an account. Password hashes never leave the store.
type User struct {
	ID          string
	Email       string
	DisplayName string
	FirstName   string
	LastName    string
	IsActive    bool
	CreatedAt   string
}

// Member is a user with their workspace role.
type Member struct {
	User
	MemberID string
	Role     int
}

// Workspace is the single top-level container.
type Workspace struct {
	ID, Name, Slug, CreatedAt, UpdatedAt string
}

// APIToken describes a token; the secret itself is only returned at creation.
type APIToken struct {
	ID, UserID, Label, CreatedAt string
	LastUsedAt                   *string
}

// ErrBadLogin is returned for an unknown email or a wrong password.
var ErrBadLogin = errors.New("invalid email or password")

var dummyHash = sync.OnceValue(func() []byte {
	h, _ := bcrypt.GenerateFromPassword([]byte("not-a-real-password"), bcrypt.DefaultCost)
	return h
})

const userCols = `u.id, u.email, u.display_name, u.first_name, u.last_name, u.is_active, u.created_at`

func scanUser(sc interface{ Scan(...any) error }, extra ...any) (*User, error) {
	u := &User{}
	err := sc.Scan(append([]any{&u.ID, &u.Email, &u.DisplayName, &u.FirstName, &u.LastName, &u.IsActive, &u.CreatedAt}, extra...)...)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

func hashToken(t string) []byte {
	h := sha256.Sum256([]byte(t))
	return h[:]
}

func randomToken(prefix string) string {
	b := make([]byte, 32)
	rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

// EnsureWorkspace returns the workspace with slug, creating it if missing.
func (s *Store) EnsureWorkspace(ctx context.Context, slug string) (*Workspace, error) {
	if ws, err := s.Workspace(ctx, slug); err == nil {
		return ws, nil
	}
	ts := now()
	_, err := s.w.ExecContext(ctx, `INSERT INTO workspaces (id, name, slug, created_at, updated_at) VALUES (?, ?, ?, ?, ?) ON CONFLICT (slug) DO NOTHING`,
		newID(), slug, slug, ts, ts)
	if err != nil {
		return nil, err
	}
	return s.workspace(ctx, s.w, slug)
}

// Workspace looks a workspace up by slug.
func (s *Store) Workspace(ctx context.Context, slug string) (*Workspace, error) {
	return s.workspace(ctx, s.r, slug)
}

func (s *Store) workspace(ctx context.Context, q querier, slug string) (*Workspace, error) {
	ws := &Workspace{}
	err := q.QueryRowContext(ctx, `SELECT id, name, slug, created_at, updated_at FROM workspaces WHERE slug = ?`, slug).
		Scan(&ws.ID, &ws.Name, &ws.Slug, &ws.CreatedAt, &ws.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return ws, err
}

// UserCount returns how many users exist; 0 means setup is still needed.
func (s *Store) UserCount(ctx context.Context) (int, error) {
	var n int
	err := s.r.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func validateNewUser(email, password string) error {
	if !strings.Contains(email, "@") || len(email) > 254 {
		return invalid("email", "Enter a valid email address.")
	}
	if len(password) < 8 {
		return invalid("password", "Password must be at least 8 characters.")
	}
	if len(password) > 72 {
		return invalid("password", "Password must be at most 72 bytes.")
	}
	return nil
}

func insertUser(ctx context.Context, tx *sql.Tx, wsID, email, password, display string, role int) (*User, error) {
	email = strings.TrimSpace(email)
	if err := validateNewUser(email, password); err != nil {
		return nil, err
	}
	if role != RoleAdmin && role != RoleMember && role != RoleGuest {
		return nil, invalid("role", "Role must be one of 5, 15, 20.")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	display = strings.TrimSpace(display)
	if display == "" {
		display = strings.SplitN(email, "@", 2)[0]
	}
	first, last, _ := strings.Cut(display, " ")
	u := &User{ID: newID(), Email: email, DisplayName: display, FirstName: first, LastName: strings.TrimSpace(last), IsActive: true, CreatedAt: now()}
	_, err = tx.ExecContext(ctx, `INSERT INTO users (id, email, password_hash, display_name, first_name, last_name, is_active, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?)`, u.ID, u.Email, string(hash), u.DisplayName, u.FirstName, u.LastName, u.CreatedAt, u.CreatedAt)
	if isUnique(err) {
		return nil, &Error{Kind: KindConflict, Field: "email", Msg: "A user with this email already exists."}
	}
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO workspace_members (id, workspace_id, user_id, role, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		newID(), wsID, u.ID, role, u.CreatedAt, u.CreatedAt)
	return u, err
}

// Setup creates the first admin and the workspace. It is refused once any
// user exists.
func (s *Store) Setup(ctx context.Context, slug, email, password, display string) (*User, error) {
	ws, err := s.EnsureWorkspace(ctx, slug)
	if err != nil {
		return nil, err
	}
	var u *User
	err = s.tx(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return forbidden("Setup has already been completed.")
		}
		u, err = insertUser(ctx, tx, ws.ID, email, password, display, RoleAdmin)
		return err
	})
	return u, err
}

// CreateUser adds a user to the workspace with role.
func (s *Store) CreateUser(ctx context.Context, wsID, email, password, display string, role int) (*User, error) {
	var u *User
	err := s.tx(ctx, func(tx *sql.Tx) (err error) {
		u, err = insertUser(ctx, tx, wsID, email, password, display, role)
		return err
	})
	return u, err
}

// Authenticate checks an email and password.
func (s *Store) Authenticate(ctx context.Context, email, password string) (*User, error) {
	var hash string
	u, err := scanUser(s.r.QueryRowContext(ctx, `SELECT `+userCols+`, u.password_hash FROM users u WHERE u.email = ?`, strings.TrimSpace(email)), &hash)
	if errors.Is(err, ErrNotFound) {
		// Spend the same time as a real check so emails cannot be probed by timing.
		bcrypt.CompareHashAndPassword(dummyHash(), []byte(password))
		return nil, ErrBadLogin
	}
	if err != nil {
		return nil, err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil || !u.IsActive {
		return nil, ErrBadLogin
	}
	return u, nil
}

// CreateSession returns a new random session token for userID.
func (s *Store) CreateSession(ctx context.Context, userID string) (string, error) {
	tok := randomToken("")
	t := time.Now().UTC()
	_, err := s.w.ExecContext(ctx, `INSERT INTO sessions (token_hash, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		hashToken(tok), userID, t.Format(TimeLayout), t.Add(SessionTTL).Format(TimeLayout))
	return tok, err
}

// SessionUser resolves a session token. ErrNotFound if unknown or expired.
func (s *Store) SessionUser(ctx context.Context, token string) (*User, error) {
	h := hashToken(token)
	var stored []byte
	u, err := scanUser(s.r.QueryRowContext(ctx, `SELECT `+userCols+`, s.token_hash FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ? AND s.expires_at > ? AND u.is_active = 1`, h, now()), &stored)
	if err != nil {
		return nil, err
	}
	if subtle.ConstantTimeCompare(h, stored) != 1 {
		return nil, ErrNotFound
	}
	return u, nil
}

// DeleteSession logs a session out.
func (s *Store) DeleteSession(ctx context.Context, token string) error {
	_, err := s.w.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, hashToken(token))
	return err
}

// CreateToken makes an API token and returns its secret once.
func (s *Store) CreateToken(ctx context.Context, userID, label string) (*APIToken, string, error) {
	secret := randomToken("trk_")
	t := &APIToken{ID: newID(), UserID: userID, Label: strings.TrimSpace(label), CreatedAt: now()}
	_, err := s.w.ExecContext(ctx, `INSERT INTO api_tokens (id, user_id, label, token_hash, created_at) VALUES (?, ?, ?, ?, ?)`,
		t.ID, t.UserID, t.Label, hashToken(secret), t.CreatedAt)
	if err != nil {
		return nil, "", err
	}
	return t, secret, nil
}

// ListTokens lists a user's tokens, newest first.
func (s *Store) ListTokens(ctx context.Context, userID string) ([]*APIToken, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT id, user_id, label, created_at, last_used_at FROM api_tokens WHERE user_id = ? ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*APIToken{}
	for rows.Next() {
		t := &APIToken{}
		if err := rows.Scan(&t.ID, &t.UserID, &t.Label, &t.CreatedAt, &t.LastUsedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DeleteToken revokes one of userID's tokens.
func (s *Store) DeleteToken(ctx context.Context, userID, id string) error {
	res, err := s.w.ExecContext(ctx, `DELETE FROM api_tokens WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// TokenUser resolves an API token secret to its active owner.
func (s *Store) TokenUser(ctx context.Context, secret string) (*User, error) {
	h := hashToken(secret)
	var stored []byte
	var id string
	var lastUsed *string
	u, err := scanUser(s.r.QueryRowContext(ctx, `SELECT `+userCols+`, t.token_hash, t.id, t.last_used_at FROM api_tokens t JOIN users u ON u.id = t.user_id
		WHERE t.token_hash = ? AND u.is_active = 1`, h), &stored, &id, &lastUsed)
	if err != nil {
		return nil, err
	}
	if subtle.ConstantTimeCompare(h, stored) != 1 {
		return nil, ErrNotFound
	}
	// Record use at most once a minute so reads do not all queue on the writer.
	ts := time.Now().UTC()
	if lastUsed == nil || *lastUsed < ts.Add(-time.Minute).Format(TimeLayout) {
		s.w.ExecContext(ctx, `UPDATE api_tokens SET last_used_at = ? WHERE id = ?`, ts.Format(TimeLayout), id)
	}
	return u, nil
}

// MemberRole returns userID's role in the workspace, or ErrNotFound.
func (s *Store) MemberRole(ctx context.Context, wsID, userID string) (int, error) {
	var role int
	err := s.r.QueryRowContext(ctx, `SELECT role FROM workspace_members WHERE workspace_id = ? AND user_id = ?`, wsID, userID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return role, err
}

// UserWorkspace returns the workspace userID belongs to and their role there,
// or ErrNotFound. There is one workspace, so the first membership is the one.
func (s *Store) UserWorkspace(ctx context.Context, userID string) (*Workspace, int, error) {
	ws := &Workspace{}
	var role int
	err := s.r.QueryRowContext(ctx, `SELECT w.id, w.name, w.slug, w.created_at, w.updated_at, m.role
		FROM workspace_members m JOIN workspaces w ON w.id = m.workspace_id WHERE m.user_id = ? ORDER BY w.created_at LIMIT 1`, userID).
		Scan(&ws.ID, &ws.Name, &ws.Slug, &ws.CreatedAt, &ws.UpdatedAt, &role)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, 0, ErrNotFound
	}
	return ws, role, err
}

// ListMembers lists workspace members ordered by display name.
func (s *Store) ListMembers(ctx context.Context, wsID string) ([]*Member, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT `+userCols+`, m.id, m.role FROM workspace_members m JOIN users u ON u.id = m.user_id
		WHERE m.workspace_id = ? ORDER BY u.display_name, u.email`, wsID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Member{}
	for rows.Next() {
		m := &Member{}
		u, err := scanUser(rows, &m.MemberID, &m.Role)
		if err != nil {
			return nil, err
		}
		m.User = *u
		out = append(out, m)
	}
	return out, rows.Err()
}

// GetMember returns one workspace member by user id or membership id.
func (s *Store) GetMember(ctx context.Context, wsID, id string) (*Member, error) {
	m := &Member{}
	u, err := scanUser(s.r.QueryRowContext(ctx, `SELECT `+userCols+`, m.id, m.role FROM workspace_members m JOIN users u ON u.id = m.user_id
		WHERE m.workspace_id = ? AND (u.id = ? OR m.id = ?)`, wsID, id, id), &m.MemberID, &m.Role)
	if err != nil {
		return nil, err
	}
	m.User = *u
	return m, nil
}

// UserByEmail looks a user up by email (case-insensitive).
func (s *Store) UserByEmail(ctx context.Context, email string) (*User, error) {
	return scanUser(s.r.QueryRowContext(ctx, `SELECT `+userCols+` FROM users u WHERE u.email = ?`, strings.TrimSpace(email)))
}

// UsersByIDs batch-loads users.
func (s *Store) UsersByIDs(ctx context.Context, ids []string) (map[string]*User, error) {
	out := map[string]*User{}
	if len(ids) == 0 {
		return out, nil
	}
	ph, args := in(dedupe(ids))
	rows, err := s.r.QueryContext(ctx, `SELECT `+userCols+` FROM users u WHERE u.id IN `+ph, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out[u.ID] = u
	}
	return out, rows.Err()
}

// activeMembers returns which of ids are workspace members with at least minRole.
func activeMembers(ctx context.Context, q querier, wsID string, ids []string, minRole int) (map[string]bool, error) {
	out := map[string]bool{}
	if len(ids) == 0 {
		return out, nil
	}
	ph, args := in(ids)
	rows, err := q.QueryContext(ctx, `SELECT m.user_id FROM workspace_members m JOIN users u ON u.id = m.user_id
		WHERE m.workspace_id = ? AND u.is_active = 1 AND m.role >= ? AND m.user_id IN `+ph, append([]any{wsID, minRole}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}
