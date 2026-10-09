package whatsapp_campaign_usecase

import (
	"errors"
	"testing"

	"vozko/domain/campaign"
	"vozko/domain/lead"
	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/whatsapp/template"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
	"vozko/domain/workflow"
	"vozko/usecases/campaignautomation"
)

func stoppedSelectionSend(campaigns *mockCampaignRepo, source string) {
	campaigns.campaigns["camp-1"] = &wc.Campaign{
		ID: "camp-1", WorkspaceID: "ws-1", Name: "Matrículas", Type: wc.CampaignTypeStandard, TemplateID: "tmpl-1", BusinessPhoneID: "phone-1",
		Status: wc.CampaignStatusStopped, Source: source,
	}
}

func updateFixture(t *testing.T, source string) (*mockCampaignRepo, wc.UpdateCampaignUseCase) {
	t.Helper()
	campaigns := newMockCampaignRepo()
	stoppedSelectionSend(campaigns, source)
	phones := newOwnershipBusinessPhoneRepo()
	phones.phones["phone-1"] = &businessphone.WhatsAppBusinessPhoneNumber{ID: "phone-1", OwnerWorkspaceID: "ws-1", WABAId: "waba-1"}
	templates := newMockTemplateRepo()
	templates.templates["tmpl-1"] = &template.Template{ID: "tmpl-1", Status: template.TemplateStatusApproved, WABAId: "waba-1"}
	templates.templates["tmpl-2"] = &template.Template{ID: "tmpl-2", Status: template.TemplateStatusApproved, WABAId: "waba-1"}
	return campaigns, NewUpdateCampaignUseCase(campaigns, newMockEntryRepo(), templates, phones, nil)
}

func foreignWorkflowStep(t *testing.T) *campaignautomation.Step {
	t.Helper()
	step, err := campaignautomation.NewStep(noWorkflows{byID: map[string]*workflow.Workflow{"wf-other": {ID: "wf-other", WorkspaceID: "ws-2"}}}, noAgents{})
	if err != nil {
		t.Fatalf("NewStep: %v", err)
	}
	return step
}

func TestASelectionSendKeepsTheTemplateAndNumberItWasReviewedWith(t *testing.T) {
	cases := []struct {
		name  string
		input *wc.Campaign
		err   error
	}{
		{name: "another template", input: &wc.Campaign{Name: "Matrículas", TemplateID: "tmpl-2"}, err: campaign.ErrSelectionSendLocked},
		{name: "another number", input: &wc.Campaign{Name: "Matrículas", BusinessPhoneID: "phone-2"}, err: campaign.ErrSelectionSendLocked},
		{name: "a new name", input: &wc.Campaign{Name: "Matrículas 2027"}},
		{name: "archived with the same template", input: &wc.Campaign{Name: "Matrículas", TemplateID: "tmpl-1", BusinessPhoneID: "phone-1", Archived: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			campaigns, uc := updateFixture(t, campaign.SourceLeadSelection)
			_, err := uc.Execute("camp-1", tc.input)
			if !errors.Is(err, tc.err) {
				t.Fatalf("Execute = %v, want %v", err, tc.err)
			}
			if tc.err != nil && campaigns.campaigns["camp-1"].TemplateID != "tmpl-1" {
				t.Fatal("a refused change was saved")
			}
		})
	}
}

func TestAPlainCampaignMayChangeItsTemplate(t *testing.T) {
	_, uc := updateFixture(t, "")
	if _, err := uc.Execute("camp-1", &wc.Campaign{Name: "Matrículas", TemplateID: "tmpl-2"}); err != nil {
		t.Fatalf("Execute = %v", err)
	}
}

func TestUpdateRunsTheSharedWorkflowStepItself(t *testing.T) {
	_, uc := updateFixture(t, "")
	if _, err := uc.Execute("camp-1", &wc.Campaign{Name: "Matrículas", WorkflowID: "wf-other", EnableWorkflow: true}); !errors.Is(err, campaign.ErrAutomationUnavailable) {
		t.Fatalf("Execute without the step = %v, want ErrAutomationUnavailable", err)
	}
	uc.(*updateCampaignUseCase).SetAutomation(foreignWorkflowStep(t))
	if _, err := uc.Execute("camp-1", &wc.Campaign{Name: "Matrículas", WorkflowID: "wf-other", EnableWorkflow: true}); !errors.Is(err, campaign.ErrWorkflowForbidden) {
		t.Fatalf("Execute = %v, want ErrWorkflowForbidden", err)
	}
}

