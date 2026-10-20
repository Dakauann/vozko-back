package tiktok

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	ttdomain "vozko/domain/tiktok"
)

const (
	CodeNoPermission       = 40001
	CodeInvalidParameter   = 40002
	CodeNotFound           = 40007
	CodeBudgetBlocked      = 40064
	CodeRateLimited        = 40100
	CodeInvalidToken       = 40105
	CodeUnsupportedFile    = 40908
	CodeSystemError        = 51065
	CodeTransport          = -1
	CodeDecode             = -2
	CodeResponseTooLarge   = -3
	CodeLimiterUnavailable = -4
	CodeLocalRateLimited   = -5
)

type Error struct {
	HTTPStatus int
	Code       int
	Message    string
	RequestID  string
	Ambiguous  bool
	Err        error
	retryAfter time.Duration
}

func (e *Error) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "tiktok: code %d", e.Code)
	if e.HTTPStatus != 0 {
		fmt.Fprintf(&b, " http %d", e.HTTPStatus)
	}
	if e.Message != "" {
		b.WriteString(": " + e.Message)
	}
	if e.RequestID != "" {
		b.WriteString(" (request " + e.RequestID + ")")
	}
	if e.Err != nil {
		b.WriteString(": " + e.Err.Error())
	}
	return b.String()
}

func (e *Error) Unwrap() error { return e.Err }

func asError(err error) (*Error, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}

func hasCode(err error, codes ...int) bool {
	e, ok := asError(err)
	if !ok {
		return false
	}
	for _, c := range codes {
		if e.Code == c {
			return true
		}
	}
	return false
}

func IsAuth(err error) bool {
	if hasCode(err, CodeInvalidToken) {
		return true
	}
	e, ok := asError(err)
	return ok && e.HTTPStatus == http.StatusUnauthorized
}

func IsPermission(err error) bool {
	if hasCode(err, CodeNoPermission) {
		return true
	}
	e, ok := asError(err)
	return ok && e.HTTPStatus == http.StatusForbidden
}

func IsRateLimited(err error) bool {
	if hasCode(err, CodeRateLimited, CodeLocalRateLimited) {
		return true
	}
	e, ok := asError(err)
	return ok && e.HTTPStatus == http.StatusTooManyRequests
}

func IsBudgetBlocked(err error) bool { return hasCode(err, CodeBudgetBlocked) }

func IsNotFound(err error) bool { return hasCode(err, CodeNotFound) }

func IsInvalidRequest(err error) bool { return hasCode(err, CodeInvalidParameter, CodeUnsupportedFile) }

func IsAmbiguous(err error) bool {
	e, ok := asError(err)
	return ok && e.Ambiguous
}

func IsRetryable(err error) bool {
	e, ok := asError(err)
	if !ok {
		return false
	}
	switch e.Code {
	case CodeRateLimited, CodeLocalRateLimited, CodeLimiterUnavailable, CodeSystemError, CodeTransport:
		return !e.Ambiguous
	}
	return e.HTTPStatus >= 500 && !e.Ambiguous
}

func IsDefiniteRefusal(err error) bool {
	e, ok := asError(err)
	if !ok || e.Ambiguous {
		return false
	}
	switch e.Code {
	case CodeLimiterUnavailable, CodeLocalRateLimited, CodeRateLimited,
		CodeNoPermission, CodeInvalidParameter, CodeNotFound, CodeBudgetBlocked, CodeInvalidToken, CodeUnsupportedFile:
		return true
	}
	return e.HTTPStatus >= 400 && e.HTTPStatus < 500
}

type Classifier struct{}

var _ ttdomain.ErrorClassifier = Classifier{}

func (Classifier) IsAuth(err error) bool            { return IsAuth(err) }
func (Classifier) IsPermission(err error) bool      { return IsPermission(err) }
func (Classifier) IsRateLimited(err error) bool     { return IsRateLimited(err) }
func (Classifier) IsBudgetBlocked(err error) bool   { return IsBudgetBlocked(err) }
func (Classifier) IsNotFound(err error) bool        { return IsNotFound(err) }
func (Classifier) IsInvalidRequest(err error) bool  { return IsInvalidRequest(err) }
func (Classifier) IsEligibility(err error) bool     { return IsPermission(err) }
func (Classifier) IsRetryable(err error) bool       { return IsRetryable(err) }
func (Classifier) IsAmbiguous(err error) bool       { return IsAmbiguous(err) }
func (Classifier) IsDefiniteRefusal(err error) bool { return IsDefiniteRefusal(err) }
