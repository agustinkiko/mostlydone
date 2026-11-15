package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"mostlydone/internal/store"
)

func TestClientIP(t *testing.T) {
	a := &API{}
	a.TrustedProxies, _ = ParseTrustedProxies("10.0.0.0/8, 2001:db8::/32")
	cases := []struct {
		name   string
		header string // API.ClientIPHeader
		peer   string
		lines  []string
		want   string
	}{
		{"header unset", "", "10.1.1.1:5000", []string{"1.2.3.4"}, "10.1.1.1"},
		{"untrusted peer, spoofed header", "X-Forwarded-For", "198.51.100.7:5000", []string{"1.2.3.4"}, "198.51.100.7"},
		{"single value", "CF-Connecting-IP", "10.1.1.1:5000", []string{"203.0.113.9"}, "203.0.113.9"},
		{"no header from trusted peer", "CF-Connecting-IP", "10.1.1.1:5000", nil, "10.1.1.1"},
		{"xff chain", "X-Forwarded-For", "10.1.1.1:5000", []string{"9.9.9.9, 203.0.113.9, 10.2.2.2"}, "203.0.113.9"},
		{"multiple lines", "X-Forwarded-For", "10.1.1.1:5000", []string{"9.9.9.9, 203.0.113.9", "10.2.2.2"}, "203.0.113.9"},
		{"all trusted", "X-Forwarded-For", "10.1.1.1:5000", []string{"10.3.3.3, 10.2.2.2"}, "10.1.1.1"},
		{"garbage rightmost", "X-Forwarded-For", "10.1.1.1:5000", []string{"203.0.113.9, nonsense"}, "10.1.1.1"},
		{"garbage stops the walk", "X-Forwarded-For", "10.1.1.1:5000", []string{"203.0.113.9, nonsense, 10.2.2.2"}, "10.1.1.1"},
		{"ipv6 client", "CF-Connecting-IP", "10.1.1.1:5000", []string{"2606:4700::1111"}, "2606:4700::1111"},
		{"ipv6 trusted peer", "CF-Connecting-IP", "[2001:db8::1]:5000", []string{"203.0.113.9"}, "203.0.113.9"},
		{"ipv4-mapped client", "CF-Connecting-IP", "10.1.1.1:5000", []string{"::ffff:203.0.113.9"}, "203.0.113.9"},
		{"ipv4-mapped trusted entry", "X-Forwarded-For", "10.1.1.1:5000", []string{"203.0.113.9, ::ffff:10.2.2.2"}, "203.0.113.9"},
		{"ipv4-mapped peer", "CF-Connecting-IP", "[::ffff:10.1.1.1]:5000", []string{"203.0.113.9"}, "203.0.113.9"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a.ClientIPHeader = c.header
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = c.peer
			for _, l := range c.lines {
				r.Header.Add(c.header, l)
			}
			if got := a.clientIP(r); got != c.want {
				t.Fatalf("clientIP = %q, want %q", got, c.want)
			}
		})
	}
}

func TestParseTrustedProxies(t *testing.T) {
	got, err := ParseTrustedProxies(" 10.42.0.0/16, ,192.0.2.1,2001:db8::1 ,")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"10.42.0.0/16", "192.0.2.1/32", "2001:db8::1/128"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		eq(t, "prefix", got[i].String(), want[i])
	}
	if got, err := ParseTrustedProxies(""); err != nil || len(got) != 0 {
		t.Fatalf("empty: %v, %v", got, err)
	}
	for _, bad := range []string{"nope", "10.0.0.0/33", "10.0.0.1, 10.0.0.x"} {
		if _, err := ParseTrustedProxies(bad); err == nil {
			t.Errorf("%q: want error", bad)
		}
	}
}

// A client that reaches the server directly cannot dodge the throttle by
// changing the header on each attempt.
func TestLoginThrottleIgnoresSpoofedHeader(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	a := New(s, "main", false)
	a.SetSetupToken(testSetupToken, "")
	a.ClientIPHeader = "CF-Connecting-IP"
	a.TrustedProxies, _ = ParseTrustedProxies("10.0.0.0/8")
	h := a.Handler()
	(&client{t: t, h: h, csrf: true}).setupAdmin("ada@example.com", "password123", "Ada")

	login := func(ip string) int {
		raw, _ := json.Marshal(map[string]any{"email": "ada@example.com", "password": "wrong-password"})
		r := httptest.NewRequest("POST", "/auth/login", bytes.NewReader(raw))
		r.RemoteAddr = "198.51.100.7:4321" // not a trusted proxy
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set(csrfHeader, csrfValue)
		r.Header.Set("CF-Connecting-IP", ip)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	for i := 0; i < maxLoginFailures; i++ {
		eq(t, "bad login", login(fmt.Sprintf("203.0.113.%d", i)), 401)
	}
	eq(t, "throttled", login("203.0.113.99"), 429)
}
