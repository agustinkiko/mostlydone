// Package api serves the Plane v1-shaped REST API under /api/v1/ and the
// login/token endpoints under /auth/.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"net/netip"
	"strconv"
	"strings"

	"mostlydone/internal/store"
)

const (
	maxBody        = 1 << 20
	defaultPerPage = 100
	maxPerPage     = 250 // see SPEC.md, Pagination ruling
	cookieName     = "mostlydone_session"
	csrfHeader     = "X-Requested-With"
	csrfValue      = "mostlydone"
)

// API holds the handlers' dependencies.
type API struct {
	s             *store.Store
	slug          string
	secureCookies bool
	logins        *loginLimiter
	setup         *setupGate
	limiter       *Limiter // nil: off

	// ClientIPHeader, when set, names the proxy header that carries the real
	// client address for the login throttle. See clientIP.
	ClientIPHeader string
	// TrustedProxies are the peers whose ClientIPHeader is believed.
	TrustedProxies []netip.Prefix
}

// New returns the API for the workspace with slug.
func New(s *store.Store, slug string, secureCookies bool) *API {
	return &API{s: s, slug: slug, secureCookies: secureCookies, logins: newLoginLimiter(), setup: &setupGate{}}
}

// ErrUnauthenticated means no usable credentials were sent.
var ErrUnauthenticated = errors.New("authentication credentials were not provided")

// ErrBadToken means a token was sent but is unknown or revoked.
var ErrBadToken = errors.New("given API token is not valid")

// bearer returns the API token from X-API-Key or Authorization: Bearer.
func bearer(r *http.Request) string {
	if k := strings.TrimSpace(r.Header.Get("X-API-Key")); k != "" {
		return k
	}
	if h := r.Header.Get("Authorization"); len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

// TokenAuth returns the function the MCP layer uses to authenticate: it
// resolves the API token in `X-API-Key` or `Authorization: Bearer` to its
// owner. It returns ErrUnauthenticated when no token is sent and ErrBadToken
// when the token is unknown. Session cookies are not accepted.
func TokenAuth(s *store.Store) func(*http.Request) (*store.User, error) {
	return func(r *http.Request) (*store.User, error) {
		tok := bearer(r)
		if tok == "" {
			return nil, ErrUnauthenticated
		}
		u, err := s.TokenUser(r.Context(), tok)
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrBadToken
		}
		return u, err
	}
}

// authenticate resolves a token or a session cookie.
func (a *API) authenticate(r *http.Request) (u *store.User, viaToken bool, err error) {
	if bearer(r) != "" {
		u, err = TokenAuth(a.s)(r)
		return u, true, err
	}
	ck, cerr := r.Cookie(cookieName)
	if cerr != nil || ck.Value == "" {
		return nil, false, ErrUnauthenticated
	}
	u, err = a.s.SessionUser(r.Context(), ck.Value)
	if errors.Is(err, store.ErrNotFound) {
		return nil, false, errors.New("session expired or invalid")
	}
	return u, false, err
}

// Handler serves /api/v1/... and /auth/....
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	a.routes(mux)
	a.authRoutes(mux)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		// Plane paths end in "/"; accept them without it too.
		if strings.HasPrefix(r.URL.Path, "/api/v1/") && !strings.HasSuffix(r.URL.Path, "/") {
			r.URL.Path += "/"
			r.URL.RawPath = ""
		}
		w.Header().Set("Cache-Control", "no-store")
		mux.ServeHTTP(w, r)
	})
}

// rc is one request with its resolved caller, workspace and project.
type rc struct {
	w        http.ResponseWriter
	r        *http.Request
	a        *API
	user     *store.User
	viaToken bool
	ws       *store.Workspace
	role     int
	p        *store.Project
}

func safeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

// handle registers an /api/v1 route. minRole is the workspace role needed.
func (a *API) handle(mux *http.ServeMux, pattern string, minRole int, fn func(*rc)) {
	method, path, _ := strings.Cut(pattern, " ")
	mux.HandleFunc(method+" /api/v1"+path+"{$}", func(w http.ResponseWriter, r *http.Request) {
		c := &rc{w: w, r: r, a: a}
		var err error
		c.user, c.viaToken, err = a.authenticate(r)
		if err != nil {
			c.authFail(err)
			return
		}
		if !a.limiter.Admit(w, limitKey(r, c.viaToken, c.user)) {
			return
		}
		if !c.csrfOK() {
			return
		}
		if slug := r.PathValue("ws"); slug != "" {
			if c.ws, err = a.s.Workspace(r.Context(), slug); err != nil {
				c.json(http.StatusNotFound, map[string]any{"error": "Workspace does not exist"})
				return
			}
			if c.role, err = a.s.MemberRole(r.Context(), c.ws.ID, c.user.ID); err != nil {
				c.denied()
				return
			}
		}
		if c.role < minRole {
			c.denied()
			return
		}
		if pid := r.PathValue("pid"); pid != "" {
			if c.p, err = a.s.GetProject(r.Context(), c.ws.ID, pid); err != nil {
				c.fail(err)
				return
			}
		}
		fn(c)
	})
}

