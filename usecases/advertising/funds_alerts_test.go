package advertising

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	ads "vozko/domain/advertising"
	"vozko/domain/notification"
)

type capturedNotifications struct {
	sent []notification.Notification
	err  error
}

func (c *capturedNotifications) Notify(n notification.Notification) error {
	c.sent = append(c.sent, n)
	return c.err
}

func alertAccount() *ads.AdAccount {
	return &ads.AdAccount{ID: "acc-1", WorkspaceID: "ws-1", MetaAccountID: "1762972444913096", BusinessID: "biz-1", Name: "Vozko CRM BRL", Currency: "BRL"}
}

func TestEachFundsProblemSendsOneEmailPerDayWithTheFixAtMeta(t *testing.T) {
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
		notifier := &capturedNotifications{}
		alerts := NewFundsAlerts(notifier, "https://app.vozko.test/dashboard")
		funds := ads.Funds{Level: ads.FundsLow, Reason: reason, RoomMinor: &room}
		if err := alerts.Alert(context.Background(), alertAccount(), funds); err != nil {
			t.Fatalf("%s: %v", reason, err)
		}
		if len(notifier.sent) != 1 {
			t.Fatalf("%s: sent %d", reason, len(notifier.sent))
		}
		n := notifier.sent[0]
		if n.WorkspaceID != "ws-1" || n.Template != fundsAlertTemplate || n.DedupKey != "meta_ads_funds:acc-1:"+string(reason) || n.DedupTTL != 24*time.Hour {
			t.Fatalf("%s: notification %+v", reason, n)
		}
		message, _ := n.Placeholders["Message"].(string)
		if !strings.Contains(message, phrase) {
			t.Errorf("%s: message %q lacks %q", reason, message, phrase)
		}
		url, _ := n.Placeholders["ActionURL"].(string)
		limit := reason == ads.ReasonSpendLimitLow || reason == ads.ReasonSpendLimitReached
		if limit && url != "https://app.vozko.test/dashboard/advertising/overview?account=acc-1" {
			t.Errorf("%s: the limit is changed in Vozko, got %q", reason, url)
		}
		if !limit && !strings.HasPrefix(url, "https://business.facebook.com/") {
			t.Errorf("%s: funds and payments are fixed in Meta billing, got %q", reason, url)
		}
		if n.Placeholders["AccountName"] != "Vozko CRM BRL" || n.Placeholders["ManagerURL"] != "https://app.vozko.test/dashboard/advertising?account=acc-1" {
			t.Errorf("%s: placeholders %+v", reason, n.Placeholders)
		}
	}
}

func TestAFundsAlertNeverCallsMetaMoneyOnlySaldo(t *testing.T) {
	notifier := &capturedNotifications{}
	room := int64(0)
	_ = NewFundsAlerts(notifier, "https://app.vozko.test/dashboard").Alert(context.Background(), alertAccount(), ads.Funds{Level: ads.FundsOut, Reason: ads.ReasonFundsOut, RoomMinor: &room})
	for key, value := range notifier.sent[0].Placeholders {
		text, _ := value.(string)
		if strings.Contains(strings.ToLower(text), "saldo") {
			t.Errorf("%s says saldo, which is the Vozko wallet: %q", key, text)
		}
	}
}

func TestOnlyProblemsAreEmailed(t *testing.T) {
	notifier := &capturedNotifications{}
	alerts := NewFundsAlerts(notifier, "https://app.vozko.test/dashboard")
	for _, level := range []ads.FundsLevel{ads.FundsOK, ads.FundsUnknown} {
		if err := alerts.Alert(context.Background(), alertAccount(), ads.Funds{Level: level, Reason: ads.ReasonBillingUnreadable}); err != nil {
			t.Fatal(err)
		}
	}
	if len(notifier.sent) != 0 {
		t.Fatalf("sent %+v", notifier.sent)
	}
}

func TestAFailedEmailIsReported(t *testing.T) {
	notifier := &capturedNotifications{err: errors.New("queue down")}
	room := int64(0)
	err := NewFundsAlerts(notifier, "https://app.vozko.test/dashboard").Alert(context.Background(), alertAccount(), ads.Funds{Level: ads.FundsOut, Reason: ads.ReasonFundsOut, RoomMinor: &room})
	if err == nil {
		t.Fatal("a failed email must be reported so the next sync retries it")
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
