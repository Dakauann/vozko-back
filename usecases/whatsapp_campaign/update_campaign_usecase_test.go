package whatsapp_campaign_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/campaign"
	"vozko/domain/whatsapp/template"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
	"vozko/domain/workflow"
	"vozko/usecases/campaignautomation"
)

func schoolWorkflowStep(t *testing.T) *campaignautomation.Step {
	t.Helper()
	step, err := campaignautomation.NewStep(noWorkflows{byID: map[string]*workflow.Workflow{
		"wf-school": {ID: "wf-school", WorkspaceID: "ws-1", Graph: workflow.Graph{Nodes: []workflow.Node{
			{ID: "n1", Config: map[string]interface{}{"text": "Olá da {{campvars.escola}}"}},
		}}},
	}}, noAgents{})
	if err != nil {
		t.Fatalf("NewStep: %v", err)
	}
	return step
}

type editFixture struct {
	stored    *mockCampaignRepo
	campaigns *countedCampaigns
	entries   *mockEntryRepo
	templates *mockTemplateRepo
	uc        *updateCampaignUseCase
}

func newEditFixture(t *testing.T) editFixture {
	t.Helper()
	stored, fixture := updateFixture(t, "")
	fixtureUC := fixture.(*updateCampaignUseCase)
	counted := &countedCampaigns{mockCampaignRepo: stored}
	entries := newMockEntryRepo()
	templates := fixtureUC.templateRepo.(*mockTemplateRepo)
	uc := NewUpdateCampaignUseCase(counted, entries, templates, fixtureUC.businessPhoneRepo, nil).(*updateCampaignUseCase)
	uc.SetTemplateGrants(allowGrants{})
	uc.SetExitRiskGate(admittingGate{})
	uc.SetAutomation(schoolWorkflowStep(t))
	return editFixture{stored: stored, campaigns: counted, entries: entries, templates: templates, uc: uc}
}

func (f editFixture) savedEntry(id string, status wce.SendStatus, metadata map[string]interface{}, variables ...string) {
	f.entries.entries[id] = &wce.WhatsAppCampaignEntry{ID: id, CampaignID: "camp-1", LeadID: "lead-" + id, Status: status, Metadata: metadata, Variables: variables}
}

func (f editFixture) current() *wc.Campaign {
	return f.stored.campaigns["camp-1"]
}

func TestAnEditThatKeepsTheAutomationNeverReadsTheSavedEntries(t *testing.T) {
	f := newEditFixture(t)
	f.current().WorkflowID = "wf-school"
	f.current().EnableWorkflow = true
	f.savedEntry("e1", wce.SendStatusPending, nil)

	in := &wc.Campaign{Name: "Matrículas 2027", WorkflowID: "wf-school", EnableWorkflow: true}
	if _, err := f.uc.Execute(context.Background(), "camp-1", in); err != nil {
		t.Fatalf("a rename must not be refused by entries the automation already accepted: %v", err)
	}
	if f.current().Name != "Matrículas 2027" {
		t.Fatalf("name = %q", f.current().Name)
	}
}

func TestANewAutomationIsCheckedAgainstTheSavedEntriesNotTheEditForm(t *testing.T) {
	f := newEditFixture(t)
	f.savedEntry("e1", wce.SendStatusPending, map[string]interface{}{"escola": "Prisma"})
	f.savedEntry("e2", wce.SendStatusSent, nil)
	formCopy := []wc.PhoneInput{{Number: "5511999990000", Metadata: map[string]interface{}{"escola": "Prisma"}}}

	in := &wc.Campaign{Name: "Matrículas", WorkflowID: "wf-school", EnableWorkflow: true, PhoneInputs: formCopy}
	_, err := f.uc.Execute(context.Background(), "camp-1", in)
	if !errors.Is(err, campaign.ErrWorkflowVarsMissing) {
		t.Fatalf("Execute = %v, want ErrWorkflowVarsMissing for the saved entry without the variable", err)
	}
	if f.campaigns.updates != 0 || f.current().WorkflowID != "" {
		t.Fatalf("a refused automation was saved: %+v", f.current())
	}
}

func TestANewAutomationEverySavedEntryFillsIsSaved(t *testing.T) {
	f := newEditFixture(t)
	f.savedEntry("e1", wce.SendStatusPending, map[string]interface{}{"escola": "Prisma"})
	f.savedEntry("e2", wce.SendStatusRead, map[string]interface{}{"escola": "Áurea"})

	in := &wc.Campaign{Name: "Matrículas", WorkflowID: "wf-school", EnableWorkflow: true}
	if _, err := f.uc.Execute(context.Background(), "camp-1", in); err != nil {
		t.Fatalf("Execute = %v", err)
	}
	if f.current().WorkflowID != "wf-school" || !f.current().EnableWorkflow {
		t.Fatalf("automation not saved: %+v", f.current())
	}
}

func TestAnEditOfACampaignWhoseTemplateWasLaterPausedStillSaves(t *testing.T) {
	f := newEditFixture(t)
	f.templates.templates["tmpl-1"].Status = template.TemplateStatusPaused

	if _, err := f.uc.Execute(context.Background(), "camp-1", &wc.Campaign{Name: "Novo nome", TemplateID: "tmpl-1", BusinessPhoneID: "phone-1"}); err != nil {
		t.Fatalf("a template the edit does not touch must not block it: %v", err)
	}
	if _, err := f.uc.Execute(context.Background(), "camp-1", &wc.Campaign{Name: "Novo nome", TemplateID: "tmpl-2", BusinessPhoneID: "phone-1"}); err != nil {
		t.Fatalf("moving to a ready template = %v", err)
	}
	f.templates.templates["tmpl-1"].Status = template.TemplateStatusPaused
	if _, err := f.uc.Execute(context.Background(), "camp-1", &wc.Campaign{Name: "Novo nome", TemplateID: "tmpl-1"}); err == nil {
		t.Fatal("moving to a paused template must be refused")
	}
}

