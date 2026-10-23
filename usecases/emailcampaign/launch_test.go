package emailcampaign_usecase

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"vozko/domain/campaign"
	"vozko/domain/emailcampaign"
	"vozko/domain/emailsending"
)

func TestNewRefusesMissingDependenciesAndPlainHTTPBase(t *testing.T) {
	if _, err := New(Deps{}); !errors.Is(err, ErrIncomplete) {
		t.Fatalf("err = %v", err)
	}
	w := newWorld(t)
	deps := w.svc.deps
	deps.PublicBaseURL = "http://api.example.com"
	if _, err := New(deps); !errors.Is(err, ErrPublicBaseURL) {
		t.Fatalf("err = %v", err)
	}
}

func TestReviewSortsTheAudience(t *testing.T) {
	w := newWorld(t)
	w.audience(
		person{email: "ok@x.com", subscribed: true},
		person{email: ""},
		person{email: "not an email"},
		person{email: "OK@x.com", subscribed: true},
		person{email: "nosub@x.com"},
		person{email: "optout@x.com", subscribed: true, optedOut: true},
		person{email: "blocked@x.com", subscribed: true, blocked: true},
		person{email: "bounced@x.com", subscribed: true, suppressed: true},
		person{email: "left@x.com", unsubscribed: true},
		person{email: "plain@x.com"},
	)
	c := w.draft(nil)
	r, err := w.svc.Review(context.Background(), w.actor(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	a := r.Audience
	if a.Matched != 10 || a.WithEmail != 9 || a.Sendable != 3 {
		t.Fatalf("audience = %+v", a)
	}
	want := map[emailcampaign.SkipReason]int{
		emailcampaign.SkipNoEmail: 1, emailcampaign.SkipInvalidAddress: 1, emailcampaign.SkipDuplicate: 1, emailcampaign.SkipUnsubscribed: 1,
		emailcampaign.SkipOptedOut: 1, emailcampaign.SkipBlocked: 1, emailcampaign.SkipSuppressed: 1,
	}
	if len(a.Skipped) != len(want) {
		t.Errorf("skipped = %v, want %v", a.Skipped, want)
	}
	for reason, n := range want {
		if a.Skipped[reason] != n {
			t.Errorf("skipped[%s] = %d, want %d", reason, a.Skipped[reason], n)
		}
	}
	if r.Send.Units != 3 || r.Send.TotalMicros != 3*583 {
		t.Fatalf("send quote = %+v", r.Send)
	}
	if len(r.Check.Blocking) != 0 {
		t.Fatalf("blocking = %+v", r.Check.Blocking)
	}
}

func TestReviewFlagsAShortWalletAndSenderProblems(t *testing.T) {
	w := newWorld(t)
	w.audience(person{email: "ok@x.com", subscribed: true})
	*w.wallet = 100
	w.accounts.status = emailsending.AccountPausedByProvider
	c := w.draft(nil)
	r, err := w.svc.Review(context.Background(), w.actor(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r.MissingMicros != 483 || r.BalanceMicros != 100 {
		t.Fatalf("review = %+v", r)
	}
	codes := map[emailcampaign.IssueCode]bool{}
	for _, issue := range r.Check.Blocking {
		codes[issue.Code] = true
	}
	if !codes[emailcampaign.IssueBalanceInsufficient] || !codes[emailcampaign.IssueAccountPaused] {
		t.Fatalf("blocking = %+v", r.Check.Blocking)
	}
}

func TestLaunchMaterializesAndEnqueuesWithoutCharging(t *testing.T) {
	w := newWorld(t)
	w.audience(
		person{email: "a@x.com", subscribed: true},
		person{email: "b@x.com", subscribed: true},
		person{email: "c@x.com"},
	)
	c := w.draft(nil)
	got, err := w.svc.Launch(context.Background(), w.actor(), c.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != emailcampaign.StatusRunning || got.LaunchedBy != "user-1" {
		t.Fatalf("campaign = %+v", got)
	}
	if charges := w.billing.charges(); len(charges) != 0 {
		t.Fatalf("a launch charges nothing, the first batch is charged before the first send: %v", charges)
	}
	rows := w.recips.byAddress(c.ID)
	if len(rows) != 3 || rows["a@x.com"].Status != emailcampaign.RecipientPending || rows["c@x.com"].Status != emailcampaign.RecipientPending {
		t.Fatalf("an address without a stored subscription is sent too: %+v", rows)
	}
	if ids := w.pub.entryIDs(); len(ids) != 3 {
		t.Fatalf("enqueued = %v", ids)
	}
	if !w.sub.subscribed[QueueNamespace.DispatchTopic(c.ID)] {
		t.Fatal("the consumer subscribes to the campaign")
	}
	if _, err := w.svc.Launch(context.Background(), w.actor(), c.ID, nil); !errors.Is(err, campaign.ErrAlreadyRunning) {
		t.Fatalf("a second launch err = %v", err)
	}
}

func TestLaunchRefusesBlockedCampaignsWithTheIssues(t *testing.T) {
	w := newWorld(t)
	w.audience(person{email: "a@x.com", optedOut: true})
	c := w.draft(nil)
	_, err := w.svc.Launch(context.Background(), w.actor(), c.ID, nil)
	var blocked *LaunchBlockedError
	if !errors.As(err, &blocked) || blocked.Report.Blocking[0].Code != emailcampaign.IssueAudienceEmpty {
		t.Fatalf("err = %v", err)
	}
	if w.campaign(c.ID).Status != emailcampaign.StatusDraft || len(w.billing.charges()) != 0 {
		t.Fatal("a refused launch changes nothing")
	}
}

func TestLaunchIsRefusedWhenTheWalletDoesNotCoverTheFirstBatch(t *testing.T) {
	w := newWorld(t)
	var people []person
	for i := 0; i < 1500; i++ {
		people = append(people, person{email: fmt.Sprintf("p%d@x.com", i), subscribed: true})
	}
	w.audience(people...)
	c := w.draft(nil)
	*w.wallet = 1000*583 - 1
	_, err := w.svc.Launch(context.Background(), w.actor(), c.ID, nil)
	var blocked *LaunchBlockedError
	if !errors.As(err, &blocked) || len(blocked.Report.Blocking) != 1 || blocked.Report.Blocking[0].Code != emailcampaign.IssueBalanceInsufficient || blocked.Report.Blocking[0].MissingMicros != 1 {
		t.Fatalf("err = %v", err)
	}
	if got := w.campaign(c.ID); got.Status != emailcampaign.StatusDraft {
		t.Fatalf("status = %s", got.Status)
	}
	if len(w.pub.entryIDs()) != 0 {
		t.Fatal("nothing is enqueued without payment")
	}
}

func TestLaunchWithABTestEnqueuesOnlyTheTestShare(t *testing.T) {
	w := newWorld(t)
	var people []person
	for i := 0; i < 200; i++ {
		people = append(people, person{email: fmt.Sprintf("p%d@x.com", i), subscribed: true})
	}
	w.audience(people...)
	ab := &emailcampaign.ABTest{VariantBSubject: "Outra", TestPercent: 20, WinnerMetric: emailcampaign.WinnerOpens, DecideAfterMinutes: 60}
	c := w.draft(ab)
	if _, err := w.svc.Launch(context.Background(), w.actor(), c.ID, nil); err != nil {
		t.Fatal(err)
	}
	counts := map[emailcampaign.Variant]int{}
	for _, r := range w.recips.byAddress(c.ID) {
		counts[r.Variant]++
	}
	if got := len(w.pub.entryIDs()); got != counts[emailcampaign.VariantA]+counts[emailcampaign.VariantB] || counts[emailcampaign.VariantWinner] == 0 {
		t.Fatalf("enqueued %d, variants %+v", got, counts)
	}
}

func TestScheduleAndTheScheduledStart(t *testing.T) {
	w := newWorld(t)
	w.audience(person{email: "a@x.com", subscribed: true})
	c := w.draft(nil)
	past := now.Add(-time.Minute)
	var blocked *LaunchBlockedError
	if _, err := w.svc.Launch(context.Background(), w.actor(), c.ID, &past); !errors.As(err, &blocked) || blocked.Report.Blocking[0].Code != emailcampaign.IssueScheduleInPast {
		t.Fatalf("err = %v", err)
	}
	at := now.Add(2 * time.Hour)
	got, err := w.svc.Launch(context.Background(), w.actor(), c.ID, &at)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != emailcampaign.StatusScheduled || !got.ScheduledAt.Equal(at) || len(w.billing.charges()) != 0 {
		t.Fatalf("scheduled = %+v", got)
	}
	w.svc.deps.Now = func() time.Time { return at.Add(time.Minute) }
	if err := w.svc.StartScheduled(context.Background()); err != nil {
		t.Fatal(err)
	}
	if st := w.campaign(c.ID).Status; st != emailcampaign.StatusRunning {
		t.Fatalf("status after the schedule = %s", st)
	}
}

func TestAFailedScheduledStartReturnsToDraftWithTheReason(t *testing.T) {
	w := newWorld(t)
	w.audience(person{email: "a@x.com", subscribed: true})
	c := w.draft(nil)
	at := now.Add(2 * time.Hour)
	if _, err := w.svc.Launch(context.Background(), w.actor(), c.ID, &at); err != nil {
		t.Fatal(err)
	}
	*w.wallet = 0
	w.billing.balance = 0
	w.svc.deps.Now = func() time.Time { return at.Add(time.Minute) }
	if err := w.svc.StartScheduled(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := w.campaign(c.ID)
	if got.Status != emailcampaign.StatusDraft || got.HoldReason != emailcampaign.HoldReason(emailcampaign.IssueBalanceInsufficient) {
		t.Fatalf("campaign = %+v", got)
	}
	if len(w.notes.drafts) != 1 {
		t.Fatal("the team is told the schedule failed")
	}
}

func TestOnlyDraftsAndScheduledCampaignsAreEditable(t *testing.T) {
	w := newWorld(t)
	w.audience(person{email: "a@x.com", subscribed: true})
	c := w.draft(nil)
	if _, err := w.svc.Update(context.Background(), w.actor(), c.ID, emailcampaign.Draft{Name: "Novo nome", Document: readyDocument()}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.Update(context.Background(), w.actor(), c.ID, emailcampaign.Draft{Name: ""}); !errors.Is(err, emailcampaign.ErrNameRequired) {
		t.Fatalf("err = %v", err)
	}
	if _, err := w.svc.Update(context.Background(), w.actor(), c.ID, emailcampaign.Draft{Name: "Pronta", Subject: "Oferta", Document: readyDocument(), Selection: c.Selection}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.Launch(context.Background(), w.actor(), c.ID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.Update(context.Background(), w.actor(), c.ID, emailcampaign.Draft{Name: "x"}); !errors.Is(err, emailcampaign.ErrNotEditable) {
		t.Fatalf("err = %v", err)
	}
}
