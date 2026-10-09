package whatsapp_campaign_usecase

import (
	"testing"

	"vozko/domain/whatsapp/template"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
)

func TestSendTemplateMessage_FetchesEntryOnce(t *testing.T) {
	h := newTestHarness()
	h.consumer.WhatsAppClientFactory = &mockWhatsAppClientFactory{client: h.waClient, returnReal: true}
	campaign := &wc.Campaign{ID: "camp-1", WorkspaceID: "ws-1", BusinessPhoneID: "bp-1", TemplateID: "tmpl-1"}
	tmpl := approvedMarketingTemplate("tmpl-1")
	h.entryRepo.entries["e-1"] = &wce.WhatsAppCampaignEntry{ID: "e-1", LeadID: "lead-1"}

	res := h.consumer.sendTemplateMessage(campaign, tmpl, h.entryRepo.entries["e-1"], "+5511999999999")

	if res != sendResultSuccess {
		t.Fatalf("expected sendResultSuccess, got %v", res)
	}
	if n := h.entryRepo.findByIDCount(); n != 0 {
		t.Errorf("the caller hands the entry over, it is never re-read on the success path: got %d fetches", n)
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

	res := h.consumer.sendTemplateMessage(campaign, tmpl, h.entryRepo.entries["e-2"], "+5511988887777")

	if res != sendResultSuccess {
		t.Fatalf("expected sendResultSuccess, got %v", res)
	}
	if n := h.entryRepo.findByIDCount(); n != 0 {
		t.Errorf("the caller hands the entry over, it is never re-read with variables: got %d fetches", n)
	}
	if got := h.entryRepo.getStatus("e-2"); got != wce.SendStatusSent {
		t.Errorf("expected status Sent, got %v", got)
	}
}
