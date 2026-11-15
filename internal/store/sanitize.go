package store

import (
	"html"
	"strings"
)

// golang.org/x/net/html is not in the module cache, so this is a small,
// conservative tokenizer. It keeps only allow-listed tags and attributes,
// drops everything else (text inside dangerous elements included), re-escapes
// all text and attribute values, and closes any tags left open.

var allowedTags = map[string]bool{
	"p": true, "br": true, "hr": true, "div": true, "span": true,
	"strong": true, "b": true, "em": true, "i": true, "u": true, "s": true, "strike": true, "del": true,
	"code": true, "pre": true, "blockquote": true, "ul": true, "ol": true, "li": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"a": true, "img": true, "table": true, "thead": true, "tbody": true, "tr": true, "th": true, "td": true,
	"input": true, // task-list checkboxes; only type=checkbox survives
	"label": true,
}

var voidTags = map[string]bool{"br": true, "hr": true, "img": true, "input": true}

// Elements dropped together with everything up to their closing tag.
var dropContent = map[string]bool{
	"script": true, "style": true, "iframe": true, "object": true, "noscript": true, "template": true,
	"textarea": true, "title": true, "xmp": true, "noembed": true, "noframes": true, "svg": true, "math": true,
	"select": true, "frameset": true,
}

var allowedAttrs = map[string]map[string]bool{
	"*":     {"class": true},
	"a":     {"href": true, "title": true, "target": true, "rel": true},
	"img":   {"src": true, "alt": true, "title": true, "width": true, "height": true},
	"ol":    {"start": true},
	"td":    {"colspan": true, "rowspan": true},
	"th":    {"colspan": true, "rowspan": true},
	"input": {"type": true, "checked": true, "disabled": true},
	"li":    {"data-type": true, "data-checked": true},
	"ul":    {"data-type": true},
}

// SanitizeHTML returns s reduced to the allow-list.
func SanitizeHTML(s string) string {
	var out strings.Builder
	var stack []string
	i := 0
	for i < len(s) {
		lt := strings.IndexByte(s[i:], '<')
		if lt < 0 {
			out.WriteString(escapeText(s[i:]))
			break
		}
		out.WriteString(escapeText(s[i : i+lt]))
		i += lt
		rest := s[i:]
		switch {
		case strings.HasPrefix(rest, "<!--"):
			end := strings.Index(rest[4:], "-->")
			if end < 0 {
				return closeAll(&out, stack)
			}
			i += 4 + end + 3
		case strings.HasPrefix(rest, "<!") || strings.HasPrefix(rest, "<?"):
			end := strings.IndexByte(rest, '>')
			if end < 0 {
				return closeAll(&out, stack)
			}
			i += end + 1
		case strings.HasPrefix(rest, "</") && len(rest) > 2 && isLetter(rest[2]):
			name, _ := tagName(rest[2:])
			end := strings.IndexByte(rest, '>')
			if end < 0 {
				return closeAll(&out, stack)
			}
			i += end + 1
			for j := len(stack) - 1; j >= 0; j-- {
				if stack[j] == name {
					for k := len(stack) - 1; k >= j; k-- {
						out.WriteString("</" + stack[k] + ">")
					}
					stack = stack[:j]
					break
				}
			}
		case len(rest) > 1 && isLetter(rest[1]):
			name, n := tagName(rest[1:])
			attrs, consumed, ok := parseAttrs(rest[1+n:])
			if !ok {
				return closeAll(&out, stack)
			}
			i += 1 + n + consumed
			if dropContent[name] {
				i += skipUntilClose(s[i:], name)
				continue
			}
			if !allowedTags[name] {
				continue
			}
			out.WriteString("<" + name)
			for _, a := range attrs {
				if v, ok := cleanAttr(name, a[0], a[1]); ok {
					out.WriteString(" " + a[0] + `="` + html.EscapeString(v) + `"`)
				}
			}
			out.WriteString(">")
			if !voidTags[name] {
				stack = append(stack, name)
			}
		default:
			out.WriteString("&lt;")
			i++
		}
	}
	return closeAll(&out, stack)
}

func closeAll(out *strings.Builder, stack []string) string {
	for k := len(stack) - 1; k >= 0; k-- {
		out.WriteString("</" + stack[k] + ">")
	}
	return out.String()
}

func escapeText(t string) string { return html.EscapeString(html.UnescapeString(t)) }

func isLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

func tagName(s string) (string, int) {
	n := 0
	for n < len(s) && (isLetter(s[n]) || s[n] >= '0' && s[n] <= '9' || s[n] == '-' || s[n] == ':') {
		n++
	}
	return strings.ToLower(s[:n]), n
}

// parseAttrs reads attributes up to and including the closing '>'.
func parseAttrs(s string) (attrs [][2]string, consumed int, ok bool) {
	i := 0
	for {
		for i < len(s) && (isSpace(s[i]) || s[i] == '/') {
			i++
		}
		if i >= len(s) {
			return nil, 0, false
		}
		if s[i] == '>' {
			return attrs, i + 1, true
		}
		start := i
		for i < len(s) && !isSpace(s[i]) && s[i] != '=' && s[i] != '>' && s[i] != '/' {
			i++
		}
		name := strings.ToLower(s[start:i])
		for i < len(s) && isSpace(s[i]) {
			i++
		}
		val := ""
		if i < len(s) && s[i] == '=' {
			i++
			for i < len(s) && isSpace(s[i]) {
				i++
			}
			if i >= len(s) {
				return nil, 0, false
			}
			if q := s[i]; q == '"' || q == '\'' {
				end := strings.IndexByte(s[i+1:], q)
				if end < 0 {
					return nil, 0, false
				}
				val = s[i+1 : i+1+end]
				i += end + 2
			} else {
				vs := i
				for i < len(s) && !isSpace(s[i]) && s[i] != '>' {
					i++
				}
				val = s[vs:i]
			}
		}
		if name != "" {
			attrs = append(attrs, [2]string{name, html.UnescapeString(val)})
		}
	}
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' }

func skipUntilClose(s, name string) int {
	lower := strings.ToLower(s)
	idx := strings.Index(lower, "</"+name)
	if idx < 0 {
		return len(s)
	}
	end := strings.IndexByte(s[idx:], '>')
	if end < 0 {
		return len(s)
	}
	return idx + end + 1
}

func cleanAttr(tag, name, val string) (string, bool) {
	if !allowedAttrs[tag][name] && !allowedAttrs["*"][name] {
		return "", false
	}
	switch name {
	case "href", "src":
		return val, safeURL(val, name == "href")
	case "type":
		return val, strings.EqualFold(val, "checkbox")
	case "target":
		return "_blank", true
	}
	return val, true
}

// safeURL allows http(s), mailto (links only) and relative URLs.
func safeURL(u string, link bool) bool {
	var b strings.Builder
	for _, r := range u {
		if r > ' ' && r != 0x7f {
			b.WriteRune(r)
		}
	}
	clean := strings.ToLower(b.String())
	colon := strings.IndexByte(clean, ':')
	if colon < 0 {
		return true
	}
	if cut := strings.IndexAny(clean, "/?#"); cut >= 0 && cut < colon {
		return true
	}
	switch clean[:colon] {
	case "http", "https":
		return true
	case "mailto":
		return link
	}
	return false
}
