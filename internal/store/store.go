// Package store owns the SQLite database: schema, migrations, all SQL and the
// data rules (sequence ids, default states, completed_at, activity writing).
// It has no HTTP types; both the REST API and the MCP layer call it.
package store

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// TimeLayout is the wire and storage format for datetimes. Stored as text it
// sorts in time order.
const TimeLayout = "2006-01-02T15:04:05.000000Z"

// Workspace roles.
const (
	RoleGuest  = 5
	RoleMember = 15
	RoleAdmin  = 20
)

// Store is the database handle. Writes go through a single connection so they
// queue in Go instead of failing with SQLITE_BUSY; reads use a small pool.
type Store struct {
	w *sql.DB
	r *sql.DB

	// TrashRetention is how long a deleted project stays restorable.
	TrashRetention time.Duration
	now            func() time.Time // clock; injectable for tests
}

// Open opens (creating if needed) the database file and applies migrations.
func Open(path string) (*Store, error) {
	common := "_busy_timeout=10000&_foreign_keys=1&_synchronous=NORMAL"
	w, err := sql.Open("sqlite", path+"?_txlock=immediate&_journal_mode=WAL&"+common)
	if err != nil {
		return nil, err
	}
	w.SetMaxOpenConns(1)
	w.SetConnMaxIdleTime(0)
	if err := w.Ping(); err != nil {
		w.Close()
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	if err := migrate(w, path); err != nil {
		w.Close()
		return nil, err
	}
	r, err := sql.Open("sqlite", path+"?_query_only=1&"+common)
	if err != nil {
		w.Close()
		return nil, err
	}
	r.SetMaxOpenConns(4)
	r.SetMaxIdleConns(4)
	return &Store{w: w, r: r, TrashRetention: DefaultTrashRetention, now: time.Now}, nil
}

// Close closes both handles.
func (s *Store) Close() error {
	return errors.Join(s.r.Close(), s.w.Close())
}

func migrate(db *sql.DB, path string) error { return migrateFS(db, migrationFS, path) }

// querier is satisfied by *sql.DB and *sql.Tx.
type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (s *Store) tx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// Error kinds the API maps to status codes.
type Kind int

const (
	KindInvalid   Kind = iota + 1 // 400
	KindForbidden                 // 403
	KindNotFound                  // 404
	KindConflict                  // 409
)

// Error is a rule violation reported to the client. Field set means a field
// error (`{"field": ["msg"]}`, or `{"field": "msg"}` for conflicts); otherwise
// a view error (`{"error": "msg"}` plus ID/Code when set). Body, when set,
// is the exact response body (for Plane's few irregular error shapes).
type Error struct {
	Kind  Kind
	Field string
	Msg   string
	ID    string
	Code  string
	Body  map[string]any
}

func (e *Error) Error() string {
	if e.Field != "" {
		return e.Field + ": " + e.Msg
	}
	return e.Msg
}

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = &Error{Kind: KindNotFound, Msg: "The requested resource does not exist."}

func invalid(field, msg string) error { return &Error{Kind: KindInvalid, Field: field, Msg: msg} }
func viewErr(msg string) error        { return &Error{Kind: KindInvalid, Msg: msg} }
func forbidden(msg string) error      { return &Error{Kind: KindForbidden, Msg: msg} }
func conflict(msg, id string) error   { return &Error{Kind: KindConflict, Msg: msg, ID: id} }

// Opt is an optional input field. Set is true when the key was present in the
// JSON input (even if null), which is how PATCH tells "absent" from "clear".
type Opt[T any] struct {
	Set bool
	V   T
}

// Some returns a set Opt.
func Some[T any](v T) Opt[T] { return Opt[T]{Set: true, V: v} }

func (o *Opt[T]) UnmarshalJSON(b []byte) error {
	o.Set = true
	return json.Unmarshal(b, &o.V)
}

// Ptr returns a pointer to v.
func Ptr[T any](v T) *T { return &v }

func now() string    { return time.Now().UTC().Format(TimeLayout) }
func newID() string  { return uuid.NewString() }
func today() string  { return time.Now().UTC().Format(time.DateOnly) }
func epoch() float64 { return float64(time.Now().UnixMicro()) / 1e6 }

func isUUID(s string) bool {
	_, err := uuid.Parse(s)
	return err == nil && len(s) == 36
}

// in returns "(?,?,?)" and the args for an IN clause. Callers must not pass an
// empty slice.
func in(vals []string) (string, []any) {
	args := make([]any, len(vals))
	for i, v := range vals {
		args[i] = v
	}
	return "(" + strings.TrimSuffix(strings.Repeat("?,", len(vals)), ",") + ")", args
}

func dedupe(vals []string) []string {
	seen := make(map[string]bool, len(vals))
	out := vals[:0:0]
	for _, v := range vals {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

func validDate(field string, v *string) error {
	if v == nil {
		return nil
	}
	if _, err := time.Parse(time.DateOnly, *v); err != nil {
		return invalid(field, "Date has wrong format. Use one of these formats instead: YYYY-MM-DD.")
	}
	return nil
}

// checkExternal enforces the 409 rule: an (external_source, external_id) pair
// may appear once per scope. table and scopeCol are code constants.
func checkExternal(ctx context.Context, q querier, table, scopeCol, scope string, src, id *string, self, what string) error {
	if src == nil || id == nil || *src == "" || *id == "" {
		return nil
	}
	var existing string
	err := q.QueryRowContext(ctx, `SELECT id FROM `+table+` WHERE `+scopeCol+` = ? AND external_source = ? AND external_id = ? AND id != ?`,
		scope, *src, *id, self).Scan(&existing)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return conflict(what+" with the same external id and external source already exists", existing)
}

// mergeExt applies optional external_source/external_id inputs to current values.
func mergeExt(curSrc, curID *string, src, id Opt[*string]) (*string, *string) {
	if src.Set {
		curSrc = src.V
	}
	if id.Set {
		curID = id.V
	}
	return curSrc, curID
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// Page is an offset window for list queries.
type Page struct {
	Limit  int
	Offset int
}

func (p Page) clause() string {
	if p.Limit <= 0 {
		return ""
	}
	return fmt.Sprintf(" LIMIT %d OFFSET %d", p.Limit, p.Offset)
}

// orderClause turns an allow-listed order_by value (optional leading "-")
// into SQL; unknown names fall back to def.
func orderClause(orderBy string, allowed map[string]string, def string) string {
	desc := strings.HasPrefix(orderBy, "-")
	frag, ok := allowed[strings.TrimPrefix(orderBy, "-")]
	if !ok {
		return def
	}
	if desc {
		return frag + " DESC"
	}
	return frag + " ASC"
}
