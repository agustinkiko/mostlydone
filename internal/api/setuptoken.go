package api

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"os"
	"sync"
)

// setupGate holds the one-time token that first-run setup requires. It lives
// in the API value (not a global) so each API instance has its own.
type setupGate struct {
	mu    sync.Mutex
	token string
	file  string
}

func (g *setupGate) pending() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.token != ""
}

func (g *setupGate) check(got string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.token == "" || got == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(g.token)) == 1
}

func (g *setupGate) clear() {
	g.mu.Lock()
	file := g.file
	g.token, g.file = "", ""
	g.mu.Unlock()
	if file != "" {
		os.Remove(file)
	}
}

// NewSetupToken returns a random token with 192 bits of entropy.
func NewSetupToken() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand does not fail on supported platforms
	}
	return "setup_" + hex.EncodeToString(b)
}

// SetSetupToken makes POST /auth/setup require token. If file is not empty the
// token is also written there with mode 0600; a write failure is returned but
// the token stays active (the caller logs it). An empty token disables setup.
func (a *API) SetSetupToken(token, file string) error {
	a.setup.mu.Lock()
	a.setup.token, a.setup.file = token, ""
	a.setup.mu.Unlock()
	if token == "" || file == "" {
		return nil
	}
	if err := writeTokenFile(file, token); err != nil {
		return err
	}
	a.setup.mu.Lock()
	a.setup.file = file
	a.setup.mu.Unlock()
	return nil
}

func writeTokenFile(file, token string) error {
	if err := os.WriteFile(file, []byte(token+"\n"), 0o600); err != nil {
		return err
	}
	// WriteFile keeps the mode of a pre-existing file; force 0600.
	if err := os.Chmod(file, 0o600); err != nil {
		os.Remove(file)
		return errors.Join(errors.New("chmod setup token file"), err)
	}
	return nil
}

// SetupTokenPending reports whether setup still needs a token.
func (a *API) SetupTokenPending() bool { return a.setup.pending() }
