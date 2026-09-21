package instagram

import (
	"encoding/json"
	"fmt"
	"strings"
)

type permissionList []string

func (p *permissionList) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		*p = nil
		return nil
	}

	switch trimmed[0] {
	case '[':
		var list []string
		if err := json.Unmarshal(data, &list); err != nil {
			return fmt.Errorf("permissions: decode array: %w", err)
		}
		*p = normalizePermissions(list)
		return nil

	case '"':
		var raw string
		if err := json.Unmarshal(data, &raw); err != nil {
			return fmt.Errorf("permissions: decode string: %w", err)
		}
		*p = normalizePermissions(strings.Split(raw, ","))
		return nil

	default:
		*p = nil
		return nil
	}
}

func (p permissionList) Strings() []string { return p }

func normalizePermissions(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

type graphID string

func (g *graphID) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		*g = ""
		return nil
	}

	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return fmt.Errorf("id: decode string: %w", err)
		}
		*g = graphID(strings.TrimSpace(s))
		return nil
	}

	var n json.Number
	if err := json.Unmarshal(data, &n); err != nil {
		return fmt.Errorf("id: decode number: %w", err)
	}
	*g = graphID(n.String())
	return nil
}

func (g graphID) String() string { return string(g) }