func (c *rc) authFail(err error) {
	msg := "Authentication credentials were not provided."
	switch {
	case errors.Is(err, ErrBadToken):
		msg = "Given API token is not valid"
	case !errors.Is(err, ErrUnauthenticated):
		if strings.HasPrefix(err.Error(), "session") {
			msg = "Session expired or invalid."
		} else {
			c.fail(err)
			return
		}
	}
	c.json(http.StatusUnauthorized, map[string]any{"detail": msg})
}

// csrfOK enforces the header on unsafe requests not authenticated by token.
func (c *rc) csrfOK() bool {
	if c.viaToken || safeMethod(c.r.Method) || c.r.Header.Get(csrfHeader) == csrfValue {
		return true
	}
	c.json(http.StatusForbidden, map[string]any{"detail": "CSRF check failed: send the header X-Requested-With: mostlydone."})
	return false
}

func (c *rc) denied() {
	c.json(http.StatusForbidden, map[string]any{"detail": "You do not have permission to perform this action."})
}

func (c *rc) json(status int, v any) {
	c.w.Header().Set("Content-Type", "application/json")
	c.w.WriteHeader(status)
	if v != nil {
		json.NewEncoder(c.w).Encode(v)
	}
}

func (c *rc) noContent() { c.w.WriteHeader(http.StatusNoContent) }

// fail maps store errors to Plane's error shapes.
func (c *rc) fail(err error) {
	var e *store.Error
	if !errors.As(err, &e) {
		log.Printf("%s %s: %v", c.r.Method, c.r.URL.Path, err)
		c.json(http.StatusInternalServerError, map[string]any{"error": "Something went wrong please try again later"})
		return
	}
	status := map[store.Kind]int{store.KindInvalid: 400, store.KindForbidden: 403, store.KindNotFound: 404, store.KindConflict: 409}[e.Kind]
	switch {
	case e.Body != nil:
		c.json(status, e.Body)
	case e.Field != "" && e.Kind == store.KindConflict:
		c.json(status, map[string]any{e.Field: e.Msg})
	case e.Field != "":
		c.json(status, map[string]any{e.Field: []string{e.Msg}})
	default:
		body := map[string]any{"error": e.Msg}
		if e.ID != "" {
			body["id"] = e.ID
		}
		if e.Code != "" {
			body["code"] = e.Code
		}
		c.json(status, body)
	}
}

// decode reads a JSON object body into v. On failure it has already replied.
func (c *rc) decode(v any) bool {
	var raw json.RawMessage
	if err := json.NewDecoder(c.r.Body).Decode(&raw); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			c.json(http.StatusRequestEntityTooLarge, map[string]any{"detail": "Request body too large."})
			return false
		}
		c.json(http.StatusBadRequest, map[string]any{"detail": "JSON parse error - " + err.Error()})
		return false
	}
	if t := strings.TrimSpace(string(raw)); !strings.HasPrefix(t, "{") {
		c.json(http.StatusBadRequest, map[string]any{"detail": "Request body must be a JSON object or array."})
		return false
	}
	if err := json.Unmarshal(raw, v); err != nil {
		var te *json.UnmarshalTypeError
		if errors.As(err, &te) && te.Field != "" {
			c.json(http.StatusBadRequest, map[string]any{te.Field: []string{"Incorrect type. Expected " + te.Type.String() + "."}})
			return false
		}
		c.json(http.StatusBadRequest, map[string]any{"error": "Please provide valid detail"})
		return false
	}
	return true
}

func (c *rc) isAdmin() bool { return c.role >= store.RoleAdmin }

