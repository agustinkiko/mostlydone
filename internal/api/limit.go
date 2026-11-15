package api

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
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

// ParseTrustedProxies parses a comma-separated list of CIDRs and bare IPs
// (a bare IP is a /32 or /128). Blank entries are skipped.
func ParseTrustedProxies(list string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, e := range strings.Split(list, ",") {
		if e = strings.TrimSpace(e); e == "" {
			continue
		}
		p, err := netip.ParsePrefix(e)
		if err != nil {
			ip, ipErr := netip.ParseAddr(e)
			if ipErr != nil {
				return nil, fmt.Errorf("invalid trusted proxy %q: want an IP or CIDR", e)
			}
			ip = ip.Unmap()
			p = netip.PrefixFrom(ip, ip.BitLen())
		}
		out = append(out, p.Masked())
	}
	return out, nil
}

func (a *API) trusted(ip netip.Addr) bool {
	for _, p := range a.TrustedProxies {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// clientIP is the address the login throttle counts against. The socket
// address is the default. ClientIPHeader is read only when the peer is a
// trusted proxy, and then from the right: each trusted hop appends, so the
// first untrusted entry is the last one a trusted proxy vouches for. Entries
// further left are client-controlled.
func (a *API) clientIP(r *http.Request) string {
	peer := r.RemoteAddr
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		peer = host
	}
	pa, err := netip.ParseAddr(peer)
	if err != nil || a.ClientIPHeader == "" || !a.trusted(pa.Unmap()) {
		return peer
	}
	var parts []string
	for _, line := range r.Header.Values(a.ClientIPHeader) {
		parts = append(parts, strings.Split(line, ",")...)
	}
	for i := len(parts) - 1; i >= 0; i-- {
		ip, err := netip.ParseAddr(strings.TrimSpace(parts[i]))
		if err != nil {
			break
		}
		if ip = ip.Unmap(); !a.trusted(ip) {
			return ip.String()
		}
	}
	return peer
}
