package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"mostlydone/internal/store"
)

func exec(args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := run(args, &out, &errb)
	return code, out.String(), errb.String()
}

func TestRunDispatch(t *testing.T) {
	if code, out, _ := exec("help"); code != 0 || !strings.Contains(out, "usage") {
		t.Fatalf("help: %d %q", code, out)
	}
	if code, _, errs := exec("bogus"); code != 2 || !strings.Contains(errs, "unknown command") {
		t.Fatalf("unknown: %d %q", code, errs)
	}
	if code, _, errs := exec("backup", "-h"); code != 0 || !strings.Contains(errs, "usage: mostlydone backup") {
		t.Fatalf("-h: %d %q", code, errs)
	}
	if code, _, _ := exec("backup"); code != 2 {
		t.Fatalf("missing arg: %d", code)
	}

	dir := t.TempDir()
	db := filepath.Join(dir, "a.db")
	s, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	dest := filepath.Join(dir, "b.db")
	if code, out, errs := exec("backup", "-db", db, dest); code != 0 || !strings.Contains(out, "schema_version=") {
		t.Fatalf("backup: %d %q %q", code, out, errs)
	}
	if code, _, _ := exec("backup", "-db", db, dest); code != 1 {
		t.Fatalf("second backup should fail, got %d", code)
	}
	target := filepath.Join(dir, "c.db")
	if code, out, errs := exec("restore", "-db", target, dest); code != 0 {
		t.Fatalf("restore: %d %q %q", code, out, errs)
	}
	if code, _, _ := exec("restore", "-db", target, dest); code != 1 {
		t.Fatalf("restore without -force should fail, got %d", code)
	}
	if code, out, _ := exec("restore", "-db", target, "-force", dest); code != 0 || !strings.Contains(out, "moved to") {
		t.Fatalf("restore -force: %d %q", code, out)
	}
}
