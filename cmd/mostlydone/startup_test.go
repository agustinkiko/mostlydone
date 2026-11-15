package main

import (
	"context"
	"os"
	osexec "os/exec"
	"strings"
	"testing"
	"time"
)

// TestMain lets the tests below re-exec this binary as the real server.
func TestMain(m *testing.M) {
	if os.Getenv("MOSTLYDONE_TEST_SERVE") == "1" {
		os.Args = os.Args[:1]
		main()
		return
	}
	os.Exit(m.Run())
}

// A config value that stops startup must be rejected before the lock file,
// database, admin or setup token exist.
func TestBadConfigLeavesNoFiles(t *testing.T) {
	cases := []struct{ name, val string }{
		{"MOSTLYDONE_TRUSTED_PROXIES", "nonsense"},
		{"MOSTLYDONE_RATE_LIMIT", "abc"},
		{"MOSTLYDONE_RATE_LIMIT_BURST", "abc"},
		{"MOSTLYDONE_ADDR", "no-port"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := osexec.CommandContext(ctx, os.Args[0])
			cmd.Env = append(os.Environ(),
				"MOSTLYDONE_TEST_SERVE=1",
				"MOSTLYDONE_ADDR=127.0.0.1:0",
				"MOSTLYDONE_DB="+dir+"/a.db",
				"MOSTLYDONE_ADMIN_EMAIL=admin@example.test",
				"MOSTLYDONE_ADMIN_PASSWORD=password-123",
				c.name+"="+c.val,
			)
			out, err := cmd.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("server started instead of failing:\n%s", out)
			}
			if err == nil {
				t.Fatalf("exit 0, want failure:\n%s", out)
			}
			if !strings.Contains(string(out), c.name) {
				t.Errorf("output does not name %s:\n%s", c.name, out)
			}
			left, _ := os.ReadDir(dir)
			for _, e := range left {
				t.Errorf("left behind: %s", e.Name())
			}
		})
	}
}
