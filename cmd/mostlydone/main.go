// Command mostlydone serves MostlyDone: REST API, MCP and web UI from one
// binary and one SQLite file.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"mostlydone/internal/api"
	"mostlydone/internal/backup"
	"mostlydone/internal/dblock"
	"mostlydone/internal/mcp"
	"mostlydone/internal/store"
	"mostlydone/internal/webui"
)

func env(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

// rateLimitConfig reads MOSTLYDONE_RATE_LIMIT (requests per minute per
// credential; unset or 0 = off) and MOSTLYDONE_RATE_LIMIT_BURST (default: the
// per-minute value).
func rateLimitConfig() (rpm, burst int, err error) {
	if v := os.Getenv("MOSTLYDONE_RATE_LIMIT"); v != "" {
		if rpm, err = strconv.Atoi(v); err != nil || rpm < 0 {
			return 0, 0, fmt.Errorf("MOSTLYDONE_RATE_LIMIT must be a whole number of requests per minute (0 = off), got %q", v)
		}
	}
	burst = rpm
	if v := os.Getenv("MOSTLYDONE_RATE_LIMIT_BURST"); v != "" {
		if burst, err = strconv.Atoi(v); err != nil || burst < 1 {
			return 0, 0, fmt.Errorf("MOSTLYDONE_RATE_LIMIT_BURST must be a whole number of at least 1, got %q", v)
		}
	}
	return rpm, burst, nil
}

// newHandler builds the server. When the database has no users it arms a
// one-time setup token, logs it and (best effort) saves it to
// <dbPath>.setup-token; an empty dbPath skips the file.
func newHandler(s *store.Store, slug string, secure bool, dbPath string) http.Handler {
	a := api.New(s, slug, secure)
	a.ClientIPHeader = os.Getenv("MOSTLYDONE_CLIENT_IP_HEADER")
	proxies, err := api.ParseTrustedProxies(os.Getenv("MOSTLYDONE_TRUSTED_PROXIES"))
	if err != nil {
		log.Fatalf("MOSTLYDONE_TRUSTED_PROXIES: %v", err)
	}
	a.TrustedProxies = proxies
	if a.ClientIPHeader != "" && len(proxies) == 0 {
		log.Printf("warning: MOSTLYDONE_CLIENT_IP_HEADER is ignored until MOSTLYDONE_TRUSTED_PROXIES is set")
	}
	rpm, burst, err := rateLimitConfig()
	if err != nil {
		log.Fatalf("%v", err)
	}
	limiter := a.SetRateLimit(rpm, burst)
	if n, err := s.UserCount(context.Background()); err != nil {
		log.Fatalf("count users: %v", err)
	} else if n > 0 {
		// Setup is done; drop a token file left behind by an earlier run.
		if dbPath != "" {
			os.Remove(dbPath + ".setup-token")
		}
	} else {
		tok := api.NewSetupToken()
		file := ""
		if dbPath != "" {
			file = dbPath + ".setup-token"
		}
		if err := a.SetSetupToken(tok, file); err != nil {
			log.Printf("warning: could not write setup token file %s: %v", file, err)
			file = ""
		}
		log.Printf("FIRST RUN: no users exist. Open the web UI and create the admin account with this one-time setup token:\n\n    %s\n", tok)
		if file != "" {
			log.Printf("The token is also saved in %s (readable only by its owner). It is deleted once setup completes.", file)
		}
	}
	apiH := a.Handler()
	mux := http.NewServeMux()
	mux.Handle("/api/", apiH)
	mux.Handle("/auth/", apiH)
	mux.Handle("/mcp", mcp.Handler(s, api.TokenAuth(s), limiter))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("ok\n"))
	})
	mux.Handle("/", webui.Handler())
	return mux
}

const usage = `usage:
  mostlydone                              run the server (default)
  mostlydone backup [-db PATH] [-force] DEST
                                          online backup; safe while the server runs
  mostlydone restore [-db PATH] [-force] SRC
                                          offline restore; server must be stopped
  mostlydone help                         show this message

-db defaults to $MOSTLYDONE_DB, then ./mostlydone.db.
`

// run dispatches subcommands and returns an exit code. No args means serve,
// which is handled by main (it never returns normally).
func run(args []string, stdout, stderr io.Writer) int {
	switch args[0] {
	case "help", "-h", "-help", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	case "backup", "restore":
		return runBackup(args[0], args[1:], stdout, stderr)
	}
	fmt.Fprintf(stderr, "unknown command %q\n\n%s", args[0], usage)
	return 2
}

