package emailcampaignhttp

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"vozko/domain/balance"
	"vozko/domain/campaign"
	"vozko/domain/emailcampaign"
	"vozko/domain/emailconsent"
	"vozko/domain/emaildesign"
	"vozko/domain/emailsending"
	balance_usecase "vozko/usecases/balance"
	emailcampaign_usecase "vozko/usecases/emailcampaign"
)

func domainErrors() []error {
	errs := []error{
		emailcampaign.ErrCampaignNotFound, emailcampaign.ErrNotEditable, emailcampaign.ErrNameRequired, emailcampaign.ErrNameTooLong, emailcampaign.ErrPreheaderTooLong,
		emailcampaign.ErrAudienceInvalid, emailcampaign.ErrABTestPercent, emailcampaign.ErrABTestMetric, emailcampaign.ErrABTestWindow, emailcampaign.ErrABTestSubject,
		emailcampaign.ErrABTestWinnerPreset, emailcampaign.ErrStatusInvalid, emailcampaign.ErrFinished, emailcampaign.ErrNotDraft,
		emailcampaign.ErrNotScheduled, emailcampaign.ErrNotLaunched, emailcampaign.ErrAlreadyHeld, emailcampaign.ErrRecipientNotFound,
		emailcampaign.ErrRecipientFilterInvalid, emailcampaign.ErrDesignNotFound, emailcampaign.ErrDesignKindInvalid, emailcampaign.ErrThumbnailInvalid,
		emailcampaign.ErrDesignNameRequired, emailcampaign.ErrTestBatchFailed,
		campaign.ErrAlreadyRunning, campaign.ErrNotRunning, campaign.ErrAlreadyStopped, campaign.ErrStatusInvalid, campaign.ErrActionInvalid,
		emailcampaign_usecase.ErrTestRecipients, emailcampaign_usecase.ErrTestAddressInvalid, emailsending.ErrAccountReactivation,
		emailcampaign_usecase.ErrDaysInvalid,
		emailcampaign_usecase.ErrSenderUnavailable,
		balance.ErrInsufficientBalance, balance.ErrPriceUnavailable, balance_usecase.ErrEmailUnitsInvalid, balance_usecase.ErrExchangeRateUnavailable,
		emaildesign.ErrMalformedDocument, emaildesign.ErrInvalidDocument, emaildesign.ErrInvalidSubject,
		emailsending.ErrNoDefaultSender, emailsending.ErrAccountPaused, emailsending.ErrDomainNotVerified, emailsending.ErrDailyCapReached,
		&emailcampaign_usecase.LaunchBlockedError{Report: emailcampaign.LaunchReport{Blocking: []emailcampaign.Issue{{Code: emailcampaign.IssueAudienceEmpty}}}},
		&balance_usecase.InsufficientEmailBalanceError{MissingMicros: 1200},
		&emaildesign.ValidationError{Kind: emaildesign.ErrInvalidDocument, Issues: []emaildesign.Issue{{Code: "footer_missing"}}},
		&emaildesign.ValidationError{Kind: emaildesign.ErrInvalidSubject, Issues: []emaildesign.Issue{{Code: "subject_empty"}}},
		&emailsending.DailyCapError{Cap: 500},
	}
	errs = append(errs, audienceErrors...)
	return append(errs, emailconsent.KnownErrors()...)
}

var codePattern = regexp.MustCompile(`^email_[a-z_]+$`)

func TestEveryDomainErrorHasACodeAndNeverA500(t *testing.T) {
	for _, err := range domainErrors() {
		wrapped := fmt.Errorf("context: %w", err)
		r, ok := classify(wrapped)
		if !ok {
			t.Errorf("%v has no code", err)
			continue
		}
		if r.status >= 500 && r.status != http.StatusBadGateway && r.status != http.StatusServiceUnavailable {
			t.Errorf("%v maps to %d", err, r.status)
		}
		if !codePattern.MatchString(r.code) {
			t.Errorf("%v maps to a malformed code %q", err, r.code)
		}
	}
}

func TestStructuredRefusalsCarryTheirDetails(t *testing.T) {
	cases := []struct {
		err     error
		code    string
		status  int
		details string
	}{
		{&balance_usecase.InsufficientEmailBalanceError{MissingMicros: 1200}, CodeBalanceInsufficient, http.StatusPaymentRequired, `"missing_micros":1200`},
		{&emailcampaign_usecase.LaunchBlockedError{Report: emailcampaign.LaunchReport{Blocking: []emailcampaign.Issue{{Code: emailcampaign.IssueBalanceInsufficient, MissingMicros: 80}}}}, CodeBlocked, http.StatusUnprocessableEntity, `"missing_micros":80`},
		{&emaildesign.ValidationError{Kind: emaildesign.ErrInvalidSubject, Issues: []emaildesign.Issue{{Code: "subject_empty"}}}, CodeSubjectInvalid, http.StatusUnprocessableEntity, `"subject_empty"`},
		{emailconsent.ErrUnsubscribeTokenInvalid, CodeTokenInvalid, http.StatusBadRequest, ""},
		{fmt.Errorf("revive: %w", emailsending.ErrAccountReactivation), "email_account_reactivation_failed", http.StatusServiceUnavailable, ""},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		writeError(rec, tc.err)
		if rec.Code != tc.status {
			t.Fatalf("%v: status %d", tc.err, rec.Code)
		}
		var body struct {
			Code    string          `json:"code"`
			Details json.RawMessage `json:"details"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.Code != tc.code || (tc.details != "" && !regexp.MustCompile(regexp.QuoteMeta(tc.details)).Match(body.Details)) {
			t.Fatalf("%v: body %s", tc.err, rec.Body.String())
		}
	}
}

func TestUnknownErrorsAreInternal(t *testing.T) {
	rec := httptest.NewRecorder()
	writeError(rec, errors.New("database exploded"))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d", rec.Code)
	}
}
