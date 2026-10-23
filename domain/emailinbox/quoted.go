package emailinbox

import (
	"regexp"
	"strings"
)

var (
	attributionLine   = regexp.MustCompile(`(?i)^(on\s.+\swrote|em\s.+\sescreveu|el\s.+\sescribi[oó]|le\s.+\sa\s[ée]crit|am\s.+\sschrieb)\s*:\s*$`)
	attributionStart  = regexp.MustCompile(`(?i)^(on|em|el|le|am)\s.+`)
	attributionEnd    = regexp.MustCompile(`(?i)^.*(wrote|escreveu|escribi[oó]|a\s[ée]crit|schrieb)\s*:\s*$`)
	separatorLine     = regexp.MustCompile(`(?i)^(-{2,}\s*(original message|mensagem original|mensaje original|forwarded message|mensagem encaminhada)\s*-{2,}|_{10,})$`)
	headerBlockStart  = regexp.MustCompile(`(?i)^(de|from|von)\s*:\s*\S`)
	headerBlockFollow = regexp.MustCompile(`(?i)^(enviado|enviada|sent|date|data|para|to|cc|assunto|subject|gesendet|an|betreff)\s*:`)
)

const headerBlockWindow = 4

func StripQuoted(text string) (string, string) {
	normalized := strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(normalized, "\n")
	cut := quoteStart(lines)
	if cut < 0 {
		return strings.TrimSpace(normalized), ""
	}
	body := strings.TrimSpace(strings.Join(lines[:cut], "\n"))
	if body == "" {
		return strings.TrimSpace(normalized), ""
	}
	return body, strings.TrimSpace(strings.Join(lines[cut:], "\n"))
}

func quoteStart(lines []string) int {
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		switch {
		case line == "":
			continue
		case attributionLine.MatchString(line), separatorLine.MatchString(line):
			return i
		case attributionStart.MatchString(line) && i+1 < len(lines) && attributionEnd.MatchString(strings.TrimSpace(lines[i+1])):
			return i
		case headerBlockStart.MatchString(line) && followedByHeaders(lines[i+1:]):
			return i
		}
	}
	return trailingQuote(lines)
}

func followedByHeaders(rest []string) bool {
	for i := 0; i < len(rest) && i < headerBlockWindow; i++ {
		if headerBlockFollow.MatchString(strings.TrimSpace(rest[i])) {
			return true
		}
	}
	return false
}

func trailingQuote(lines []string) int {
	start := -1
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		switch {
		case line == "":
			continue
		case strings.HasPrefix(line, ">"):
			if start < 0 {
				start = i
			}
		default:
			start = -1
		}
	}
	return start
}
