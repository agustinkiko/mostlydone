package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"mostlydone/internal/store"
)

// maxBuckets is the map size past which idle (full) buckets are swept.
const maxBuckets = 10000

// Limiter is a per-credential token bucket for /api/v1 and /mcp. A nil
// *Limiter means rate limiting is off. Buckets live in memory only; a restart
// clears them.
//
// Each bucket refills continuously at rpm/60 tokens per second up to burst.
type Limiter struct {
	rpm, burst int

	mu        sync.Mutex
	now       func() time.Time
	buckets   map[string]*bucket
	nextSweep time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

// NewLimiter returns a limiter allowing rpm requests per minute per key with
// bursts up to burst. rpm <= 0 returns nil (off); burst <= 0 means rpm.
func NewLimiter(rpm, burst int) *Limiter {
	if rpm <= 0 {
		return nil
	}
	if burst <= 0 {
		burst = rpm
	}
	return &Limiter{
		rpm:     rpm,
		burst:   burst,
		now:     time.Now,
		buckets: map[string]*bucket{},
	}
}

// Decision is the outcome of one Allow call.
type Decision struct {
	OK         bool
	Limit      int           // requests per minute
	Remaining  int           // whole tokens left
	Reset      time.Time     // when the bucket is full again
	RetryAfter time.Duration // when !OK: time until one token is available
}

// Allow takes one token from key's bucket if there is one.
func (l *Limiter) Allow(key string) Decision {
	rpm, burst := l.rpm, l.burst
	rate := float64(rpm) / 60 // tokens per second
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	b := l.buckets[key]
	if b == nil {
		l.sweep(now)
		b = &bucket{tokens: float64(burst), last: now}
		l.buckets[key] = b
	} else {
		b.tokens = math.Min(float64(burst), b.tokens+now.Sub(b.last).Seconds()*rate)
		b.last = now
	}
	d := Decision{Limit: rpm}
	if b.tokens >= 1 {
		b.tokens--
		d.OK = true
	} else {
		d.RetryAfter = time.Duration(math.Ceil((1-b.tokens)/rate*1000)) * time.Millisecond
	}
	d.Remaining = int(math.Floor(b.tokens))
	d.Reset = now.Add(time.Duration(math.Ceil((float64(burst)-b.tokens)/rate*1000)) * time.Millisecond)
	return d
}

// sweep drops full (idle) buckets once the map is large. Caller holds mu. A
// dropped bucket is recreated full, so this changes nothing observable. At most
// one sweep per second keeps a map of busy keys from costing O(n) per request.
func (l *Limiter) sweep(now time.Time) {
	if len(l.buckets) < maxBuckets || now.Before(l.nextSweep) {
		return
	}
	l.nextSweep = now.Add(time.Second)
	for k, b := range l.buckets {
		if b.tokens+now.Sub(b.last).Seconds()*float64(l.rpm)/60 >= float64(l.burst) {
			delete(l.buckets, k)
		}
	}
}

// Admit charges key one request and sets the X-RateLimit-* headers. When the
// bucket is empty it also writes the 429 and returns false. A nil limiter
// admits everything and sets nothing.
func (l *Limiter) Admit(w http.ResponseWriter, key string) bool {
	if l == nil {
		return true
	}
	d := l.Allow(key)
	h := w.Header()
	h.Set("X-RateLimit-Limit", strconv.Itoa(d.Limit))
	h.Set("X-RateLimit-Remaining", strconv.Itoa(d.Remaining))
	h.Set("X-RateLimit-Reset", strconv.FormatInt(d.Reset.Unix(), 10))
	if d.OK {
		return true
	}
	secs := max(1, int(math.Ceil(d.RetryAfter.Seconds())))
	h.Set("Retry-After", strconv.Itoa(secs))
	h.Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)
	json.NewEncoder(w).Encode(map[string]string{
		"detail": "Request was throttled. Expected available in " + strconv.Itoa(secs) + " seconds.",
	})
	return false
}

// TokenKey is the limiter key of an API token request ("" when none is sent).
// Only a hash of the secret is used.
func TokenKey(r *http.Request) string {
	tok := bearer(r)
	if tok == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(tok))
	return "token:" + hex.EncodeToString(sum[:])
}

// limitKey is the bucket of an authenticated request: the token for token
// requests, else the user (all of a user's browser sessions share one bucket).
func limitKey(r *http.Request, viaToken bool, u *store.User) string {
	if viaToken {
		return TokenKey(r)
	}
	return "user:" + u.ID
}

// SetRateLimit turns the API limiter on (rpm > 0) or off and returns it, so
// the caller can share it with the MCP handler.
func (a *API) SetRateLimit(rpm, burst int) *Limiter {
	a.limiter = NewLimiter(rpm, burst)
	return a.limiter
}
