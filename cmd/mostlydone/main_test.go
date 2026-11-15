package main

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"mostlydone/internal/store"
)

func TestRouting(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	h := newHandler(s, "main", false)
	cases := []struct {
		path string
		code int
		body string
	}{
		{"/healthz", 200, "ok"},
		{"/", 200, "<title>MostlyDone</title>"},
		{"/projects/WEB/board", 200, "<title>MostlyDone</title>"},
		{"/mcp", 401, "detail"},
		{"/api/v1/users/me/", 401, "detail"},
		{"/auth/me", 200, "setup_required"},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", c.path, nil))
		if w.Code != c.code || !strings.Contains(w.Body.String(), c.body) {
			t.Errorf("GET %s: %d %q", c.path, w.Code, w.Body.String())
		}
	}
}
