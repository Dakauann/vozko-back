package emailcampaign_usecase

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"vozko/domain/emailcampaign"
)

func TestADraftThatPassesTheLaunchCheckIsLeftReadyWithoutMoney(t *testing.T) {
	w := newWorld(t)
	w.audience(person{email: "a@x.com", subscribed: true})
	c := w.draft(nil)
	got, err := w.svc.Ready(context.Background(), w.actor(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != emailcampaign.StatusReady || len(w.billing.charges()) != 0 || len(w.pub.entryIDs()) != 0 || len(w.recips.byAddress(c.ID)) != 0 {
		t.Fatalf("ready = %+v, charges %v", got, w.billing.charges())
	}
	if _, err := w.svc.Ready(context.Background(), w.actor(), c.ID); !errors.Is(err, emailcampaign.ErrNotDraft) {
		t.Fatalf("ready twice = %v", err)
	}
}

func TestADraftThatFailsTheLaunchCheckIsNotLeftReady(t *testing.T) {
	w := newWorld(t)
	w.audience(person{email: "a@x.com", optedOut: true})
	c := w.draft(nil)
	_, err := w.svc.Ready(context.Background(), w.actor(), c.ID)
	var blocked *LaunchBlockedError
	if !errors.As(err, &blocked) || blocked.Report.Blocking[0].Code != emailcampaign.IssueAudienceEmpty || w.campaign(c.ID).Status != emailcampaign.StatusDraft {
		t.Fatalf("err = %v, status %s", err, w.campaign(c.ID).Status)
	}
}

func TestStartingAReadyCampaignChecksAgainThenLaunchesLikeADraft(t *testing.T) {
	w := newWorld(t)
	w.audience(person{email: "a@x.com", subscribed: true})
	c := w.draft(nil)
	if _, err := w.svc.Ready(context.Background(), w.actor(), c.ID); err != nil {
		t.Fatal(err)
	}
	w.audience(person{email: "a@x.com", optedOut: true})
	_, err := w.svc.Launch(context.Background(), w.actor(), c.ID, nil)
	var blocked *LaunchBlockedError
	if !errors.As(err, &blocked) || blocked.Report.Blocking[0].Code != emailcampaign.IssueAudienceEmpty || w.campaign(c.ID).Status != emailcampaign.StatusReady {
		t.Fatalf("a ready campaign whose audience emptied = %v", err)
	}
	w.audience(person{email: "a@x.com", subscribed: true}, person{email: "b@x.com", subscribed: true})
	got, err := w.svc.Launch(context.Background(), w.actor(), c.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != emailcampaign.StatusRunning || len(w.billing.charges()) != 0 || len(w.pub.entryIDs()) != 2 {
		t.Fatalf("started = %+v, enqueued %v", got, w.pub.entryIDs())
	}
}

func TestAReadyCampaignIsEditedBackToDraftScheduledOrDeleted(t *testing.T) {
	w := newWorld(t)
	w.audience(person{email: "a@x.com", subscribed: true})
	ready := func() emailcampaign.Campaign {
		c := w.draft(nil)
		if _, err := w.svc.Ready(context.Background(), w.actor(), c.ID); err != nil {
			t.Fatal(err)
		}
		return c
	}
	edited := ready()
	got, err := w.svc.Update(context.Background(), w.actor(), edited.ID, emailcampaign.Draft{Name: "Outro nome", Subject: "Oferta", Document: readyDocument(), Selection: edited.Selection})
	if err != nil || got.Status != emailcampaign.StatusDraft || got.Name != "Outro nome" {
		t.Fatalf("edited = %+v, %v", got, err)
	}
	scheduled := ready()
	at := now.Add(48 * time.Hour)
	got, err = w.svc.Launch(context.Background(), w.actor(), scheduled.ID, &at)
	if err != nil || got.Status != emailcampaign.StatusScheduled {
		t.Fatalf("scheduled = %+v, %v", got, err)
	}
	deleted := ready()
	if !slices.Contains(emailcampaign.AllowedActions(w.campaign(deleted.ID).Status), emailcampaign.CommandLaunch) {
		t.Fatal("a ready campaign offers the start")
	}
	if err := w.svc.Delete(context.Background(), w.actor(), deleted.ID); err != nil {
		t.Fatalf("delete = %v", err)
	}
}
