package advertisinghttp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vozko/domain/advertising"
	adsuc "vozko/usecases/advertising"
)

type errorPayload struct {
	Code     string            `json:"code"`
	Message  string            `json:"message"`
	Expected map[string]string `json:"expected"`
}

func written(t *testing.T, err error) (int, errorPayload) {
	t.Helper()
	rec := httptest.NewRecorder()
	writeError(rec, err, "fallback")
	var payload errorPayload
	if decodeErr := json.Unmarshal(rec.Body.Bytes(), &payload); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	return rec.Code, payload
}

func TestNewAdsErrorsHaveStableCodes(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{advertising.ErrRuleNotFound, http.StatusNotFound, "not_found"},
		{advertising.ErrAudienceNotFound, http.StatusNotFound, "not_found"},
		{advertising.ErrSavedAudienceNotFound, http.StatusNotFound, "not_found"},
		{advertising.ErrLeadFormNotFound, http.StatusNotFound, "not_found"},
		{advertising.ErrBusinessPhoneNotFound, http.StatusNotFound, "business_phone_not_found"},
		{advertising.ErrAudienceTermsNotAccepted, http.StatusConflict, "audience_terms_not_accepted"},
		{advertising.ErrBudgetKindLocked, http.StatusConflict, "budget_kind_locked"},
		{advertising.ErrVideoNotReady, http.StatusConflict, "video_not_ready"},
		{advertising.ErrPageNotGranted, http.StatusConflict, "page_not_granted"},
		{advertising.ErrNumberNotLinked, http.StatusConflict, "number_not_linked"},
		{advertising.ErrNumberNotOwned, http.StatusConflict, "number_not_owned"},
		{advertising.ErrAccountLinkedElsewhere, http.StatusConflict, "account_linked_elsewhere"},
		{advertising.ErrJobNotRunnable, http.StatusConflict, "job_not_runnable"},
		{advertising.ErrNothingToChange, http.StatusBadRequest, "nothing_to_change"},
		{advertising.ErrEditNotForLevel, http.StatusUnprocessableEntity, "not_for_level"},
		{advertising.ErrBreakdownCombination, http.StatusUnprocessableEntity, "invalid_breakdown"},
		{advertising.ErrNoCustomersMatched, http.StatusUnprocessableEntity, "no_customers_matched"},
		{fmt.Errorf("%w: line 3", adsuc.ErrCustomerFileUnreadable), http.StatusUnprocessableEntity, "customer_file_unreadable"},
		{fmt.Errorf("%w: spend cap must be above what was already spent", advertising.ErrInvalidBudget), http.StatusBadRequest, "invalid_budget"},
		{advertising.ErrAccountReadOnly, http.StatusConflict, "account_read_only"},
		{advertising.ErrAccountAdminRequired, http.StatusConflict, "account_admin_required"},
	}
	for _, c := range cases {
		status, payload := written(t, c.err)
		if status != c.status || payload.Code != c.code || payload.Message == "" {
			t.Errorf("%v: got %d %+v, want %d %s", c.err, status, payload, c.status, c.code)
		}
	}
}

func TestValidationErrorsListEveryField(t *testing.T) {
	status, payload := written(t, &advertising.ValidationError{Issues: []advertising.FieldIssue{
		{Field: "cells[0].share", Code: "invalid"}, {Field: "name", Code: "required"},
	}})
	if status != http.StatusUnprocessableEntity || payload.Code != "invalid_draft" ||
		payload.Expected["cells[0].share"] != "invalid" || payload.Expected["name"] != "required" {
		t.Fatalf("got %d %+v", status, payload)
	}
}

func TestBudgetMessagesNoLongerAssumeADailyBudget(t *testing.T) {
	for _, err := range []error{advertising.ErrNoBudget, advertising.ErrInvalidBudget} {
		if _, payload := written(t, err); strings.Contains(strings.ToLower(payload.Message), "diário") {
			t.Errorf("%v: %q", err, payload.Message)
		}
	}
}

func TestUnknownErrorsAreServerErrors(t *testing.T) {
	if status, _ := written(t, fmt.Errorf("boom")); status != http.StatusInternalServerError {
		t.Fatalf("got %d", status)
	}
}

func TestMetaBudgetTooLowKeepsMetasExplanation(t *testing.T) {
	err := &advertising.RemoteError{Kind: advertising.FailureRejected, Code: 100, Subcode: 1885272, UserMessage: "Seu orçamento do conjunto de anúncios deve ser superior a R$5,19"}
	status, payload := written(t, err)
	if status != http.StatusUnprocessableEntity || payload.Code != "budget_below_minimum" || payload.Message != err.UserMessage {
		t.Fatalf("got %d %+v", status, payload)
	}
}

func TestMetaWritePermissionRefusalIsReadOnly(t *testing.T) {
	status, payload := written(t, &advertising.RemoteError{Kind: advertising.FailurePermission, Code: 200, Subcode: 2490585})
	if status != http.StatusConflict || payload.Code != "account_read_only" {
		t.Fatalf("got %d %+v", status, payload)
	}
}

func TestMetaMissingPaymentMethodKeepsMetasExplanation(t *testing.T) {
	err := &advertising.RemoteError{Kind: advertising.FailureRejected, Code: 100, Subcode: 1359188, UserMessage: "Atualize a forma de pagamento"}
	status, payload := written(t, err)
	if status != http.StatusConflict || payload.Code != "no_payment_method" || payload.Message != err.UserMessage {
		t.Fatalf("got %d %+v", status, payload)
	}
}
