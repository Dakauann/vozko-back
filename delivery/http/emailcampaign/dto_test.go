package emailcampaignhttp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"vozko/domain/emailcampaign"
	balance_usecase "vozko/usecases/balance"
	emailcampaign_usecase "vozko/usecases/emailcampaign"
)

func TestTheExchangeRateRefusalIsCoded(t *testing.T) {
	rec := httptest.NewRecorder()
	writeError(rec, balance_usecase.ErrExchangeRateUnavailable)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), `"email_exchange_rate_unavailable"`) {
		t.Fatalf("refusal = %d %s", rec.Code, rec.Body.String())
	}
}

func TestCampaignResponseCarriesActionsHoldBillingAndZone(t *testing.T) {
	resume := time.Date(2026, 10, 22, 3, 0, 0, 0, time.UTC)
	settled := time.Date(2026, 10, 21, 18, 0, 0, 0, time.UTC)
	view := emailcampaign_usecase.CampaignView{
		Campaign: emailcampaign.Campaign{ID: "c1", Name: "Black Friday", Status: emailcampaign.StatusHeld, HoldReason: emailcampaign.HoldDailyCap},
		Actions:  []emailcampaign.Command{emailcampaign.CommandResume, emailcampaign.CommandPause, emailcampaign.CommandStop},
		Hold:     &emailcampaign.Hold{Reason: emailcampaign.HoldDailyCap, ResumeAt: &resume},
		Billing: emailcampaign.Billing{BatchSize: 1000, CurrentBatch: 2, ChargedUnits: 1010, ChargedMicros: 606000, SentUnits: 1006,
			RefundedUnits: 4, RefundedMicros: 2400, FinalMicros: 603600, SettledAt: &settled},
		Money:    &emailcampaign_usecase.BillingMoney{ExchangeRate: 5.5, ChargedBRL: 3.33, RefundedBRL: 0.01, FinalBRL: 3.32},
		TimeZone: "America/Sao_Paulo",
	}
	raw, err := json.Marshal(campaignResponse(view))
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	for _, want := range []string{
		`"actions":["resume","pause","stop"]`,
		`"hold":{"reason":"daily_cap","resume_at":"2026-10-22T03:00:00Z"}`,
		`"batch_size":1000`, `"current_batch":2`, `"charged_units":1010`, `"charged_micros":606000`, `"sent_units":1006`,
		`"refunded_units":4`, `"refunded_micros":2400`, `"final_micros":603600`,
		`"settled_at":"2026-10-21T18:00:00Z"`, `"refund_pending_micros":0`, `"exchange_rate":5.5`, `"charged_brl":3.33`, `"refunded_brl":0.01`, `"final_brl":3.32`, `"refund_pending_brl":0`,
		`"time_zone":"America/Sao_Paulo"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s in %s", want, body)
		}
	}
	view.Money, view.Hold, view.Billing.SettledAt = nil, nil, nil
	raw, _ = json.Marshal(campaignResponse(view))
	body = string(raw)
	for _, want := range []string{`"exchange_rate":null`, `"charged_brl":null`, `"refund_pending_brl":null`, `"settled_at":null`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s in %s", want, body)
		}
	}
	if strings.Contains(body, `"hold"`) {
		t.Errorf("no hold, no hold object: %s", body)
	}
	for _, gone := range []string{`"charge"`, `"total_micros"`, `"total_brl"`, `"unit_micros"`} {
		if strings.Contains(body, gone) {
			t.Errorf("the up-front run charge is gone, found %s in %s", gone, body)
		}
	}
}

func TestCampaignListCarriesMetricsAndActions(t *testing.T) {
	items := []emailcampaign_usecase.ListItem{{
		Campaign: emailcampaign.Campaign{ID: "c1", Status: emailcampaign.StatusRunning},
		Metrics:  emailcampaign.ListMetrics{Sent: 800, Delivered: 760, DeliveryRate: 9500, OpenRate: 5000, ClickRate: 1250},
		Actions:  []emailcampaign.Command{emailcampaign.CommandPause, emailcampaign.CommandStop},
	}}
	raw, _ := json.Marshal(campaignList(items, 1, 1, 50))
	body := string(raw)
	for _, want := range []string{`"metrics":{"sent":800,"delivered":760,"delivery_rate":9500,"open_rate":5000,"click_rate":1250}`, `"actions":["pause","stop"]`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s in %s", want, body)
		}
	}
}

func TestReviewResponseIsSnakeCase(t *testing.T) {
	view := emailcampaign_usecase.ReviewView{
		Review: emailcampaign_usecase.Review{
			Audience: emailcampaign.AudienceSummary{Matched: 10, WithEmail: 9, Sendable: 7,
				Skipped: map[emailcampaign.SkipReason]int{emailcampaign.SkipNoEmail: 1, emailcampaign.SkipUnsubscribed: 2}},
			BalanceMicros: 1_000_000, MissingMicros: 0, AfterFirstBatchMicros: 995_336, BatchSize: 1000,
		},
		Send:         emailcampaign_usecase.PricedQuote{Quote: emailcampaign_usecase.Quote{Units: 8, UnitMicros: 583, TotalMicros: 4664}, UnitBRL: 0.003207, TotalBRL: 0.025652},
		FirstBatch:   emailcampaign_usecase.PricedQuote{Quote: emailcampaign_usecase.Quote{Units: 8, UnitMicros: 583, TotalMicros: 4664}, UnitBRL: 0.003207, TotalBRL: 0.025652},
		ExchangeRate: 5.5, BalanceBRL: 5.5, AfterFirstBatchBRL: 5.474348, TimeZone: "America/Sao_Paulo",
	}
	raw, _ := json.Marshal(reviewResponse(view))
	body := string(raw)
	for _, want := range []string{
		`"audience":{"matched":10,"with_email":9,"sendable":7,"never_mailed":0,"skipped":{"no_email":1,"unsubscribed":2},"variables":[]}`,
		`"send":{"units":8,"unit_micros":583,"total_micros":4664,"unit_brl":0.003207,"total_brl":0.025652}`,
		`"first_batch":{"units":8,"unit_micros":583,"total_micros":4664,"unit_brl":0.003207,"total_brl":0.025652}`, `"batch_size":1000`,
		`"exchange_rate":5.5`, `"balance_brl":5.5`, `"missing_brl":0`, `"after_first_batch_micros":995336`, `"after_first_batch_brl":5.474348`, `"time_zone":"America/Sao_Paulo"`,
		`"check":{"blocking":`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s in %s", want, body)
		}
	}
	for _, gone := range []string{`"test_sends"`, `"daily_limit"`, `"remaining_today"`} {
		if strings.Contains(body, gone) {
			t.Errorf("the free test allowance is gone, found %s in %s", gone, body)
		}
	}
	if strings.Contains(body, `"Matched"`) || strings.Contains(body, `"validation"`) || strings.Contains(body, `"verdict"`) || strings.Contains(body, `"eligible"`) {
		t.Errorf("domain or retired field names leaked: %s", body)
	}
}

func TestRecipientsCarryTheLeadName(t *testing.T) {
	page := emailcampaign_usecase.RecipientPage{Items: []emailcampaign.RecipientRow{{Recipient: emailcampaign.Recipient{ID: "r1", Address: "ana@x.com"}, LeadName: "Ana Souza"}}, Total: 1, Page: 1, PageSize: 50}
	raw, _ := json.Marshal(recipientList(page))
	if !strings.Contains(string(raw), `"lead_name":"Ana Souza"`) {
		t.Fatalf("body = %s", raw)
	}
}

func TestTheCustomerQuoteNeverCarriesOurCost(t *testing.T) {
	q := emailcampaign_usecase.PricedQuote{Quote: emailcampaign_usecase.Quote{Units: 2, UnitMicros: 583, UnitCostMicros: 165, TotalMicros: 1166}}
	raw, err := json.Marshal(quoteDTO(q))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "cost") || strings.Contains(string(raw), "165") {
		t.Fatalf("the quote a customer reads exposes our cost: %s", raw)
	}
}
