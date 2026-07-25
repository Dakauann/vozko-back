package httpx

import (
	"net/url"
	"strconv"
	"strings"

	"vozko/domain/shared"
)

func ParsePagination(values url.Values) shared.Pagination {
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
