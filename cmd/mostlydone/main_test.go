package main

import (
	"context"
	"net/http/httptest"
	"os"
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
	h := newHandler(s, "main", false, "")
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

func TestSetupTokenFileLifecycle(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "t.db")
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	file := dbPath + ".setup-token"

	// No users: a token is armed and saved with mode 0600.
	newHandler(s, "main", false, dbPath)
	fi, err := os.Stat(file)
	if err != nil {
		t.Fatalf("token file not written: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("token file mode %v, want 0600", fi.Mode().Perm())
	}

	// Once a user exists, a later start removes a stale token file and arms nothing.
	if _, err := s.Setup(context.Background(), "main", "a@example.test", "password-123", "A"); err != nil {
		t.Fatal(err)
	}
	newHandler(s, "main", false, dbPath)
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("stale token file not removed: %v", err)
	}
}
