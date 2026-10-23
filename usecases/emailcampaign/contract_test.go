package emailcampaign_usecase

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"vozko/domain/emailcampaign"
	"vozko/domain/emailsending"
	balance_usecase "vozko/usecases/balance"
	"vozko/usecases/campaignqueue"
)

func TestListCarriesMetricsAndActionsFromOneSummaryQuery(t *testing.T) {
	w := newWorld(t)
	draft := w.draft(nil)
	running := w.draft(nil)
	w.campaigns.rows[running.ID] = func() emailcampaign.Campaign {
		c := w.campaigns.rows[running.ID]
		c.Status = emailcampaign.StatusRunning
		return c
	}()
	w.metrics.summaries = map[string]emailcampaign.Tally{running.ID: {Sent: 800, Delivered: 760, Opens: 380, Clicks: 95}}
	items, total, err := w.svc.List(context.Background(), w.actor(), emailcampaign.ListQuery{})
	if err != nil || total != 2 || len(items) != 2 {
		t.Fatalf("items = %+v total = %d err = %v", items, total, err)
	}
	if w.metrics.summaryCalls != 1 {
		t.Fatalf("summary queries = %d, want one for the page", w.metrics.summaryCalls)
	}
	byID := map[string]ListItem{}
	for _, it := range items {
		byID[it.Campaign.ID] = it
	}
	if got := byID[running.ID]; got.Metrics != emailcampaign.NewListMetrics(emailcampaign.Tally{Sent: 800, Delivered: 760, Opens: 380, Clicks: 95}) ||
		!slices.Equal(got.Actions, []emailcampaign.Command{emailcampaign.CommandPause, emailcampaign.CommandStop}) {
		t.Fatalf("running = %+v", got)
	}
	if got := byID[draft.ID]; got.Metrics != (emailcampaign.ListMetrics{}) || !slices.Contains(got.Actions, emailcampaign.CommandDelete) {
		t.Fatalf("draft = %+v", got)
	}
}

func TestPresentShowsTheRunningBillingInMicrosAndReais(t *testing.T) {
	w, c := launched(t, nil,
		person{email: "a@x.com", subscribed: true},
		person{email: "b@x.com", subscribed: true})
	w.svc.handle(campaignqueue.Message{CampaignID: c.ID, EntryID: pendingOf(w, c, "a@x.com").ID})
	view, err := w.svc.Present(context.Background(), w.campaign(c.ID))
	if err != nil {
		t.Fatal(err)
	}
	if view.Billing.ChargedUnits != 2 || view.Billing.RefundPendingMicros != 583 || view.Billing.SettledAt != nil {
		t.Fatalf("billing = %+v", view.Billing)
	}
	if view.Money == nil || view.Money.ExchangeRate != 5.5 || view.Money.ChargedBRL != 0.01 || view.Money.RefundPendingBRL != 0.003207 {
		t.Fatalf("money = %+v", view.Money)
	}
	if view.TimeZone != emailsending.TimeZoneName || !slices.Equal(view.Actions, []emailcampaign.Command{emailcampaign.CommandPause, emailcampaign.CommandStop}) || view.Hold != nil {
		t.Fatalf("view = %+v", view)
	}
}

func TestPresentHoldCarriesTheAccountReasonAndTheResumeTime(t *testing.T) {
	w := newWorld(t)
	c := w.draft(nil)
	held := w.campaigns.rows[c.ID]
	held.Status, held.HoldReason = emailcampaign.StatusHeld, emailcampaign.HoldAccountPaused
	w.accounts.reason = "revisão de reclamações"
	view, err := w.svc.Present(context.Background(), held)
	if err != nil {
		t.Fatal(err)
	}
	if view.Hold == nil || view.Hold.AccountStatusReason != "revisão de reclamações" || view.Hold.ResumeAt != nil {
		t.Fatalf("hold = %+v", view.Hold)
	}
	held.HoldReason = emailcampaign.HoldDailyCap
	view, err = w.svc.Present(context.Background(), held)
	if err != nil || view.Hold == nil || view.Hold.ResumeAt == nil || !view.Hold.ResumeAt.Equal(emailsending.NextDayStart(now)) {
		t.Fatalf("daily cap hold = %+v err = %v", view.Hold, err)
	}
}

func TestPresentOmitsReaisWhenTheRateCannotBeRead(t *testing.T) {
	w := newWorld(t)
	c := w.draft(nil)
	w.rates.err = errors.New("no rate")
	view, err := w.svc.Present(context.Background(), w.campaign(c.ID))
	if err != nil {
		t.Fatalf("a passive read never fails on the rate: %v", err)
	}
	if view.Money != nil {
		t.Fatalf("money = %+v, want none", view.Money)
	}
}