func TestEntriesCannotBeAddedToASelectionSend(t *testing.T) {
	campaigns := newMockCampaignRepo()
	stoppedSelectionSend(campaigns, campaign.SourceLeadSelection)
	uc := NewAddEntriesUseCase(campaigns, newMockEntryRepo(), &qsLeadRepo{})
	_, err := uc.Execute(wc.AddEntriesInput{CampaignID: "camp-1", PhoneNumbers: validPhones(1)})
	if !errors.Is(err, campaign.ErrSelectionSendLocked) {
		t.Fatalf("Execute = %v, want ErrSelectionSendLocked", err)
	}
}

func TestAddEntriesRunsTheSharedWorkflowStepItself(t *testing.T) {
	campaigns := newMockCampaignRepo()
	stoppedSelectionSend(campaigns, "")
	campaigns.campaigns["camp-1"].WorkflowID = "wf-other"
	uc := NewAddEntriesUseCase(campaigns, newMockEntryRepo(), &qsLeadRepo{})
	if _, err := uc.Execute(wc.AddEntriesInput{CampaignID: "camp-1", PhoneNumbers: validPhones(1)}); !errors.Is(err, campaign.ErrAutomationUnavailable) {
		t.Fatalf("Execute without the step = %v, want ErrAutomationUnavailable", err)
	}
	uc.(*addEntriesUseCase).SetAutomation(foreignWorkflowStep(t))
	if _, err := uc.Execute(wc.AddEntriesInput{CampaignID: "camp-1", PhoneNumbers: validPhones(1)}); !errors.Is(err, campaign.ErrWorkflowForbidden) {
		t.Fatalf("Execute = %v, want ErrWorkflowForbidden", err)
	}
}

func TestAQuickSendNeverStartsASelectionSend(t *testing.T) {
	h := newHarness(t, wc.CampaignStatusStopped, "c-1")
	c, _ := h.camp.FindByID("c-1")
	c.Source = campaign.SourceLeadSelection
	h.camp.put(c)
	if _, err := h.uc.Execute(wc.QuickSendInput{CampaignID: "c-1"}); !errors.Is(err, campaign.ErrSelectionSendLocked) {
		t.Fatalf("Execute = %v, want ErrSelectionSendLocked", err)
	}
	if len(h.pub.published) != 0 {
		t.Fatal("a refused quick send published entries")
	}
}

func TestQuickSendRunsTheSharedWorkflowStepItself(t *testing.T) {
	h := newHarness(t, wc.CampaignStatusStopped, "c-1")
	c, _ := h.camp.FindByID("c-1")
	c.WorkflowID = "wf-other"
	h.camp.put(c)
	if _, err := h.uc.Execute(wc.QuickSendInput{CampaignID: "c-1", PhoneNumbers: validPhones(1)}); !errors.Is(err, campaign.ErrAutomationUnavailable) {
		t.Fatalf("Execute without the step = %v, want ErrAutomationUnavailable", err)
	}
	h.uc.(*quickSendUseCase).SetAutomation(foreignWorkflowStep(t))
	if _, err := h.uc.Execute(wc.QuickSendInput{CampaignID: "c-1", PhoneNumbers: validPhones(1)}); !errors.Is(err, campaign.ErrWorkflowForbidden) {
		t.Fatalf("Execute = %v, want ErrWorkflowForbidden", err)
	}
}

type lockedEntries struct {
	wce.Repository
	entry   *wce.WhatsAppCampaignEntry
	writes  []string
	toggled []bool
	resets  int
}

func (f *lockedEntries) FindByID(string) (*wce.WhatsAppCampaignEntry, error) { return f.entry, nil }

func (f *lockedEntries) FindByCampaignAndNumber(string, string) (*wce.WhatsAppCampaignEntry, error) {
	return nil, nil
}

func (f *lockedEntries) Create(*wce.WhatsAppCampaignEntry) error {
	f.writes = append(f.writes, "create")
	return nil
}

func (f *lockedEntries) Delete(string) error {
	f.writes = append(f.writes, "delete")
	return nil
}

