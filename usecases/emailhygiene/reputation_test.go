package emailhygiene

import (
	"context"
	"testing"
	"time"

	"vozko/domain/emailaddress"
	h "vozko/domain/emailhygiene"
	es "vozko/domain/emailsending"
)

func newReputation() (*Reputation, *fakeBad, *fakeActivity) {
	bad, activity := &fakeBad{}, &fakeActivity{}
	return NewReputation(ReputationDeps{Bad: bad, Activity: activity}), bad, activity
}

func message(kind es.RunKind) es.Message {
	return es.Message{ID: "m1", WorkspaceID: "ws", Address: "ana@loja.com.br", RunKind: kind}
}

func TestAHardBounceGoesToTheSharedListEvenOnReplay(t *testing.T) {
	r, bad, _ := newReputation()
	ev := es.DeliveryEvent{Kind: es.EventBounce, OccurredAt: clock, Bounce: &es.Bounce{Kind: es.BounceHard, SubType: "NoEmail"}}
	replayed := es.EventEffect{Changed: true, Suppress: true, SuppressReason: es.SuppressHardBounce}
	reason, err := r.Record(context.Background(), message(es.RunCampaign), ev, replayed)
	if err != nil || reason != "" {
		t.Fatalf("reason=%q err=%v", reason, err)
	}
	if len(bad.added) != 1 || bad.added[0] != h.HashOf("ana@loja.com.br") {
		t.Fatalf("added = %v", bad.added)
	}
}

func TestAComplaintStaysWithTheWorkspace(t *testing.T) {
	r, bad, _ := newReputation()
	ev := es.DeliveryEvent{Kind: es.EventComplaint, OccurredAt: clock}
	if _, err := r.Record(context.Background(), message(es.RunCampaign), ev, es.EventEffect{Suppress: true, SuppressReason: es.SuppressComplaint}); err != nil {
		t.Fatal(err)
	}
	if len(bad.added) != 0 {
		t.Fatal("a complaint is about one sender and never reaches the shared list")
	}
}

func TestSoftBouncesSuppressOnlyAfterTheStreak(t *testing.T) {
	cases := []struct {
		name   string
		bounce es.Bounce
		streak h.SoftStreak
		want   es.SuppressReason
		counts bool
	}{
		{name: "a short streak only counts", bounce: es.Bounce{Kind: es.BounceSoft, SubType: "MailboxFull"}, streak: h.SoftStreak{Count: 3, StartedAt: clock.Add(-20 * 24 * time.Hour)}, counts: true},
		{name: "five over two weeks suppresses for this workspace", bounce: es.Bounce{Kind: es.BounceSoft, SubType: "MailboxFull"}, streak: h.SoftStreak{Count: 5, StartedAt: clock.Add(-15 * 24 * time.Hour)}, want: es.SuppressSoftBounce, counts: true},
		{name: "a message problem is not counted", bounce: es.Bounce{Kind: es.BounceSoft, SubType: "ContentRejected"}, streak: h.SoftStreak{Count: 9, StartedAt: clock.Add(-60 * 24 * time.Hour)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, _, activity := newReputation()
			activity.streak = tc.streak
			b := tc.bounce
			got, err := r.Record(context.Background(), message(es.RunCampaign), es.DeliveryEvent{Kind: es.EventBounce, OccurredAt: clock, Bounce: &b}, es.EventEffect{Changed: true})
			if err != nil || got != tc.want {
				t.Fatalf("reason=%q err=%v, want %q", got, err, tc.want)
			}
			if (len(activity.soft) == 1) != tc.counts {
				t.Fatalf("streak updates = %v, want counted %v", activity.soft, tc.counts)
			}
		})
	}
}

