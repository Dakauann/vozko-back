package emailcampaign_usecase

import (
	"context"
	"errors"
	"strings"
	"testing"

	audience "vozko/domain/campaignaudience"
	"vozko/domain/emailcampaign"
	"vozko/domain/lead"
	campaignaudience_usecase "vozko/usecases/campaignaudience"
	"vozko/usecases/campaignqueue"
)

var bg = context.Background()

func (w *world) uploadList(text string) campaignaudience_usecase.Preview {
	w.t.Helper()
	preview, err := w.aud.Upload(bg, w.actor(), audience.ChannelEmail, campaignaudience_usecase.Upload{FileName: "lista.csv", Data: []byte(text)}, campaignaudience_usecase.View{})
	if err != nil {
		w.t.Fatal(err)
	}
	return preview
}

func (w *world) listDraft(listID string, policies ...audience.Variable) emailcampaign.Campaign {
	w.t.Helper()
	c, err := w.svc.Create(bg, w.actor(), emailcampaign.Draft{
		Name: "Matrículas", Subject: "Oferta para {{first_name|você}}", Document: readyDocument(), ListID: listID, MergePolicies: policies,
	})
	if err != nil {
		w.t.Fatal(err)
	}
	return *c
}

func TestAListDraftAttachesItsListAndLaunchTurnsRowsIntoLeads(t *testing.T) {
	w := newWorld(t)
	w.audience(person{email: "ana@x.com", subscribed: true})
	preview := w.uploadList("email;nome\nana@x.com;Ana\nbia@x.com;Bia\n")
	if preview.Existing != 1 || preview.New != 1 {
		t.Fatalf("preview = %+v", preview)
	}
	c := w.listDraft(preview.ID)
	list, err := w.lists.Get(bg, ws, preview.ID)
	if err != nil || list.AttachedTo != c.AudienceOwner() || list.ExpiresAt != nil {
		t.Fatalf("the draft owns its list: %+v, %v", list, err)
	}
	review, err := w.svc.Review(bg, w.actor(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if review.Audience.Matched != 2 || review.Audience.Sendable != 2 || review.Audience.List == nil || review.Audience.List.New != 1 {
		t.Fatalf("review = %+v", review.Audience)
	}
	if len(w.created.inputs) != 0 {
		t.Fatal("a review created leads")
	}
	if _, err := w.svc.Launch(bg, w.actor(), c.ID, nil); err != nil {
		t.Fatal(err)
	}
	if len(w.created.inputs) != 2 || w.created.inputs[1].Email != "bia@x.com" || w.created.inputs[1].Source != lead.SourceImport || w.created.inputs[1].Name != "Bia" {
		t.Fatalf("leads made at launch = %+v", w.created.inputs)
	}
	rows := w.recips.byAddress(c.ID)
	if len(rows) != 2 || rows["bia@x.com"].LeadID != "lead-bia@x.com" || rows["ana@x.com"].Status != emailcampaign.RecipientPending {
		t.Fatalf("recipients = %+v", rows)
	}
	if _, err := w.lists.Get(bg, ws, preview.ID); !errors.Is(err, audience.ErrListNotFound) {
		t.Fatalf("a launched list is released: %v", err)
	}
}

func TestADraftRefusesAListItCannotReadAndReleasesAListItDrops(t *testing.T) {
	w := newWorld(t)
	if _, err := w.svc.Create(bg, w.actor(), emailcampaign.Draft{Name: "X", ListID: "00000000-0000-4000-8000-000000000001"}); !errors.Is(err, audience.ErrListNotFound) {
		t.Fatalf("an unknown list = %v", err)
	}
	first := w.uploadList("email\nana@x.com\n")
	c := w.listDraft(first.ID)
	second := w.uploadList("email\nbia@x.com\n")
	d := emailcampaign.Draft{Name: "Matrículas", Subject: c.Subject, Document: c.Document, ListID: second.ID}
	if _, err := w.svc.Update(bg, w.actor(), c.ID, d); err != nil {
		t.Fatal(err)
	}
	if _, err := w.lists.Get(bg, ws, first.ID); !errors.Is(err, audience.ErrListNotFound) {
		t.Fatalf("the replaced list is released: %v", err)
	}
	if list, err := w.lists.Get(bg, ws, second.ID); err != nil || list.AttachedTo != c.AudienceOwner() {
		t.Fatalf("the new list is attached: %+v %v", list, err)
	}
	if err := w.svc.Delete(bg, w.actor(), c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.lists.Get(bg, ws, second.ID); !errors.Is(err, audience.ErrListNotFound) {
		t.Fatalf("a deleted draft releases its list: %v", err)
	}
}

func TestAnExcludedMergeTagSkipsRecipientsWithoutTheValueAtLaunchAndAtSend(t *testing.T) {
	w := newWorld(t)
	w.audience(person{email: "a@x.com", subscribed: true}, person{email: "b@x.com", subscribed: true})
	for id, f := range w.facts {
		if f.Email == "a@x.com" {
			f.CustomFields = map[string]any{"serie": "1º ano"}
			w.facts[id] = f
		}
	}
	c, err := w.svc.Create(bg, w.actor(), emailcampaign.Draft{
		Name: "Matrículas", Subject: "Série {{field.serie}}", Document: readyDocument(), Selection: w.draft(nil).Selection,
		MergePolicies: []audience.Variable{{Key: "field.serie", Missing: audience.MissingExclude}},
	})
	if err != nil {
		t.Fatal(err)
	}
	review, err := w.svc.Review(bg, w.actor(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if review.Audience.Sendable != 1 || review.Audience.Skipped[emailcampaign.SkipMissingVariable] != 1 {
		t.Fatalf("review = %+v", review.Audience)
	}
	if v := review.Audience.Variables; len(v) != 2 || v[0].Key != "field.serie" || v[0].Missing != 1 {
		t.Fatalf("variables = %+v", v)
	}
	if _, err := w.svc.Launch(bg, w.actor(), c.ID, nil); err != nil {
		t.Fatal(err)
	}
	rows := w.recips.byAddress(c.ID)
	if rows["b@x.com"].SkipReason != emailcampaign.SkipMissingVariable || rows["a@x.com"].Status != emailcampaign.RecipientPending {
		t.Fatalf("recipients = %+v", rows)
	}
	for id, f := range w.facts {
		if f.Email == "a@x.com" {
			f.CustomFields = nil
			w.facts[id] = f
		}
	}
	w.svc.handle(campaignqueue.Message{CampaignID: c.ID, EntryID: rows["a@x.com"].ID})
	if got := w.recips.byAddress(c.ID)["a@x.com"]; got.SkipReason != emailcampaign.SkipMissingVariable || len(w.gateway.calls) != 0 {
		t.Fatalf("the value was gone at send: %+v, sends %d", got, len(w.gateway.calls))
	}
}

func TestADefaultTextReplacesTheTagFallbackAtSend(t *testing.T) {
	w := newWorld(t)
	w.audience(person{email: "a@x.com", subscribed: true})
	for id, f := range w.facts {
		f.Name = ""
		w.facts[id] = f
	}
	c, err := w.svc.Create(bg, w.actor(), emailcampaign.Draft{
		Name: "Matrículas", Subject: "Oi {{first_name|você}}", Document: readyDocument(), Selection: w.draft(nil).Selection,
		MergePolicies: []audience.Variable{{Key: "first_name", Missing: audience.MissingText, Text: "família"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.Launch(bg, w.actor(), c.ID, nil); err != nil {
		t.Fatal(err)
	}
	w.svc.handle(campaignqueue.Message{CampaignID: c.ID, EntryID: w.recips.byAddress(c.ID)["a@x.com"].ID})
	if len(w.gateway.calls) != 1 || w.gateway.calls[0].content.Subject != "Oi família" || !strings.Contains(w.gateway.calls[0].content.HTML, "Olá família") {
		t.Fatalf("sends = %+v", w.gateway.calls)
	}
}

func TestReviewFailsClosedWithoutTheAudienceStep(t *testing.T) {
	w := newWorld(t)
	w.audience(person{email: "a@x.com", subscribed: true})
	c := w.draft(nil)
	w.svc.SetAudience(nil)
	if _, err := w.svc.Review(bg, w.actor(), c.ID); !errors.Is(err, audience.ErrUnavailable) {
		t.Fatalf("review = %v", err)
	}
}
