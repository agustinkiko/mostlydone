package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mostlydone/internal/store"
)

func setupAPI(t *testing.T) (*API, *client, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	a := New(s, "main", false)
	return a, &client{t: t, h: a.Handler(), csrf: true}, filepath.Join(dir, "t.db.setup-token")
}

func body(tok string) map[string]any {
	m := map[string]any{"email": "ada@example.com", "password": "password123", "display_name": "Ada"}
	if tok != "" {
		m["setup_token"] = tok
	}
	return m
}

func TestSetupToken(t *testing.T) {
	a, c, file := setupAPI(t)
	tok := NewSetupToken()
	if !strings.HasPrefix(tok, "setup_") || len(tok) < len("setup_")+32 {
		t.Fatalf("weak token %q", tok)
	}
	if err := a.SetSetupToken(tok, file); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("token file mode %v", fi.Mode().Perm())
	}
	if got, _ := os.ReadFile(file); strings.TrimSpace(string(got)) != tok {
		t.Fatalf("file content %q", got)
	}
	me := c.must(200, "GET", "/auth/me", nil)
	eq(t, "setup_token_required", me["setup_token_required"], true)

	c.must(403, "POST", "/auth/setup", body(""))
	c.must(403, "POST", "/auth/setup", body("setup_wrong"))
	got := c.must(403, "POST", "/auth/setup", body(""))
	if got["detail"] == nil {
		t.Fatalf("403 body lacks detail: %v", got)
	}
	c.must(201, "POST", "/auth/setup", body(tok))

	if a.SetupTokenPending() {
		t.Fatal("token still pending after setup")
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("token file not removed: %v", err)
	}
	// Replaying the token is refused with the "already completed" error.
	got = c.must(403, "POST", "/auth/setup", body(tok))
	eq(t, "replay error", got["error"], "Setup has already been completed.")
	me = c.must(200, "GET", "/auth/me", nil)
	eq(t, "setup_required", me["setup_required"], false)
}

func TestSetupRefusedWithoutArmedToken(t *testing.T) {
	// The env-var bootstrap path creates a user and arms no token.
	a, c, file := setupAPI(t)
	if _, err := a.s.Setup(t.Context(), "main", "root@example.com", "password123", ""); err != nil {
		t.Fatal(err)
	}
	if a.SetupTokenPending() {
		t.Fatal("token pending without being armed")
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("token file exists on bootstrap path")
	}
	c.must(403, "POST", "/auth/setup", body(""))
	c.must(403, "POST", "/auth/setup", body("setup_anything"))
}

func TestSetupTokenThrottle(t *testing.T) {
	a, c, _ := setupAPI(t)
	tok := NewSetupToken()
	a.SetSetupToken(tok, "")
	for i := 0; i < maxLoginFailures; i++ {
		c.must(403, "POST", "/auth/setup", body("setup_guess"))
	}
	// Blocked now, even with the right token.
	c.must(429, "POST", "/auth/setup", body(tok))
}
