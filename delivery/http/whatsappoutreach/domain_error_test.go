package whatsappoutreach

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vozko/domain/balance"
	"vozko/domain/whatsapp/template"
	wo "vozko/domain/whatsapp_outreach"
)

func TestWriteDomainError_MonthlySendCapReached(t *testing.T) {
	rec := httptest.NewRecorder()

	(&Handler{}).writeDomainError(rec, fmt.Errorf("charge: %w", balance.ErrMonthlySendCapReached), nil)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "monthly_send_cap_reached") {
		t.Fatalf("body %s must carry the monthly_send_cap_reached code", rec.Body.String())
	}
}

func TestWriteDomainError_OptedOutLeadAndAnUnknownRate(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{err: fmt.Errorf("start: %w", wo.ErrLeadOptedOut), status: http.StatusConflict, code: "lead_opted_out"},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		(&Handler{}).writeDomainError(rec, tc.err, nil)
		if rec.Code != tc.status || !strings.Contains(rec.Body.String(), `"`+tc.code+`"`) {
			t.Fatalf("%v: status = %d body = %s, want %d %s", tc.err, rec.Code, rec.Body.String(), tc.status, tc.code)
		}
	}
}

func TestWriteDomainError_AQuoteOutOfRangeIsUnprocessable(t *testing.T) {
	rec := httptest.NewRecorder()
	(&Handler{}).writeDomainError(rec, fmt.Errorf("quote: %w", template.ErrQuoteOutOfRange), nil)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), `"quote_out_of_range"`) {
		t.Fatalf("status = %d body = %s, want 422 quote_out_of_range", rec.Code, rec.Body.String())
	}
}

func TestWriteDomainError_AnswersTheDomainCodeOfEveryRefusal(t *testing.T) {
	refusals := []error{
		wo.ErrLeadOptedOut, wo.ErrLeadBlocked, wo.ErrWithinSpamWindow,
		wo.ErrInvalidPhone, wo.ErrDepartmentForbidden, wo.ErrTemplateForbidden, wo.ErrPhoneNotConnected,
		wo.ErrBusinessPhoneNotFound, wo.ErrTemplateNotFound, balance.ErrInsufficientBalance, balance.ErrBalanceNotFound,
		balance.ErrMonthlySendCapReached, balance.ErrSendWindowClosed, template.ErrSendInProgress, template.ErrPricingUnavailable, template.ErrQuoteOutOfRange,
		template.ErrTemplateNotSendable, template.ErrTemplatePhoneMismatch, template.ErrIdempotencyKeyRequired,
		template.ErrWorkspaceRequired, template.ErrBillingNotConfigured, wo.ErrSendOutcomeUnknown, wo.ErrConversationNotFound,
	}
	for _, err := range refusals {
		rec := httptest.NewRecorder()
		(&Handler{}).writeDomainError(rec, err, nil)
		if !strings.Contains(rec.Body.String(), `"`+wo.ErrorCode(err)+`"`) || rec.Code == http.StatusBadGateway {
			t.Fatalf("%v: status = %d body = %s, want code %s", err, rec.Code, rec.Body.String(), wo.ErrorCode(err))
		}
	}
	rec := httptest.NewRecorder()
	(&Handler{}).writeDomainError(rec, fmt.Errorf("meta answered 500"), nil)
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), `"send_failed"`) {
		t.Fatalf("an unknown failure: status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestWriteDomainError_AnUnknownOutcomeIsNeverAnsweredAsARetryableFailure(t *testing.T) {
	rec := httptest.NewRecorder()
	(&Handler{}).writeDomainError(rec, fmt.Errorf("send: %w", wo.ErrSendOutcomeUnknown), nil)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"send_outcome_unknown"`) || !strings.Contains(rec.Body.String(), "confira a conversa") {
		t.Fatalf("status = %d body = %s, want 409 send_outcome_unknown", rec.Code, rec.Body.String())
	}
}

func TestWriteDomainError_AMissingConversationIsNotFound(t *testing.T) {
	rec := httptest.NewRecorder()
	(&Handler{}).writeDomainError(rec, wo.ErrConversationNotFound, nil)
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), `"conversation_not_found"`) {
		t.Fatalf("status = %d body = %s, want 404 conversation_not_found", rec.Code, rec.Body.String())
	}
}
