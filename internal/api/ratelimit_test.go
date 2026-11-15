package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"mostlydone/internal/store"
)

func testLimiter(rpm, burst int) (*Limiter, *time.Time) {
	now := time.Unix(1_000_000, 0)
	l := NewLimiter(rpm, burst)
	l.now = func() time.Time { return now }
	return l, &now
}

func TestLimiterBurstRefillAndRetryAfter(t *testing.T) {
	l, now := testLimiter(60, 3) // 1 token/second
	for i, want := range []int{2, 1, 0} {
		d := l.Allow("k")
		if !d.OK || d.Remaining != want || d.Limit != 60 {
			t.Fatalf("call %d: %+v", i, d)
		}
	}
	if d := l.Allow("k"); d.OK || d.Remaining != 0 || d.RetryAfter != time.Second || !d.Reset.Equal(now.Add(3*time.Second)) {
		t.Fatalf("empty: %+v", d)
	}
	*now = now.Add(500 * time.Millisecond)
	if d := l.Allow("k"); d.OK || d.RetryAfter != 500*time.Millisecond {
		t.Fatalf("half refilled: %+v", d)
	}
	*now = now.Add(500 * time.Millisecond)
	if d := l.Allow("k"); !d.OK {
		t.Fatalf("after refill: %+v", d)
	}
	// Refill is capped at burst.
	*now = now.Add(time.Hour)
	if d := l.Allow("k"); !d.OK || d.Remaining != 2 {
		t.Fatalf("capped: %+v", d)
	}
}

func TestLimiterKeysAreIndependent(t *testing.T) {
	l, _ := testLimiter(60, 1)
	if !l.Allow("a").OK || l.Allow("a").OK {
		t.Fatal("a should be exhausted after one request")
	}
	if !l.Allow("b").OK {
		t.Fatal("b should be unaffected by a")
	}
}

func TestLimiterSweepBoundsMemory(t *testing.T) {
	l, now := testLimiter(60, 2)
	for i := range maxBuckets {
		l.Allow("k" + strconv.Itoa(i))
	}
	*now = now.Add(time.Minute) // every bucket is full again
	l.Allow("new")
	if n := len(l.buckets); n > 2 {
		t.Fatalf("%d buckets after sweep", n)
	}
}

// rateLimited returns a handler with the limiter on and the first user set up.
func rateLimited(t *testing.T, rpm, burst int) (http.Handler, *store.Store) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	a := New(s, "main", false)
	a.SetSetupToken(testSetupToken, "")
	a.SetRateLimit(rpm, burst)
	h := a.Handler()
	c := &client{t: t, h: h, csrf: true}
	c.setupAdmin("ada@example.com", "password123", "")
	return h, s
}

func get(h http.Handler, path, key string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", path, nil)
	if key != "" {
		r.Header.Set("X-API-Key", key)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func newToken(t *testing.T, s *store.Store, label string) string {
	t.Helper()
	u, err := s.UserByEmail(t.Context(), "ada@example.com")
	if err != nil {
		t.Fatal(err)
	}
	_, tok, err := s.CreateToken(t.Context(), u.ID, label)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

const projects = W + "/projects/"

func TestRateLimitOffByDefault(t *testing.T) {
	h, s := newHandler(t)
	c := &client{t: t, h: h, csrf: true}
	c.setupAdmin("ada@example.com", "password123", "")
	w := get(h, projects, newToken(t, s, "a"))
	eq(t, "status", w.Code, 200)
	for k := range w.Header() {
		if strings.HasPrefix(k, "X-Ratelimit") {
			t.Fatalf("unexpected header %s", k)
		}
	}
}

func TestRateLimitToken(t *testing.T) {
	h, s := rateLimited(t, 3, 3)
	tok := newToken(t, s, "a")
	other := newToken(t, s, "b")

	for _, want := range []string{"2", "1", "0"} {
		w := get(h, projects, tok)
		eq(t, "status", w.Code, 200)
		eq(t, "limit", w.Header().Get("X-RateLimit-Limit"), "3")
		eq(t, "remaining", w.Header().Get("X-RateLimit-Remaining"), want)
		reset, err := strconv.ParseInt(w.Header().Get("X-RateLimit-Reset"), 10, 64)
		if err != nil || time.Until(time.Unix(reset, 0)) <= 0 {
			t.Fatalf("bad reset %q", w.Header().Get("X-RateLimit-Reset"))
		}
	}
	w := get(h, projects, tok)
	eq(t, "throttled", w.Code, 429)
	ra := w.Header().Get("Retry-After")
	if n, err := strconv.Atoi(ra); err != nil || n < 1 {
		t.Fatalf("Retry-After %q", ra)
	}
	eq(t, "body", w.Body.String(), `{"detail":"Request was throttled. Expected available in `+ra+` seconds."}`+"\n")
	eq(t, "remaining", w.Header().Get("X-RateLimit-Remaining"), "0")

	// Another token has its own bucket.
	eq(t, "other token", get(h, projects, other).Code, 200)

	// Failed auth is 401 and neither counted nor throttled.
	for range 5 {
		eq(t, "bad token", get(h, projects, "nope").Code, 401)
	}
	eq(t, "unauthenticated", get(h, projects, "").Code, 401)
	eq(t, "other token again", get(h, projects, other).Code, 200)
}

func TestRateLimitSessionsShareUserBucket(t *testing.T) {
	h, _ := rateLimited(t, 3, 3)
	login := func() *client {
		c := &client{t: t, h: h, csrf: true}
		c.must(200, "POST", "/auth/login", map[string]any{"email": "ada@example.com", "password": "password123"})
		return c
	}
	a, b := login(), login()
	a.must(200, "GET", projects, nil)
	a.must(200, "GET", projects, nil)
	b.must(200, "GET", projects, nil)
	a.must(429, "GET", projects, nil)
	b.must(429, "GET", projects, nil)

	// /auth/* is not limited.
	for range 5 {
		a.must(200, "GET", "/auth/me", nil)
	}
}
