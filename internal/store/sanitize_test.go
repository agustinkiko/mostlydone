package store

import (
	"strings"
	"testing"
)

func TestSanitizeHTML(t *testing.T) {
	cases := []struct{ in, want string }{
		{`<p>Hello <strong>world</strong></p>`, `<p>Hello <strong>world</strong></p>`},
		{`<p>a<script>alert(1)</script>b</p>`, `<p>ab</p>`},
		{`<SCRIPT src=x></SCRIPT>ok`, `ok`},
		{`<img src=x onerror="alert(1)">`, `<img src="x">`},
		{`<p onclick="evil()" class="c">t</p>`, `<p class="c">t</p>`},
		{`<a href="javascript:alert(1)">x</a>`, `<a>x</a>`},
		{`<a href="  JaVaScRiPt:alert(1)">x</a>`, `<a>x</a>`},
		{`<a href="java&#x09;script:alert(1)">x</a>`, `<a>x</a>`},
		{`<a href="data:text/html,<script>">x</a>`, `<a>x</a>`},
		{`<img src="data:image/png;base64,AAA">`, `<img>`},
		{`<a href="https://example.com/?a=1&b=2" target="x">l</a>`, `<a href="https://example.com/?a=1&amp;b=2" target="_blank">l</a>`},
		{`<a href="mailto:x@y.z">m</a>`, `<a href="mailto:x@y.z">m</a>`},
		{`<iframe src="https://evil"></iframe>after`, `after`},
		{`<style>body{}</style><p>x</p>`, `<p>x</p>`},
		{`<!-- c --><p>x`, `<p>x</p>`},
		{`<div><p>unclosed`, `<div><p>unclosed</p></div>`},
		{`</p>stray`, `stray`},
		{`1 < 2 & 3 > 2`, `1 &lt; 2 &amp; 3 &gt; 2`},
		{`<p title="a"b">x</p>`, `<p>x</p>`},
		{`<svg><script>alert(1)</script></svg>x`, `x`},
		{`<input type="checkbox" checked onchange="x()">`, `<input type="checkbox" checked="">`},
		{`<input type="text">`, `<input>`},
		{`<a href="x" style="background:url(javascript:x)">s</a>`, `<a href="x">s</a>`},
		{`<p>unterminated <a href="x"`, `<p>unterminated </p>`},
	}
	for _, c := range cases {
		if got := SanitizeHTML(c.in); got != c.want {
			t.Errorf("SanitizeHTML(%q)\n got %q\nwant %q", c.in, got, c.want)
		}
	}
	for _, c := range cases {
		out := strings.ToLower(SanitizeHTML(c.in))
		for _, bad := range []string{"<script", "javascript:", "onerror", "onclick", "<iframe", "data:"} {
			if strings.Contains(out, bad) {
				t.Errorf("%q survived in %q", bad, out)
			}
		}
	}
}
