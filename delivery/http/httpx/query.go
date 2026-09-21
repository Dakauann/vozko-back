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
