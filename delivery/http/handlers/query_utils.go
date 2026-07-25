package handlers

import (
	"net/url"
	"strconv"
	"strings"

	"vozko/domain/shared"
)

func parsePagination(values url.Values) shared.Pagination {
	page := 1
	if v := strings.TrimSpace(values.Get("page")); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			page = parsed
		}
	}

	pageSize := shared.DefaultPageSize
	if v := strings.TrimSpace(values.Get("pageSize")); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			pageSize = parsed
		}
	}

	return shared.Pagination{Page: page, PageSize: pageSize}
}

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

func firstQueryValue(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func splitListValues(values []string) []string {
	result := make([]string, 0)
	for _, raw := range values {
		entries := strings.Split(raw, ",")
		for _, entry := range entries {
			trimmed := strings.TrimSpace(entry)
			if trimmed != "" {
				result = append(result, trimmed)
			}
		}
	}
	return result
}
