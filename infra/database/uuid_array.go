package database

import (
	"strings"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

func UUIDArray(ids []string) pq.StringArray {
	seen := make(map[string]struct{}, len(ids))
	out := make(pq.StringArray, 0, len(ids))
	for _, raw := range ids {
		parsed, err := uuid.Parse(strings.TrimSpace(raw))
		if err != nil {
			continue
		}
		id := parsed.String()
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
