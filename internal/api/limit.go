package api

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Login throttle: after maxLoginFailures failed sign-ins from one client
// address inside loginWindow, further attempts from that address are refused
// with 429 until the window ends. A successful sign-in clears the count.
// Counts live in memory only; a restart clears them.
const (
	maxLoginFailures = 10
	loginWindow      = 15 * time.Minute
	maxLimiterKeys   = 10000 // sweep expired entries past this size
)

type loginLimiter struct {
	mu   sync.Mutex
	now  func() time.Time
	seen map[string]*loginFailures
}

type loginFailures struct {
	n     int
	start time.Time // start of the current window
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{now: time.Now, seen: map[string]*loginFailures{}}
}

// retryAfter is zero when key may try to sign in, else how long it must wait.
func (l *loginLimiter) retryAfter(key string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	f := l.seen[key]
	if f == nil {
		return 0
	}
	left := loginWindow - l.now().Sub(f.start)
	if left <= 0 {
		delete(l.seen, key)
		return 0
	}
	if f.n < maxLoginFailures {
		return 0
	}
	return left
}

// fail records one failed sign-in for key.
func (l *loginLimiter) fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if len(l.seen) >= maxLimiterKeys {
		for k, f := range l.seen {
			if now.Sub(f.start) >= loginWindow {
				delete(l.seen, k)
			}
		}
	}
	f := l.seen[key]
	if f == nil || now.Sub(f.start) >= loginWindow {
		f = &loginFailures{start: now}
		l.seen[key] = f
	}
	f.n++
}

// clear forgets key's failures after a successful sign-in.
func (l *loginLimiter) clear(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.seen, key)
}

// clientIP is the address the login throttle counts against. Behind a proxy
// the socket address is the proxy's, so ClientIPHeader names the header the
// proxy sets (for example CF-Connecting-IP). Only set it when every request
// comes through that proxy; otherwise a client could send the header itself.
func (a *API) clientIP(r *http.Request) string {
	if a.ClientIPHeader != "" {
		if v := strings.TrimSpace(strings.Split(r.Header.Get(a.ClientIPHeader), ",")[0]); v != "" {
			return v
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
