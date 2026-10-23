package emailcampaign

import (
	"errors"
	"strings"
	"testing"

	"vozko/domain/emaildesign"
)

func TestOnlyDraftsAndReadyCampaignsTakeContentRevisions(t *testing.T) {
	want := map[Status]bool{StatusDraft: true, StatusReady: true}
	for _, s := range AllStatuses() {
		if got := s.ContentRevisable(); got != want[s] {
			t.Errorf("%q.ContentRevisable() = %v, want %v", s, got, want[s])
		}
	}
	for _, s := range ContentRevisableStatuses() {
		if !s.ContentRevisable() || !s.Editable() {
			t.Errorf("ContentRevisableStatuses has %q", s)
		}
	}
}

func TestReviseContentTouchesOnlyTheContent(t *testing.T) {
	doc := emaildesign.Document{Version: emaildesign.CurrentVersion, Settings: emaildesign.Settings{Preheader: "doc"}}
	c := Campaign{ID: "c-1", Name: "Black Friday", Status: StatusReady, SenderID: "s-1", Subject: "old", ListID: "l-1"}
	if err := c.ReviseContent(Content{Subject: "  Oferta {{first_name|cliente}}  ", Preheader: " Só hoje ", Document: doc}); err != nil {
		t.Fatal(err)
	}
	if c.Subject != "Oferta {{first_name|cliente}}" || c.Preheader != "Só hoje" || c.Document.Settings.Preheader != "doc" {
		t.Fatalf("content = %q %q %+v", c.Subject, c.Preheader, c.Document.Settings)
	}
	if c.Name != "Black Friday" || c.SenderID != "s-1" || c.ListID != "l-1" || c.Status != StatusReady {
		t.Fatalf("the rest of the campaign changed: %+v", c)
	}
}

func TestReviseContentRefusesALongPreheader(t *testing.T) {
	c := Campaign{Subject: "old"}
	err := c.ReviseContent(Content{Subject: "new", Preheader: strings.Repeat("p", MaxPreheaderRunes+1)})
	if !errors.Is(err, ErrPreheaderTooLong) || c.Subject != "old" {
		t.Fatalf("err = %v, subject = %q", err, c.Subject)
	}
}

func TestContentOfACampaign(t *testing.T) {
	doc := emaildesign.Document{Version: emaildesign.CurrentVersion}
	c := Campaign{Subject: "Oi", Preheader: "Prévia", Document: doc}
	got := c.Content()
	if got.Subject != "Oi" || got.Preheader != "Prévia" || got.Document.Version != doc.Version {
		t.Fatalf("content = %+v", got)
	}
}

func TestAContentPreheaderWinsOverTheDocument(t *testing.T) {
	doc := emaildesign.Document{Version: emaildesign.CurrentVersion, Settings: emaildesign.Settings{Preheader: "doc"}}
	if got := (Content{Preheader: "own", Document: doc}).RenderDocument(); got.Settings.Preheader != "own" {
		t.Fatalf("preheader = %q", got.Settings.Preheader)
	}
	if got := (Content{Document: doc}).RenderDocument(); got.Settings.Preheader != "doc" {
		t.Fatalf("preheader = %q", got.Settings.Preheader)
	}
	if doc.Settings.Preheader != "doc" {
		t.Fatal("the document itself changed")
	}
}
