package lead

import (
	"net/url"
	"strings"

	"vozko/domain/shared"
)

func parseSort(values url.Values, allowed map[string]string) []shared.Sort {
	rawSorts := values["sort"]
	if len(rawSorts) == 0 {
		return nil
	}

	sorts := make([]shared.Sort, 0)
	for _, raw := range rawSorts {
		entries := strings.Split(raw, ",")
		for _, entry := range entries {
			entry = strings.TrimSpace(entry)
			if entry == "" {
				continue
			}
			parts := strings.Split(entry, ":")
			fieldKey := strings.ToLower(strings.TrimSpace(parts[0]))
			field, ok := allowed[fieldKey]
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
