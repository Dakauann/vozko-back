package whatsapp_outreach

import (
	"errors"
	"fmt"
	"testing"

	"vozko/domain/balance"
	"vozko/domain/whatsapp/template"
)

func TestErrorCodeNamesEveryRefusalOfATemplateSend(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{ErrWindowAlreadyOpen, CodeWindowAlreadyOpen},
		{template.ErrSendInProgress, CodeSendInProgress},
		{ErrWithinSpamWindow, CodeWithinSpamWindow},
		{ErrLeadOptedOut, CodeLeadOptedOut},
		{ErrLeadBlocked, CodeLeadBlocked},
		{balance.ErrInsufficientBalance, CodeInsufficientBalance},
		{balance.ErrBalanceNotFound, CodeInsufficientBalance},
		{balance.ErrMonthlySendCapReached, CodeMonthlySendCapReached},
		{balance.ErrSendWindowClosed, CodeSendWindowClosed},
		{template.ErrPricingUnavailable, CodePricingUnavailable},
		{template.ErrQuoteOutOfRange, CodeQuoteOutOfRange},
		{template.ErrTemplateNotSendable, CodeTemplateNotSendable},
		{template.ErrTemplatePhoneMismatch, CodeTemplatePhoneMismatch},
		{ErrInvalidPhone, CodeInvalidPhone},
		{ErrDepartmentForbidden, CodeForbidden},
		{ErrTemplateForbidden, CodeForbidden},
		{ErrPhoneNotConnected, CodePhoneNotConnected},
		{ErrBusinessPhoneNotFound, CodeNotFound},
		{ErrTemplateNotFound, CodeNotFound},
		{template.ErrIdempotencyKeyRequired, CodeIdempotencyKeyRequired},
		{template.ErrWorkspaceRequired, CodeBillingUnavailable},
		{template.ErrBillingNotConfigured, CodeBillingUnavailable},
		{ErrSendOutcomeUnknown, CodeSendOutcomeUnknown},
		{fmt.Errorf("send: %w", ErrSendOutcomeUnknown), CodeSendOutcomeUnknown},
		{fmt.Errorf("%w: %w", ErrSendOutcomeUnknown, template.ErrSendInProgress), CodeSendOutcomeUnknown},
		{ErrConversationNotFound, CodeConversationNotFound},
		{fmt.Errorf("send: %w", ErrLeadOptedOut), CodeLeadOptedOut},
		{errors.New("meta answered 500"), ""},
		{nil, ""},
	}
	for _, tc := range cases {
		if got := ErrorCode(tc.err); got != tc.want {
			t.Fatalf("ErrorCode(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}
