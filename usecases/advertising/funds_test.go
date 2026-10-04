package advertising

import (
	"context"
	"errors"
	"testing"
	"time"

	ads "vozko/domain/advertising"
)

type recordedAlerts struct{ sent []ads.Funds }

func (r *recordedAlerts) Alert(_ context.Context, _ *ads.AdAccount, funds ads.Funds) error {
	r.sent = append(r.sent, funds)
	return nil
}

func spendDay(daysAgo int, micros int64) ads.DailyInsight {
	day := ads.CivilDay(testNow, time.UTC).AddDate(0, 0, -daysAgo)
	return ads.DailyInsight{AdMetaID: "ad-1", Day: day, Currency: "BRL", SpendMicros: micros}
}

func fundsWorld(spendCap, spent int64) (*world, *recordedAlerts) {
	w := newWorld()
	alerts := &recordedAlerts{}
	w.sync.alerts = alerts
	w.gateway.accounts[0].SpendCap, w.gateway.accounts[0].AmountSpent = spendCap, spent
	w.gateway.billing = ads.RemoteBilling{Prepay: true, Balance: 148}
	return w, alerts
}

func TestSyncStoresTheFundsOfAPrepaidAccountAndAlertsWhenTheyRunLow(t *testing.T) {
	w, alerts := fundsWorld(2635, 2500)
	w.gateway.insights = []ads.DailyInsight{spendDay(1, 700_000), spendDay(2, 700_000)}
	if _, err := w.sync.Sync(context.Background(), "ws-1", "acc-1"); err != nil {
		t.Fatal(err)
	}
	stored := w.accounts.byID["acc-1"]
	if stored.Billing != ads.BillingPrepaid || stored.DailySpendMicros != 200_000 || stored.FundsLevel != ads.FundsLow || stored.FundsLevelSince == nil {
		t.Fatalf("stored %+v", stored)
	}
	if len(alerts.sent) != 1 || alerts.sent[0].Reason != ads.ReasonFundsLow {
		t.Fatalf("alerts %+v", alerts.sent)
	}
	if len(w.gateway.billingWith) != 1 || w.gateway.billingWith[0] {
		t.Fatalf("sync must read the billing kind without the payment method, got %v", w.gateway.billingWith)
	}
}

func TestAHealthyAccountIsStoredWithoutAnAlert(t *testing.T) {
	w, alerts := fundsWorld(2635, 125)
	if _, err := w.sync.Sync(context.Background(), "ws-1", "acc-1"); err != nil {
		t.Fatal(err)
	}
	if w.accounts.byID["acc-1"].FundsLevel != ads.FundsOK || len(alerts.sent) != 0 {
		t.Fatalf("stored %+v alerts %+v", w.accounts.byID["acc-1"], alerts.sent)
	}
}

func TestUnreadableBillingLeavesTheFundsUnknownWithoutFailingTheSync(t *testing.T) {
	w, alerts := fundsWorld(2635, 2635)
	w.gateway.failOn, w.gateway.failWith = "billing", errors.New("meta is down")
	if _, err := w.sync.Sync(context.Background(), "ws-1", "acc-1"); err != nil {
		t.Fatalf("a billing read must not stop the sync: %v", err)
	}
	if stored := w.accounts.byID["acc-1"]; stored.Billing != ads.BillingUnknown || stored.FundsLevel != ads.FundsUnknown {
		t.Fatalf("an unread account must not look funded: %+v", stored)
	}
	if len(alerts.sent) != 0 {
		t.Fatalf("unknown funds are shown, not emailed: %+v", alerts.sent)
	}
}

func TestAnAnalystOnlyProfileNeverReadsBilling(t *testing.T) {
	w, _ := fundsWorld(2635, 125)
	w.gateway.accounts[0].Tasks = []string{"ANALYZE"}
	if _, err := w.sync.Sync(context.Background(), "ws-1", "acc-1"); err != nil {
		t.Fatal(err)
	}
	if len(w.gateway.billingWith) != 0 || w.accounts.byID["acc-1"].FundsLevel != ads.FundsUnknown {
		t.Fatalf("billing reads %v, stored %+v", w.gateway.billingWith, w.accounts.byID["acc-1"])
	}
}

func TestTheWatchRereadsOnlyAccountsThatNeedAttention(t *testing.T) {
	w, alerts := fundsWorld(2635, 2635)
	w.accounts.byID["acc-1"].FundsLevel = ads.FundsOut
	w.accounts.byID["acc-2"] = &ads.AdAccount{ID: "acc-2", WorkspaceID: "ws-1", GrantID: "grant-1", MetaAccountID: "222", Connection: ads.ConnectionConnected, FundsLevel: ads.FundsOK}
	if err := w.sync.WatchFunds(context.Background()); err != nil {
		t.Fatal(err)
	}
	reads := 0
	for _, call := range w.gateway.calls {
		if call == "get_account" {
			reads++
		}
	}
	if reads != 1 {
		t.Fatalf("only the account out of funds must be re-read, got %d reads: %v", reads, w.gateway.calls)
	}
	if len(alerts.sent) != 1 || alerts.sent[0].Level != ads.FundsOut {
		t.Fatalf("alerts %+v", alerts.sent)
	}
}

func TestTheWatchSeesATopUpAndStopsAlerting(t *testing.T) {
	w, alerts := fundsWorld(8785, 2635)
	since := testNow.Add(-time.Hour)
	w.accounts.byID["acc-1"].FundsLevel, w.accounts.byID["acc-1"].FundsLevelSince = ads.FundsOut, &since
	w.accounts.byID["acc-1"].LastSyncedAt = &since
	if err := w.sync.WatchFunds(context.Background()); err != nil {
		t.Fatal(err)
	}
	if stored := w.accounts.byID["acc-1"]; stored.FundsLevel != ads.FundsOK || !stored.FundsLevelSince.Equal(testNow) {
		t.Fatalf("stored %+v", stored)
	}
	if len(alerts.sent) != 0 {
		t.Fatalf("a funded account must not be alerted: %+v", alerts.sent)
	}
}
