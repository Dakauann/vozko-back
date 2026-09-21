package meta

import (
	"errors"
	"fmt"
	"net/http"
)

type Error struct {
	HTTPStatus  int    `json:"-"`
	Code        int    `json:"code"`
	Subcode     int    `json:"error_subcode"`
	Type        string `json:"type"`
	Message     string `json:"message"`
	FBTraceID   string `json:"fbtrace_id"`
	IsTransient bool   `json:"is_transient"`
	UserTitle   string `json:"error_user_title"`
	UserMsg     string `json:"error_user_msg"`
}

func (e *Error) Error() string {
	if e == nil {
		return "meta: <nil>"
	}
	msg := e.Message
	if msg == "" {
		msg = e.UserMsg
	}
	return fmt.Sprintf("meta: http=%d code=%d subcode=%d type=%s trace=%s: %s",
		e.HTTPStatus, e.Code, e.Subcode, e.Type, e.FBTraceID, msg)
}

const (
	CodeUnknown           = 1
	CodeAPIService        = 2
	CodeRateLimit         = 4
	CodePermission        = 10
	CodeUserRateLimit     = 17
	CodeInvalidParam      = 100
	CodeSessionExpired    = 102
	CodeAppRateLimit      = 32
	CodeAccessTokenError  = 190
	CodePersonUnavailable = 551
	CodeMessagingRate     = 613
	CodeWindowClosed      = 1545041
)

func (e *Error) Retryable() bool {
	if e == nil {
		return false
	}
	if e.IsTransient {
		return true
	}
	switch e.Code {
	case CodeUnknown, CodeAPIService, CodeRateLimit, CodeUserRateLimit,
		CodeAppRateLimit, CodeMessagingRate:
		return true
	}
	if e.HTTPStatus >= 500 || e.HTTPStatus == http.StatusTooManyRequests {
		return true
	}
	return false
}

func (e *Error) NeedsReauth() bool {
	if e == nil {
		return false
	}
	switch e.Code {
	case CodeAccessTokenError, CodeSessionExpired:
		return true
	}
	return false
}

func (e *Error) IsRateLimit() bool {
	if e == nil {
		return false
	}
	switch e.Code {
	case CodeRateLimit, CodeUserRateLimit, CodeAppRateLimit, CodeMessagingRate:
		return true
	}
	return e.HTTPStatus == http.StatusTooManyRequests
}

func (e *Error) IsWindowClosed() bool {
	return e != nil && e.Code == CodeWindowClosed
}

func (e *Error) IsRecipientUnreachable() bool {
	return e != nil && e.Code == CodePersonUnavailable
}

func (e *Error) IsPermission() bool {
	return e != nil && (e.Code == CodePermission || e.Code == CodeInvalidParam && e.Subcode == 33)
}

func AsError(err error) (*Error, bool) {
	var me *Error
	if errors.As(err, &me) {
		return me, true
	}
	return nil, false
}

func IsRetryable(err error) bool {
	if me, ok := AsError(err); ok {
		return me.Retryable()
	}
	var re *RequestError
	return errors.As(err, &re)
}

func IsReauthRequired(err error) bool {
	me, ok := AsError(err)
	return ok && me.NeedsReauth()
}

type RequestError struct {
	Op  string
	Err error
}

func (e *RequestError) Error() string { return fmt.Sprintf("meta: %s: %v", e.Op, e.Err) }
func (e *RequestError) Unwrap() error { return e.Err }

type errorBody struct {
	Error Error `json:"error"`
}