func TestReviewRefusesWithoutTheExchangeRate(t *testing.T) {
	w := newWorld(t)
	w.audience(person{email: "ok@x.com", subscribed: true})
	c := w.draft(nil)
	w.rates.err = errors.New("no rate")
	if _, err := w.svc.Review(context.Background(), w.actor(), c.ID); !errors.Is(err, balance_usecase.ErrExchangeRateUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestReviewCarriesReaisAndTheZone(t *testing.T) {
	w := newWorld(t)
	w.audience(person{email: "ok@x.com", subscribed: true})
	*w.wallet = 100
	c := w.draft(nil)
	r, err := w.svc.Review(context.Background(), w.actor(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r.ExchangeRate != 5.5 || r.Send.UnitBRL != 0.003207 || r.Send.TotalBRL != 0.003207 || r.BalanceBRL != 0.00055 || r.MissingBRL != 0.002657 || r.AfterFirstBatchBRL != 0 {
		t.Fatalf("money = %+v rate %v balance %v missing %v after %v", r.Send, r.ExchangeRate, r.BalanceBRL, r.MissingBRL, r.AfterFirstBatchBRL)
	}
	if r.TimeZone != emailsending.TimeZoneName {
		t.Fatalf("zone = %q", r.TimeZone)
	}
}

func TestReviewEstimatesASingleEmailBelowOneCentavo(t *testing.T) {
	w := newWorld(t)
	w.audience(person{email: "ok@x.com", subscribed: true})
	w.rates.rate = 6_000_000
	*w.wallet = 1_000_000
	c := w.draft(nil)
	r, err := w.svc.Review(context.Background(), w.actor(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r.Send.Units != 1 || r.Send.UnitBRL != 0.003498 || r.Send.TotalBRL != 0.003498 || r.FirstBatch.TotalBRL != 0.003498 {
		t.Fatalf("send = %+v first = %+v", r.Send, r.FirstBatch)
	}
	if r.BalanceBRL != 6 || r.AfterFirstBatchMicros != 999_417 || r.AfterFirstBatchBRL != 5.996502 || r.MissingBRL != 0 {
		t.Fatalf("balance %v after %d/%v missing %v", r.BalanceBRL, r.AfterFirstBatchMicros, r.AfterFirstBatchBRL, r.MissingBRL)
	}
}

func TestStopRecordsTheSettlementOnce(t *testing.T) {
	w, c := launched(t, nil,
		person{email: "a@x.com", subscribed: true},
		person{email: "b@x.com", subscribed: true})
	w.svc.handle(campaignqueue.Message{CampaignID: c.ID, EntryID: pendingOf(w, c, "a@x.com").ID})
	if _, err := w.svc.Stop(context.Background(), w.actor(), c.ID); err != nil {
		t.Fatal(err)
	}
	got := w.campaign(c.ID)
	if got.Settlement == nil || got.Settlement.RefundedUnits != 1 || got.Settlement.RefundedMicros != 583 || !got.Settlement.SettledAt.Equal(now) {
		t.Fatalf("settlement = %+v", got.Settlement)
	}
	if err := w.svc.settle(context.Background(), got); err != nil {
		t.Fatal(err)
	}
	if len(w.billing.batchRefunds) != 1 {
		t.Fatalf("a settled campaign is not refunded again: %+v", w.billing.batchRefunds)
	}
	view, err := w.svc.Present(context.Background(), got)
	if err != nil || view.Billing.RefundedMicros != 583 || view.Billing.FinalMicros != 583 || view.Billing.RefundPendingMicros != 0 {
		t.Fatalf("billing = %+v err = %v", view.Billing, err)
	}
}

func TestTimelineTellsWhenToRefreshAndTheZone(t *testing.T) {
	w := newWorld(t)
	c := w.draft(nil)
	running := w.campaigns.rows[c.ID]
	running.Status = emailcampaign.StatusRunning
	started := now.Add(-time.Hour)
	running.StartedAt = &started
	w.campaigns.rows[c.ID] = running
	got, err := w.svc.Timeline(context.Background(), w.actor(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.RefreshAfterSeconds != 15 || got.TimeZone != emailsending.TimeZoneName {
		t.Fatalf("timeline = %+v", got)
	}
}

func TestOverviewNamesItsZoneAndStartsOnTheBrasiliaDay(t *testing.T) {
	w := newWorld(t)
	got, err := w.svc.Overview(context.Background(), w.actor(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if got.TimeZone != emailsending.TimeZoneName {
		t.Fatalf("zone = %q", got.TimeZone)
	}
	if want := emailsending.DayStart(now).AddDate(0, 0, -6); !w.metrics.overviewSince.Equal(want) {
		t.Fatalf("since = %v, want %v", w.metrics.overviewSince, want)
	}
}
