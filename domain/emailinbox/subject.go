package emailinbox

import (
	"regexp"
	"strings"
)

var replyPrefix = regexp.MustCompile(`(?i)^(re|res|fw|fwd|enc|rv|tr|aw|wg|sv|vs|r)\s*(\[\d+\])?\s*:\s*`)

func NormalizeSubject(subject string) string {
	s := strings.Join(strings.Fields(subject), " ")
	for {
		trimmed := replyPrefix.ReplaceAllString(s, "")
		if trimmed == s {
			return strings.TrimSpace(s)
		}
		s = strings.TrimSpace(trimmed)
	}
}

func ReplySubject(subject string) string {
	normalized := NormalizeSubject(subject)
	if normalized == "" {
		return "Re:"
	}
	return "Re: " + normalized
}
