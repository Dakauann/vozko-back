package emailsending

import (
	"errors"
	"time"
)

var (
	ErrInvalidTenantName     = errors.New("emailsending: invalid tenant name")
	ErrAccountNotProvisioned = errors.New("emailsending: sender account not provisioned")
	ErrAccountPaused         = errors.New("emailsending: sender account paused")
	ErrAccountDormant        = errors.New("emailsending: sender account dormant until a send reactivates it")
	ErrAccountReactivation   = errors.New("emailsending: the dormant sender account could not be reactivated")
	ErrInvalidLocalPart      = errors.New("emailsending: invalid local part")
	ErrInvalidDisplayName    = errors.New("emailsending: invalid display name")
	ErrInvalidForward        = errors.New("emailsending: invalid forward address")
	ErrDomainNotVerified     = errors.New("emailsending: domain not verified")
	ErrDomainReleasing       = errors.New("emailsending: domain is being released")
	ErrFreeMailboxFrom       = errors.New("emailsending: free mailbox domains cannot be used as from")
	ErrSenderDomainMismatch  = errors.New("emailsending: sender does not belong to the domain")
	ErrInvalidSlug           = errors.New("emailsending: invalid subdomain slug")
	ErrSlugReserved          = errors.New("emailsending: the subdomain name is reserved")
	ErrSlugTaken             = errors.New("emailsending: the subdomain name is taken")
	ErrSubdomainLocked       = errors.New("emailsending: the subdomain cannot change after the first send")
	ErrUnknownReleaseState   = errors.New("emailsending: unknown domain release state")
	ErrInvalidSharedDomain   = errors.New("emailsending: invalid shared domain")
	ErrUnknownDomainKind     = errors.New("emailsending: unknown domain kind")
	ErrUnknownConnectMethod  = errors.New("emailsending: unknown domain connect method")
	ErrUnknownSetupFailure   = errors.New("emailsending: unknown domain setup failure")
	ErrInvalidMessage        = errors.New("emailsending: invalid outbound message")
	ErrInsecureUnsubscribe   = errors.New("emailsending: one click unsubscribe needs an https url")
	ErrDailyCapReached       = errors.New("emailsending: daily cap reached")
	ErrDomainTaken           = errors.New("emailsending: domain already connected")
	ErrSenderNotFound        = errors.New("emailsending: sender not found")
	ErrDomainNotFound        = errors.New("emailsending: domain not found")
	ErrAccountNotFound       = errors.New("emailsending: sender account not found")
)

type SendErrorKind string

const (
	SendErrorTransient     SendErrorKind = "transient"
	SendErrorPaused        SendErrorKind = "paused"
	SendErrorRejected      SendErrorKind = "rejected"
	SendErrorConfiguration SendErrorKind = "configuration"
)

type SendError struct {
	Kind       SendErrorKind
	Detail     string
	RetryAfter time.Duration
}

func (e *SendError) Error() string {
	return "emailsending: provider send failed (" + string(e.Kind) + "): " + e.Detail
}

func SendRetryAfter(err error) (time.Duration, bool) {
	var se *SendError
	if errors.As(err, &se) && se.Kind == SendErrorTransient && se.RetryAfter > 0 {
		return se.RetryAfter, true
	}
	return 0, false
}

func SendErrorKindOf(err error) (SendErrorKind, bool) {
	var se *SendError
	if errors.As(err, &se) {
		return se.Kind, true
	}
	return "", false
}
