package meta

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
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
	CodePageUserRateLimit = 32
	CodeInvalidParam      = 100
	CodeSessionExpired    = 102
	CodeAccessTokenError  = 190
	CodeAccessLevel       = 200
	CodePolicyBlock       = 368
	CodeDuplicatePost     = 506
	CodePersonUnavailable = 551
	CodeMessagingRate     = 613
	CodePrivateReplyUsed  = 10900
	CodePrivateReplyUser  = 10903
	CodePrivateReplyOff   = 10904
	CodePageRateLimit     = 80001
	CodeMessengerRate     = 80006
	CodeWindowClosed      = 1545041
	CodeThreadOwned       = 2018300
	CodeAutomatedQA       = 2018321
)

const (
	SubcodeRoleLost             = 492
	SubcodeOutsideWindow        = 2018278
	SubcodeOutsideWindowAlt     = 2534022
	SubcodeCannotReceive        = 2018108
	SubcodePageRestricted       = 1893063
	SubcodeMessagingNotReviewed = 2018028
	SubcodePrivateReplyInvalid  = 2534025
	SubcodeNotThreadOwner       = 2534037
	SubcodeThreadOwnedElsewhere = 2018300
	SubcodeNoMatchingUser       = 2018001
	SubcodeNoProfile            = 2018218
)

func (e *Error) Retryable() bool {
	if e == nil {
		return false
	}
	if e.IsTransient {
		return true
	}
	switch e.Code {
	case CodeUnknown, CodeAPIService:
		return true
	}
	if e.IsRateLimit() {
		return true
	}
	return e.HTTPStatus >= 500
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
	case CodeRateLimit, CodeUserRateLimit, CodePageUserRateLimit, CodeMessagingRate,
		CodePageRateLimit, CodeMessengerRate:
		return true
	}
	return e.HTTPStatus == http.StatusTooManyRequests
}

func (e *Error) IsWindowClosed() bool {
	if e == nil {
		return false
	}
	return e.Code == CodeWindowClosed ||
		e.Subcode == SubcodeOutsideWindow ||
		e.Subcode == SubcodeOutsideWindowAlt
}

func (e *Error) IsRecipientUnreachable() bool {
	if e == nil {
		return false
	}
	return e.Code == CodePersonUnavailable ||
		e.Subcode == SubcodeCannotReceive ||
		(e.Code == CodeAccessLevel && e.Subcode == CodeWindowClosed)
}

func (e *Error) IsPermission() bool {
	return e != nil && (e.Code == CodePermission || e.Code == CodeInvalidParam && e.Subcode == 33)
}

func (e *Error) IsThreadControl() bool {
	if e == nil {
		return false
	}
	return e.Code == CodeThreadOwned ||
		e.Subcode == SubcodeThreadOwnedElsewhere ||
		e.Subcode == SubcodeNotThreadOwner
}

func (e *Error) IsAccessLevel() bool {
	if e == nil || e.Code != CodeAccessLevel {
		return false
	}
	if e.Subcode == SubcodeMessagingNotReviewed {
		return true
	}
	return strings.Contains(strings.ToLower(e.Message), "admins, developers or testers")
}

func (e *Error) IsPageRestricted() bool {
	return e != nil && e.Subcode == SubcodePageRestricted
}

func (e *Error) IsPrivateReplyUsed() bool {
	if e == nil {
		return false
	}
	switch e.Code {
	case CodePrivateReplyUsed, CodePrivateReplyUser, CodePrivateReplyOff:
		return true
	}
	return e.Subcode == SubcodePrivateReplyInvalid
}

func (e *Error) IsDuplicatePost() bool {
	return e != nil && e.Code == CodeDuplicatePost
}

func (e *Error) IsRoleLost() bool {
	return e != nil && e.Code == CodeAccessTokenError && e.Subcode == SubcodeRoleLost
}

func (e *Error) IsPolicyBlock() bool {
	return e != nil && e.Code == CodePolicyBlock
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

func (e *Error) ErrorCode() (code, subcode int) {
	if e == nil {
		return 0, 0
	}
	return e.Code, e.Subcode
}
