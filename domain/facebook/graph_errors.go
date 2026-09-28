package facebook

import "errors"

type Failure string

const (
	FailureUnknown           Failure = ""
	FailureReauth            Failure = "reauth"
	FailureRoleLost          Failure = "role_lost"
	FailureRetryable         Failure = "retryable"
	FailureThreadControl     Failure = "thread_control"
	FailureWindowClosed      Failure = "window_closed"
	FailureUnreachable       Failure = "recipient_unreachable"
	FailureAccessLevel       Failure = "access_level"
	FailurePageRestricted    Failure = "page_restricted"
	FailurePrivateReplyUsed  Failure = "private_reply_used"
	FailureDuplicatePost     Failure = "duplicate_post"
	FailurePolicyBlock       Failure = "policy_block"
	FailureRejectedPermanent Failure = "rejected"
)

type graphError interface {
	error
	NeedsReauth() bool
	Retryable() bool
	IsRoleLost() bool
	IsThreadControl() bool
	IsWindowClosed() bool
	IsRecipientUnreachable() bool
	IsAccessLevel() bool
	IsPageRestricted() bool
	IsPrivateReplyUsed() bool
	IsDuplicatePost() bool
	IsPolicyBlock() bool
}

func Classify(err error) Failure {
	if err == nil {
		return FailureUnknown
	}
	var ge graphError
	if !errors.As(err, &ge) {
		return FailureUnknown
	}
	switch {
	case ge.IsRoleLost():
		return FailureRoleLost
	case ge.NeedsReauth():
		return FailureReauth
	case ge.IsThreadControl():
		return FailureThreadControl
	case ge.IsWindowClosed():
		return FailureWindowClosed
	case ge.IsRecipientUnreachable():
		return FailureUnreachable
	case ge.IsAccessLevel():
		return FailureAccessLevel
	case ge.IsPageRestricted():
		return FailurePageRestricted
	case ge.IsPrivateReplyUsed():
		return FailurePrivateReplyUsed
	case ge.IsDuplicatePost():
		return FailureDuplicatePost
	case ge.IsPolicyBlock():
		return FailurePolicyBlock
	case ge.Retryable():
		return FailureRetryable
	}
	return FailureRejectedPermanent
}

type codedError interface {
	error
	ErrorCode() (code, subcode int)
}

func ErrorCode(err error) int {
	var ce codedError
	if errors.As(err, &ce) {
		code, _ := ce.ErrorCode()
		return code
	}
	return 0
}

const (
	CodeNoProfile         = 2018218
	CodeProfilePermission = 2018247
)

func HasCode(err error, want int) bool {
	var ce codedError
	if !errors.As(err, &ce) {
		return false
	}
	code, subcode := ce.ErrorCode()
	return code == want || subcode == want
}

func PageStatusAfter(f Failure) (Status, bool) {
	switch f {
	case FailureReauth:
		return StatusTokenRevoked, true
	case FailureRoleLost:
		return StatusNeedsRole, true
	case FailurePageRestricted:
		return StatusRestricted, true
	}
	return "", false
}

const CodePermissionDenied = 10

func ErrorCodes(err error) (code, subcode int) {
	var ce codedError
	if errors.As(err, &ce) {
		return ce.ErrorCode()
	}
	return 0, 0
}
