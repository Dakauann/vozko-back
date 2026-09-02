package conversation_usecase

import (
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/lead"
	"vozko/domain/shared"
	wce "vozko/domain/whatsapp_campaign_entry"
)

type builderLeadRepo struct {
	lead.Repository
	byID  map[string]*lead.Lead
	asked []string
}

func (r *builderLeadRepo) FindByIDs(_ string, ids []string) ([]*lead.Lead, error) {
	r.asked = append(r.asked, ids...)
	out := make([]*lead.Lead, 0, len(ids))
	for _, id := range ids {
		if l, ok := r.byID[id]; ok {
			out = append(out, l)
		}
	}
	return out, nil
}

type builderWARepo struct {
	wce.Repository
	asked   []string
	entries []*wce.WhatsAppCampaignEntry
}

func (r *builderWARepo) FindByIDs(ids []string) ([]*wce.WhatsAppCampaignEntry, error) {
	r.asked = append(r.asked, ids...)
	return r.entries, nil
}

// The drift the two copies had already accumulated.
//
// The campaign-scoped copy decided whether to load WhatsApp entries from the
// REQUEST's entry type; the workspace-wide one decided per row. Those agree
// only while a page holds a single channel, which the workspace-wide page never
// does — and asking the WhatsApp repository for a Telegram conversation id is a
// query that can only return nothing.
func TestBuildInboxEntries_WhatsAppLookupIsPerRowNotPerRequest(t *testing.T) {
	wa := &builderWARepo{}
	svc := &HistoryProviderService{
		leadRepo:     &builderLeadRepo{byID: map[string]*lead.Lead{}},
		whatsappRepo: wa,
	}

	svc.buildInboxEntries([]conversation.EntryWithLastMessage{
		{EntryID: "wa-1", EntryType: shared.EntryTypeWhatsApp},
		{EntryID: "tg-1", EntryType: shared.EntryTypeTelegram},
		{EntryID: "uw-1", EntryType: shared.EntryTypeUnofficialWhatsApp},
		{EntryID: "wa-2", EntryType: shared.EntryTypeWhatsApp},
	}, "ws-1")

	want := map[string]bool{"wa-1": true, "wa-2": true}
	if len(wa.asked) != 2 {
		t.Fatalf("WhatsApp repo asked for %v; want only the WhatsApp rows", wa.asked)
	}
	for _, id := range wa.asked {
		if !want[id] {
			t.Errorf("WhatsApp repo asked for %q, which is not a WhatsApp row", id)
		}
	}
}

// Leads are fetched once per page, deduplicated. A lead with several
// conversations is common — the same person on the official and unofficial
// numbers — and a lookup per row would make the inbox N+1.
func TestBuildInboxEntries_LeadsAreBatchedAndDeduplicated(t *testing.T) {
	leads := &builderLeadRepo{byID: map[string]*lead.Lead{
		"lead-1": {ID: "lead-1", Name: "Ana", Number: "5511999999999"},
	}}
	svc := &HistoryProviderService{leadRepo: leads, whatsappRepo: &builderWARepo{}}

	entries := svc.buildInboxEntries([]conversation.EntryWithLastMessage{
		{EntryID: "a", EntryType: shared.EntryTypeWhatsApp, LeadID: "lead-1"},
		{EntryID: "b", EntryType: shared.EntryTypeWhatsApp, LeadID: "lead-1"},
		{EntryID: "c", EntryType: shared.EntryTypeWhatsApp, LeadID: ""},
	}, "ws-1")

	if len(leads.asked) != 1 || leads.asked[0] != "lead-1" {
		t.Fatalf("lead ids asked = %v; want exactly [lead-1]", leads.asked)
	}
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}
	// Both rows for that lead carry the name; the row without a lead does not
	// borrow one.
	if entries[0].LeadName != "Ana" || entries[1].LeadName != "Ana" {
		t.Errorf("lead name not applied to every row of that lead: %+v", entries[:2])
	}
	if entries[2].LeadName != "" {
		t.Errorf("a row with no lead got a name: %q", entries[2].LeadName)
	}
}

// An empty page must not reach the repositories at all.
func TestBuildInboxEntries_EmptyPageAsksNothing(t *testing.T) {
	leads := &builderLeadRepo{byID: map[string]*lead.Lead{}}
	wa := &builderWARepo{}
	svc := &HistoryProviderService{leadRepo: leads, whatsappRepo: wa}

	got := svc.buildInboxEntries(nil, "ws-1")
	if len(got) != 0 {
		t.Fatalf("got %d entries for an empty page", len(got))
	}
	if len(leads.asked) != 0 || len(wa.asked) != 0 {
		t.Errorf("an empty page still queried: leads=%v wa=%v", leads.asked, wa.asked)
	}
}

// A lead lookup that fails costs names, never rows: the conversation still has
// to render, and channels whose contacts carry their own name are unaffected.
func TestBuildInboxEntries_LeadFailureStillRendersTheRows(t *testing.T) {
	svc := &HistoryProviderService{
		leadRepo:     &builderLeadRepo{byID: nil},
		whatsappRepo: &builderWARepo{},
	}
	got := svc.buildInboxEntries([]conversation.EntryWithLastMessage{
		{EntryID: "a", EntryType: shared.EntryTypeWhatsApp, LeadID: "missing"},
	}, "ws-1")

	if len(got) != 1 || got[0].EntryID != "a" {
		t.Fatalf("a row vanished when its lead could not be loaded: %+v", got)
	}
}
