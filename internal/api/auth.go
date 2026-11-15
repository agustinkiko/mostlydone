package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"mostlydone/internal/store"
)

// authRoutes registers /auth/*. Every unsafe request that is not
// token-authenticated needs the CSRF header, login and setup included.
func (a *API) authRoutes(mux *http.ServeMux) {
	public := func(pattern string, fn func(*rc)) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			c := &rc{w: w, r: r, a: a}
			if c.csrfOK() {
				fn(c)
			}
		})
	}
	// private requires a caller; sessionOnly refuses API tokens.
	private := func(pattern string, sessionOnly bool, fn func(*rc)) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			c := &rc{w: w, r: r, a: a}
			var err error
			if c.user, c.viaToken, err = a.authenticate(r); err != nil {
				c.authFail(err)
				return
			}
			if !c.csrfOK() {
				return
			}
			if sessionOnly && c.viaToken {
				c.json(http.StatusForbidden, map[string]any{"detail": "Token management needs a browser session."})
				return
			}
			if c.ws, err = a.s.Workspace(r.Context(), a.slug); err == nil {
				c.role, _ = a.s.MemberRole(r.Context(), c.ws.ID, c.user.ID)
			}
			fn(c)
		})
	}
	public("POST /auth/setup", a.setup)
	public("POST /auth/login", a.login)
	public("POST /auth/logout", a.logout)
	public("GET /auth/me", a.me)
	private("GET /auth/tokens", true, listTokens)
	private("POST /auth/tokens", true, createToken)
	private("DELETE /auth/tokens/{id}", true, deleteToken)
	private("POST /auth/users", false, createUser)
}

type credentials struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
	Role        int    `json:"role"`
}

func (a *API) startSession(c *rc, u *store.User) bool {
	tok, err := a.s.CreateSession(c.r.Context(), u.ID)
	if err != nil {
		c.fail(err)
		return false
	}
	http.SetCookie(c.w, &http.Cookie{Name: cookieName, Value: tok, Path: "/", HttpOnly: true, Secure: a.secureCookies,
		SameSite: http.SameSiteLaxMode, MaxAge: int(store.SessionTTL / time.Second)})
	return true
}

func (a *API) setup(c *rc) {
	var in credentials
	if !c.decode(&in) {
		return
	}
	u, err := a.s.Setup(c.r.Context(), a.slug, in.Email, in.Password, in.DisplayName)
	if err != nil {
		c.fail(err)
		return
	}
	if a.startSession(c, u) {
		c.json(http.StatusCreated, map[string]any{"user": userLite(u), "role": store.RoleAdmin, "workspace_slug": a.slug})
	}
}

func (a *API) login(c *rc) {
	// Refuse before the password check, so a blocked client costs no bcrypt work.
	ip := a.clientIP(c.r)
	if wait := a.logins.retryAfter(ip); wait > 0 {
		c.w.Header().Set("Retry-After", strconv.Itoa(int(wait/time.Second)+1))
		c.json(http.StatusTooManyRequests, map[string]any{"detail": "Too many failed sign-in attempts. Try again in a few minutes."})
		return
	}
	var in credentials
	if !c.decode(&in) {
		return
	}
	u, err := a.s.Authenticate(c.r.Context(), in.Email, in.Password)
	if errors.Is(err, store.ErrBadLogin) {
		a.logins.fail(ip)
		c.json(http.StatusUnauthorized, map[string]any{"detail": "Invalid email or password."})
		return
	}
	if err != nil {
		c.fail(err)
		return
	}
	a.logins.clear(ip)
	if a.startSession(c, u) {
		c.json(http.StatusOK, map[string]any{"user": userLite(u), "workspace_slug": a.slug})
	}
}

func (a *API) logout(c *rc) {
	if ck, err := c.r.Cookie(cookieName); err == nil && ck.Value != "" {
		if err := a.s.DeleteSession(c.r.Context(), ck.Value); err != nil {
			c.fail(err)
			return
		}
	}
	http.SetCookie(c.w, &http.Cookie{Name: cookieName, Value: "", Path: "/", HttpOnly: true, Secure: a.secureCookies,
		SameSite: http.SameSiteLaxMode, MaxAge: -1})
	c.noContent()
}

// me tells the UI how to boot: who is logged in (or null), the workspace
// slug, and whether first-run setup is still needed. It never returns 401.
func (a *API) me(c *rc) {
	n, err := a.s.UserCount(c.r.Context())
	if err != nil {
		c.fail(err)
		return
	}
	out := map[string]any{"user": nil, "role": nil, "workspace_slug": a.slug, "setup_required": n == 0}
	if u, _, err := a.authenticate(c.r); err == nil {
		out["user"] = userLite(u)
		if ws, err := a.s.Workspace(c.r.Context(), a.slug); err == nil {
			if role, err := a.s.MemberRole(c.r.Context(), ws.ID, u.ID); err == nil {
				out["role"] = role
			}
		}
	}
	c.json(http.StatusOK, out)
}

func tokenJSON(t *store.APIToken) map[string]any {
	return map[string]any{"id": t.ID, "label": t.Label, "created_at": t.CreatedAt, "last_used_at": str(t.LastUsedAt)}
}

func listTokens(c *rc) {
	list, err := c.a.s.ListTokens(c.r.Context(), c.user.ID)
	if err != nil {
		c.fail(err)
		return
	}
	out := make([]map[string]any, len(list))
	for k, t := range list {
		out[k] = tokenJSON(t)
	}
	c.json(http.StatusOK, out)
}

func createToken(c *rc) {
	var in struct {
		Label string `json:"label"`
	}
	if !c.decode(&in) {
		return
	}
	t, secret, err := c.a.s.CreateToken(c.r.Context(), c.user.ID, in.Label)
	if err != nil {
		c.fail(err)
		return
	}
	out := tokenJSON(t)
	out["token"] = secret // shown once; only its hash is stored
	c.json(http.StatusCreated, out)
}

func deleteToken(c *rc) {
	if err := c.a.s.DeleteToken(c.r.Context(), c.user.ID, c.r.PathValue("id")); err != nil {
		c.fail(err)
		return
	}
	c.noContent()
}

// createUser lets an admin add a workspace member (the spec has no other way
// to get a second user in).
func createUser(c *rc) {
	if c.ws == nil || !c.isAdmin() {
		c.denied()
		return
	}
	var in credentials
	if !c.decode(&in) {
		return
	}
	if in.Role == 0 {
		in.Role = store.RoleMember
	}
	u, err := c.a.s.CreateUser(c.r.Context(), c.ws.ID, in.Email, in.Password, in.DisplayName, in.Role)
	if err != nil {
		c.fail(err)
		return
	}
	c.json(http.StatusCreated, memberJSON(&store.Member{User: *u, Role: in.Role}))
}
