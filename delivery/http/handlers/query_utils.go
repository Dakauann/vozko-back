package handlers

import (
	"net/url"
	"strconv"
	"strings"

	"vozko/domain/shared"
	"vozko/domain/workflow"
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

func stepWindowOf(values url.Values) (workflow.StepWindow, bool) {
	var window workflow.StepWindow
	for key, into := range map[string]*int{"stepsOffset": &window.Offset, "stepsLimit": &window.Limit} {
		raw := strings.TrimSpace(values.Get(key))
		if raw == "" {
			continue
		}
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			return workflow.StepWindow{}, false
		}
		*into = parsed
	}
	return window, true
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
