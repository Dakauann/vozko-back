package emailcampaign_usecase

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/emailaddress"
	"vozko/domain/emailcampaign"
	"vozko/domain/emailconsent"
	"vozko/domain/emailsending"
	"vozko/usecases/campaignqueue"
)

func launched(t *testing.T, ab *emailcampaign.ABTest, people ...person) (*world, emailcampaign.Campaign) {
	t.Helper()
	w := newWorld(t)
	w.audience(people...)
	c := w.draft(ab)
	if _, err := w.svc.Launch(context.Background(), w.actor(), c.ID, nil); err != nil {
		t.Fatal(err)
	}
	return w, w.campaign(c.ID)
}

func pendingOf(w *world, c emailcampaign.Campaign, address emailaddress.Address) emailcampaign.Recipient {
	return w.recips.byAddress(c.ID)[address]
}

func TestHandleSendsAndPersonalizes(t *testing.T) {
	w, c := launched(t, nil, person{email: "a@x.com", subscribed: true})
	rc := pendingOf(w, c, "a@x.com")
	res := w.svc.handle(campaignqueue.Message{CampaignID: c.ID, EntryID: rc.ID})
	if res.Outcome != campaignqueue.OutcomeDone {
		t.Fatalf("outcome = %+v", res)
	}
	if len(w.gateway.calls) != 1 {
		t.Fatal("one send")
	}
	call := w.gateway.calls[0]
	if call.req.Kind != emailsending.KindMarketing || call.req.RunKind != emailsending.RunCampaign || call.req.RunID != c.ID || call.req.RecipientID != rc.ID {
		t.Fatalf("request = %+v", call.req)
	}
	if call.content.Subject != "Oferta para Pessoa" || !strings.Contains(call.content.HTML, "Olá Pessoa") {
		t.Fatalf("content subject %q", call.content.Subject)
	}
	if !strings.Contains(call.content.HTML, "utm_source=email") || !strings.Contains(call.content.HTML, "utm_campaign=black-friday") {
		t.Fatal("links carry the campaign utm")
	}
	if got := pendingOf(w, c, "a@x.com"); got.Status != emailcampaign.RecipientSent || got.MessageID != "msg-1" {
		t.Fatalf("recipient = %+v", got)
	}
}

func TestHandleOutcomes(t *testing.T) {
	cases := []struct {
		name        string
		err         error
		wantOutcome campaignqueue.Outcome
		wantStatus  emailcampaign.RecipientStatus
		wantSkip    emailcampaign.SkipReason
		wantFailure string
		wantHold    emailcampaign.HoldReason
		wantNotice  bool
	}{
		{name: "suppressed at send", err: emailsending.ErrRecipientSuppressed, wantOutcome: campaignqueue.OutcomeDone, wantStatus: emailcampaign.RecipientSkipped, wantSkip: emailcampaign.SkipSuppressed},
		{name: "daily cap", err: &emailsending.DailyCapError{Cap: 500, ResumeAt: now.Add(3 * time.Hour)}, wantOutcome: campaignqueue.OutcomeRetryLater, wantStatus: emailcampaign.RecipientPending, wantHold: emailcampaign.HoldDailyCap},
		{name: "paused account", err: emailsending.ErrAccountPaused, wantOutcome: campaignqueue.OutcomeRetryLater, wantStatus: emailcampaign.RecipientPending, wantHold: emailcampaign.HoldAccountPaused, wantNotice: true},
		{name: "provider pause", err: &emailsending.SendError{Kind: emailsending.SendErrorPaused}, wantOutcome: campaignqueue.OutcomeRetryLater, wantStatus: emailcampaign.RecipientPending, wantHold: emailcampaign.HoldAccountPaused, wantNotice: true},
		{name: "sender gone", err: emailsending.ErrNoDefaultSender, wantOutcome: campaignqueue.OutcomeRetryLater, wantStatus: emailcampaign.RecipientPending, wantHold: emailcampaign.HoldSenderUnavailable, wantNotice: true},
		{name: "rejected", err: &emailsending.SendError{Kind: emailsending.SendErrorRejected}, wantOutcome: campaignqueue.OutcomeDone, wantStatus: emailcampaign.RecipientFailed, wantFailure: failureRejected},
		{name: "configuration", err: &emailsending.SendError{Kind: emailsending.SendErrorConfiguration}, wantOutcome: campaignqueue.OutcomeDone, wantStatus: emailcampaign.RecipientFailed, wantFailure: failureConfig},
		{name: "transient", err: &emailsending.SendError{Kind: emailsending.SendErrorTransient}, wantOutcome: campaignqueue.OutcomeRetryLater, wantStatus: emailcampaign.RecipientPending},
		{name: "unknown", err: errors.New("boom"), wantOutcome: campaignqueue.OutcomeRetryLater, wantStatus: emailcampaign.RecipientPending},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, c := launched(t, nil, person{email: "a@x.com", subscribed: true})
			w.gateway.err = tc.err
			rc := pendingOf(w, c, "a@x.com")
			res := w.svc.handle(campaignqueue.Message{CampaignID: c.ID, EntryID: rc.ID})
			if res.Outcome != tc.wantOutcome {
				t.Fatalf("outcome = %+v", res)
			}
			got := pendingOf(w, c, "a@x.com")
			if got.Status != tc.wantStatus || got.SkipReason != tc.wantSkip || got.FailureCode != tc.wantFailure {
				t.Fatalf("recipient = %+v", got)
			}
			camp := w.campaign(c.ID)
			if tc.wantHold != "" && (camp.Status != emailcampaign.StatusHeld || camp.HoldReason != tc.wantHold) {
				t.Fatalf("campaign = %s/%s", camp.Status, camp.HoldReason)
			}
			if tc.wantHold == "" && camp.Status != emailcampaign.StatusRunning {
				t.Fatalf("status = %s", camp.Status)
			}
			if (len(w.notes.drafts) > 0) != tc.wantNotice {
				t.Fatalf("notices = %+v", w.notes.drafts)
			}
			if tc.wantHold == emailcampaign.HoldDailyCap && res.Delay != 3*time.Hour {
				t.Fatalf("delay = %v, want until the cap resets", res.Delay)
			}
		})
	}
}

