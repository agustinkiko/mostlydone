// Package mcp serves the MCP endpoint at /mcp. It calls internal/store
// directly. Each request is authenticated, then served by a stateless,
// JSON-response MCP server whose tools act as that request's user.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"mostlydone/internal/api"
	"mostlydone/internal/store"
)

// caller is the authenticated user of one request.
type caller struct {
	user *store.User
	ws   *store.Workspace
	role int
}

type callerKey struct{}

// Handler returns the /mcp handler. auth resolves a request to its user from
// an API token (X-API-Key or Authorization: Bearer); main passes
// api.TokenAuth(s). Any auth failure is answered with 401. limiter (nil = off)
// is the API's, so a token's REST and MCP calls share one bucket; it is
// charged only after authentication succeeds.
//
// The SDK runs in stateless + JSON-response mode: no sessions are kept between
// requests and no streams stay open, so idle memory does not grow. A fresh
// server is built per request, bound to that request's user; only the schema
// cache is shared.
func Handler(s *store.Store, auth func(*http.Request) (*store.User, error), limiter *api.Limiter) http.Handler {
	cache := sdk.NewSchemaCache()
	inner := sdk.NewStreamableHTTPHandler(func(r *http.Request) *sdk.Server {
		c, ok := r.Context().Value(callerKey{}).(*caller)
		if !ok {
			return nil
		}
		return newServer(s, c, cache)
	}, &sdk.StreamableHTTPOptions{Stateless: true, JSONResponse: true})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, err := auth(r)
		if err != nil {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeDetail(w, http.StatusUnauthorized, "Authentication credentials were not provided or are not valid.")
			return
		}
		if !limiter.Admit(w, api.TokenKey(r)) {
			return
		}
		ws, role, err := s.UserWorkspace(r.Context(), u.ID)
		if err != nil {
			writeDetail(w, http.StatusForbidden, "You do not have access to a workspace.")
			return
		}
		c := &caller{user: u, ws: ws, role: role}
		inner.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), callerKey{}, c)))
	})
}

func writeDetail(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"detail": msg})
}

// tk holds what every tool handler needs for one request.
type tk struct {
	s *store.Store
	caller
}

func newServer(s *store.Store, c *caller, cache *sdk.SchemaCache) *sdk.Server {
	srv := sdk.NewServer(&sdk.Implementation{Name: "mostlydone", Version: "1"}, &sdk.ServerOptions{SchemaCache: cache})
	t := &tk{s: s, caller: *c}
	t.register(srv)
	return srv
}

// userError is a problem the caller can fix; its message goes to the agent.
type userError string

func (e userError) Error() string { return string(e) }

func errf(format string, args ...any) error { return userError(fmt.Sprintf(format, args...)) }

// friendly turns a store or lookup error into the message the agent sees.
// Unexpected errors are logged and hidden.
func friendly(err error) error {
	var se *store.Error
	var ue userError
	switch {
	case errors.As(err, &ue):
		return err
	case errors.As(err, &se):
		msg := se.Msg
		if msg == "" {
			if m, ok := se.Body["error"].(string); ok {
				msg = m
			}
		}
		if se.Field != "" && se.Field != "non_field_errors" {
			msg = se.Field + ": " + msg
		}
		return errors.New(msg)
	}
	log.Printf("mcp: %v", err)
	return errors.New("internal error")
}

func reply(v any) (*sdk.CallToolResult, any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, nil, friendly(err)
	}
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(b)}}}, nil, nil
}

func fail(err error) (*sdk.CallToolResult, any, error) { return nil, nil, friendly(err) }

func (t *tk) needWrite() error {
	if t.role < store.RoleMember {
		return errf("your account is a guest: you can read and comment, not change anything")
	}
	return nil
}

// named is an id/name pair a reference can resolve to.
type named struct{ id, name string }

