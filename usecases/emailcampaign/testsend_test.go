package emailcampaign_usecase

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"vozko/domain/emailaddress"
	"vozko/domain/emailsending"
	balance_usecase "vozko/usecases/balance"
)

func chargesOf(entries []string) []string {
	var keys []string
	for _, e := range entries {
		if key, ok := strings.CutPrefix(e, "charge "); ok {
			keys = append(keys, key)
		}
	}
	return keys
}

func TestTestSendChargesEveryRecipientBeforeAnySend(t *testing.T) {
	w := newWorld(t)
	c := w.draft(nil)
	got, err := w.svc.TestSend(context.Background(), w.actor(), c.ID, []string{"a@loja.com.br", "b@loja.com.br", "A@loja.com.br"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Outcome != TestSent || got[1].Outcome != TestSent {
		t.Fatalf("results = %+v", got)
	}
	entries := w.journal.all()
	if len(entries) != 4 || !strings.HasPrefix(entries[0], "charge ") || !strings.HasPrefix(entries[1], "charge ") ||
		entries[2] != "send a@loja.com.br" || entries[3] != "send b@loja.com.br" {
		t.Fatalf("journal = %v, want both charges before the sends", entries)
	}
	keys := chargesOf(entries)
	if keys[0] == keys[1] {
		t.Fatalf("each recipient needs its own charge key: %v", keys)
	}
	for _, key := range keys {
		if !strings.HasPrefix(key, "test:"+c.ID+":") {
			t.Fatalf("charge key %q is not scoped to the campaign test", key)
		}
	}
	if len(w.billing.refunds) != 0 {
		t.Fatalf("a delivered test is not refunded: %+v", w.billing.refunds)
	}
}

func TestTestSendRefundsEveryRecipientThatDidNotGoOut(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		outcome string
	}{
		{name: "suppressed", err: emailsending.ErrRecipientSuppressed, outcome: TestSuppressed},
		{name: "daily cap", err: &emailsending.DailyCapError{Cap: 50}, outcome: TestDailyCap},
		{name: "rejected by the provider", err: &emailsending.SendError{Kind: emailsending.SendErrorRejected, Detail: "bad"}, outcome: TestFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			c := w.draft(nil)
			w.gateway.byAddress = map[emailaddress.Address]error{"b@loja.com.br": tc.err}
			got, err := w.svc.TestSend(context.Background(), w.actor(), c.ID, []string{"a@loja.com.br", "b@loja.com.br"})
			if err != nil {
				t.Fatal(err)
			}
			if got[0].Outcome != TestSent || got[1].Outcome != tc.outcome {
				t.Fatalf("results = %+v", got)
			}
			keys := chargesOf(w.journal.all())
			if len(w.billing.refunds) != 1 || w.billing.refunds[0].key != keys[1] {
				t.Fatalf("refunds = %+v, want only the second charge %s", w.billing.refunds, keys[1])
			}
			if w.billing.refunds[0].quote != w.billing.quote(1) {
				t.Fatalf("the refund must use the charged quote, got %+v", w.billing.refunds[0].quote)
			}
		})
	}
}

func TestTestSendRefundsTheRestWhenTheGatewayRefuses(t *testing.T) {
	w := newWorld(t)
	c := w.draft(nil)
	w.gateway.byAddress = map[emailaddress.Address]error{"a@loja.com.br": emailsending.ErrAccountReactivation}
	if _, err := w.svc.TestSend(context.Background(), w.actor(), c.ID, []string{"a@loja.com.br", "b@loja.com.br"}); !errors.Is(err, emailsending.ErrAccountReactivation) {
		t.Fatalf("err = %v", err)
	}
	keys := chargesOf(w.journal.all())
	var refunded []string
	for _, r := range w.billing.refunds {
		refunded = append(refunded, r.key)
	}
	if !slices.Equal(refunded, keys) {
		t.Fatalf("refunded %v, want every charge %v", refunded, keys)
	}
	if slices.Contains(w.journal.all(), "send b@loja.com.br") {
		t.Fatal("no send follows a refused one")
	}
}

func TestTestSendWithAShortWalletSendsNothingAndReportsTheWholeShortfall(t *testing.T) {
	w := newWorld(t)
	c := w.draft(nil)
	w.billing.balance = 583 + 100
	_, err := w.svc.TestSend(context.Background(), w.actor(), c.ID, []string{"a@loja.com.br", "b@loja.com.br", "c@loja.com.br"})
	var short *balance_usecase.InsufficientEmailBalanceError
	if !errors.As(err, &short) {
		t.Fatalf("err = %v, want the insufficient balance refusal", err)
	}
	if short.MissingMicros != 3*583-(583+100) {
		t.Fatalf("missing = %d, want the shortfall of the whole test %d", short.MissingMicros, 3*583-(583+100))
	}
	if len(w.gateway.calls) != 0 {
		t.Fatalf("nothing is sent on a short wallet: %+v", w.gateway.calls)
	}
	if w.billing.balance != 583+100 || len(w.billing.refunds) != 1 {
		t.Fatalf("the charge taken before the shortfall must be refunded: balance %d refunds %+v", w.billing.balance, w.billing.refunds)
	}
}

func TestTestSendRefusesBadRecipientListsBeforeCharging(t *testing.T) {
	cases := []struct {
		name string
		to   []string
		want error
	}{
		{name: "none", to: nil, want: ErrTestRecipients},
		{name: "more than five", to: []string{"a@x.com", "b@x.com", "c@x.com", "d@x.com", "e@x.com", "f@x.com"}, want: ErrTestRecipients},
		{name: "an invalid address", to: []string{"not an email"}, want: ErrTestAddressInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			c := w.draft(nil)
			if _, err := w.svc.TestSend(context.Background(), w.actor(), c.ID, tc.to); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if len(w.journal.all()) != 0 {
				t.Fatalf("nothing is charged or sent: %v", w.journal.all())
			}
		})
	}
	if MaxTestRecipients != 5 {
		t.Fatalf("MaxTestRecipients = %d", MaxTestRecipients)
	}
}
