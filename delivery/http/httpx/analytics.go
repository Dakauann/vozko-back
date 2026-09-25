package httpx

import (
	"context"
	"errors"
	"net/http"

	"vozko/delivery/http/response"
	"vozko/domain/cache"
)

const analyticsRetryAfterSeconds = "5"

func WriteAnalyticsLimit(w http.ResponseWriter, err error, subject string) bool {
	switch {
	case errors.Is(err, cache.ErrGateBusy):
		w.Header().Set("Retry-After", analyticsRetryAfterSeconds)
		response.WriteError(w, http.StatusServiceUnavailable, subject+" analytics are busy, retry shortly", nil)
		return true
	case errors.Is(err, context.DeadlineExceeded):
		response.WriteError(w, http.StatusGatewayTimeout, subject+" analytics took too long", nil)
		return true
	}
	return false
}
