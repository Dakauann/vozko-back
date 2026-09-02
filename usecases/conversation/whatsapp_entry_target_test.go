package conversation_usecase

import (
	"errors"
	"testing"

	"vozko/domain/lead"

	wce "vozko/domain/whatsapp_campaign_entry"
)

type targetEntryRepo struct {
	wce.Repository
	entry    *wce.WhatsAppCampaignEntry
	campaign *wce.EntryCampaignInfo
	entryErr error
	campErr  error
}

func (r *targetEntryRepo) FindByID(string) (*wce.WhatsAppCampaignEntry, error) {
	return r.entry, r.entryErr
}

func (r *targetEntryRepo) GetCampaignForEntry(string) (*wce.EntryCampaignInfo, error) {
	return r.campaign, r.campErr
}

type targetLeadRepo struct {
	lead.Repository
	lead      *lead.Lead
	err       error
	askedWS   string
	askedLead string
}

func (r *targetLeadRepo) FindByID(workspaceID, id string) (*lead.Lead, error) {
	r.askedWS, r.askedLead = workspaceID, id
	return r.lead, r.err
}

func TestWhatsAppEntryTarget_ResolvesTheWholeSendTarget(t *testing.T) {
	entries := &targetEntryRepo{
		entry:    &wce.WhatsAppCampaignEntry{ID: "e1", LeadID: "lead-1"},
		campaign: &wce.EntryCampaignInfo{WorkspaceID: "ws-1", BusinessPhoneID: "phone-1"},
	}
	leads := &targetLeadRepo{lead: &lead.Lead{ID: "lead-1", Number: "5511999999999"}}

	leadID, number, phoneID, workspaceID, err := whatsappEntryTarget(entries, leads, "e1")
	if err != nil {
		t.Fatalf("whatsappEntryTarget: %v", err)
	}
	if leadID != "lead-1" || number != "5511999999999" ||
		phoneID != "phone-1" || workspaceID != "ws-1" {
		t.Fatalf("got %q %q %q %q", leadID, number, phoneID, workspaceID)
	}
	// The lead is read in the CAMPAIGN's workspace. Reading it in any other
	// would either miss it or, worse, find a different workspace's lead.
	if leads.askedWS != "ws-1" || leads.askedLead != "lead-1" {
		t.Errorf("looked up lead %q in workspace %q", leads.askedLead, leads.askedWS)
	}
}

// A missing campaign is fatal even though the entry itself resolved: the
// campaign is where both the workspace and the number to send FROM live, so
// there is nowhere to send from and no workspace to attribute it to.
func TestWhatsAppEntryTarget_MissingCampaignIsATypedError(t *testing.T) {
	for name, entries := range map[string]*targetEntryRepo{
		"nil campaign": {
			entry: &wce.WhatsAppCampaignEntry{ID: "e1", LeadID: "lead-1"},
		},
		"campaign lookup failed": {
			entry:   &wce.WhatsAppCampaignEntry{ID: "e1", LeadID: "lead-1"},
			campErr: errors.New("boom"),
		},
	} {
		_, _, _, _, err := whatsappEntryTarget(entries, &targetLeadRepo{}, "e1")
		if !errors.Is(err, ErrEntryWorkspaceUnresolved) {
			t.Errorf("%s: err = %v, want ErrEntryWorkspaceUnresolved", name, err)
		}
	}
}

func TestWhatsAppEntryTarget_PropagatesRepositoryFailures(t *testing.T) {
	entryBoom := errors.New("entry gone")
	if _, _, _, _, err := whatsappEntryTarget(
		&targetEntryRepo{entryErr: entryBoom}, &targetLeadRepo{}, "e1",
	); !errors.Is(err, entryBoom) {
		t.Errorf("entry failure = %v, want it propagated", err)
	}

	leadBoom := errors.New("lead gone")
	entries := &targetEntryRepo{
		entry:    &wce.WhatsAppCampaignEntry{ID: "e1", LeadID: "lead-1"},
		campaign: &wce.EntryCampaignInfo{WorkspaceID: "ws-1"},
	}
	if _, _, _, _, err := whatsappEntryTarget(
		entries, &targetLeadRepo{err: leadBoom}, "e1",
	); !errors.Is(err, leadBoom) {
		t.Errorf("lead failure = %v, want it propagated", err)
	}
}
