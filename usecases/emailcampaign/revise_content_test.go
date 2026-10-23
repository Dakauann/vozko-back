package emailcampaign_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/emailcampaign"
)

func (w *world) moveTo(id string, to emailcampaign.Status) {
	w.t.Helper()
	from := w.campaign(id).Status
	if ok, err := w.campaigns.SwapStatus(context.Background(), id, from, to, emailcampaign.HoldNone); err != nil || !ok {
		w.t.Fatalf("could not move %s from %s to %s: %v", id, from, to, err)
	}
}

func revisedContent() emailcampaign.Content {
	doc := readyDocument()
	doc.Settings.Preheader = "Só hoje"
	return emailcampaign.Content{Subject: "Nova oferta para {{first_name|você}}", Preheader: "Só hoje", Document: doc}
}

func TestReviseContentKeepsADraftADraft(t *testing.T) {
	w := newWorld(t)
	c := w.draft(nil)
	got, err := w.svc.ReviseContent(context.Background(), w.actor(), c.ID, revisedContent())
	if err != nil {
		t.Fatal(err)
	}
	stored := w.campaign(c.ID)
	if got.Status != emailcampaign.StatusDraft || stored.Subject != "Nova oferta para {{first_name|você}}" || stored.Preheader != "Só hoje" || stored.Document.Settings.Preheader != "Só hoje" {
		t.Fatalf("revised = %+v", stored)
	}
	if stored.Name != c.Name || stored.Selection == nil {
		t.Fatalf("the rest of the campaign changed: %+v", stored)
	}
}

func TestReviseContentSendsAReadyCampaignBackToDraft(t *testing.T) {
	w := newWorld(t)
	c := w.draft(nil)
	w.moveTo(c.ID, emailcampaign.StatusReady)
	got, err := w.svc.ReviseContent(context.Background(), w.actor(), c.ID, revisedContent())
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != emailcampaign.StatusDraft || w.campaign(c.ID).Status != emailcampaign.StatusDraft {
		t.Fatalf("status = %s", got.Status)
	}
}

func TestReviseContentRefusesEveryCampaignPastReady(t *testing.T) {
	for _, status := range []emailcampaign.Status{emailcampaign.StatusScheduled, emailcampaign.StatusRunning, emailcampaign.StatusPaused, emailcampaign.StatusHeld, emailcampaign.StatusCompleted, emailcampaign.StatusStopped} {
		w := newWorld(t)
		c := w.draft(nil)
		w.moveTo(c.ID, status)
		if _, err := w.svc.ReviseContent(context.Background(), w.actor(), c.ID, revisedContent()); !errors.Is(err, emailcampaign.ErrNotRevisable) {
			t.Fatalf("%s: err = %v", status, err)
		}
		if stored := w.campaign(c.ID); stored.Subject != c.Subject || stored.Status != status {
			t.Fatalf("%s: the campaign changed: %+v", status, stored)
		}
	}
}

func TestReviseContentStaysInsideTheWorkspace(t *testing.T) {
	w := newWorld(t)
	c := w.draft(nil)
	other := Actor{UserID: "user-2", WorkspaceID: "another-workspace"}
	if _, err := w.svc.ReviseContent(context.Background(), other, c.ID, revisedContent()); !errors.Is(err, emailcampaign.ErrCampaignNotFound) {
		t.Fatalf("err = %v", err)
	}
	if w.campaign(c.ID).Subject != c.Subject {
		t.Fatal("another workspace changed the campaign")
	}
}
