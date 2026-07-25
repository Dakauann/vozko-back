package ticket

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

func parseTicketFilters(values url.Values) []shared.Filter {
	filters := make([]shared.Filter, 0)

	if statuses := splitListValues(values["status"]); len(statuses) > 0 {
		filters = append(filters, shared.Filter{Field: "status", Operator: shared.FilterOpIn, Values: statuses})
	}

	if search := strings.TrimSpace(values.Get("search")); search != "" {
		filters = append(filters, shared.Filter{Field: "search", Operator: shared.FilterOpLike, Values: []string{search}})
	} else if search := strings.TrimSpace(values.Get("q")); search != "" {
		filters = append(filters, shared.Filter{Field: "search", Operator: shared.FilterOpLike, Values: []string{search}})
	}

	if orderID := strings.TrimSpace(values.Get("orderId")); orderID != "" {
		filters = append(filters, shared.Filter{Field: "orderId", Operator: shared.FilterOpEquals, Values: []string{orderID}})
	}

	if trackingCode := strings.TrimSpace(values.Get("trackingCode")); trackingCode != "" {
		filters = append(filters, shared.Filter{Field: "trackingCode", Operator: shared.FilterOpLike, Values: []string{trackingCode}})
	}

	if userID := strings.TrimSpace(values.Get("userId")); userID != "" {
		filters = append(filters, shared.Filter{Field: "userId", Operator: shared.FilterOpEquals, Values: []string{userID}})
	}

	if createdFrom := strings.TrimSpace(values.Get("createdFrom")); createdFrom != "" {
		filters = append(filters, shared.Filter{Field: "createdAt", Operator: shared.FilterOpGte, Values: []string{createdFrom}})
	}

	if createdTo := strings.TrimSpace(values.Get("createdTo")); createdTo != "" {
		filters = append(filters, shared.Filter{Field: "createdAt", Operator: shared.FilterOpLte, Values: []string{createdTo}})
	}

	if updatedFrom := strings.TrimSpace(values.Get("updatedFrom")); updatedFrom != "" {
		filters = append(filters, shared.Filter{Field: "updatedAt", Operator: shared.FilterOpGte, Values: []string{updatedFrom}})
	}

	if updatedTo := strings.TrimSpace(values.Get("updatedTo")); updatedTo != "" {
		filters = append(filters, shared.Filter{Field: "updatedAt", Operator: shared.FilterOpLte, Values: []string{updatedTo}})
	}

	if lastStatusFrom := strings.TrimSpace(values.Get("lastStatusFrom")); lastStatusFrom != "" {
		filters = append(filters, shared.Filter{Field: "lastStatusAt", Operator: shared.FilterOpGte, Values: []string{lastStatusFrom}})
	}

	if lastStatusTo := strings.TrimSpace(values.Get("lastStatusTo")); lastStatusTo != "" {
		filters = append(filters, shared.Filter{Field: "lastStatusAt", Operator: shared.FilterOpLte, Values: []string{lastStatusTo}})
	}

	return filters
}
