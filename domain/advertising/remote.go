package advertising

import (
	"errors"
	"fmt"
)

type Failure string

const (
	FailureUnknown    Failure = "unknown"
	FailureReauth     Failure = "reauth"
	FailureRetryable  Failure = "retryable"
	FailurePermission Failure = "permission"
	FailureRejected   Failure = "rejected"
)

type RemoteError struct {
	Kind        Failure
	Code        int
	Subcode     int
	Message     string
	UserTitle   string
	UserMessage string
}

func (e *RemoteError) Error() string {
	return fmt.Sprintf("meta ads: %s code=%d subcode=%d: %s", e.Kind, e.Code, e.Subcode, e.Message)
}

func (e *RemoteError) Explanation() string {
	switch {
	case e.UserMessage != "":
		return e.UserMessage
	case e.UserTitle != "":
		return e.UserTitle
	}
	return e.Message
}

func Classify(err error) Failure {
	var re *RemoteError
	if errors.As(err, &re) && re.Kind != "" {
		return re.Kind
	}
	return FailureUnknown
}

func Explain(err error) string {
	var re *RemoteError
	if errors.As(err, &re) {
		return re.Explanation()
	}
	return ""
}

func FailureCode(err error) string {
	var re *RemoteError
	if errors.As(err, &re) {
		return fmt.Sprintf("meta_%d", re.Code)
	}
	return string(Classify(err))
}

type RemoteReason string

const (
	ReasonNone            RemoteReason = ""
	ReasonBudgetTooLow    RemoteReason = "budget_too_low"
	ReasonAccountReadOnly RemoteReason = "account_read_only"
	ReasonNoPaymentMethod RemoteReason = "no_payment_method"
)

const (
	subcodeBudgetTooLow         = 1885272
	subcodeNoAdAccountWriteRole = 2490585
	subcodeNoPaymentMethod      = 1359188
)

func ReasonOf(err error) RemoteReason {
	var re *RemoteError
	if !errors.As(err, &re) {
		return ReasonNone
	}
	switch re.Subcode {
	case subcodeBudgetTooLow:
		return ReasonBudgetTooLow
	case subcodeNoAdAccountWriteRole:
		return ReasonAccountReadOnly
	case subcodeNoPaymentMethod:
		return ReasonNoPaymentMethod
	}
	return ReasonNone
}
