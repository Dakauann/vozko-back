package tracepayload

import (
	"encoding/json"
	"unicode/utf8"
)

const (
	MaxBytes     = 64 * 1024
	TruncatedKey = "truncated"
	Marker       = "…[truncated]"
)

const previewOverheadBytes = 256

func Map(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}
	encoded, err := json.Marshal(m)
	if err != nil {
		return map[string]interface{}{TruncatedKey: true, "error": "payload is not serialisable"}
	}
	if len(encoded) <= MaxBytes {
		return m
	}
	budget := MaxBytes/2 - previewOverheadBytes
	for {
		preview, _ := Text(string(encoded), budget)
		capped := map[string]interface{}{
			TruncatedKey: true,
			"size_bytes": len(encoded),
			"preview":    preview,
		}
		if fits(capped) || budget <= previewOverheadBytes {
			return capped
		}
		budget /= 2
	}
}

func fits(v interface{}) bool {
	encoded, err := json.Marshal(v)
	return err == nil && len(encoded) <= MaxBytes
}

func Text(s string, limit int) (string, bool) {
	if limit <= 0 {
		limit = MaxBytes
	}
	if len(s) <= limit {
		return s, false
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut], true
}

func MarkedText(s string) string {
	return MarkedTextWithin(s, MaxBytes)
}

func MarkedTextWithin(s string, limit int) string {
	if limit <= 0 {
		limit = MaxBytes
	}
	if len(s) <= limit {
		return s
	}
	cut, _ := Text(s, limit-len(Marker))
	return cut + Marker
}
