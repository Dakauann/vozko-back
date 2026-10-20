package mercadolivre

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	mldomain "vozko/domain/mercadolivre"
)

const (
	CodeLimiterUnavailable = "limiter_unavailable"
	CodeLocalRateLimited   = "local_rate_limited_by_vozko"
	CodeTransport          = "transport"
	CodeDecode             = "decode"
	CodeResponseTooLarge   = "response_too_large"
	CodePermissionPolicy   = "PA_UNAUTHORIZED_RESULT_FROM_POLICIES"
	CodeInvalidGrant       = "invalid_grant"
	CodeInvalidToken       = "invalid_token"
	CodeUnauthorizedClient = "unauthorized_client"
	CodeOperatorUser       = "invalid_operator_user_id"
	CodeInvalidClient      = "invalid_client"
	CodeUnauthorizedApp    = "unauthorized_application"
	CodeUnauthorizedScopes = "unauthorized_scopes"
)

type Error struct {
	HTTPStatus       int
	Code             string
	Message          string
	Cause            string
	Blocked          bool
	BlockedSubstatus string
	Ambiguous        bool
	Err              error

	retryAfter time.Duration
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	parts := []string{"mercadolivre"}
	if e.HTTPStatus != 0 {
		parts = append(parts, fmt.Sprintf("http %d", e.HTTPStatus))
	}
	if e.Code != "" {
		parts = append(parts, e.Code)
	}
	if e.Message != "" {
		parts = append(parts, e.Message)
	}
	if e.Err != nil {
		parts = append(parts, e.Err.Error())
	}
	return strings.Join(parts, ": ")
}

func (e *Error) Unwrap() error { return e.Err }

func asError(err error) (*Error, bool) {
	var e *Error
	if errors.As(err, &e) && e != nil {
		return e, true
	}
	return nil, false
}

func (e *Error) mentions(token string) bool {
	return strings.Contains(e.Code, token) || strings.Contains(e.Message, token) || strings.Contains(e.Cause, token)
}

func IsAuth(err error) bool {
	e, ok := asError(err)
	if !ok || IsAppBlocked(err) || e.Code == CodeOperatorUser {
		return false
	}
	switch e.Code {
	case CodeInvalidGrant, CodeInvalidToken, CodeUnauthorizedClient:
		return true
	}
	return e.HTTPStatus == http.StatusUnauthorized
}

func IsOperatorUser(err error) bool {
	e, ok := asError(err)
	return ok && e.mentions(CodeOperatorUser)
}

func IsAppBlocked(err error) bool {
	e, ok := asError(err)
	if !ok {
		return false
	}
	switch e.Code {
	case CodeInvalidClient, CodeUnauthorizedApp, CodeUnauthorizedScopes:
		return true
	}
	return false
}

func IsPermission(err error) bool {
	e, ok := asError(err)
	return ok && e.HTTPStatus == http.StatusForbidden && e.mentions(CodePermissionPolicy)
}

func IsRateLimit(err error) bool {
	e, ok := asError(err)
	return ok && (e.HTTPStatus == http.StatusTooManyRequests || e.Code == CodeLocalRateLimited)
}

func IsBlocked(err error) (substatus string, blocked bool) {
	e, ok := asError(err)
	if !ok || !e.Blocked {
		return "", false
	}
	return e.BlockedSubstatus, true
}

func IsModerationPending(err error) bool {
	e, ok := asError(err)
	return ok && e.HTTPStatus == http.StatusAccepted
}

func IsNotFound(err error) bool {
	e, ok := asError(err)
	return ok && e.HTTPStatus == http.StatusNotFound
}

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
	case CodeLimiterUnavailable, CodeLocalRateLimited, CodeTransport:
		return true
	}
	return e.HTTPStatus == http.StatusTooManyRequests || e.HTTPStatus >= 500
}

type errorBody struct {
	Message          string          `json:"message"`
	Error            string          `json:"error"`
	Code             string          `json:"code"`
	ErrorDescription string          `json:"error_description"`
	Cause            json.RawMessage `json:"cause"`
}

func parseError(status int, raw []byte) error {
	e := &Error{HTTPStatus: status}
	var body errorBody
	if err := json.Unmarshal(raw, &body); err == nil {
		e.Code = strings.TrimSpace(body.Error)
		if e.Code == "" {
			e.Code = strings.TrimSpace(body.Code)
		}
		e.Message = strings.TrimSpace(body.Message)
		if e.Message == "" {
			e.Message = strings.TrimSpace(body.ErrorDescription)
		}
		e.Cause = flattenCause(body.Cause)
	} else {
		e.Message = truncate(strings.TrimSpace(string(raw)), 300)
	}
	classifyBlock(e)
	return e
}

func flattenCause(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var single string
	if json.Unmarshal(raw, &single) == nil {
		return strings.TrimSpace(single)
	}
	var list []json.RawMessage
	if json.Unmarshal(raw, &list) != nil {
		return ""
	}
	parts := make([]string, 0, len(list))
	for _, item := range list {
		var s string
		if json.Unmarshal(item, &s) == nil {
			parts = append(parts, s)
			continue
		}
		var obj struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		if json.Unmarshal(item, &obj) == nil {
			parts = append(parts, strings.TrimSpace(strings.Join([]string{obj.Code, obj.Message}, " ")))
		}
	}
	return strings.Join(parts, ", ")
}

var substatusToken = regexp.MustCompile(`block(?:ed)?_by_[a-z0-9_]+`)

type blockPhrase struct {
	phrase    string
	substatus mldomain.Substatus
}

var documentedBlockPhrases = []blockPhrase{
	{"a mediation is in process", mldomain.SubstatusBlockedByMediation},
	{"has not been yet delivered", mldomain.SubstatusBlockedByFulfillment},
	{"cancelled", mldomain.SubstatusBlockedByCancelledOrder},
}

func classifyBlock(e *Error) {
	if e.HTTPStatus == http.StatusUnavailableForLegalReasons {
		e.Blocked = true
		e.BlockedSubstatus = string(mldomain.SubstatusBlockedByDeactivatedAccount)
		return
	}
	if e.HTTPStatus != http.StatusForbidden || e.mentions(CodePermissionPolicy) {
		return
	}
	text := strings.ToLower(e.Code + " " + e.Message + " " + e.Cause)
	for _, token := range substatusToken.FindAllString(text, -1) {
		if mldomain.KnownSubstatus(token) {
			e.Blocked = true
			e.BlockedSubstatus = token
			return
		}
	}
	for _, p := range documentedBlockPhrases {
		if strings.Contains(text, p.phrase) {
			e.Blocked = true
			e.BlockedSubstatus = string(p.substatus)
			return
		}
	}
	if strings.Contains(text, "blocked") || strings.Contains(text, "bloquead") {
		e.Blocked = true
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
