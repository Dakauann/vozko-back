package sheet

import (
	"strings"
	"unicode"

	"vozko/domain/shared"
)

type Target struct {
	Field   string
	Aliases []string
	Group   string
}

func HeaderKey(header string) string {
	folded := shared.FoldForMatch(header)
	words := strings.FieldsFunc(folded, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	return strings.Join(words, " ")
}

func baseKey(key string) string {
	return strings.TrimSpace(strings.TrimRightFunc(key, func(r rune) bool {
		return unicode.IsDigit(r) || r == ' '
	}))
}

func Guess(headers []string, targets []Target, capacity map[string]int) []string {
	fields := make([]string, len(headers))
	used := map[string]int{}
	room := func(t Target) bool {
		if t.Group == "" {
			return used[t.Field] < 1
		}
		limit, ok := capacity[t.Group]
		if !ok {
			limit = 1
		}
		return used[t.Group] < limit
	}
	take := func(t Target) {
		if t.Group == "" {
			used[t.Field]++
			return
		}
		used[t.Group]++
	}
	keys := make([]string, len(headers))
	for i, h := range headers {
		keys[i] = HeaderKey(h)
	}
	for _, loose := range []bool{false, true} {
		for i, key := range keys {
			if fields[i] != "" || key == "" {
				continue
			}
			if loose {
				key = baseKey(key)
			}
			for _, t := range targets {
				if room(t) && matches(t, key) {
					fields[i] = t.Field
					take(t)
					break
				}
			}
		}
	}
	return fields
}

func matches(t Target, key string) bool {
	if key == "" {
		return false
	}
	for _, alias := range t.Aliases {
		if HeaderKey(alias) == key {
			return true
		}
	}
	return false
}

func ColumnIndex(headers []string, name string) int {
	name = strings.TrimSpace(name)
	if name == "" {
		return -1
	}
	for i, h := range headers {
		if strings.EqualFold(strings.TrimSpace(h), name) {
			return i
		}
	}
	return -1
}
