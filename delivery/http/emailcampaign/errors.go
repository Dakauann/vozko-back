package emailcampaignhttp

import (
	"errors"
	"log"
	"net/http"

	"vozko/delivery/http/response"
	"vozko/domain/balance"
	"vozko/domain/campaign"
	"vozko/domain/emailcampaign"
	"vozko/domain/emailconsent"
	"vozko/domain/emaildesign"
	"vozko/domain/emailsending"
	"vozko/domain/lead"
	"vozko/domain/selection"
	balance_usecase "vozko/usecases/balance"
	emailcampaign_usecase "vozko/usecases/emailcampaign"
)

const (
	CodeBlocked             = "email_campaign_blocked"
	CodeBalanceInsufficient = "email_balance_insufficient"
	CodeDesignInvalid       = "email_design_invalid"
	CodeSubjectInvalid      = "email_subject_invalid"
	CodeAudienceInvalid     = "email_audience_invalid"
	CodeTokenInvalid        = "email_unsubscribe_token_invalid"
	consentCodePrefix       = "email_"
)

type mapping struct {
	err    error
	status int
	code   string
}

var mappings = []mapping{
	{emailcampaign.ErrCampaignNotFound, http.StatusNotFound, "email_campaign_not_found"},
	{emailcampaign.ErrNotEditable, http.StatusConflict, "email_campaign_not_editable"},
	{emailcampaign.ErrNameRequired, http.StatusBadRequest, "email_campaign_name_required"},
	{emailcampaign.ErrNameTooLong, http.StatusBadRequest, "email_campaign_name_too_long"},
	{emailcampaign.ErrPreheaderTooLong, http.StatusBadRequest, "email_preheader_too_long"},
	{emailcampaign.ErrAudienceInvalid, http.StatusBadRequest, CodeAudienceInvalid},
	{emailcampaign.ErrABTestPercent, http.StatusBadRequest, "email_ab_test_percent_invalid"},
	{emailcampaign.ErrABTestMetric, http.StatusBadRequest, "email_ab_test_metric_invalid"},
	{emailcampaign.ErrABTestWindow, http.StatusBadRequest, "email_ab_test_window_invalid"},
	{emailcampaign.ErrABTestSubject, http.StatusBadRequest, "email_ab_test_subject_invalid"},
	{emailcampaign.ErrABTestWinnerPreset, http.StatusBadRequest, "email_ab_test_winner_preset"},
	{emailcampaign.ErrStatusInvalid, http.StatusBadRequest, "email_campaign_status_invalid"},
	{emailcampaign.ErrFinished, http.StatusConflict, "email_campaign_finished"},
	{emailcampaign.ErrNotDraft, http.StatusConflict, "email_campaign_not_draft"},
	{emailcampaign.ErrNotScheduled, http.StatusConflict, "email_campaign_not_scheduled"},
	{emailcampaign.ErrNotLaunched, http.StatusConflict, "email_campaign_not_launched"},
	{emailcampaign.ErrAlreadyHeld, http.StatusConflict, "email_campaign_already_held"},
	{emailcampaign.ErrTestBatchFailed, http.StatusConflict, "email_campaign_test_batch_failed"},
	{emailcampaign.ErrRecipientNotFound, http.StatusNotFound, "email_recipient_not_found"},
	{emailcampaign.ErrRecipientFilterInvalid, http.StatusBadRequest, "email_recipient_filter_invalid"},
	{emailcampaign.ErrDesignNotFound, http.StatusNotFound, "email_design_not_found"},
	{emailcampaign.ErrDesignKindInvalid, http.StatusBadRequest, "email_design_kind_invalid"},
	{emailcampaign.ErrThumbnailInvalid, http.StatusBadRequest, "email_design_thumbnail_invalid"},
	{emailcampaign.ErrDesignNameRequired, http.StatusBadRequest, "email_design_name_required"},
	{campaign.ErrAlreadyRunning, http.StatusConflict, "email_campaign_already_running"},
	{campaign.ErrNotRunning, http.StatusConflict, "email_campaign_not_running"},
	{campaign.ErrAlreadyStopped, http.StatusConflict, "email_campaign_already_stopped"},
	{campaign.ErrStatusInvalid, http.StatusConflict, "email_campaign_status_invalid"},
	{campaign.ErrActionInvalid, http.StatusBadRequest, "email_campaign_action_invalid"},
	{emailcampaign_usecase.ErrTestRecipients, http.StatusBadRequest, "email_test_recipients_invalid"},
	{emailcampaign_usecase.ErrTestAddressInvalid, http.StatusBadRequest, "email_test_address_invalid"},
	{emailsending.ErrAccountReactivation, http.StatusServiceUnavailable, "email_account_reactivation_failed"},
	{emailcampaign_usecase.ErrDaysInvalid, http.StatusBadRequest, "email_overview_days_invalid"},
	{emailcampaign_usecase.ErrSenderUnavailable, http.StatusConflict, "email_sender_missing"},
	{balance.ErrInsufficientBalance, http.StatusPaymentRequired, CodeBalanceInsufficient},
	{balance.ErrBalanceNotFound, http.StatusPaymentRequired, CodeBalanceInsufficient},
	{balance.ErrPriceUnavailable, http.StatusServiceUnavailable, "email_price_unavailable"},
	{balance_usecase.ErrEmailUnitsInvalid, http.StatusBadRequest, "email_units_invalid"},
	{balance_usecase.ErrExchangeRateUnavailable, http.StatusServiceUnavailable, "email_exchange_rate_unavailable"},
	{emaildesign.ErrMalformedDocument, http.StatusBadRequest, "email_design_malformed"},
	{emaildesign.ErrUnknownBlockKind, http.StatusBadRequest, "email_design_malformed"},
	{emaildesign.ErrInvalidDocument, http.StatusUnprocessableEntity, CodeDesignInvalid},
	{emaildesign.ErrInvalidSubject, http.StatusUnprocessableEntity, CodeSubjectInvalid},
	{emailsending.ErrNoDefaultSender, http.StatusConflict, "email_sender_missing"},
	{emailsending.ErrSenderNotFound, http.StatusConflict, "email_sender_missing"},
	{emailsending.ErrAccountNotProvisioned, http.StatusConflict, "email_sender_missing"},
	{emailsending.ErrAccountNotFound, http.StatusConflict, "email_sender_missing"},
	{emailsending.ErrDomainNotFound, http.StatusConflict, "email_sender_missing"},
	{emailsending.ErrAccountPaused, http.StatusConflict, "email_account_paused"},
	{emailsending.ErrDomainNotVerified, http.StatusConflict, "email_sender_not_verified"},
	{emailsending.ErrSenderDomainMismatch, http.StatusConflict, "email_sender_not_verified"},
	{emailsending.ErrFreeMailboxFrom, http.StatusConflict, "email_sender_not_verified"},
	{emailsending.ErrDailyCapReached, http.StatusTooManyRequests, "email_daily_cap_reached"},
	{lead.ErrLeadForbidden, http.StatusForbidden, "email_audience_forbidden"},
	{selection.ErrScopeDenied, http.StatusForbidden, "email_audience_forbidden"},
}

