package dblock

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestLock(t *testing.T) {
	db := filepath.Join(t.TempDir(), "x.db")
	a, err := Acquire(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(db); !errors.Is(err, ErrLocked) {
		t.Fatalf("want ErrLocked, got %v", err)
	}
	a.Close()
	a.Close()
	b, err := Acquire(db)
	if err != nil {
		t.Fatal(err)
	}
	b.Close()
}
