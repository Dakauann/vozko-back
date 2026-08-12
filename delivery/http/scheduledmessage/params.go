package scheduledmessage

import (
	"net/http"
	"strconv"
	"strings"

	sm "vozko/domain/scheduled_message"
)

const (
	defaultPageSize = 50
	maxPageSize     = 200
)

// parseStatuses reads the comma-separated status filter.
//
// Unknown values are dropped rather than rejected: a client sending a status
// this build does not know should get the statuses it asked for that do exist,
// not a 400 for the whole request.
func parseStatuses(raw string) []sm.Status {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	known := map[sm.Status]bool{
		sm.StatusPending:  true,
		sm.StatusSending:  true,
		sm.StatusSent:     true,
		sm.StatusFailed:   true,
		sm.StatusCanceled: true,
	}

	out := []sm.Status{}
	for _, part := range strings.Split(raw, ",") {
		status := sm.Status(strings.TrimSpace(part))
		if known[status] {
			out = append(out, status)
		}
	}
	return out
}

func parsePagination(r *http.Request) (page, pageSize int) {
	page = 1
	pageSize = defaultPageSize

	if v, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && v > 0 {
		page = v
	}
	if v, err := strconv.Atoi(r.URL.Query().Get("page_size")); err == nil && v > 0 {
		pageSize = v
		if pageSize > maxPageSize {
			pageSize = maxPageSize
		}
	}
	return page, pageSize
}
