package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestPrefs(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	id := f.admin.ID

	got, err := f.s.UserPrefs(ctx, id)
	if err != nil || got != (Prefs{Theme: "system", Density: "comfortable"}) {
		t.Fatalf("new user prefs = %+v, %v", got, err)
	}

	got, err = f.s.UpdatePrefs(ctx, id, map[string]any{"theme": "dark"})
	if err != nil || got != (Prefs{Theme: "dark", Density: "comfortable"}) {
		t.Fatalf("set theme = %+v, %v", got, err)
	}
	got, err = f.s.UpdatePrefs(ctx, id, map[string]any{"density": "compact"})
	if err != nil || got != (Prefs{Theme: "dark", Density: "compact"}) {
		t.Fatalf("set density = %+v, %v", got, err)
	}

	for name, patch := range map[string]map[string]any{
		"unknown key": {"theme": "light", "font": "mono"},
		"bad value":   {"theme": "light", "density": "tiny"},
		"non-string":  {"theme": 5},
	} {
		if _, err := f.s.UpdatePrefs(ctx, id, patch); kind(err) != KindInvalid {
			t.Fatalf("%s: err = %v, want invalid", name, err)
		}
	}
	if got, _ := f.s.UserPrefs(ctx, id); got != (Prefs{Theme: "dark", Density: "compact"}) {
		t.Fatalf("rejected patches changed prefs: %+v", got)
	}
}

func TestPrefsPersist(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "t.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.Setup(ctx, "main", "a@example.com", "password123", "A")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdatePrefs(ctx, u.ID, map[string]any{"theme": "light"}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got, _ := s.UserPrefs(ctx, u.ID); got != (Prefs{Theme: "light", Density: "comfortable"}) {
		t.Fatalf("after reopen = %+v", got)
	}
}