func csv(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// page parses per_page and cursor ("<per_page>:<page>:<is_prev>").
func (c *rc) page() (perPage, page int, ok bool) {
	q := c.r.URL.Query()
	perPage = defaultPerPage
	if v := q.Get("per_page"); v != "" {
		n, err := strconv.Atoi(v)
		switch {
		case err != nil:
			c.json(400, map[string]any{"detail": "Invalid per_page parameter."})
			return 0, 0, false
		case n <= 0:
			c.json(400, map[string]any{"detail": "Invalid per_page value. Must be greater than zero."})
			return 0, 0, false
		case n > maxPerPage:
			c.json(400, map[string]any{"detail": fmt.Sprintf("Invalid per_page value. Cannot exceed %d.", maxPerPage)})
			return 0, 0, false
		}
		perPage = n
	}
	if cur := q.Get("cursor"); cur != "" {
		parts := strings.Split(cur, ":")
		bad := len(parts) != 3
		if !bad {
			size, e1 := strconv.Atoi(parts[0])
			idx, e2 := strconv.Atoi(parts[1])
			_, e3 := strconv.Atoi(parts[2])
			bad = e1 != nil || e2 != nil || e3 != nil || size < 0 || size > maxPerPage || idx < 0
			page = idx
		}
		if bad {
			c.json(400, map[string]any{"detail": "Invalid cursor parameter."})
			return 0, 0, false
		}
	}
	return perPage, page, true
}

func (c *rc) envelope(results []map[string]any, total, perPage, page int) {
	c.json(200, map[string]any{
		"grouped_by": nil, "sub_grouped_by": nil, "extra_stats": nil,
		"total_count":       total,
		"total_results":     total,
		"total_pages":       int(math.Ceil(float64(total) / float64(perPage))),
		"count":             len(results),
		"next_cursor":       fmt.Sprintf("%d:%d:0", perPage, page+1),
		"prev_cursor":       fmt.Sprintf("%d:%d:1", perPage, page-1),
		"next_page_results": (page+1)*perPage < total,
		"prev_page_results": page > 0,
		"results":           results,
	})
}

// listAll paginates an in-memory list, then shapes only the page.
func (c *rc) listAll(objs []map[string]any) {
	perPage, page, ok := c.page()
	if !ok {
		return
	}
	start := min(page*perPage, len(objs))
	end := min(start+perPage, len(objs))
	pageObjs, err := c.shape(objs[start:end])
	if err != nil {
		c.fail(err)
		return
	}
	c.envelope(pageObjs, len(objs), perPage, page)
}

// one shapes and writes a single object.
func (c *rc) one(status int, obj map[string]any) {
	out, err := c.shape([]map[string]any{obj})
	if err != nil {
		c.fail(err)
		return
	}
	c.json(status, out[0])
}

// shape applies expand and fields on GET requests.
func (c *rc) shape(objs []map[string]any) ([]map[string]any, error) {
	if c.r.Method != http.MethodGet {
		return objs, nil
	}
	q := c.r.URL.Query()
	if err := c.expand(objs, csv(q.Get("expand"))); err != nil {
		return nil, err
	}
	fields := csv(q.Get("fields"))
	if len(fields) == 0 {
		return objs, nil
	}
	out := make([]map[string]any, len(objs))
	for k, o := range objs {
		m := map[string]any{}
		for _, f := range fields {
			if v, ok := o[f]; ok {
				m[f] = v
			}
		}
		out[k] = m
	}
	return out, nil
}

// expand replaces id values with nested objects, one batch query per name.
func (c *rc) expand(objs []map[string]any, names []string) error {
	ctx := c.r.Context()
	for _, name := range names {
		var ids []string
		for _, o := range objs {
			switch v := o[name].(type) {
			case string:
				ids = append(ids, v)
			case []string:
				ids = append(ids, v...)
			}
		}
		if len(ids) == 0 {
			continue
		}
		var lookup func(string) (any, bool)
		switch name {
		case "assignees", "actor", "created_by":
			users, err := c.a.s.UsersByIDs(ctx, ids)
			if err != nil {
				return err
			}
			lookup = func(id string) (any, bool) { u, ok := users[id]; return userLite(u), ok }
		case "labels":
			labels, err := c.a.s.LabelsByIDs(ctx, ids)
			if err != nil {
				return err
			}
			lookup = func(id string) (any, bool) { l, ok := labels[id]; return labelJSON(l, c.ws.ID), ok }
		case "state":
			states, err := c.a.s.StatesByIDs(ctx, ids)
			if err != nil {
				return err
			}
			lookup = func(id string) (any, bool) { s, ok := states[id]; return stateLite(s), ok }
		case "project":
			projects, err := c.a.s.ProjectsByIDs(ctx, ids)
			if err != nil {
				return err
			}
			lookup = func(id string) (any, bool) { p, ok := projects[id]; return projectLite(p), ok }
		case "parent":
			issues, err := c.a.s.IssuesByIDs(ctx, ids)
			if err != nil {
				return err
			}
			lookup = func(id string) (any, bool) {
				i, ok := issues[id]
				if !ok {
					return nil, false
				}
				return map[string]any{"id": i.ID, "sequence_id": i.SequenceID, "project_id": i.ProjectID}, true
			}
		default:
			continue
		}
		for _, o := range objs {
			switch v := o[name].(type) {
			case string:
				if x, ok := lookup(v); ok {
					o[name] = x
				}
			case []string:
				list := []any{}
				for _, id := range v {
					if x, ok := lookup(id); ok {
						list = append(list, x)
					}
				}
				o[name] = list
			}
		}
	}
	return nil
}
