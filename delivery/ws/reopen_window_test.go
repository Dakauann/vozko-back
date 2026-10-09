package ws

import (
	"errors"
	"fmt"
	"testing"

	"vozko/domain/balance"
	wo "vozko/domain/whatsapp_outreach"
)

func TestAReopenWindowRefusalCarriesItsReason(t *testing.T) {
	cases := []struct {
		err  error
		code string
	}{
		{fmt.Errorf("send: %w", wo.ErrLeadOptedOut), wo.CodeLeadOptedOut},
		{wo.ErrLeadBlocked, wo.CodeLeadBlocked},
		{balance.ErrInsufficientBalance, wo.CodeInsufficientBalance},
		{wo.ErrWithinSpamWindow, wo.CodeWithinSpamWindow},
		{fmt.Errorf("send: %w", wo.ErrSendOutcomeUnknown), wo.CodeSendOutcomeUnknown},
		{wo.ErrConversationNotFound, wo.CodeConversationNotFound},
		{errors.New("meta answered 500"), "template_send_failed"},
	}
	for _, tc := range cases {
		got := reopenWindowRefusal("r-1", "e-1", "whatsapp", tc.err)
		if got.Code != tc.code || got.RequestID != "r-1" || got.EntryID != "e-1" || got.EntryType != "whatsapp" || got.Message == "" {
			t.Fatalf("%v: payload %+v, want code %s", tc.err, got, tc.code)
		}
	}
}

func TestAReopenWindowRefusalNeverEchoesTheProviderError(t *testing.T) {
	got := reopenWindowRefusal("r-1", "e-1", "whatsapp", errors.New("token EAAB secret expired"))
	if got.Message != "Failed to send template" {
		t.Fatalf("message = %q", got.Message)
	}
}

func TestAReopenWindowWithAnUnknownOutcomeTellsTheOperatorToCheckBeforeResending(t *testing.T) {
	got := reopenWindowRefusal("r-1", "e-1", "whatsapp", wo.ErrSendOutcomeUnknown)
	if got.Message != reopenWindowOutcomeUnknownMessage {
		t.Fatalf("message = %q", got.Message)
	}
}