func TestDeliveriesAndEngagementFeedTheActivity(t *testing.T) {
	r, _, activity := newReputation()
	ctx := context.Background()
	if _, err := r.Record(ctx, message(es.RunCampaign), es.DeliveryEvent{Kind: es.EventDelivery, OccurredAt: clock}, es.EventEffect{}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Record(ctx, message(es.RunTriggered), es.DeliveryEvent{Kind: es.EventDelivery, OccurredAt: clock}, es.EventEffect{}); err != nil {
		t.Fatal(err)
	}
	if len(activity.delivered) != 2 || activity.delivered[0] != "ws:m1:true" || activity.delivered[1] != "ws:m1:false" {
		t.Fatalf("delivered = %v, only campaigns count toward the sunset", activity.delivered)
	}
	for _, ev := range []es.DeliveryEvent{
		{Kind: es.EventOpen, OccurredAt: clock, UserAgent: "Mozilla/5.0"},
		{Kind: es.EventOpen, OccurredAt: clock, IsBot: true},
		{Kind: es.EventClick, OccurredAt: clock, IsBot: true},
	} {
		if _, err := r.Record(ctx, message(es.RunCampaign), ev, es.EventEffect{}); err != nil {
			t.Fatal(err)
		}
	}
	if len(activity.engaged) != 0 {
		t.Fatalf("machine opens and bot clicks never engage, got %v", activity.engaged)
	}
	if _, err := r.Record(ctx, message(es.RunCampaign), es.DeliveryEvent{Kind: es.EventClick, OccurredAt: clock}, es.EventEffect{}); err != nil {
		t.Fatal(err)
	}
	if err := r.Replied(ctx, "ws", emailaddress.Address("ana@loja.com.br"), clock); err != nil {
		t.Fatal(err)
	}
	if len(activity.engaged) != 2 {
		t.Fatalf("a click and a reply engage, got %v", activity.engaged)
	}
}

func TestAddressStatusExplainsTheVerdictAndQueuesAnUncheckedDomain(t *testing.T) {
	checks, kick := newChecks(), &fakeKick{}
	disposable, err := NewDisposable(DisposableDeps{Store: &fakeLists{}, Source: fakeSource{}}, listWith("mailinator.com"))
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(ServiceDeps{
		Standing:   fakeStanding{rows: map[emailaddress.Address]h.Standing{"ana@loja.com.br": {Domain: h.DomainAccepts, BadAddress: true}}},
		Domains:    newDomains(newResolver(), checks, kick),
		Disposable: disposable,
	})
	svc.now = func() time.Time { return clock }
	ctx := context.Background()
	got, err := svc.AddressStatus(ctx, "ws", "Ana@Loja.com.br")
	if err != nil {
		t.Fatal(err)
	}
	if got.Address != "ana@loja.com.br" || got.Verdict != h.VerdictBadAddress || got.Standing.Refusal() != es.SuppressBadAddress {
		t.Fatalf("status = %+v", got)
	}
	got, err = svc.AddressStatus(ctx, "ws", "contato@gmial.com")
	if err != nil {
		t.Fatal(err)
	}
	if got.Verdict != h.VerdictDomainPending || got.Facts.Suggestion != "gmail.com" || !got.Facts.Role || got.Suggested != "contato@gmail.com" {
		t.Fatalf("status = %+v", got)
	}
	if kick.kicks != 1 {
		t.Fatal("an unchecked domain is queued without any lookup in the request")
	}
	if _, err := svc.AddressStatus(ctx, "ws", "not an address"); err != ErrAddressInvalid {
		t.Fatalf("err = %v", err)
	}
}

func TestFactsAreReadFromTheInMemorySets(t *testing.T) {
	disposable, err := NewDisposable(DisposableDeps{Store: &fakeLists{}, Source: fakeSource{}}, listWith("mailinator.com"))
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(ServiceDeps{Disposable: disposable})
	if !svc.Facts("x@inbox.mailinator.com").Disposable {
		t.Fatal("a subdomain of a disposable provider is disposable")
	}
}

func TestAssessManyResolvesEachDistinctDomainOnceAndJudgesEveryAddress(t *testing.T) {
	r, checks := newResolver(), newChecks()
	r.set("nomail.com.br", h.DNSAnswer{MX: h.LookupFound, MXHosts: []string{"."}})
	disposable, err := NewDisposable(DisposableDeps{Store: &fakeLists{}, Source: fakeSource{}}, listWith("mailinator.com"))
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(ServiceDeps{
		Standing:   fakeStanding{rows: map[emailaddress.Address]h.Standing{"gone@loja.com.br": {BadAddress: true, Domain: h.DomainAccepts}}},
		Domains:    newDomains(r, checks, &fakeKick{}),
		Disposable: disposable,
	})
	svc.now = func() time.Time { return clock }
	got, err := svc.AssessMany(context.Background(), "ws", []emailaddress.Address{
		"ana@loja.com.br", "bia@loja.com.br", "gone@loja.com.br", "x@nomail.com.br", "y@mailinator.com", "contato@gmial.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[emailaddress.Address]h.Verdict{
		"ana@loja.com.br": h.VerdictValid, "bia@loja.com.br": h.VerdictValid, "gone@loja.com.br": h.VerdictBadAddress,
		"x@nomail.com.br": h.VerdictNoMailDomain, "y@mailinator.com": h.VerdictDisposable, "contato@gmial.com": h.VerdictTypo,
	}
	for a, v := range want {
		if got[a].Verdict != v {
			t.Fatalf("%s = %s, want %s", a, got[a].Verdict, v)
		}
	}
	if got["contato@gmial.com"].Suggested != "contato@gmail.com" {
		t.Fatalf("typo suggestion = %q", got["contato@gmial.com"].Suggested)
	}
	if r.count("loja.com.br") != 1 {
		t.Fatalf("loja.com.br looked up %d times, once per distinct domain", r.count("loja.com.br"))
	}
}
