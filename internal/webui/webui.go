// Package webui serves the embedded React build (web/ is built into dist/)
// with an SPA fallback, pre-compressed assets and security headers.
//
// On a checkout where the UI has not been built, dist/ holds only .keep and
// the handler serves a small placeholder page instead.
package webui

import (
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
)

//go:embed all:dist
var embedded embed.FS

const placeholder = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>Mostlydone</title></head>
<body><h1>Mostlydone</h1><p>The web UI is not built. Run <code>make ui</code>, then rebuild. The REST API is at <code>/api/v1/</code>.</p></body></html>
`

// CSP allows only same-origin scripts; inline styles are allowed because
// React style attributes and the editor use them.
const csp = "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; " +
	"frame-ancestors 'none'; base-uri 'self'; form-action 'self'"

// Handler serves every path not under /api/, /auth/, /mcp or /healthz.
func Handler() http.Handler {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic(err) // dist is embedded at compile time; this cannot fail
	}
	return newHandler(sub)
}

func newHandler(files fs.FS) http.Handler {
	index, err := fs.ReadFile(files, "index.html")
	if err != nil {
		index = []byte(placeholder)
	}
	indexGz, _ := fs.ReadFile(files, "index.html.gz")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			h.Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "assets" || strings.HasPrefix(name, "assets/") {
			if !serveFile(w, r, files, name, "public, max-age=31536000, immutable") {
				http.NotFound(w, r)
			}
			return
		}
		if name != "" && name != "index.html" && !strings.HasSuffix(name, ".gz") && serveFile(w, r, files, name, "no-cache") {
			return
		}
		// SPA fallback: the client router handles every other path.
		write(w, r, index, indexGz, "text/html; charset=utf-8", "no-cache")
	})
}

// serveFile writes a regular file (or its .gz sibling) and reports whether it existed.
func serveFile(w http.ResponseWriter, r *http.Request, files fs.FS, name, cache string) bool {
	if strings.HasSuffix(name, ".gz") || !fs.ValidPath(name) {
		return false
	}
	st, err := fs.Stat(files, name)
	if err != nil || st.IsDir() {
		return false
	}
	raw, err := fs.ReadFile(files, name)
	if err != nil {
		return false
	}
	gz, _ := fs.ReadFile(files, name+".gz")
	ctype := mime.TypeByExtension(path.Ext(name))
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	write(w, r, raw, gz, ctype, cache)
	return true
}

func write(w http.ResponseWriter, r *http.Request, raw, gz []byte, ctype, cache string) {
	h := w.Header()
	h.Set("Content-Type", ctype)
	h.Set("Cache-Control", cache)
	body := raw
	if gz != nil {
		h.Add("Vary", "Accept-Encoding")
		if acceptsGzip(r.Header.Get("Accept-Encoding")) {
			h.Set("Content-Encoding", "gzip")
			body = gz
		}
	}
	h.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		w.Write(body)
	}
}

// acceptsGzip reads an Accept-Encoding header: gzip (or *) listed without q=0.
func acceptsGzip(header string) bool {
	for _, part := range strings.Split(header, ",") {
		coding, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		coding = strings.ToLower(strings.TrimSpace(coding))
		if coding != "gzip" && coding != "*" {
			continue
		}
		q := 1.0
		for _, p := range strings.Split(params, ";") {
			k, v, ok := strings.Cut(strings.TrimSpace(p), "=")
			if ok && strings.EqualFold(strings.TrimSpace(k), "q") {
				if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
					q = f
				}
			}
		}
		return q > 0
	}
	return false
}
