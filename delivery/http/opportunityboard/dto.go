package opportunityboard

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"

	"vozko/domain/crmfilter"
	oppboard_usecase "vozko/usecases/oppboard"
)

const (
	crmBoardDefaultPageSize = 50
	crmBoardMaxPageSize     = 200
)

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

func oppSortParams(q map[string][]string) (field, order string) {
	field = strings.TrimSpace(get(q, "sortField"))
	order = strings.TrimSpace(get(q, "sortOrder"))
	if combined := strings.TrimSpace(get(q, "sort")); combined != "" {
		parts := strings.SplitN(combined, ":", 2)
		if field == "" {
			field = strings.TrimSpace(parts[0])
		}
		if order == "" && len(parts) == 2 {
			order = strings.TrimSpace(parts[1])
		}
	}
	return field, order
}

func decodeFilterParam(raw string) (crmfilter.Filter, error) {
	var f crmfilter.Filter
	data := decodeMaybeBase64(raw)
	if len(data) == 0 {
		return f, nil
	}
	if err := json.Unmarshal(data, &f); err != nil {
		return crmfilter.Filter{}, err
	}
	return f, nil
}

func decodeOppOwnersParam(raw string) ([]oppboard_usecase.Owner, error) {
	data := decodeMaybeBase64(raw)
	if len(data) == 0 {
		return nil, nil
	}
	var owners []oppboard_usecase.Owner
	if err := json.Unmarshal(data, &owners); err != nil {
		return nil, err
	}
	return owners, nil
}

func decodeOppOptionsParam(raw string) ([]oppboard_usecase.Option, error) {
	data := decodeMaybeBase64(raw)
	if len(data) == 0 {
		return nil, nil
	}
	var options []oppboard_usecase.Option
	if err := json.Unmarshal(data, &options); err != nil {
		return nil, err
	}
	return options, nil
}

func decodeMaybeBase64(raw string) []byte {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if decoded, err := base64.StdEncoding.DecodeString(raw); err == nil {
		return decoded
	}
	if decoded, err := base64.URLEncoding.DecodeString(raw); err == nil {
		return decoded
	}
	return []byte(raw)
}