var audienceErrors = []error{
	selection.ErrUnknownMode, selection.ErrEmptySelection, selection.ErrAmbiguousSelection, selection.ErrTooManyIDs,
	selection.ErrFilterRequired, selection.ErrLimitRequired, selection.ErrEveryoneUnconfirmed, selection.ErrFingerprintMismatch,
	selection.ErrInvalidCount, selection.ErrCountChanged, selection.ErrModeUnsupported, selection.ErrResolverUnavailable,
	selection.ErrInvalidPage, selection.ErrInvalidIDs, selection.ErrUnknownSort, selection.ErrCountRequired,
}

var consentStatuses = map[error]int{
	emailconsent.ErrSubscriptionNotFound:    http.StatusNotFound,
	emailconsent.ErrUnsubscribeTokenInvalid: http.StatusBadRequest,
}

type refusal struct {
	status  int
	code    string
	details any
}

func classify(err error) (refusal, bool) {
	var blocked *emailcampaign_usecase.LaunchBlockedError
	if errors.As(err, &blocked) {
		return refusal{status: http.StatusUnprocessableEntity, code: CodeBlocked, details: map[string]any{"issues": blocked.Report.Blocking, "hints": blocked.Report.Hints}}, true
	}
	var short *balance_usecase.InsufficientEmailBalanceError
	if errors.As(err, &short) {
		return refusal{status: http.StatusPaymentRequired, code: CodeBalanceInsufficient, details: map[string]any{"missing_micros": short.MissingMicros}}, true
	}
	var invalid *emaildesign.ValidationError
	if errors.As(err, &invalid) {
		code := CodeDesignInvalid
		if errors.Is(invalid.Kind, emaildesign.ErrInvalidSubject) {
			code = CodeSubjectInvalid
		}
		return refusal{status: http.StatusUnprocessableEntity, code: code, details: map[string]any{"issues": invalid.Issues}}, true
	}
	for _, m := range mappings {
		if errors.Is(err, m.err) {
			return refusal{status: m.status, code: m.code}, true
		}
	}
	for _, target := range audienceErrors {
		if errors.Is(err, target) {
			return refusal{status: http.StatusBadRequest, code: CodeAudienceInvalid}, true
		}
	}
	if code := emailconsent.ErrorCode(err); code != "" {
		status, ok := consentStatuses[unwrapConsent(err)]
		if !ok {
			status = http.StatusBadRequest
		}
		return refusal{status: status, code: consentCodePrefix + code}, true
	}
	return refusal{}, false
}

func unwrapConsent(err error) error {
	for target := range consentStatuses {
		if errors.Is(err, target) {
			return target
		}
	}
	return nil
}

func writeError(w http.ResponseWriter, err error) {
	if r, ok := classify(err); ok {
		if r.details != nil {
			response.WriteErrorWithDetails(w, r.status, r.code, err.Error(), r.details)
			return
		}
		response.WriteErrorWithCode(w, r.status, r.code, err.Error(), nil)
		return
	}
	log.Printf("[email-campaign] request failed: %v", err)
	response.WriteError(w, http.StatusInternalServerError, "Falha ao processar a solicitação de e-mail", nil)
}
