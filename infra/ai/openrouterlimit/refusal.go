package openrouterlimit

import (
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"vozko/domain/ai"
)

const (
	inFlightReason   = "in_flight_budget_exhausted"
	inFlightSource   = "openrouter_in_flight_budget"
	retryAfterHeader = "Retry-After"

	maxRetryAfterSeconds = 24 * 60 * 60
)

func Refusal(metadata map[string]any, header http.Header, cause error) error {
	reason, _ := metadata["reason"].(string)
	source, _ := metadata["limit_source"].(string)
	if reason != inFlightReason || source != inFlightSource {
		return nil
	}
	return &ai.ProviderBusyError{Reason: reason, Limit: source, RetryAfter: retryAfter(metadata, header), Err: cause}
}

func RefusalInBody(raw []byte, header http.Header, cause error) error {
	var body struct {
		Error struct {
			Metadata map[string]any `json:"metadata"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &body) != nil {
		return nil
	}
	return Refusal(body.Error.Metadata, header, cause)
}

func retryAfter(metadata map[string]any, header http.Header) time.Duration {
	if wait, ok := seconds(header.Get(retryAfterHeader)); ok {
		return wait
	}
	echoed, _ := metadata["headers"].(map[string]any)
	for name, value := range echoed {
		if !strings.EqualFold(name, retryAfterHeader) {
			continue
		}
		if wait, ok := seconds(value); ok {
			return wait
		}
	}
	return 0
}

func seconds(value any) (time.Duration, bool) {
	var n int
	switch v := value.(type) {
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return 0, false
		}
		n = parsed
	case float64:
		if v != math.Trunc(v) || v <= 0 || v > maxRetryAfterSeconds {
			return 0, false
		}
		n = int(v)
	default:
		return 0, false
	}
	if n <= 0 || n > maxRetryAfterSeconds {
		return 0, false
	}
	return time.Duration(n) * time.Second, true
}