func TestANewTemplateMustBeFillableByTheEntriesStillWaitingToBeSent(t *testing.T) {
	f := newEditFixture(t)
	f.templates.templates["tmpl-2"].Components = []template.TemplateComponent{{Type: "BODY", Text: "Olá {{1}}"}}
	f.savedEntry("e1", wce.SendStatusSent, nil)
	f.savedEntry("e2", wce.SendStatusPending, nil, "Bia")

	if _, err := f.uc.Execute(context.Background(), "camp-1", &wc.Campaign{Name: "Matrículas", TemplateID: "tmpl-2"}); err != nil {
		t.Fatalf("entries already sent are not sent again with the new template: %v", err)
	}

	f.current().TemplateID = "tmpl-1"
	f.savedEntry("e3", wce.SendStatusPending, nil)
	_, err := f.uc.Execute(context.Background(), "camp-1", &wc.Campaign{Name: "Matrículas", TemplateID: "tmpl-2"})
	if !errors.Is(err, wc.ErrCampaignTemplateVariablesMismatch) {
		t.Fatalf("Execute = %v, want ErrCampaignTemplateVariablesMismatch", err)
	}
}

func TestAnEditNeverChangesWhetherTheCampaignIsArchived(t *testing.T) {
	f := newEditFixture(t)
	f.current().Archived = true

	if _, err := f.uc.Execute(context.Background(), "camp-1", &wc.Campaign{Name: "Matrículas"}); err != nil {
		t.Fatalf("Execute = %v", err)
	}
	if !f.current().Archived {
		t.Fatal("editing an archived campaign unarchived it")
	}
}

func TestArchivingTouchesOnlyTheArchiveFlag(t *testing.T) {
	f := newEditFixture(t)
	f.templates.templates["tmpl-1"].Status = template.TemplateStatusPaused

	if err := f.uc.SetArchived(context.Background(), "camp-1", true); err != nil {
		t.Fatalf("archiving a campaign whose template was paused = %v", err)
	}
	if !f.current().Archived || f.current().Name != "Matrículas" {
		t.Fatalf("archive = %+v", f.current())
	}
	if err := f.uc.SetArchived(context.Background(), "camp-1", false); err != nil || f.current().Archived {
		t.Fatalf("unarchive = %v, archived %v", err, f.current().Archived)
	}
	if err := f.uc.SetArchived(context.Background(), "", true); !errors.Is(err, wc.ErrCampaignNotFound) {
		t.Fatalf("SetArchived without an id = %v", err)
	}
}

func TestAnEditSavesTheScheduledStart(t *testing.T) {
	f := newEditFixture(t)
	start := time.Now().Add(2 * time.Hour).Truncate(time.Second)

	if _, err := f.uc.Execute(context.Background(), "camp-1", &wc.Campaign{Name: "Matrículas", ScheduledStart: start}); err != nil {
		t.Fatalf("Execute = %v", err)
	}
	if !f.current().ScheduledStart.Equal(start) {
		t.Fatalf("scheduled start = %v, want %v", f.current().ScheduledStart, start)
	}

	if _, err := f.uc.Execute(context.Background(), "camp-1", &wc.Campaign{Name: "Matrículas"}); err != nil {
		t.Fatalf("Execute = %v", err)
	}
	if !f.current().ScheduledStart.IsZero() {
		t.Fatalf("turning the schedule off kept %v", f.current().ScheduledStart)
	}
}

func TestAScheduledStartOutsideTheWindowIsRefused(t *testing.T) {
	cases := []struct {
		name  string
		start time.Time
		want  error
	}{
		{name: "in the past", start: time.Now().Add(-time.Hour), want: wc.ErrCampaignScheduledStartInvalid},
		{name: "too soon", start: time.Now().Add(time.Minute), want: wc.ErrCampaignScheduledStartTooSoon},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newEditFixture(t)
			if _, err := f.uc.Execute(context.Background(), "camp-1", &wc.Campaign{Name: "Matrículas", ScheduledStart: tc.start}); !errors.Is(err, tc.want) {
				t.Fatalf("Execute = %v, want %v", err, tc.want)
			}
			if f.campaigns.updates != 0 {
				t.Fatal("a refused schedule was saved")
			}
		})
	}
}

func TestAKeptScheduleIsNotRevalidated(t *testing.T) {
	f := newEditFixture(t)
	kept := time.Now().Add(time.Minute).UTC()
	f.current().ScheduledStart = kept

	if _, err := f.uc.Execute(context.Background(), "camp-1", &wc.Campaign{Name: "Novo nome", ScheduledStart: kept}); err != nil {
		t.Fatalf("an unchanged schedule must not block a rename: %v", err)
	}
}

func TestARunningCampaignCannotBeRescheduled(t *testing.T) {
	f := newEditFixture(t)
	f.current().Status = wc.CampaignStatusRunning
	start := time.Now().Add(2 * time.Hour)

	if _, err := f.uc.Execute(context.Background(), "camp-1", &wc.Campaign{Name: "Matrículas", ScheduledStart: start}); !errors.Is(err, wc.ErrCampaignRunning) {
		t.Fatalf("Execute = %v, want ErrCampaignRunning", err)
	}
	if _, err := f.uc.Execute(context.Background(), "camp-1", &wc.Campaign{Name: "Matrículas 2"}); err != nil {
		t.Fatalf("a running campaign can still be renamed: %v", err)
	}
}
