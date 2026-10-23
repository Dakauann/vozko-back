package emailcampaign_usecase

import (
	"context"
	"errors"
	"slices"
	"testing"

	"vozko/domain/emailcampaign"
	"vozko/domain/emailsending"
	"vozko/usecases/campaignqueue"
)

func TestTheSenderIsReactivatedBeforeTheFirstBatchIsCharged(t *testing.T) {
	w := newWorld(t)
	w.audience(person{email: "a@x.com", subscribed: true})
	c := w.draft(nil)
	if _, err := w.svc.Launch(context.Background(), w.actor(), c.ID, nil); err != nil {
		t.Fatal(err)
	}
	w.svc.handle(campaignqueue.Message{CampaignID: c.ID, EntryID: pendingOf(w, c, "a@x.com").ID})
	entries := w.journal.all()
	revived := slices.Index(entries, "reactivate "+ws)
	charged := slices.Index(entries, "charge batch "+batchKey(c.ID, 1))
	if revived < 0 || charged < 0 || revived > charged {
		t.Fatalf("journal = %v, want the reactivation before the first batch", entries)
	}
}

func TestLaunchThatCannotReactivateChargesNothing(t *testing.T) {
	w := newWorld(t)
	w.audience(person{email: "a@x.com", subscribed: true})
	c := w.draft(nil)
	w.revival.err = emailsending.ErrAccountReactivation
	if _, err := w.svc.Launch(context.Background(), w.actor(), c.ID, nil); !errors.Is(err, emailsending.ErrAccountReactivation) {
		t.Fatalf("err = %v", err)
	}
	if charges := w.billing.charges(); len(charges) != 0 {
		t.Fatalf("charged = %v, want nothing", charges)
	}
	if got := w.campaign(c.ID); got.Status != emailcampaign.StatusDraft {
		t.Fatalf("status = %s, want the draft kept", got.Status)
	}
}

func TestReviewTreatsADormantSenderAsReady(t *testing.T) {
	w := newWorld(t)
	w.audience(person{email: "a@x.com", subscribed: true})
	w.accounts.status = emailsending.AccountDormant
	c := w.draft(nil)
	r, err := w.svc.Review(context.Background(), w.actor(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Check.Blocking) != 0 {
		t.Fatalf("a dormant sender wakes on launch, blocking = %+v", r.Check.Blocking)
	}
}
