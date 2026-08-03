package whatsapp_campaign_usecase

import (
	"context"
	"testing"
	"time"

	"vozko/domain/whatsapp/template"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
	wsc "vozko/domain/workspace_config"
)

// The hot path used to re-query the same fat entry row up to 4× per message
// (spam check, variables, template-info, send record). After consolidating to a
// single fetch threaded through the helpers, a successful send must hit
// EntryRepo.FindByID exactly once, while preserving status + metadata behavior.

func TestSendTemplateMessage_FetchesEntryOnce(t *testing.T) {
	h := newTestHarness()
	h.consumer.WhatsAppClientFactory = &mockWhatsAppClientFactory{client: h.waClient, returnReal: true}
	campaign := &wc.Campaign{ID: "camp-1", WorkspaceID: "ws-1", BusinessPhoneID: "bp-1", TemplateID: "tmpl-1"}
	tmpl := approvedMarketingTemplate("tmpl-1") // no parameters
	h.entryRepo.entries["e-1"] = &wce.WhatsAppCampaignEntry{ID: "e-1", LeadID: "lead-1"}

	res := h.consumer.sendTemplateMessage(campaign, tmpl, "e-1", "+5511999999999")

	if res != sendResultSuccess {
		t.Fatalf("expected sendResultSuccess, got %v", res)
	}
	if n := h.entryRepo.findByIDCount(); n != 1 {
		t.Errorf("expected exactly 1 entry fetch on the success path, got %d", n)
	}
	if got := h.entryRepo.getStatus("e-1"); got != wce.SendStatusSent {
		t.Errorf("expected status Sent, got %v", got)
	}
	if h.entryRepo.entries["e-1"].Metadata == nil || h.entryRepo.entries["e-1"].Metadata["template_info"] == nil {
		t.Errorf("expected template_info to be stored in entry metadata")
	}
}

func TestSendTemplateMessage_WithVariables_SingleFetch(t *testing.T) {
	h := newTestHarness()
	h.consumer.WhatsAppClientFactory = &mockWhatsAppClientFactory{client: h.waClient, returnReal: true}
	campaign := &wc.Campaign{ID: "camp-2", WorkspaceID: "ws-1", BusinessPhoneID: "bp-1", TemplateID: "tmpl-2"}
	tmpl := &template.Template{
		ID:         "tmpl-2",
		Name:       "t2",
		Status:     template.TemplateStatusApproved,
		Category:   template.TemplateCategoryMarketing,
		Components: []template.TemplateComponent{{Type: "BODY", Text: "Olá {{1}}"}},
	}
	if tmpl.ParameterCount() != 1 {
		t.Fatalf("test setup: expected 1 parameter, got %d", tmpl.ParameterCount())
	}
	h.entryRepo.entries["e-2"] = &wce.WhatsAppCampaignEntry{ID: "e-2", LeadID: "lead-2", Variables: []string{"Maria"}}

	res := h.consumer.sendTemplateMessage(campaign, tmpl, "e-2", "+5511988887777")

	if res != sendResultSuccess {
		t.Fatalf("expected sendResultSuccess, got %v", res)
	}
	// The variables read must reuse the single fetch, still exactly one.
	if n := h.entryRepo.findByIDCount(); n != 1 {
		t.Errorf("expected exactly 1 entry fetch with variables, got %d", n)
	}
	if got := h.entryRepo.getStatus("e-2"); got != wce.SendStatusSent {
		t.Errorf("expected status Sent, got %v", got)
	}
}

func TestSendTemplateMessage_SpamSkip_Preserved(t *testing.T) {
	h := newTestHarness()
	// Enable spam protection and make the lead's last send recent.
	h.consumer.WorkspaceConfigRepo = spamWorkspaceConfigRepo{days: 7}
	h.consumer.LeadCampaignSendRepo = recentLeadSendRepo{last: time.Now().UTC()}

	campaign := &wc.Campaign{ID: "camp-3", WorkspaceID: "ws-1", BusinessPhoneID: "bp-1", TemplateID: "tmpl-1"}
	tmpl := approvedMarketingTemplate("tmpl-1")
	h.entryRepo.entries["e-3"] = &wce.WhatsAppCampaignEntry{ID: "e-3", LeadID: "lead-3"}

	res := h.consumer.sendTemplateMessage(campaign, tmpl, "e-3", "+5511977776666")

	if res != sendResultConfigError {
		t.Fatalf("expected spam skip to return config error, got %v", res)
	}
	if got := h.entryRepo.getStatus("e-3"); got != wce.SendStatusNotEligiblePossibleSpam {
		t.Errorf("expected status NotEligiblePossibleSpam, got %v", got)
	}
	// Even the spam-skip path fetches the entry at most once.
	if n := h.entryRepo.findByIDCount(); n > 1 {
		t.Errorf("expected at most 1 entry fetch on the spam path, got %d", n)
	}
}

// --- configurable mocks for the spam path ---

type spamWorkspaceConfigRepo struct{ days int }

func (m spamWorkspaceConfigRepo) GetByWorkspaceID(_ context.Context, _ string) (*wsc.WorkspaceConfig, error) {
	return &wsc.WorkspaceConfig{CampaignSpamProtectionDays: m.days}, nil
}
func (m spamWorkspaceConfigRepo) Upsert(_ context.Context, _ *wsc.WorkspaceConfig) error { return nil }
func (m spamWorkspaceConfigRepo) EnsureExists(_ context.Context, _ string) error         { return nil }

type recentLeadSendRepo struct{ last time.Time }

func (m recentLeadSendRepo) Record(_, _, _ string) error { return nil }
func (m recentLeadSendRepo) GetLastSendTime(_, _ string) (*time.Time, error) {
	return &m.last, nil
}
func (m recentLeadSendRepo) GetLastSendTimesBatch(_ []string, _ string) (map[string]time.Time, error) {
	return nil, nil
}
