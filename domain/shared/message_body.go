package shared

import (
	"regexp"
	"strconv"
	"strings"
)

// Positional message bodies: the text handling that campaigns and seeded
// conversations have in common.
//
// It lives here because the compiler put it here. domain/unofficial_whatsapp_campaign
// imports domain/unofficial_whatsapp, so the channel package cannot import the
// campaign package to reuse MessageSpec.VariantFor and .Render, and the choice
// was between copying the logic and lifting it somewhere both can reach.
//
// Only what is PURE and IDENTICAL in both moved. The policy around it did not:
// how many variants an operator may proofread, how long a body may be on a
// given channel, and everything about media and menus stay with whoever owns
// that rule.

// positionalPlaceholder matches OUR variable syntax, {{1}}, {{2}}, ...
//
// Positional rather than named, matching the official WhatsApp template
// contract, so the CSV importer, the variables columns and the operator's
// mental model transfer between transports unchanged. A NAMED placeholder,
// {{nome}}, is the PROVIDER's own syntax substituted from its own lead store;
// nothing here matches or rewrites one.
var positionalPlaceholder = regexp.MustCompile(`\{\{(\d+)\}\}`)

// PositionalParameters is the exact SET of variables one body uses.
//
// A set rather than a count, because that is the comparison
// PositionalParametersAgree has to make. Never nil.
func PositionalParameters(body string) map[int]struct{} {
	out := map[int]struct{}{}
	for _, match := range positionalPlaceholder.FindAllStringSubmatch(body, -1) {
		if n, err := strconv.Atoi(match[1]); err == nil {
			out[n] = struct{}{}
		}
	}
	return out
}

// HighestPositionalParameter is the highest variable used by any body.
//
// The maximum across bodies rather than per body, because an importer collects
// one set of columns for the whole run: a recipient needs enough variables for
// whichever variant they happen to be assigned. The HIGHEST rather than the
// count, because a body using {{1}} and {{3}} needs three columns, not two.
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

// PositionalParametersAgree reports whether every body uses the same variables.
//
// The same SET, not merely the same count. A variant reading {{1}} beside one
// reading {{2}} would send a raw "{{2}}" to everyone assigned the second,
// because the importer only ever collected one column.
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

// RenderPositional substitutes positional variables into one body.
//
// A placeholder with no matching variable is left AS WRITTEN rather than
// blanked, so a misconfigured message is visibly wrong instead of silently
// sending a sentence with a hole in it.
//
// Rendering happens in the DOMAIN, and for an outbound send the result is
// passed through the channel's own sanitiser before it reaches the provider.
// That order is load-bearing: the provider performs its OWN {{...}}
// substitution from ITS lead store, so a brace that survives to the wire leaks
// another tenant's data into a customer's chat.
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

// VariantIndexFor picks which of several bodies a given key receives.
//
// Deterministic in the key rather than random, and that is what makes the
// feature debuggable: the same recipient always gets the same variant, so a
// resumed, retried or re-run job never gives one person two different texts,
// and "which text did this contact get" is answerable from the row.
//
// FNV-1a, inline: the distribution only has to be even across a handful of
// buckets, and importing hash/fnv for four lines would be heavier than the
// arithmetic it replaces.
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

// NonEmptyTrimmed trims every value and drops the ones that were only space.
//
// Never nil, because callers assign the result straight back onto a slice
// field, and a nil there marshals as null where the wire shape promises a list.
func NonEmptyTrimmed(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// TruncateRunes cuts s to at most max runes, reporting whether it cut.
//
// Runes, never bytes: slicing bytes splits a multi-byte character and writes
// invalid UTF-8 into a message row or a prompt. Portuguese is the first
// language this product ships in, so that is not hypothetical.
//
// The bool is what makes an "…" or a "truncated" flag possible at the call
// site, and it is why this is the canonical shape rather than the one-return
// variant several packages had grown separately.
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
