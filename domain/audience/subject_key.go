package audience

import (
	"strings"
	"unicode"
)

const (
	MaxSubjectWords = 3

	MaxSubjectKeyRunes = 60

	MaxRankedSubjects = 12
)

var nonSubjects = map[string]bool{
	"n a": true, "na": true, "nenhum": true, "nenhuma": true, "none": true,
	"nao informado": true, "nao identificado": true, "indefinido": true,
	"desconhecido": true, "unknown": true, "outro": true, "other": true,
	"sem assunto": true, "nao se aplica": true, "n d": true,
}

func SubjectKey(text string) string {
	var b strings.Builder
	b.Grow(len(text))
	lastWasSpace := true
	for _, r := range strings.ToLower(strings.TrimSpace(text)) {
		folded := foldRune(r)
		if folded == 0 {
			if !lastWasSpace {
				b.WriteRune(' ')
				lastWasSpace = true
			}
			continue
		}
		b.WriteRune(folded)
		lastWasSpace = false
	}

	words := strings.Fields(b.String())
	if len(words) > MaxSubjectWords {
		words = words[:MaxSubjectWords]
	}
	key := strings.Join(words, " ")
	if nonSubjects[key] {
		return ""
	}
	key, _ = TruncateRunes(key, MaxSubjectKeyRunes)
	return strings.TrimSpace(key)
}

func foldRune(r rune) rune {
	switch {
	case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		return r
	case strings.ContainsRune("áàâãä", r):
		return 'a'
	case strings.ContainsRune("éèêë", r):
		return 'e'
	case strings.ContainsRune("íìîï", r):
		return 'i'
	case strings.ContainsRune("óòôõö", r):
		return 'o'
	case strings.ContainsRune("úùûü", r):
		return 'u'
	case r == 'ç':
		return 'c'
	case r == 'ñ':
		return 'n'
	case unicode.IsLetter(r) || unicode.IsDigit(r):
		return r
	}
	return 0
}

type SubjectCount struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Count int    `json:"count"`
}