func runBackup(cmd string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.SetOutput(stderr)
	db := fs.String("db", env("MOSTLYDONE_DB", "./mostlydone.db"), "database path")
	var force bool
	if cmd == "backup" {
		fs.BoolVar(&force, "force", false, "overwrite DEST if it exists")
		fs.Usage = func() { fmt.Fprintf(stderr, "usage: mostlydone backup [-db PATH] [-force] DEST\n"); fs.PrintDefaults() }
	} else {
		fs.BoolVar(&force, "force", false, "replace the existing database (it is moved aside first)")
		fs.Usage = func() { fmt.Fprintf(stderr, "usage: mostlydone restore [-db PATH] [-force] SRC\n"); fs.PrintDefaults() }
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}
	ctx := context.Background()
	if cmd == "backup" {
		info, err := backup.Backup(ctx, *db, fs.Arg(0), force)
		if err != nil {
			fmt.Fprintf(stderr, "backup failed: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "backup ok: %s size=%d schema_version=%d took=%s\n", info.Path, info.Size, info.SchemaVersion, info.Duration.Round(time.Millisecond))
		return 0
	}
	info, err := backup.Restore(ctx, fs.Arg(0), *db, force)
	if err != nil {
		fmt.Fprintf(stderr, "restore failed: %v\n", err)
		return 1
	}
	if info.Aside != "" {
		fmt.Fprintf(stdout, "previous database moved to %s\n", info.Aside)
	}
	fmt.Fprintf(stdout, "restore ok: %s size=%d schema_version=%d took=%s\n", info.Path, info.Size, info.SchemaVersion, info.Duration.Round(time.Millisecond))
	return 0
}

// purgeSessions deletes expired sessions now and then every hour until ctx ends.
func purgeSessions(ctx context.Context, s *store.Store) {
	tick := time.NewTicker(time.Hour)
	defer tick.Stop()
	for {
		if n, err := s.PurgeExpiredSessions(ctx); err != nil {
			log.Printf("purge sessions: %v", err)
		} else if n > 0 {
			log.Printf("purged %d expired sessions", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func main() {
	if len(os.Args) > 1 {
		os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
	}
	serve()
}

func serve() {
	addr := env("MOSTLYDONE_ADDR", ":8080")
	dbPath := env("MOSTLYDONE_DB", "./mostlydone.db")
	slug := env("MOSTLYDONE_WORKSPACE_SLUG", "main")
	secure := os.Getenv("MOSTLYDONE_SECURE_COOKIES") == "true"

	// One server per database file; also keeps `restore` out while serving.
	lock, err := dblock.Acquire(dbPath)
	if err != nil {
		log.Fatalf("%v (lock file %s)", err, dblock.Path(dbPath))
	}
	defer lock.Close()

	s, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	if _, err := s.EnsureWorkspace(ctx, slug); err != nil {
		log.Fatalf("create workspace: %v", err)
	}
	if email, pw := os.Getenv("MOSTLYDONE_ADMIN_EMAIL"), os.Getenv("MOSTLYDONE_ADMIN_PASSWORD"); email != "" && pw != "" {
		if n, err := s.UserCount(ctx); err != nil {
			log.Fatalf("count users: %v", err)
		} else if n == 0 {
			if _, err := s.Setup(ctx, slug, email, pw, ""); err != nil {
				log.Fatalf("create admin: %v", err)
			}
			log.Printf("created admin %s", email)
		}
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           newHandler(s, slug, secure, dbPath),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	stop, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	purged := make(chan struct{})
	go func() {
		defer close(purged)
		s.RunTrashPurge(stop, time.Hour, log.Printf)
	}()
	sessionsPurged := make(chan struct{})
	go func() {
		defer close(sessionsPurged)
		purgeSessions(stop, s)
	}()
	go func() {
		log.Printf("listening on %s (db %s, workspace %q)", addr, dbPath, slug)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("serve: %v", err)
		}
	}()
	<-stop.Done()
	log.Print("shutting down")
	shutdownCtx, done := context.WithTimeout(context.Background(), 15*time.Second)
	defer done()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
	<-purged // don't close the database under a running purge
	<-sessionsPurged
}
