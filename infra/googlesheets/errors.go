package googlesheets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"vozko/domain/googlesheet"
)

type APIError struct {
	Status     int
	Reason     string
	Message    string
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	return fmt.Sprintf("googlesheets: google answered %d %s: %s", e.Status, e.Reason, e.Message)
}

type TransportError struct {
	Err error
}

func (e *TransportError) Error() string { return "googlesheets: " + e.Err.Error() }

func (e *TransportError) Unwrap() error { return e.Err }

func newAPIError(res *http.Response, raw []byte) *APIError {
	var body struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Status  string `json:"status"`
			Errors  []struct {
				Reason string `json:"reason"`
			} `json:"errors"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw, &body)
	reason := body.Error.Status
	if len(body.Error.Errors) > 0 && body.Error.Errors[0].Reason != "" {
		reason = body.Error.Errors[0].Reason
	}
	out := &APIError{Status: res.StatusCode, Reason: reason, Message: body.Error.Message}
	if seconds, err := strconv.Atoi(strings.TrimSpace(res.Header.Get("Retry-After"))); err == nil && seconds > 0 {
		out.RetryAfter = time.Duration(seconds) * time.Second
	}
	return out
}

type Classifier struct{}

var _ googlesheet.ErrorClassifier = Classifier{}

func (Classifier) Classify(err error) googlesheet.Failure {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return googlesheet.Failure{Kind: googlesheet.FailureTransient, Detail: transportDetail(err)}
	}
	detail := strings.TrimSpace(apiErr.Message)
	switch {
	case apiErr.Status == http.StatusUnauthorized:
		return googlesheet.Failure{Kind: googlesheet.FailureAuth, Detail: detail}
	case apiErr.Status == http.StatusTooManyRequests || strings.Contains(strings.ToLower(apiErr.Reason), "ratelimit"):
		return googlesheet.Failure{Kind: googlesheet.FailureRateLimited, RetryAfter: apiErr.RetryAfter, Detail: detail}
	case apiErr.Status == http.StatusForbidden:
		return googlesheet.Failure{Kind: googlesheet.FailurePermission, Detail: detail}
	case apiErr.Status == http.StatusNotFound:
		return googlesheet.Failure{Kind: googlesheet.FailureNotFound, Detail: detail}
	case apiErr.Status == http.StatusBadRequest && strings.Contains(strings.ToLower(detail), "unable to parse range"):
		return googlesheet.Failure{Kind: googlesheet.FailureNotFound, Detail: "a aba da planilha foi renomeada ou removida no Google"}
	case apiErr.Status == http.StatusBadRequest:
		return googlesheet.Failure{Kind: googlesheet.FailureInvalid, Detail: detail}
	}
	return googlesheet.Failure{Kind: googlesheet.FailureTransient, Detail: detail}
}

func transportDetail(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "o Google demorou demais para responder"
	}
	if err == nil {
		return ""
	}
	return err.Error()
}
