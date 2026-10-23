package carriercampaign

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"vozko/domain/campaign"
	cc "vozko/domain/carriercampaign"
	cm "vozko/domain/carriermessaging"
	"vozko/domain/rcs"
	"vozko/domain/selection"
	"vozko/domain/sms"
	ccuc "vozko/usecases/carriercampaign"
)

var codeShape = regexp.MustCompile(`^carrier_campaign_[a-z_]+$`)

func TestEveryRefusalHasAStableCode(t *testing.T) {
	for _, code := range RefusalCodes() {
		if !codeShape.MatchString(code) {
			t.Fatalf("code %q is not a stable snake_case code with the carrier_campaign prefix", code)
		}
	}
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body = %q", rec.Body.String())
	}
	return body
}

func TestWriteErrorMapsWrappedErrors(t *testing.T) {
	cases := []struct {
		err     error
		status  int
		code    string
		details map[string]any
	}{
		{fmt.Errorf("variant 2: %w", cc.ErrVariantChannelMismatch), http.StatusUnprocessableEntity, "carrier_campaign_variant_channel_mismatch", nil},
		{fmt.Errorf("variant 1: card 2: %w", rcs.ErrSuggestionInvalid), http.StatusUnprocessableEntity, "carrier_campaign_content_invalid", nil},
		{fmt.Errorf("%w: %w", cc.ErrAudienceInvalid, errors.New("selection")), http.StatusUnprocessableEntity, "carrier_campaign_audience_invalid", nil},
		{fmt.Errorf("variable {{1}}: %w", campaign.ErrBindingSensitive), http.StatusUnprocessableEntity, "carrier_campaign_binding_sensitive", nil},
		{campaign.ErrNotRunning, http.StatusConflict, "carrier_campaign_not_running", nil},
		{ccuc.ErrOnlyDraftDeletes, http.StatusConflict, "carrier_campaign_only_draft_deletes", nil},
		{cc.ErrCampaignNotFound, http.StatusNotFound, "carrier_campaign_not_found", nil},
		{sms.ErrTextTooLong, http.StatusUnprocessableEntity, "carrier_campaign_sms_text_too_long", nil},
		{&cm.ShortfallError{MissingMicros: 42}, http.StatusPaymentRequired, "carrier_campaign_insufficient_balance", map[string]any{"missing_micros": float64(42)}},
		{&cm.QuietHoursError{NextOpen: time.Date(2026, 10, 22, 11, 0, 0, 0, time.UTC)}, http.StatusConflict, "carrier_campaign_quiet_hours", map[string]any{"next_open": "2026-10-22T11:00:00Z"}},
		{&campaign.MissingVariableError{Missing: []campaign.MissingVariable{{Slot: 2, Source: campaign.BindCity}}}, http.StatusUnprocessableEntity, "carrier_campaign_missing_variable", map[string]any{"slot": float64(2), "source": "lead.city"}},
		{rcs.ErrSenderCannotSend, http.StatusConflict, "carrier_campaign_not_ready", map[string]any{"blocker": ccuc.BlockerSenderNotReady}},
		{fmt.Errorf("route: %w", sms.ErrRouteCannotSend), http.StatusConflict, "carrier_campaign_not_ready", map[string]any{"blocker": ccuc.BlockerRouteNotReady}},
		{&selection.CountChangedError{Expected: 40, Matched: 43}, http.StatusConflict, "carrier_campaign_audience_changed", map[string]any{"expected": float64(40), "matched": float64(43)}},
		{selection.ErrEveryoneUnconfirmed, http.StatusConflict, "carrier_campaign_audience_unconfirmed", nil},
		{selection.ErrFingerprintMismatch, http.StatusConflict, "carrier_campaign_audience_unconfirmed", nil},
		{selection.ErrCountRequired, http.StatusConflict, "carrier_campaign_audience_unconfirmed", nil},
		{cm.ErrChannelNotLaunched, http.StatusConflict, "carrier_campaign_channel_coming_soon", nil},
		{errors.New("boom"), http.StatusInternalServerError, "carrier_campaign_request_failed", nil},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		writeError(rec, tc.err, "test")
		body := decodeBody(t, rec)
		if rec.Code != tc.status || body["code"] != tc.code {
			t.Fatalf("%v = %d %v, want %d %s", tc.err, rec.Code, body, tc.status, tc.code)
		}
		if tc.details != nil {
			details, _ := body["details"].(map[string]any)
			for k, v := range tc.details {
				if details[k] != v {
					t.Fatalf("%v details %v, want %s=%v", tc.err, details, k, v)
				}
			}
		}
	}
}
