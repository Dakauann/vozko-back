package crmboard

import (
	"encoding/json"
	"net/http"
	"strconv"

	"vozko/delivery/http/httpx"
	"vozko/domain/crmfilter"
	"vozko/infra/http/middleware"
	crmboard_usecase "vozko/usecases/crmboard"
)

const (
	crmBoardDefaultPageSize = 50
	crmBoardMaxPageSize     = 200
)

func selectedDepartmentID(r *http.Request) string {
	if filter := middleware.GetDepartmentFilter(r); filter != nil && filter.SelectedDepartmentID != nil {
		return *filter.SelectedDepartmentID
	}
	return ""
}

func crmBoardPagination(q map[string][]string) (page, pageSize int) {
	page = 1
	if v := get(q, "page"); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p > 0 {
			page = p
		}
	}
	pageSize = crmBoardDefaultPageSize
	if v := get(q, "pageSize"); v != "" {
		if ps, err := strconv.Atoi(v); err == nil && ps > 0 {
			pageSize = ps
		}
	}
	if pageSize > crmBoardMaxPageSize {
		pageSize = crmBoardMaxPageSize
	}
	return page, pageSize
}

func get(q map[string][]string, key string) string {
	if vs := q[key]; len(vs) > 0 {
		return vs[0]
	}
	return ""
}

// decodeFilterParam is the shared httpx decoder; kept as a named function here
// because the board handler calls it three times.
func decodeFilterParam(raw string) (crmfilter.Filter, error) {
	return httpx.DecodeFilterParam(raw)
}

func decodeOwnersParam(raw string) ([]crmboard_usecase.Owner, error) {
	data := decodeMaybeBase64(raw)
	if len(data) == 0 {
		return nil, nil
	}
	var owners []crmboard_usecase.Owner
	if err := json.Unmarshal(data, &owners); err != nil {
		return nil, err
	}
	return owners, nil
}

func decodeMaybeBase64(raw string) []byte {
	return httpx.DecodeMaybeBase64(raw)
}
