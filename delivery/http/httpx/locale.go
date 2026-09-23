package httpx

import (
	"net/http"
	"strings"
)

var supportedLocales = map[string]bool{"pt": true, "en": true, "es": true, "de": true}

func RequestLocale(r *http.Request) string {
	if locale, ok := normalizeLocale(r.URL.Query().Get("locale")); ok {
		return locale
	}
	for _, part := range strings.Split(r.Header.Get("Accept-Language"), ",") {
		candidate := part
		if index := strings.Index(candidate, ";"); index >= 0 {
			candidate = candidate[:index]
		}
		if locale, ok := normalizeLocale(candidate); ok {
			return locale
		}
	}
	return ""
}

func normalizeLocale(raw string) (string, bool) {
	trimmed := strings.ToLower(strings.TrimSpace(raw))
	if trimmed == "" {
		return "", false
	}
	if index := strings.IndexAny(trimmed, "-_"); index > 0 {
		trimmed = trimmed[:index]
	}
	if !supportedLocales[trimmed] {
		return "", false
	}
	return trimmed, true
}
