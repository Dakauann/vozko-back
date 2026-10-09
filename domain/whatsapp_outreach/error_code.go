package whatsapp_outreach

import (
	"errors"

	"vozko/domain/balance"
	"vozko/domain/whatsapp/template"
)

const (
	CodeWindowAlreadyOpen      = "window_already_open"
	CodeSendInProgress         = "send_in_progress"
	CodeWithinSpamWindow       = "within_spam_window"
	CodeLeadOptedOut           = "lead_opted_out"
	CodeLeadBlocked            = "lead_blocked"
	CodeInsufficientBalance    = "insufficient_balance"
	CodeMonthlySendCapReached  = "monthly_send_cap_reached"
	CodeSendWindowClosed       = "send_window_closed"
	CodePricingUnavailable     = "pricing_unavailable"
	CodeQuoteOutOfRange        = "quote_out_of_range"
	CodeTemplateNotSendable    = "template_not_sendable"
	CodeTemplatePhoneMismatch  = "template_phone_mismatch"
	CodeInvalidPhone           = "invalid_phone"
	CodeForbidden              = "forbidden"
	CodePhoneNotConnected      = "phone_not_connected"
	CodeNotFound               = "not_found"
	CodeIdempotencyKeyRequired = "idempotency_key_required"
	CodeBillingUnavailable     = "billing_unavailable"
	CodeSendOutcomeUnknown     = "send_outcome_unknown"
	CodeConversationNotFound   = "conversation_not_found"
)

var errorCodes = []struct {
	err  error
	code string
}{
	{ErrSendOutcomeUnknown, CodeSendOutcomeUnknown},
	{ErrWindowAlreadyOpen, CodeWindowAlreadyOpen},
	{template.ErrSendInProgress, CodeSendInProgress},
	{ErrWithinSpamWindow, CodeWithinSpamWindow},
	{ErrLeadOptedOut, CodeLeadOptedOut},
	{balance.ErrInsufficientBalance, CodeInsufficientBalance},
	{balance.ErrBalanceNotFound, CodeInsufficientBalance},
	{balance.ErrMonthlySendCapReached, CodeMonthlySendCapReached},
	{balance.ErrSendWindowClosed, CodeSendWindowClosed},
	{template.ErrPricingUnavailable, CodePricingUnavailable},
	{template.ErrQuoteOutOfRange, CodeQuoteOutOfRange},
	{template.ErrTemplateNotSendable, CodeTemplateNotSendable},
	{template.ErrTemplatePhoneMismatch, CodeTemplatePhoneMismatch},
	{ErrInvalidPhone, CodeInvalidPhone},
	{ErrLeadBlocked, CodeLeadBlocked},
	{ErrDepartmentForbidden, CodeForbidden},
	{ErrTemplateForbidden, CodeForbidden},
	{ErrPhoneNotConnected, CodePhoneNotConnected},
	{ErrBusinessPhoneNotFound, CodeNotFound},
	{ErrTemplateNotFound, CodeNotFound},
	{template.ErrIdempotencyKeyRequired, CodeIdempotencyKeyRequired},
	{template.ErrWorkspaceRequired, CodeBillingUnavailable},
	{template.ErrBillingNotConfigured, CodeBillingUnavailable},
	{ErrConversationNotFound, CodeConversationNotFound},
}

func ErrorCode(err error) string {
	if err == nil {
		return ""
	}
	for _, known := range errorCodes {
		if errors.Is(err, known.err) {
			return known.code
		}
	}
	return ""
}
