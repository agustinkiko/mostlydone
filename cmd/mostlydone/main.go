// Command mostlydone serves MostlyDone: REST API, MCP and web UI from one
// binary and one SQLite file.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"mostlydone/internal/api"
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

// newHandler builds the server. When the database has no users it arms a
// one-time setup token, logs it and (best effort) saves it to
// <dbPath>.setup-token; an empty dbPath skips the file.
func newHandler(s *store.Store, slug string, secure bool, dbPath string) http.Handler {
	a := api.New(s, slug, secure)
	a.ClientIPHeader = os.Getenv("MOSTLYDONE_CLIENT_IP_HEADER")
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
	mux.Handle("/mcp", mcp.Handler(s, api.TokenAuth(s)))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("ok\n"))
	})
	mux.Handle("/", webui.Handler())
	return mux
}

func main() {
	addr := env("MOSTLYDONE_ADDR", ":8080")
	dbPath := env("MOSTLYDONE_DB", "./mostlydone.db")
	slug := env("MOSTLYDONE_WORKSPACE_SLUG", "main")
	secure := os.Getenv("MOSTLYDONE_SECURE_COOKIES") == "true"

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
}