func TestASuccessfulSendReleasesTheHold(t *testing.T) {
	w, c := launched(t, nil,
		person{email: "a@x.com", subscribed: true},
		person{email: "b@x.com", subscribed: true})
	w.gateway.err = emailsending.ErrAccountPaused
	w.svc.handle(campaignqueue.Message{CampaignID: c.ID, EntryID: pendingOf(w, c, "a@x.com").ID})
	w.gateway.err = nil
	w.svc.handle(campaignqueue.Message{CampaignID: c.ID, EntryID: pendingOf(w, c, "a@x.com").ID})
	if got := w.campaign(c.ID); got.Status != emailcampaign.StatusRunning || got.HoldReason != "" {
		t.Fatalf("campaign = %s/%s", got.Status, got.HoldReason)
	}
}

func TestHandleRechecksConsentBeforeSending(t *testing.T) {
	w, c := launched(t, nil, person{email: "a@x.com", subscribed: true})
	sub := w.subs.rows["a@x.com"]
	gone, _, _ := sub.Unsubscribe(emailconsent.UnsubscribeLink, now)
	w.subs.rows["a@x.com"] = gone
	res := w.svc.handle(campaignqueue.Message{CampaignID: c.ID, EntryID: pendingOf(w, c, "a@x.com").ID})
	if res.Outcome != campaignqueue.OutcomeDone || len(w.gateway.calls) != 0 {
		t.Fatalf("outcome = %+v calls = %d", res, len(w.gateway.calls))
	}
	if got := pendingOf(w, c, "a@x.com"); got.SkipReason != emailcampaign.SkipUnsubscribed {
		t.Fatalf("recipient = %+v", got)
	}
}

func TestHandleSendsWithoutAStoredSubscriptionButHonorsAGlobalOptOut(t *testing.T) {
	w, c := launched(t, nil, person{email: "a@x.com"}, person{email: "b@x.com"})
	b := pendingOf(w, c, "b@x.com")
	facts := w.facts[b.LeadID]
	facts.OptedOut = true
	w.facts[b.LeadID] = facts
	if res := w.svc.handle(campaignqueue.Message{CampaignID: c.ID, EntryID: pendingOf(w, c, "a@x.com").ID}); res.Outcome != campaignqueue.OutcomeDone || len(w.gateway.calls) != 1 {
		t.Fatalf("outcome = %+v calls = %d", res, len(w.gateway.calls))
	}
	if res := w.svc.handle(campaignqueue.Message{CampaignID: c.ID, EntryID: b.ID}); res.Outcome != campaignqueue.OutcomeDone || len(w.gateway.calls) != 1 {
		t.Fatalf("outcome = %+v calls = %d", res, len(w.gateway.calls))
	}
	if got := pendingOf(w, c, "b@x.com"); got.SkipReason != emailcampaign.SkipOptedOut {
		t.Fatalf("recipient = %+v", got)
	}
}

func TestHandleRespectsTheCampaignStatus(t *testing.T) {
	w, c := launched(t, nil, person{email: "a@x.com", subscribed: true})
	rc := pendingOf(w, c, "a@x.com")
	if _, err := w.svc.Pause(context.Background(), w.actor(), c.ID); err != nil {
		t.Fatal(err)
	}
	if res := w.svc.handle(campaignqueue.Message{CampaignID: c.ID, EntryID: rc.ID}); res.Outcome != campaignqueue.OutcomeRetryLater {
		t.Fatalf("paused outcome = %+v", res)
	}
	if res := w.svc.handle(campaignqueue.Message{CampaignID: c.ID, EntryID: "nope"}); res.Outcome != campaignqueue.OutcomeRetryLater && res.Outcome != campaignqueue.OutcomeDrop {
		t.Fatalf("outcome = %+v", res)
	}
	if _, err := w.svc.Stop(context.Background(), w.actor(), c.ID); err != nil {
		t.Fatal(err)
	}
	if res := w.svc.handle(campaignqueue.Message{CampaignID: c.ID, EntryID: rc.ID}); res.Outcome != campaignqueue.OutcomeDrop {
		t.Fatalf("stopped outcome = %+v", res)
	}
	if len(w.gateway.calls) != 0 {
		t.Fatal("nothing is sent while paused or stopped")
	}
	if len(w.billing.charges()) != 0 || len(w.billing.batchRefunds) != 0 {
		t.Fatalf("nothing was sent, so nothing is charged or refunded: %v %+v", w.billing.charges(), w.billing.batchRefunds)
	}
}

