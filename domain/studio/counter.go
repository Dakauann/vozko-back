package studio

import (
	"slices"
	"unicode"
	"unicode/utf8"
)

const (
	MinCounterValue      = -1e12
	MaxCounterValue      = 1e12
	MaxCounterDecimals   = 3
	MaxCounterAffixRunes = 16
)

type Counter struct {
	Value    float64 `json:"value"`
	Decimals int     `json:"decimals"`
	Locale   string  `json:"locale"`
	Grouping bool    `json:"grouping"`
	Prefix   string  `json:"prefix,omitempty"`
	Suffix   string  `json:"suffix,omitempty"`
}

func CounterLocales() []string { return []string{"pt-BR", "en-US", "es-ES", "de-DE"} }

func controlRune(r rune) bool {
	return unicode.IsControl(r) || r == '\u2028' || r == '\u2029'
}

func plainTextIssue(text string, maxRunes int) string {
	for _, r := range text {
		if controlRune(r) {
			return CodeInvalid
		}
	}
	if utf8.RuneCountInString(text) > maxRunes {
		return CodeTooLarge
	}
	return ""
}

func affixIssue(affix string) string {
	return plainTextIssue(affix, MaxCounterAffixRunes)
}

func (c Clip) counterKeys() int {
	if c.Keyframes == nil {
		return 0
	}
	return len(c.Keyframes.Counter)
}

func (c Clip) counterIssue() string {
	counter := c.Counter
	switch {
	case counter == nil:
		if c.counterKeys() > 0 {
			return CodeInvalid
		}
		return ""
	case !c.animatesText():
		return CodeInvalid
	case !slices.Contains(CounterLocales(), counter.Locale):
		return CodeUnknown
	case counter.Decimals < 0 || counter.Decimals > MaxCounterDecimals || !within(counter.Value, MinCounterValue, MaxCounterValue):
		return CodeOutOfRange
	}
	if code := affixIssue(counter.Prefix); code != "" {
		return code
	}
	return affixIssue(counter.Suffix)
}
