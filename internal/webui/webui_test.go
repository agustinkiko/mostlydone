package webui

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func gz(t *testing.T, s string) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := gzip.NewWriter(&b)
	zw.Write([]byte(s))
	zw.Close()
	return b.Bytes()
}

func testFS(t *testing.T) fstest.MapFS {
	return fstest.MapFS{
		"index.html":            {Data: []byte("<!doctype html><title>Mostlydone</title><script type=module src=/assets/app-abc.js></script>")},
		"index.html.gz":         {Data: gz(t, "<!doctype html><title>Mostlydone</title><script type=module src=/assets/app-abc.js></script>")},
		"assets/app-abc.js":     {Data: []byte("console.log(1)")},
		"assets/app-abc.js.gz":  {Data: gz(t, "console.log(1)")},
		"assets/style-abc.css":  {Data: []byte("body{}")},
		"robots.txt":            {Data: []byte("User-agent: *")},
		".keep":                 {Data: nil},
		"assets/nested/x.woff2": {Data: []byte("font")},
	}
}

func get(h http.Handler, path string, hdr map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", path, nil)
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestSPAFallback(t *testing.T) {
	h := newHandler(testFS(t))
	for _, p := range []string{"/", "/p/WEB/items", "/p/WEB/items/WEB-12?state_group=started", "/settings/tokens", "/index.html", "/a/../b"} {
		w := get(h, p, nil)
		if w.Code != 200 || !strings.Contains(w.Body.String(), "<title>Mostlydone</title>") {
			t.Errorf("GET %s: %d %q", p, w.Code, w.Body.String())
		}
		if got := w.Header().Get("Cache-Control"); got != "no-cache" {
			t.Errorf("GET %s: Cache-Control %q", p, got)
		}
		if got := w.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
			t.Errorf("GET %s: Content-Type %q", p, got)
		}
	}
}

func TestAssets(t *testing.T) {
	h := newHandler(testFS(t))
	w := get(h, "/assets/style-abc.css", nil)
	if w.Code != 200 || w.Body.String() != "body{}" {
		t.Fatalf("css: %d %q", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("css Cache-Control %q", got)
	}
	if got := w.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/css") {
		t.Errorf("css Content-Type %q", got)
	}
	for _, p := range []string{"/assets/missing.js", "/assets/", "/assets/nested", "/assets/app-abc.js.gz"} {
		if w := get(h, p, nil); w.Code != 404 {
			t.Errorf("GET %s: %d, want 404", p, w.Code)
		}
	}
	if w := get(h, "/robots.txt", nil); w.Code != 200 || w.Body.String() != "User-agent: *" {
		t.Errorf("root file: %d %q", w.Code, w.Body.String())
	}
}

func TestGzipNegotiation(t *testing.T) {
	h := newHandler(testFS(t))
	cases := []struct {
		accept string
		gzip   bool
	}{
		{"", false},
		{"gzip", true},
		{"br, gzip, deflate", true},
		{"gzip;q=0", false},
		{"gzip; q=0.5", true},
		{"identity", false},
		{"*", true},
		{"deflate, *;q=0", false},
	}
	for _, c := range cases {
		w := get(h, "/assets/app-abc.js", map[string]string{"Accept-Encoding": c.accept})
		if w.Code != 200 {
			t.Fatalf("%q: %d", c.accept, w.Code)
		}
		gotGzip := w.Header().Get("Content-Encoding") == "gzip"
		if gotGzip != c.gzip {
			t.Errorf("%q: gzip=%v, want %v", c.accept, gotGzip, c.gzip)
		}
		body := w.Body.Bytes()
		if gotGzip {
			zr, err := gzip.NewReader(bytes.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			body, _ = io.ReadAll(zr)
		}
		if string(body) != "console.log(1)" {
			t.Errorf("%q: body %q", c.accept, body)
		}
		if w.Header().Get("Vary") != "Accept-Encoding" {
			t.Errorf("%q: Vary %q", c.accept, w.Header().Get("Vary"))
		}
		if !strings.HasPrefix(w.Header().Get("Content-Type"), "text/javascript") {
			t.Errorf("%q: Content-Type %q", c.accept, w.Header().Get("Content-Type"))
		}
	}
	// index.html has a .gz sibling too.
	w := get(h, "/p/X/items", map[string]string{"Accept-Encoding": "gzip"})
	if w.Header().Get("Content-Encoding") != "gzip" {
		t.Errorf("index not gzipped")
	}
}

func TestSecurityHeaders(t *testing.T) {
	h := newHandler(testFS(t))
	for _, p := range []string{"/", "/assets/app-abc.js", "/assets/missing.js"} {
		w := get(h, p, nil)
		if got := w.Header().Get("Content-Security-Policy"); got != csp || !strings.Contains(got, "frame-ancestors 'none'") {
			t.Errorf("%s: CSP %q", p, got)
		}
		if w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Referrer-Policy") != "same-origin" {
			t.Errorf("%s: headers %v", p, w.Header())
		}
	}
	r := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 405 {
		t.Errorf("POST: %d", w.Code)
	}
	r = httptest.NewRequest("HEAD", "/assets/app-abc.js", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || w.Body.Len() != 0 || w.Header().Get("Content-Length") != "14" {
		t.Errorf("HEAD: %d len=%d cl=%q", w.Code, w.Body.Len(), w.Header().Get("Content-Length"))
	}
}

func TestPlaceholderWhenUnbuilt(t *testing.T) {
	h := newHandler(fstest.MapFS{".keep": {Data: nil}})
	w := get(h, "/p/WEB/board", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "<title>Mostlydone</title>") || !strings.Contains(w.Body.String(), "not built") {
		t.Errorf("placeholder: %d %q", w.Code, w.Body.String())
	}
	if w := get(h, "/assets/app.js", nil); w.Code != 404 {
		t.Errorf("asset on unbuilt: %d", w.Code)
	}
}

func TestEmbeddedHandler(t *testing.T) {
	// Works whether or not the UI was built.
	w := get(Handler(), "/", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "<title>Mostlydone</title>") {
		t.Errorf("embedded: %d %q", w.Code, w.Body.String())
	}
}