// pick resolves ref (an id or a case-insensitive name) among items. The error
// names the valid options.
func pick(kind, scope, ref string, items []named) (string, error) {
	ref = strings.TrimSpace(ref)
	var hits []named
	for _, it := range items {
		if it.id == ref {
			return it.id, nil
		}
		if strings.EqualFold(it.name, ref) {
			hits = append(hits, it)
		}
	}
	if len(hits) == 1 {
		return hits[0].id, nil
	}
	if len(hits) > 1 {
		return "", errf("%s %q is ambiguous in %s; use its id", kind, ref, scope)
	}
	names := make([]string, len(items))
	for i, it := range items {
		names[i] = it.name
	}
	if len(names) == 0 {
		return "", errf("unknown %s %q: %s has none yet", kind, ref, scope)
	}
	return "", errf("unknown %s %q in %s; valid: %s", kind, ref, scope, strings.Join(names, ", "))
}

// pickAll resolves several refs; nil in gives nil out.
func pickAll(kind, scope string, refs []string, items []named) ([]string, error) {
	if refs == nil {
		return nil, nil
	}
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		id, err := pick(kind, scope, r, items)
		if err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, nil
}

func (t *tk) project(ctx context.Context, ref string) (*store.Project, error) {
	p, err := t.s.ProjectByRef(ctx, t.ws.ID, strings.TrimSpace(ref))
	if err == nil {
		return p, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	list, err := t.s.ListProjects(ctx, t.ws.ID, "")
	if err != nil {
		return nil, err
	}
	opts := make([]string, len(list))
	for i, p := range list {
		opts[i] = p.Identifier + " (" + p.Name + ")"
	}
	if len(opts) == 0 {
		return nil, errf("unknown project %q: there are no projects yet; use create_project", ref)
	}
	return nil, errf("unknown project %q; valid: %s", ref, strings.Join(opts, ", "))
}

// item resolves "PROJ-12" to a work item.
func (t *tk) item(ctx context.Context, ref string) (*store.Issue, error) {
	ref = strings.TrimSpace(ref)
	var seq int
	i := strings.LastIndex(ref, "-")
	if i <= 0 {
		return nil, errf("bad work item %q: use PROJ-12", ref)
	}
	if _, err := fmt.Sscanf(ref[i+1:], "%d", &seq); err != nil || seq <= 0 {
		return nil, errf("bad work item %q: use PROJ-12", ref)
	}
	is, err := t.s.IssueByIdentifier(ctx, t.ws.ID, ref[:i], seq)
	if errors.Is(err, store.ErrNotFound) {
		return nil, errf("work item %q not found", ref)
	}
	return is, err
}

func (t *tk) members(ctx context.Context) ([]*store.Member, error) {
	return t.s.ListMembers(ctx, t.ws.ID)
}

func userName(u *store.User) string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.Email
}

// userIDs resolves people by id, email, display name or "me".
func (t *tk) userIDs(ctx context.Context, refs []string) ([]string, error) {
	if refs == nil {
		return nil, nil
	}
	list, err := t.members(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if strings.EqualFold(ref, "me") {
			out = append(out, t.user.ID)
			continue
		}
		var hits []*store.Member
		exact := ""
		for _, m := range list {
			switch {
			case m.ID == ref || strings.EqualFold(m.Email, ref):
				exact = m.ID
			case m.DisplayName != "" && strings.EqualFold(m.DisplayName, ref):
				hits = append(hits, m)
			}
		}
		switch {
		case exact != "":
			out = append(out, exact)
		case len(hits) == 1:
			out = append(out, hits[0].ID)
		default:
			opts := make([]string, len(list))
			for i, m := range list {
				opts[i] = userName(&m.User) + " <" + m.Email + ">"
			}
			if len(hits) > 1 {
				return nil, errf("person %q is ambiguous; use an email", ref)
			}
			return nil, errf("unknown person %q; use an email, name or me. Members: %s", ref, strings.Join(opts, ", "))
		}
	}
	return out, nil
}
