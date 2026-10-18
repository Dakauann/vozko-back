package lead

import (
	"strings"
	"testing"
	"time"

	"vozko/domain/lead"
	"vozko/domain/recordevent"
)

func TestTheConsentRecordRoundTripsThroughItsJSONColumn(t *testing.T) {
	at := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	sent := at.Add(time.Minute)
	l := &lead.Lead{ID: "11111111-1111-1111-1111-111111111111", WorkspaceID: "22222222-2222-2222-2222-222222222222", Number: "5511987654321",
		OptedOutAt: &at, OptOutSource: lead.OptOutLeadRequest,
		ConsentRecord: &lead.ConsentRecord{Kind: lead.ConsentWithdraw, Source: lead.ConsentKeyword,
			Actor:    lead.ConsentActor{Type: lead.ConsentActorLead, ID: "11111111-1111-1111-1111-111111111111"},
			Evidence: lead.ConsentEvidence{Type: lead.EvidenceMessage, Ref: "wamid.1", Excerpt: "SAIR"},
			At:       at, EventID: "33333333-3333-3333-3333-333333333333", ConfirmationSentAt: &sent}}

	row, err := toSchema(l)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(row.WhatsAppConsentRecord), `"excerpt":"SAIR"`) {
		t.Fatalf("column = %s", row.WhatsAppConsentRecord)
	}
	back, err := toDomain(row)
	if err != nil {
		t.Fatal(err)
	}
	got := back.ConsentRecord
	if got == nil || got.EventID != l.ConsentRecord.EventID || got.Evidence != l.ConsentRecord.Evidence || got.Actor != l.ConsentRecord.Actor ||
		got.ConfirmationSentAt == nil || !got.ConfirmationSentAt.Equal(sent) || !got.At.Equal(at) {
		t.Fatalf("round trip = %+v", got)
	}
}

func TestALeadWithoutAConsentRecordStoresNull(t *testing.T) {
	row, err := toSchema(&lead.Lead{ID: "11111111-1111-1111-1111-111111111111", WorkspaceID: "ws", Number: "5511987654321"})
	if err != nil || row.WhatsAppConsentRecord != nil {
		t.Fatalf("column = %s, %v", row.WhatsAppConsentRecord, err)
	}
	back, err := toDomain(row)
	if err != nil || back.ConsentRecord != nil {
		t.Fatalf("record = %+v, %v", back.ConsentRecord, err)
	}
}

func TestABrokenConsentRecordIsAnErrorNotASilentGap(t *testing.T) {
	row, err := toSchema(&lead.Lead{ID: "11111111-1111-1111-1111-111111111111", WorkspaceID: "ws", Number: "5511987654321"})
	if err != nil {
		t.Fatal(err)
	}
	row.WhatsAppConsentRecord = []byte(`[1,2]`)
	if _, err := toDomain(row); err == nil {
		t.Fatal("an unreadable consent record must fail the read")
	}
}

func TestAnEventKeepsTheIDItWasGiven(t *testing.T) {
	events := []recordevent.Event{
		{ID: "33333333-3333-3333-3333-333333333333", Kind: lead.EventConsentWithdrawn, Changes: []recordevent.Change{{Field: lead.FieldConsent}}},
		{Kind: lead.EventUpdated, Changes: []recordevent.Change{{Field: lead.FieldName}}},
	}
	rows, err := eventRows("ws", "l", events, time.Now())
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows = %+v, %v", rows, err)
	}
	if rows[0].ID != "33333333-3333-3333-3333-333333333333" || rows[1].ID == "" || rows[1].ID == rows[0].ID {
		t.Fatalf("ids = %q %q", rows[0].ID, rows[1].ID)
	}
}

func TestEveryLeadWriterCarriesTheConsentRecord(t *testing.T) {
	if !strings.Contains(saveSQL, "whatsapp_consent_record = ?") {
		t.Fatalf("saveSQL lacks the consent record:\n%s", saveSQL)
	}
	if !strings.Contains(anonymizeLeadSQL, "whatsapp_consent_record = NULL") {
		t.Fatalf("anonymizing must erase the consent record and its excerpt:\n%s", anonymizeLeadSQL)
	}
	if !strings.Contains(importEnrichSQL, "whatsapp_consent_record = COALESCE(NULLIF(v.consent_record, '')::jsonb, leads.whatsapp_consent_record)") {
		t.Fatalf("an enriching import must write a new grant's record and keep any other:\n%s", importEnrichSQL)
	}
	if got, want := strings.Count(saveSQL, "?"), 25; got != want {
		t.Fatalf("saveSQL binds %d placeholders, want %d", got, want)
	}
}
