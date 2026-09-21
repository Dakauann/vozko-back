package shared

import (
	"regexp"
	"strconv"
	"strings"
)

var positionalPlaceholder = regexp.MustCompile(`\{\{(\d+)\}\}`)

func PositionalParameters(body string) map[int]struct{} {
	out := map[int]struct{}{}
	for _, match := range positionalPlaceholder.FindAllStringSubmatch(body, -1) {
		if n, err := strconv.Atoi(match[1]); err == nil {
			out[n] = struct{}{}
		}
	}
	return out
}

func HighestPositionalParameter(bodies []string) int {
	highest := 0
	for _, body := range bodies {
		for n := range PositionalParameters(body) {
			if n > highest {
				highest = n
			}
		}
	}
	return highest
}

func PositionalParametersAgree(bodies []string) bool {
	if len(bodies) < 2 {
		return true
	}
	first := PositionalParameters(bodies[0])
	for _, body := range bodies[1:] {
		other := PositionalParameters(body)
		if len(other) != len(first) {
			return false
		}
		for n := range first {
			if _, ok := other[n]; !ok {
				return false
			}
		}
	}
	return true
}

func RenderPositional(body string, vars []string) string {
	if len(vars) == 0 {
		return body
	}
	return positionalPlaceholder.ReplaceAllStringFunc(body, func(token string) string {
		match := positionalPlaceholder.FindStringSubmatch(token)
		n, err := strconv.Atoi(match[1])
		if err != nil || n < 1 || n > len(vars) {
			return token
		}
		return vars[n-1]
	})
}

func VariantIndexFor(key string, variants int) int {
	if variants <= 1 {
		return 0
	}
	var h uint32 = 2166136261
	for i := 0; i < len(key); i++ {
		h ^= uint32(key[i])
		h *= 16777619
	}
	return int(h % uint32(variants))
}

func NonEmptyTrimmed(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func TruncateRunes(s string, max int) (string, bool) {
	if max <= 0 {
		return "", s != ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s, false
	}
	return string(runes[:max]), true
}