func (f *lockedEntries) UpdateMetadata(string, map[string]interface{}) error {
	f.writes = append(f.writes, "metadata")
	return nil
}

func (f *lockedEntries) UpsertCampaignEntries(string, []wce.WhatsAppCampaignEntry) error {
	f.writes = append(f.writes, "variables")
	return nil
}

func (f *lockedEntries) UpdateAutomationEnabled(_ string, enabled *bool) error {
	f.toggled = append(f.toggled, *enabled)
	return nil
}

func (f *lockedEntries) ResetAllStatuses(string) (int64, error) {
	f.resets++
	return 0, nil
}

func TestTheRecipientsOfASelectionSendCannotBeEdited(t *testing.T) {
	number, name, on := "5511987654000", "Bia", true
	cases := []struct {
		name   string
		source string
		input  wc.UpdateEntryInput
		err    error
	}{
		{name: "another number", source: campaign.SourceLeadSelection, input: wc.UpdateEntryInput{Number: &number}, err: campaign.ErrSelectionSendLocked},
		{name: "another name", source: campaign.SourceLeadSelection, input: wc.UpdateEntryInput{Name: &name}, err: campaign.ErrSelectionSendLocked},
		{name: "other variables", source: campaign.SourceLeadSelection, input: wc.UpdateEntryInput{Variables: []string{"Bia"}}, err: campaign.ErrSelectionSendLocked},
		{name: "other metadata", source: campaign.SourceLeadSelection, input: wc.UpdateEntryInput{Metadata: map[string]interface{}{"k": "v"}}, err: campaign.ErrSelectionSendLocked},
		{name: "the AI toggle with another number", source: campaign.SourceLeadSelection, input: wc.UpdateEntryInput{AutomationEnabled: &on, Number: &number}, err: campaign.ErrSelectionSendLocked},
		{name: "only the AI toggle", source: campaign.SourceLeadSelection, input: wc.UpdateEntryInput{AutomationEnabled: &on}},
		{name: "a plain campaign", input: wc.UpdateEntryInput{Number: &number}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entries := &lockedEntries{entry: &wce.WhatsAppCampaignEntry{ID: "e-1", CampaignID: "c-1", LeadID: "l-1"}}
			leads := &entryEditLeads{current: &lead.Lead{ID: "l-1", WorkspaceID: "ws-1", Number: "5511987654321"}}
			uc := NewUpdateEntryUseCase(
				entryEditCampaigns{campaign: &wc.Campaign{ID: "c-1", WorkspaceID: "ws-1", Status: wc.CampaignStatusStopped, Source: tc.source}},
				entries, leads, &entryEditMerger{},
			)
			tc.input.CampaignID, tc.input.EntryID = "c-1", "e-1"

			_, err := uc.Execute(tc.input)

			if !errors.Is(err, tc.err) {
				t.Fatalf("Execute = %v, want %v", err, tc.err)
			}
			if tc.err != nil && (len(entries.writes) != 0 || len(entries.toggled) != 0 || len(leads.resolved) != 0) {
				t.Fatalf("a refused edit wrote %v toggled %v resolved %v", entries.writes, entries.toggled, leads.resolved)
			}
		})
	}
}

func TestASelectionSendCannotBeReset(t *testing.T) {
	campaigns := newMockCampaignRepo()
	stoppedSelectionSend(campaigns, campaign.SourceLeadSelection)
	campaigns.campaigns["camp-1"].ResetCode = "123456"
	entries := &lockedEntries{}
	uc := NewResetCampaignUseCase(campaigns, entries)

	if _, err := uc.PrepareReset("camp-1"); !errors.Is(err, campaign.ErrSelectionSendLocked) {
		t.Fatalf("PrepareReset = %v, want ErrSelectionSendLocked", err)
	}
	if _, err := uc.ConfirmReset(wc.ResetCampaignInput{CampaignID: "camp-1", ResetCode: "123456"}); !errors.Is(err, campaign.ErrSelectionSendLocked) {
		t.Fatalf("ConfirmReset = %v, want ErrSelectionSendLocked", err)
	}
	if entries.resets != 0 || campaigns.campaigns["camp-1"].ResetCode != "123456" {
		t.Fatalf("a refused reset reset %d entries, code %q", entries.resets, campaigns.campaigns["camp-1"].ResetCode)
	}
}
