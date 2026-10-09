package whatsapp_campaign_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/lead"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
)

type entryEditCampaigns struct {
	wc.Repository
	campaign *wc.Campaign
}

func (f entryEditCampaigns) FindByID(string) (*wc.Campaign, error) { return f.campaign, nil }

type entryEditEntries struct {
	wce.Repository
	entry *wce.WhatsAppCampaignEntry
}

func (f entryEditEntries) FindByID(string) (*wce.WhatsAppCampaignEntry, error) { return f.entry, nil }

type entryEditLeads struct {
	lead.Repository
	current  *lead.Lead
	resolved []string
}

func (f *entryEditLeads) FindByID(string, string) (*lead.Lead, error) { return f.current, nil }

func (f *entryEditLeads) FindOrCreate(_ string, number string, _ lead.LeadUpdate) (*lead.Lead, bool, error) {
	f.resolved = append(f.resolved, number)
	return f.current, false, nil
}

type mergeCall struct {
	workspaceID string
	leadID      string
	update      lead.LeadUpdate
}

type entryEditMerger struct {
	calls []mergeCall
	err   error
}

func (m *entryEditMerger) MergeIncoming(_ context.Context, workspaceID, leadID string, update lead.LeadUpdate) (*lead.Lead, error) {
	m.calls = append(m.calls, mergeCall{workspaceID: workspaceID, leadID: leadID, update: update})
	return nil, m.err
}

func entryEditFixture(current *lead.Lead, merger LeadMerger) (*updateEntryUseCase, *entryEditLeads) {
	leads := &entryEditLeads{current: current}
	return NewUpdateEntryUseCase(
		entryEditCampaigns{campaign: &wc.Campaign{ID: "c-1", WorkspaceID: "ws-1"}},
		entryEditEntries{entry: &wce.WhatsAppCampaignEntry{ID: "e-1", CampaignID: "c-1", LeadID: "l-1"}},
		leads,
		merger,
	), leads
}

func TestUpdateEntry_ANameEditIsMergedIntoTheKnownLeadAsImportedData(t *testing.T) {
	merger := &entryEditMerger{}
	uc, leads := entryEditFixture(&lead.Lead{ID: "l-1", WorkspaceID: "ws-1", Number: "5511987654321", Name: "Ana Souza", NameSource: lead.SourceManual}, merger)
	name := "Aninha"
	if _, err := uc.Execute(wc.UpdateEntryInput{CampaignID: "c-1", EntryID: "e-1", Name: &name}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(merger.calls) != 1 {
		t.Fatalf("merges = %+v", merger.calls)
	}
	call := merger.calls[0]
	if call.workspaceID != "ws-1" || call.leadID != "l-1" || call.update.Source != lead.SourceImport || call.update.Name != "Aninha" {
		t.Fatalf("the entry name must merge into lead l-1 as an import, got %+v", call)
	}
	if len(leads.resolved) != 0 {
		t.Fatalf("the lead must not be looked up again by number, got %v", leads.resolved)
	}
}

func TestUpdateEntry_ALeadWithoutANumberStillTakesTheName(t *testing.T) {
	merger := &entryEditMerger{}
	uc, _ := entryEditFixture(&lead.Lead{ID: "l-1", WorkspaceID: "ws-1", Name: "Ana"}, merger)
	name := "Ana Souza"
	if _, err := uc.Execute(wc.UpdateEntryInput{CampaignID: "c-1", EntryID: "e-1", Name: &name}); err != nil || len(merger.calls) != 1 {
		t.Fatalf("Execute: %v, merges %+v", err, merger.calls)
	}
}

func TestUpdateEntry_AFailedMergeFailsTheEdit(t *testing.T) {
	merger := &entryEditMerger{err: lead.ErrLeadNotFound}
	uc, _ := entryEditFixture(&lead.Lead{ID: "l-1", WorkspaceID: "ws-1", Number: "5511987654321"}, merger)
	name := "Aninha"
	if _, err := uc.Execute(wc.UpdateEntryInput{CampaignID: "c-1", EntryID: "e-1", Name: &name}); !errors.Is(err, lead.ErrLeadNotFound) {
		t.Fatalf("err = %v, want ErrLeadNotFound", err)
	}
}

func TestUpdateEntry_WithoutAMergerANameEditIsRefused(t *testing.T) {
	uc, _ := entryEditFixture(&lead.Lead{ID: "l-1", WorkspaceID: "ws-1", Number: "5511987654321"}, nil)
	name := "Aninha"
	if _, err := uc.Execute(wc.UpdateEntryInput{CampaignID: "c-1", EntryID: "e-1", Name: &name}); err == nil {
		t.Fatal("a name edit without the lead merge must be refused")
	}
}

func TestUpdateEntry_WithoutANameLeavesTheLeadAlone(t *testing.T) {
	merger := &entryEditMerger{}
	uc, _ := entryEditFixture(&lead.Lead{ID: "l-1", WorkspaceID: "ws-1", Number: "5511987654321"}, merger)
	if _, err := uc.Execute(wc.UpdateEntryInput{CampaignID: "c-1", EntryID: "e-1"}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(merger.calls) != 0 {
		t.Fatalf("no name means no lead write, got %+v", merger.calls)
	}
}
