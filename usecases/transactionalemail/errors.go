package transactionalemail

import (
	"errors"
	"fmt"
	"math"

	"vozko/domain/balance"
	"vozko/domain/emaildesign"
	"vozko/domain/emailhygiene"
	es "vozko/domain/emailsending"
)

const (
	CodeUnavailable            = "email_unavailable"
	CodeRequestInvalid         = "email_request_invalid"
	CodeMessageInvalid         = "email_message_invalid"
	CodeSubjectInvalid         = "email_subject_invalid"
	CodeAddressInvalid         = "email_address_invalid"
	CodeRecipientMissing       = "email_recipient_missing"
	CodeLeadNotFound           = "email_lead_not_found"
	CodeLeadBlocked            = "email_lead_blocked"
	CodeLeadOptedOut           = "email_lead_opted_out"
	CodeLeadNoEmail            = "email_lead_no_email"
	CodeDesignNotFound         = "email_design_not_found"
	CodeBalanceInsufficient    = "email_balance_insufficient"
	CodeChargeFailed           = "email_charge_failed"
	CodeRecipientSuppressed    = "email_recipient_suppressed"
	CodeSenderMissing          = "email_sender_missing"
	CodeSenderNotVerified      = "email_sender_not_verified"
	CodeAccountPaused          = "email_account_paused"
	CodeAccountReactivation    = "email_account_reactivation_failed"
	CodeDailyCapReached        = "email_daily_cap_reached"
	CodeSendFailed             = "email_send_failed"
	CodeSendInProgress         = "email_send_in_progress"
	CodeSendThrottled          = "email_send_throttled"
	CodeIdempotencyKeyRequired = "email_idempotency_key_required"
)

var (
	ErrIncomplete             = errors.New("transactionalemail: a required dependency is missing")
	ErrUnavailable            = errors.New("transactionalemail: platform email is not available in this context")
	ErrWorkspaceRequired      = errors.New("transactionalemail: the workspace is required")
	ErrIdempotencyKeyRequired = errors.New("transactionalemail: an idempotency key is required")
	ErrOriginInvalid          = errors.New("transactionalemail: unknown origin")
	ErrBodyInvalid            = errors.New("transactionalemail: the body needs exactly one of text, design or document")
	ErrRecipientMissing       = errors.New("transactionalemail: no recipient address")
	ErrLeadNotFound           = errors.New("transactionalemail: the lead was not found")
	ErrLeadBlocked            = errors.New("transactionalemail: the lead is blocked")
	ErrLeadOptedOut           = errors.New("transactionalemail: the lead asked not to be contacted")
	ErrLeadNoEmail            = errors.New("transactionalemail: the lead has no email address")
	ErrSentByInvalid          = errors.New("transactionalemail: the email must be sent by a person, an agent or a workflow of the business")
	ErrSendInProgress         = errors.New("transactionalemail: another call holds the charge of this send; try again later")
)

type Refusal struct {
	Code string
	Err  error
}

func (r *Refusal) Error() string {
	return r.Code + ": " + r.Err.Error()
}

func (r *Refusal) Unwrap() error {
	return r.Err
}

func refuse(code string, err error) error {
	return &Refusal{Code: code, Err: err}
}

func CodeOf(err error) string {
	var refusal *Refusal
	if errors.As(err, &refusal) {
		return refusal.Code
	}
	for _, programming := range []error{ErrWorkspaceRequired, ErrIdempotencyKeyRequired, ErrOriginInvalid, ErrSentByInvalid} {
		if errors.Is(err, programming) {
			return CodeRequestInvalid
		}
	}
	return CodeSendFailed
}

var sendCodes = []struct {
	targets []error
	code    string
}{
	{[]error{es.ErrRecipientSuppressed}, CodeRecipientSuppressed},
	{[]error{es.ErrNoDefaultSender, es.ErrSenderNotFound, es.ErrAccountNotProvisioned, es.ErrAccountNotFound, es.ErrDomainNotFound}, CodeSenderMissing},
	{[]error{es.ErrDomainNotVerified, es.ErrSenderDomainMismatch, es.ErrFreeMailboxFrom}, CodeSenderNotVerified},
	{[]error{es.ErrAccountPaused}, CodeAccountPaused},
	{[]error{es.ErrAccountReactivation}, CodeAccountReactivation},
	{[]error{es.ErrDailyCapReached}, CodeDailyCapReached},
	{[]error{es.ErrInvalidMessage, emaildesign.ErrInvalidDocument, emaildesign.ErrInvalidSubject}, CodeMessageInvalid},
}

func Retryable(err error) bool {
	var refusal *Refusal
	if !errors.As(err, &refusal) {
		return false
	}
	return refusal.Code == CodeSendThrottled || refusal.Code == CodeSendInProgress || emailhygiene.RetryableRefusal(refusal.Err)
}

func RetryAfterSeconds(err error) (int64, bool) {
	if CodeOf(err) != CodeSendThrottled {
		return 0, false
	}
	wait, ok := es.SendRetryAfter(err)
	if !ok {
		return 0, false
	}
	return int64(math.Ceil(wait.Seconds())), true
}

func RetryHint(err error) string {
	if !Retryable(err) {
		return ""
	}
	if seconds, ok := RetryAfterSeconds(err); ok {
		return fmt.Sprintf("; é temporário e nada foi cobrado, tente de novo em %d segundos", seconds)
	}
	return "; é temporário e nada foi cobrado, tente de novo em instantes"
}

func SendRefusal(err error) error { return sendRefusal(err) }

func ChargeRefusal(err error) error { return chargeRefusal(err) }

func Refuse(code string, err error) error { return refuse(code, err) }

func sendRefusal(err error) error {
	if code, refused := emailhygiene.RecipientRefusalCode(err); refused {
		return refuse(code, err)
	}
	if _, ok := es.SendRetryAfter(err); ok {
		return refuse(CodeSendThrottled, err)
	}
	if kind, ok := es.SendErrorKindOf(err); ok && kind == es.SendErrorPaused {
		return refuse(CodeAccountPaused, err)
	}
	for _, entry := range sendCodes {
		for _, target := range entry.targets {
			if errors.Is(err, target) {
				return refuse(entry.code, err)
			}
		}
	}
	return refuse(CodeSendFailed, err)
}

func chargeRefusal(err error) error {
	if errors.Is(err, balance.ErrInsufficientBalance) {
		return refuse(CodeBalanceInsufficient, err)
	}
	return refuse(CodeChargeFailed, err)
}
