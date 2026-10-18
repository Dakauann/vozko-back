package advertising

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	ads "vozko/domain/advertising"
	"vozko/domain/notification"
	"vozko/domain/workspace"
	"vozko/usecases/notification/notificationtest"
)

func alertAccount() *ads.AdAccount {
	return &ads.AdAccount{ID: "acc-1", WorkspaceID: "ws-1", MetaAccountID: "1762972444913096", BusinessID: "biz-1", Name: "Vozko CRM BRL", Currency: "BRL"}
}

func TestEachFundsProblemNotifiesTheAdsTeamOncePerDayWithTheFixAtMeta(t *testing.T) {
	room := int64(2510)
	cases := map[ads.FundsReason]string{
		ads.ReasonFundsLow:          "R$ 25,10",
		ads.ReasonFundsOut:          "pararam",
		ads.ReasonSpendLimitLow:     "R$ 25,10",
		ads.ReasonSpendLimitReached: "limite de gastos",
		ads.ReasonPaymentFailed:     "não conseguiu cobrar",
		ads.ReasonGracePeriod:       "não conseguiu cobrar",
	}
	for reason, phrase := range cases {
		publisher := &notificationtest.Publisher{}
		alerts := NewFundsAlerts(publisher, "https://app.vozko.test/")
		funds := ads.Funds{Level: ads.FundsLow, Reason: reason, RoomMinor: &room}
		if err := alerts.Alert(context.Background(), alertAccount(), funds); err != nil {
			t.Fatalf("%s: %v", reason, err)
		}
		d := publisher.Only(t)
		if d.WorkspaceID != "ws-1" || d.Category != notification.CategoryAdvertising || !d.Email {
			t.Fatalf("%s: draft %+v", reason, d)
		}
		if !d.Audience.Equal(notification.Holding(workspace.ResourceAds, workspace.ActionUpdate)) {
			t.Fatalf("%s: audience %+v, want the people who manage ads", reason, d.Audience)
		}
		if d.DedupKey != "meta_ads_funds:acc-1:"+string(reason) || d.DedupWindow != 24*time.Hour {
			t.Fatalf("%s: dedup %q %v", reason, d.DedupKey, d.DedupWindow)
		}
		if !strings.Contains(d.Body, phrase) {
			t.Errorf("%s: body %q lacks %q", reason, d.Body, phrase)
		}
		stopped := reason == ads.ReasonFundsOut || reason == ads.ReasonSpendLimitReached || reason == ads.ReasonPaymentFailed
		if stopped != (d.Severity == notification.SeverityCritical) {
			t.Errorf("%s: severity %s; critical only when the ads already stopped", reason, d.Severity)
		}
		limit := reason == ads.ReasonSpendLimitLow || reason == ads.ReasonSpendLimitReached
		if limit && d.ActionURL != "/dashboard/advertising/overview?account=acc-1" {
			t.Errorf("%s: the limit is changed in the app, got %q", reason, d.ActionURL)
		}
		if !limit && !strings.HasPrefix(d.ActionURL, "https://business.facebook.com/") {
			t.Errorf("%s: funds and payments are fixed in Meta billing, got %q", reason, d.ActionURL)
		}

		email := d.EmailCopy
		if email == nil || email.Template != fundsAlertTemplate || !strings.HasPrefix(email.Subject, d.Title) {
			t.Fatalf("%s: email %+v", reason, email)
		}
		message, _ := email.Placeholders["Message"].(string)
		if message != d.Body {
			t.Errorf("%s: the email and the bell must say the same thing", reason)
		}
		if email.Placeholders["AccountName"] != "Vozko CRM BRL" || email.Placeholders["ManagerURL"] != "https://app.vozko.test/dashboard/advertising?account=acc-1" {
			t.Errorf("%s: placeholders %+v", reason, email.Placeholders)
		}
		if limit && email.Placeholders["ActionURL"] != "https://app.vozko.test/dashboard/advertising/overview?account=acc-1" {
			t.Errorf("%s: email action %v", reason, email.Placeholders["ActionURL"])
		}
	}
}

func TestAFundsAlertNeverCallsMetaMoneyOnlySaldo(t *testing.T) {
	publisher := &notificationtest.Publisher{}
	room := int64(0)
	_ = NewFundsAlerts(publisher, "https://app.vozko.test").Alert(context.Background(), alertAccount(), ads.Funds{Level: ads.FundsOut, Reason: ads.ReasonFundsOut, RoomMinor: &room})
	d := publisher.Only(t)
	texts := []string{d.Title, d.Body, d.ActionLabel}
	for _, value := range d.EmailCopy.Placeholders {
		text, _ := value.(string)
		texts = append(texts, text)
	}
	for _, text := range texts {
		if strings.Contains(strings.ToLower(text), "saldo") {
			t.Errorf("says saldo, which is the Vozko wallet: %q", text)
		}
	}
}

func TestOnlyProblemsAreNotified(t *testing.T) {
	publisher := &notificationtest.Publisher{}
	alerts := NewFundsAlerts(publisher, "https://app.vozko.test")
	for _, level := range []ads.FundsLevel{ads.FundsOK, ads.FundsUnknown} {
		if err := alerts.Alert(context.Background(), alertAccount(), ads.Funds{Level: level, Reason: ads.ReasonBillingUnreadable}); err != nil {
			t.Fatal(err)
		}
	}
	if drafts := publisher.Drafts(); len(drafts) != 0 {
		t.Fatalf("published %+v", drafts)
	}
}

func TestAFailedNotificationIsReported(t *testing.T) {
	publisher := &notificationtest.Publisher{Err: errors.New("db down")}
	room := int64(0)
	err := NewFundsAlerts(publisher, "https://app.vozko.test").Alert(context.Background(), alertAccount(), ads.Funds{Level: ads.FundsOut, Reason: ads.ReasonFundsOut, RoomMinor: &room})
	if err == nil {
		t.Fatal("a failed notification must be reported so the next sync retries it")
	}
}

func TestMinorAmountsReadLikeMoney(t *testing.T) {
	cases := map[string]string{"BRL": "R$ 25,10", "USD": "US$ 25,10", "EUR": "EUR 25,10"}
	for currency, want := range cases {
		if got := formatMinor(currency, 2510); got != want {
			t.Errorf("%s: %q", currency, got)
		}
	}
	if got := formatMinor("JPY", 2510); got != "JPY 2510" {
		t.Errorf("zero decimal currency: %q", got)
	}
}
