package emailcampaign_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/emailcampaign"
	"vozko/domain/emailhygiene"
	"vozko/domain/emailsending"
	"vozko/usecases/campaignqueue"
)

func TestReviewSkipsWhatCannotBeMailedWithItsOwnReason(t *testing.T) {
	w := newWorld(t)
	w.audience(
		person{email: "ok@loja.com.br"},
		person{email: "gone@loja.com.br"},
		person{email: "ana@nomail.com.br"},
		person{email: "ana@mailinator.com"},
		person{email: "ana@slow.com.br"},
		person{email: "ana@sunset.com.br"},
		person{email: "contato@gmial.com"},
	)
	long := now.AddDate(-1, 0, 0)
	w.hygiene.standing["gone@loja.com.br"] = emailhygiene.Standing{BadAddress: true, Domain: emailhygiene.DomainAccepts}
	w.hygiene.standing["ana@nomail.com.br"] = emailhygiene.Standing{Domain: emailhygiene.DomainNoMail}
	w.hygiene.standing["ana@slow.com.br"] = emailhygiene.Standing{Domain: emailhygiene.DomainUnknown}
	w.hygiene.standing["ana@sunset.com.br"] = emailhygiene.Standing{Domain: emailhygiene.DomainAccepts, Activity: emailhygiene.Activity{Known: true, FirstDeliveredAt: &long, SunsetSince: &long, CampaignsSinceEngaged: 8}}
	w.hygiene.facts["ana@mailinator.com"] = emailhygiene.Facts{Disposable: true}
	w.hygiene.facts["contato@gmial.com"] = emailhygiene.Facts{Suggestion: "gmail.com", Role: true}
	c := w.draft(nil)
	r, err := w.svc.Review(context.Background(), w.actor(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := map[emailcampaign.SkipReason]int{
		emailcampaign.SkipBadAddress: 1, emailcampaign.SkipNoMailDomain: 1, emailcampaign.SkipDisposable: 1,
		emailcampaign.SkipDomainUnknown: 1, emailcampaign.SkipInactive: 1,
	}
	if r.Audience.Sendable != 2 || len(r.Audience.Skipped) != len(want) {
		t.Fatalf("audience = %+v, a typo and a role address are warnings and still go out", r.Audience)
	}
	for reason, n := range want {
		if r.Audience.Skipped[reason] != n {
			t.Fatalf("skipped[%s] = %d, want %d", reason, r.Audience.Skipped[reason], n)
		}
	}
	if w.hygiene.reads != 1 {
		t.Fatalf("standing reads = %d, one read per chunk in place of the suppression read", w.hygiene.reads)
	}
}

func TestAnUncheckedDomainIsQueuedAndBlocksTheLaunchUntilItIsKnown(t *testing.T) {
	w := newWorld(t)
	w.audience(person{email: "ok@loja.com.br"}, person{email: "ana@new.com.br"})
	w.hygiene.standing["ana@new.com.br"] = emailhygiene.Standing{Domain: emailhygiene.DomainPending, DomainStale: true}
	c := w.draft(nil)
	r, err := w.svc.Review(context.Background(), w.actor(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r.Audience.Skipped[emailcampaign.SkipDomainChecking] != 1 || len(w.hygiene.requested) == 0 || w.hygiene.requested[0] != "new.com.br" {
		t.Fatalf("audience = %+v requested = %v", r.Audience, w.hygiene.requested)
	}
	if r.Check.Ready() || r.Check.Blocking[0].Code != emailcampaign.IssueDomainsChecking {
		t.Fatalf("blocking = %+v", r.Check.Blocking)
	}
	_, err = w.svc.Launch(context.Background(), w.actor(), c.ID, nil)
	var blocked *LaunchBlockedError
	if !errors.As(err, &blocked) {
		t.Fatalf("err = %v, nothing goes out while a domain is unchecked", err)
	}
	w.hygiene.standing["ana@new.com.br"] = emailhygiene.Standing{Domain: emailhygiene.DomainAccepts, Activity: w.hygiene.standing["ok@loja.com.br"].Activity}
	delete(w.hygiene.standing, "ana@new.com.br")
	if _, err := w.svc.Launch(context.Background(), w.actor(), c.ID, nil); err != nil {
		t.Fatalf("once the domain is known the launch goes ahead: %v", err)
	}
}

func TestASendRefusedByTheSharedListSkipsWithItsReason(t *testing.T) {
	w := newWorld(t)
	w.audience(person{email: "a@loja.com.br"})
	c := w.draft(nil)
	if _, err := w.svc.Launch(context.Background(), w.actor(), c.ID, nil); err != nil {
		t.Fatal(err)
	}
	w.gateway.err = emailsending.SuppressedError{Reason: emailsending.SuppressBadAddress}
	w.svc.handle(campaignqueue.Message{CampaignID: c.ID, EntryID: w.recips.byAddress(c.ID)["a@loja.com.br"].ID})
	rows := w.recips.byAddress(c.ID)
	if rows["a@loja.com.br"].Status != emailcampaign.RecipientSkipped || rows["a@loja.com.br"].SkipReason != emailcampaign.SkipBadAddress {
		t.Fatalf("recipient = %+v", rows["a@loja.com.br"])
	}
}

func TestAScheduledStartWaitingForDomainChecksTriesAgainInAMinute(t *testing.T) {
	w := newWorld(t)
	w.audience(person{email: "a@loja.com.br"}, person{email: "b@new.com.br"})
	c := w.draft(nil)
	at := now.Add(2 * time.Hour)
	w.hygiene.standing["b@new.com.br"] = emailhygiene.Standing{Domain: emailhygiene.DomainPending, DomainStale: true}
	if _, err := w.svc.Launch(context.Background(), w.actor(), c.ID, &at); err != nil {
		t.Fatalf("a domain still being checked never blocks a schedule: %v", err)
	}
	w.svc.deps.Now = func() time.Time { return at.Add(time.Minute) }
	if err := w.svc.StartScheduled(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := w.campaign(c.ID)
	if got.Status != emailcampaign.StatusScheduled || got.ScheduledAt == nil || !got.ScheduledAt.Equal(at.Add(2*time.Minute)) {
		t.Fatalf("campaign = %+v, it waits a minute instead of failing", got)
	}
	if len(w.notes.drafts) != 0 {
		t.Fatal("nobody is told a schedule failed when it is only waiting")
	}
}
