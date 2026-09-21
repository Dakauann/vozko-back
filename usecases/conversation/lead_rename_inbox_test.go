package conversation_usecase

import (
	"context"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/lead"
	"vozko/domain/shared"
)

type fakeChannelContacts struct {
	byID map[string]ContactDisplay
}

func (f *fakeChannelContacts) ContactsByIDs(_ context.Context, ids []string) (map[string]ContactDisplay, error) {
	out := map[string]ContactDisplay{}
	for _, id := range ids {
		if c, ok := f.byID[id]; ok {
			out[id] = c
		}
	}
	return out, nil
}

func (f *fakeChannelContacts) ContactForConversation(context.Context, string) (ContactDisplay, string, error) {
	return ContactDisplay{}, "", nil
}

func (f *fakeChannelContacts) AuthorsByHandle(context.Context, string, []string) (map[string]ContactDisplay, error) {
	return nil, nil
}

func TestHydrateContactSenders_OperatorsLeadNameSurvivesThePushname(t *testing.T) {
	svc := &HistoryProviderService{}
	svc.SetContactIdentityLookup(shared.EntryTypeUnofficialWhatsApp, &fakeChannelContacts{
		byID: map[string]ContactDisplay{
			"lead-1": {ContactID: "lead-1", Ref: "5584994409624@s.whatsapp.net",
				Handle: "+5584994409624", Name: "Dakauann", PictureURL: "pic"},
		},
	})

	entries := []conversation.InboxEntry{{
		EntryID:   "uw-1",
		EntryType: string(shared.EntryTypeUnofficialWhatsApp),
		LeadID:    "lead-1",
		LeadName:  "Dakauann Teste",
	}}
	svc.hydrateContactSenders(entries)

	if entries[0].LeadName != "Dakauann Teste" {
		t.Fatalf("LeadName = %q, want the operator's name; the pushname overruled it",
			entries[0].LeadName)
	}
	if entries[0].LeadNumber != "+5584994409624" || entries[0].LeadPicture != "pic" {
		t.Errorf("contact-owned fields lost: %+v", entries[0])
	}
}

func TestHydrateContactSenders_StillFillsABlankLeadName(t *testing.T) {
	svc := &HistoryProviderService{}
	svc.SetContactIdentityLookup(shared.EntryTypeUnofficialWhatsApp, &fakeChannelContacts{
		byID: map[string]ContactDisplay{
			"lead-1": {ContactID: "lead-1", Handle: "+5584994409624", Name: "Dakauann"},
		},
	})

	entries := []conversation.InboxEntry{
		{EntryID: "uw-1", EntryType: string(shared.EntryTypeUnofficialWhatsApp), LeadID: "lead-1"},
		{EntryID: "uw-2", EntryType: string(shared.EntryTypeUnofficialWhatsApp), LeadID: "lead-1", LeadName: "   "},
	}
	svc.hydrateContactSenders(entries)

	for i := range entries {
		if entries[i].LeadName != "Dakauann" {
			t.Errorf("entry %d name = %q, want the contact fallback", i, entries[i].LeadName)
		}
	}
}

func TestHydrateContactSenders_SenderLabelUsesTheDisplayedName(t *testing.T) {
	svc := &HistoryProviderService{}
	svc.SetContactIdentityLookup(shared.EntryTypeUnofficialWhatsApp, &fakeChannelContacts{
		byID: map[string]ContactDisplay{
			"lead-1": {ContactID: "lead-1", Ref: "5584994409624@s.whatsapp.net", Name: "Dakauann"},
		},
	})

	entries := []conversation.InboxEntry{{
		EntryID:           "uw-1",
		EntryType:         string(shared.EntryTypeUnofficialWhatsApp),
		LeadID:            "lead-1",
		LeadName:          "Dakauann Teste",
		LastMessageSender: "5584994409624@s.whatsapp.net",
	}}
	svc.hydrateContactSenders(entries)

	if entries[0].LastMessageSender != "Dakauann Teste" {
		t.Fatalf("LastMessageSender = %q, want the name the row displays",
			entries[0].LastMessageSender)
	}
}

func TestHydrateContactSenders_LeavesARealSenderLabelAlone(t *testing.T) {
	svc := &HistoryProviderService{}
	svc.SetContactIdentityLookup(shared.EntryTypeUnofficialWhatsApp, &fakeChannelContacts{
		byID: map[string]ContactDisplay{
			"lead-1": {ContactID: "lead-1", Ref: "5584994409624@s.whatsapp.net", Name: "Dakauann"},
		},
	})

	entries := []conversation.InboxEntry{{
		EntryID:           "uw-1",
		EntryType:         string(shared.EntryTypeUnofficialWhatsApp),
		LeadID:            "lead-1",
		LeadName:          "Dakauann Teste",
		LastMessageSender: "Maria (atendimento)",
	}}
	svc.hydrateContactSenders(entries)

	if entries[0].LastMessageSender != "Maria (atendimento)" {
		t.Fatalf("an operator's sender label was overwritten: %q", entries[0].LastMessageSender)
	}
}

