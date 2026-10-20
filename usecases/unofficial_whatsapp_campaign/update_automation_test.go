package unofficial_whatsapp_campaign

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/agent"
	"vozko/domain/campaign"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	"vozko/domain/workflow"
	"vozko/usecases/campaignautomation"
)

type schoolWorkflows struct{}

func (schoolWorkflows) Execute(string) (*workflow.Workflow, error) {
	return &workflow.Workflow{ID: "wf-school", WorkspaceID: "ws-1", Graph: workflow.Graph{Nodes: []workflow.Node{
		{ID: "n1", Config: map[string]interface{}{"text": "Olá da {{campvars.escola}}"}},
	}}}, nil
}

type absentAgents struct{}

func (absentAgents) Execute(string) (*agent.Agent, error) { return nil, agent.ErrAgentNotFound }

func schoolUpdate(t *testing.T) (*fakeCampaignRepo, *fakeEntryRepo, uwc.UpdateCampaignUseCase) {
	t.Helper()
	campaigns, entries, gateway := stoppedUnofficial("")
	step, err := campaignautomation.NewStep(schoolWorkflows{}, absentAgents{})
	if err != nil {
		t.Fatalf("NewStep: %v", err)
	}
	uc := NewUpdateCampaignUseCase(campaigns, entries, gateway)
	uc.(*updateCampaignUseCase).SetAutomation(step)
	return campaigns, entries, uc
}

func withWorkflow(in *uwc.Campaign) *uwc.Campaign {
	in.WorkflowID = "wf-school"
	in.EnableWorkflow = true
	return in
}

func TestAnUnofficialEditChecksANewAutomationAgainstTheSavedEntries(t *testing.T) {
	campaigns, entries, uc := schoolUpdate(t)
	entries.put(&uwc.Entry{ID: "e1", CampaignID: "camp-1", Number: "5511999990001"})
	in := withWorkflow(edited("Olá {{1}}"))
	in.Targets = []uwc.TargetInput{{Number: "5511999990001", Metadata: map[string]interface{}{"escola": "Prisma"}}}

	if _, err := uc.Execute(context.Background(), "camp-1", in, uw.Unrestricted()); !errors.Is(err, campaign.ErrWorkflowVarsMissing) {
		t.Fatalf("Execute = %v, want ErrWorkflowVarsMissing from the saved entry", err)
	}
	if got, _ := campaigns.FindByID("camp-1"); got.WorkflowID != "" {
		t.Fatalf("a refused automation was saved: %+v", got)
	}
}

func TestAnUnofficialEditThatKeepsTheAutomationIsNotRefusedByOldEntries(t *testing.T) {
	campaigns, entries, uc := schoolUpdate(t)
	current, _ := campaigns.FindByID("camp-1")
	campaigns.put(withWorkflow(current))
	entries.put(&uwc.Entry{ID: "e1", CampaignID: "camp-1", Number: "5511999990001"})

	renamed := withWorkflow(edited("Olá {{1}}"))
	renamed.Name = "Matrículas outubro"
	if saved, err := uc.Execute(context.Background(), "camp-1", renamed, uw.Unrestricted()); err != nil || saved.Name != "Matrículas outubro" {
		t.Fatalf("Execute = %+v, %v", saved, err)
	}
}
