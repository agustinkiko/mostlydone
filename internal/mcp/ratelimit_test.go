package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"mostlydone/internal/api"
)

func TestHTTPRateLimitSharedWithREST(t *testing.T) {
	e := newEnv(t)
	_, tok, err := e.s.CreateToken(context.Background(), e.admin.ID, "t")
	if err != nil {
		t.Fatal(err)
	}
	a := api.New(e.s, "main", false)
	limiter := a.SetRateLimit(3, 3)
	rest := a.Handler()
	mcpH := Handler(e.s, api.TokenAuth(e.s), limiter)

	for range 3 {
		r := httptest.NewRequest("GET", "/api/v1/workspaces/main/projects/", nil)
		r.Header.Set("X-API-Key", tok)
		w := httptest.NewRecorder()
		rest.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("rest status %d", w.Code)
		}
	}
	w := post(mcpH, tok, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("mcp status %d, want 429", w.Code)
	}
	if w.Header().Get("Retry-After") == "" || w.Header().Get("X-RateLimit-Remaining") != "0" || w.Header().Get("X-RateLimit-Limit") != "3" {
		t.Fatalf("headers %v", w.Header())
	}
	if w := post(mcpH, "nope", `{}`); w.Code != http.StatusUnauthorized {
		t.Fatalf("bad token status %d", w.Code)
	}
}
