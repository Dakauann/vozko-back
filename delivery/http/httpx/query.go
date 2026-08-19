package httpx

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"vozko/domain/crmfilter"
	"vozko/domain/shared"
)

// ParseSort reads the repeated/comma-separated `sort` query parameter into
// domain sorts.
//
// Accepted shapes, all equivalent: `?sort=name:asc&sort=createdAt:desc`,
// `?sort=name:asc,createdAt:desc`. Keys are matched case-insensitively against
// `allowed`, which maps the client-facing key onto whatever the domain calls
// the field; an unknown key is dropped rather than rejected, so a stale
// bookmark degrades to the default order instead of a 400.
//
// This lives in httpx because six handler packages had each grown their own
// byte-identical copy. Callers pass their own `allowed` map, which is the only
// part that was ever really per-endpoint.
func ParseSort(values url.Values, allowed map[string]string) []shared.Sort {
	rawSorts := values["sort"]
	if len(rawSorts) == 0 {
		return nil
	}

	sorts := make([]shared.Sort, 0)
	for _, raw := range rawSorts {
		for _, entry := range strings.Split(raw, ",") {
			entry = strings.TrimSpace(entry)
			if entry == "" {
				continue
			}
			parts := strings.Split(entry, ":")
			field, ok := allowed[strings.ToLower(strings.TrimSpace(parts[0]))]
			if !ok {
				continue
			}

			direction := shared.SortAsc
			if len(parts) > 1 {
				if dir := strings.ToLower(strings.TrimSpace(parts[1])); dir == string(shared.SortDesc) {
					direction = shared.SortDesc
				}
			}

			sorts = append(sorts, shared.Sort{Field: field, Direction: direction})
		}
	}

	return sorts
}

// ParseBoolQuery reads a tri-state boolean: nil when the parameter is absent or
// unparseable ("no opinion"), otherwise the value. The nil case matters — for a
// filter, "absent" and "false" are different questions.
func ParseBoolQuery(raw string) *bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "true", "1", "yes", "on":
		v := true
		return &v
	case "false", "0", "no", "off":
		v := false
		return &v
	}
	return nil
}

// ParseIntQuery reads an optional integer, returning nil when absent or
// unparseable.
func ParseIntQuery(raw string) *int {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	v, err := strconv.Atoi(trimmed)
	if err != nil {
		return nil
	}
	return &v
}

// ParseCSVQuery splits a repeated/comma-separated parameter into its non-blank
// values, e.g. `?category=deal,objection&category=event`.
func ParseCSVQuery(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, raw := range values {
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if _, dup := seen[part]; dup {
				continue
			}
			seen[part] = struct{}{}
			out = append(out, part)
		}
	}
	return out
}

// DecodeFilterParam decodes a crmfilter expression carried in a query string.
//
// The value is JSON, optionally base64-encoded (standard or URL alphabet) so
// its braces and quotes never need escaping. A blank value is the empty filter,
// not an error: "no filter" is a legitimate request.
func DecodeFilterParam(raw string) (crmfilter.Filter, error) {
	var f crmfilter.Filter
	data := DecodeMaybeBase64(raw)
	if len(data) == 0 {
		return f, nil
	}
	if err := json.Unmarshal(data, &f); err != nil {
		return crmfilter.Filter{}, err
	}
	return f, nil
}

// DecodeMaybeBase64 returns the raw bytes of a parameter that may or may not be
// base64-encoded, trying both alphabets before falling back to the literal
// text.
func DecodeMaybeBase64(raw string) []byte {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if decoded, err := base64.StdEncoding.DecodeString(raw); err == nil {
		return decoded
	}
	if decoded, err := base64.URLEncoding.DecodeString(raw); err == nil {
		return decoded
	}
	return []byte(raw)
}