func TestCompletionSettlesTheUnsentShare(t *testing.T) {
	w, c := launched(t, nil,
		person{email: "a@x.com", subscribed: true},
		person{email: "b@x.com", subscribed: true},
		person{email: "c@x.com", subscribed: true})
	w.svc.handle(campaignqueue.Message{CampaignID: c.ID, EntryID: pendingOf(w, c, "a@x.com").ID})
	if done, _ := w.svc.completeIfDone(context.Background(), c.ID); done {
		t.Fatal("pending recipients keep the campaign open")
	}
	w.gateway.err = emailsending.ErrRecipientSuppressed
	w.svc.handle(campaignqueue.Message{CampaignID: c.ID, EntryID: pendingOf(w, c, "b@x.com").ID})
	w.gateway.err = &emailsending.SendError{Kind: emailsending.SendErrorRejected}
	w.svc.handle(campaignqueue.Message{CampaignID: c.ID, EntryID: pendingOf(w, c, "c@x.com").ID})
	done, err := w.svc.completeIfDone(context.Background(), c.ID)
	if err != nil || !done {
		t.Fatalf("done = %v err = %v", done, err)
	}
	if got := w.campaign(c.ID); got.Status != emailcampaign.StatusCompleted {
		t.Fatalf("status = %s", got.Status)
	}
	if refunds := w.billing.batchRefunds; len(refunds) != 1 || refunds[0].units != 2 {
		t.Fatalf("refunds = %+v, want the suppressed and the rejected one", refunds)
	}
}

func TestTickDecidesTheWinnerAndSendsTheRest(t *testing.T) {
	var people []person
	for i := 0; i < 60; i++ {
		people = append(people, person{email: "p" + string(rune('a'+i%26)) + string(rune('a'+i/26)) + "@x.com", subscribed: true})
	}
	ab := &emailcampaign.ABTest{VariantBSubject: "Assunto B", TestPercent: 50, WinnerMetric: emailcampaign.WinnerOpens, DecideAfterMinutes: 30}
	w, c := launched(t, ab, people...)
	first := len(w.pub.entryIDs())
	var winnerRecipient emailcampaign.Recipient
	for _, r := range w.recips.byAddress(c.ID) {
		if r.Variant == emailcampaign.VariantWinner {
			winnerRecipient = r
		}
	}
	if winnerRecipient.ID == "" {
		t.Skip("the hash put everyone in the test share")
	}
	if res := w.svc.handle(campaignqueue.Message{CampaignID: c.ID, EntryID: winnerRecipient.ID}); res.Outcome != campaignqueue.OutcomeRetryLater {
		t.Fatalf("the remainder waits for the decision, outcome = %+v", res)
	}
	w.metrics.variants = map[emailcampaign.Variant]emailcampaign.VariantResult{emailcampaign.VariantA: {Delivered: 10, Opens: 1}, emailcampaign.VariantB: {Delivered: 10, Opens: 5}}
	w.svc.deps.Now = func() time.Time { return now.Add(31 * time.Minute) }
	if err := w.svc.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := w.campaign(c.ID)
	if got.ABTest.Winner != emailcampaign.VariantB {
		t.Fatalf("winner = %q", got.ABTest.Winner)
	}
	if len(w.pub.entryIDs()) <= first {
		t.Fatal("the remainder is enqueued after the decision")
	}
	w.svc.handle(campaignqueue.Message{CampaignID: c.ID, EntryID: winnerRecipient.ID})
	last := w.gateway.calls[len(w.gateway.calls)-1]
	if last.content.Subject != "Assunto B" {
		t.Fatalf("the remainder uses the winning subject, got %q", last.content.Subject)
	}
}

func TestStatusStoreListsSendingCampaigns(t *testing.T) {
	w, c := launched(t, nil, person{email: "a@x.com", subscribed: true})
	ids, err := statusStore{s: w.svc}.ListRunningCampaignIDs()
	if err != nil || len(ids) != 1 || ids[0] != c.ID {
		t.Fatalf("ids = %v err = %v", ids, err)
	}
	n, err := pendingCounter{s: w.svc}.CountPendingEntries(c.ID)
	if err != nil || n != 1 {
		t.Fatalf("pending = %d err = %v", n, err)
	}
}
