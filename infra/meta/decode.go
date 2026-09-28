package meta

import (
	"encoding/json"
	"fmt"
	"strings"

	mm "vozko/domain/metamessaging"
)

type PermissionList []string

func (p *PermissionList) UnmarshalJSON(data []byte) error {
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

func (p PermissionList) Strings() []string { return p }

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

type GraphID = mm.GraphID
