package emailinbox

import (
	"html"
	"strings"
	"unicode/utf8"
)

const (
	MaxTranscriptTextBytes = 100 << 10
	MaxReplyTextBytes      = 100 << 10
	MaxReferences          = 20
	truncationMark         = "…"
)

func TruncateText(text string, max int) (string, bool) {
	if len(text) <= max {
		return text, false
	}
	limit := max - len(truncationMark)
	if limit < 0 {
		limit = 0
	}
	for limit > 0 && !utf8.RuneStart(text[limit]) {
		limit--
	}
	return text[:limit] + truncationMark, true
}

func ReplyHTML(text string) string {
	normalized := strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	var b strings.Builder
	b.WriteString(`<!DOCTYPE html><html><body><div style="font-family:Arial,Helvetica,sans-serif;font-size:14px;line-height:1.5;color:#1f2328">`)
	for _, paragraph := range strings.Split(normalized, "\n\n") {
		paragraph = strings.Trim(paragraph, "\n")
		if strings.TrimSpace(paragraph) == "" {
			continue
		}
		escaped := strings.Split(paragraph, "\n")
		for i, line := range escaped {
			escaped[i] = html.EscapeString(line)
		}
		b.WriteString(`<p style="margin:0 0 12px">`)
		b.WriteString(strings.Join(escaped, "<br>"))
		b.WriteString("</p>")
	}
	b.WriteString("</div></body></html>")
	return b.String()
}

func NormalizeMessageID(raw string) string {
	id := strings.TrimSpace(raw)
	id = strings.TrimSuffix(strings.TrimPrefix(id, "<"), ">")
	id = strings.TrimSpace(id)
	if id == "" || strings.ContainsAny(id, " \t\r\n<>") {
		return ""
	}
	return id
}

func ReferenceChain(prior []string, add ...string) []string {
	seen := make(map[string]struct{}, len(prior)+len(add))
	out := make([]string, 0, len(prior)+len(add))
	for _, raw := range append(append([]string{}, prior...), add...) {
		id := NormalizeMessageID(raw)
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	if len(out) <= MaxReferences {
		return out
	}
	bounded := make([]string, 0, MaxReferences)
	bounded = append(bounded, out[0])
	return append(bounded, out[len(out)-(MaxReferences-1):]...)
}