type headerLeadRepo struct {
	lead.Repository

	byID      map[string]*lead.Lead
	askedWS   string
	askedLead string
}

func (r *headerLeadRepo) FindByID(workspaceID, id string) (*lead.Lead, error) {
	r.askedWS, r.askedLead = workspaceID, id
	if l, ok := r.byID[id]; ok {
		return l, nil
	}
	return nil, lead.ErrLeadNotFound
}

type headerContacts struct {
	contact ContactDisplay
	ws      string
}

func (h *headerContacts) ContactsByIDs(context.Context, []string) (map[string]ContactDisplay, error) {
	return nil, nil
}

func (h *headerContacts) ContactForConversation(context.Context, string) (ContactDisplay, string, error) {
	return h.contact, h.ws, nil
}

func (h *headerContacts) AuthorsByHandle(context.Context, string, []string) (map[string]ContactDisplay, error) {
	return nil, nil
}

func TestGetEntryInfo_HeaderPrefersTheLeadNameOverThePushname(t *testing.T) {
	leads := &headerLeadRepo{byID: map[string]*lead.Lead{
		"lead-1": {ID: "lead-1", Name: "DakauannT", Number: "558494409624"},
	}}
	svc := &HistoryProviderService{leadRepo: leads}
	svc.SetContactIdentityLookup(shared.EntryTypeUnofficialWhatsApp, &headerContacts{
		ws: "ws-1",
		contact: ContactDisplay{
			ContactID: "contact-1", LeadID: "lead-1",
			Handle: "+5584994409624", Name: "Dakauann", PictureURL: "pic",
		},
	})

	name, handle, picture, _, _, _, err := svc.GetEntryInfo(
		"uw-conv-1", string(shared.EntryTypeUnofficialWhatsApp))
	if err != nil {
		t.Fatalf("GetEntryInfo: %v", err)
	}
	if name != "DakauannT" {
		t.Fatalf("header name = %q, want the CRM lead's name", name)
	}
	if leads.askedWS != "ws-1" || leads.askedLead != "lead-1" {
		t.Errorf("looked up lead %q in workspace %q", leads.askedLead, leads.askedWS)
	}
	if handle != "+5584994409624" || picture != "pic" {
		t.Errorf("contact-owned header fields lost: %q %q", handle, picture)
	}
}

func TestGetEntryInfo_HeaderKeepsThePushnameWithoutALead(t *testing.T) {
	leads := &headerLeadRepo{byID: map[string]*lead.Lead{}}
	svc := &HistoryProviderService{leadRepo: leads}
	svc.SetContactIdentityLookup(shared.EntryTypeUnofficialWhatsApp, &headerContacts{
		ws:      "ws-1",
		contact: ContactDisplay{ContactID: "group-1", Name: "Teste grupos", IsGroup: true},
	})

	name, _, _, _, _, _, err := svc.GetEntryInfo(
		"uw-conv-2", string(shared.EntryTypeUnofficialWhatsApp))
	if err != nil {
		t.Fatalf("GetEntryInfo: %v", err)
	}
	if name != "Teste grupos" {
		t.Fatalf("header name = %q, want the group's own name", name)
	}
	if leads.askedLead != "" {
		t.Errorf("a contact with no lead must not cost a lead query, asked for %q", leads.askedLead)
	}
}

func TestGetEntryInfo_HeaderFallsBackWhenTheLeadNameIsBlank(t *testing.T) {
	leads := &headerLeadRepo{byID: map[string]*lead.Lead{
		"lead-1": {ID: "lead-1", Name: "   "},
	}}
	svc := &HistoryProviderService{leadRepo: leads}
	svc.SetContactIdentityLookup(shared.EntryTypeUnofficialWhatsApp, &headerContacts{
		ws: "ws-1",
		contact: ContactDisplay{
			ContactID: "contact-1", LeadID: "lead-1", Name: "Dakauann",
		},
	})

	name, _, _, _, _, _, err := svc.GetEntryInfo(
		"uw-conv-1", string(shared.EntryTypeUnofficialWhatsApp))
	if err != nil {
		t.Fatalf("GetEntryInfo: %v", err)
	}
	if name != "Dakauann" {
		t.Fatalf("header name = %q, want the pushname fallback", name)
	}
}
