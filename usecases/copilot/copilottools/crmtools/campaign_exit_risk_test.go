package crmtools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vozko/domain/buttonaction"
	"vozko/domain/copilot"
	buttonaction_usecase "vozko/usecases/buttonaction"
	"vozko/usecases/copilot/copilottools"
	"vozko/usecases/copilot/copilottools/tooltest"
)

type fakeExitVerdicts struct {
	state      buttonaction.VerdictState
	err        error
	asked      buttonaction_usecase.Actor
	templateID string
}

func (f *fakeExitVerdicts) ExitVerdict(_ context.Context, a buttonaction_usecase.Actor, templateID, _ string) (*buttonaction_usecase.VerdictView, error) {
	f.asked, f.templateID = a, templateID
	if f.err != nil {
		return nil, f.err
	}
	verdict := buttonaction.Verdict{State: f.state}
	if f.state == buttonaction.VerdictOK {
		verdict.ExitWords = []string{"sair"}
	}
	return &buttonaction_usecase.VerdictView{Verdict: verdict}, nil
}

func withoutExit() campaignFixture {
	f := newCampaignFixture()
	f.exit.state = buttonaction.VerdictNone
	f.create.withoutExit = true
	return f
}

func proposed(t *testing.T, tool copilot.Tool, cc copilot.Context, args map[string]interface{}) (map[string]interface{}, []copilot.ChoiceField) {
	t.Helper()
	choices, err := tool.(copilot.ChoiceAsker).Choices(creating(cc), cc, args)
	if err != nil {
		t.Fatal(err)
	}
	return copilot.StripChoices(args, choices), choices
}

func TestACampaignWithoutAnExitAsksTheOperatorOnTheCard(t *testing.T) {
	f := withoutExit()
	cc := tooltest.Member()
	tool := NewCreateCampaignTool(f.deps)

	_, choices := proposed(t, tool, cc, createCampaignArgsFor(nil))
	if len(choices) != 1 || choices[0].Kind != copilottools.ExitRiskChoiceKind || choices[0].Default != "" {
		t.Fatalf("choices = %+v", choices)
	}
	if f.exit.templateID != knownTemplate || f.exit.asked.UserID != cc.UserID || f.exit.asked.WorkspaceID != cc.WorkspaceID {
		t.Fatalf("verdict asked for %q as %+v", f.exit.templateID, f.exit.asked)
	}
	fields := tool.(copilot.Describer).Describe(creating(cc), cc, createCampaignArgsFor(nil))
	if tooltest.FieldValue(fields, "risks") == "" {
		t.Fatalf("the card does not show the risk: %+v", fields)
	}

	withExit := newCampaignFixture()
	if _, choices := proposed(t, NewCreateCampaignTool(withExit.deps), cc, createCampaignArgsFor(nil)); len(choices) != 0 {
		t.Fatalf("a template with an exit asked for an acknowledgement: %+v", choices)
	}
	if got := tooltest.FieldValue(NewCreateCampaignTool(withExit.deps).(copilot.Describer).Describe(creating(cc), cc, createCampaignArgsFor(nil)), "leadExit"); got != "/sair" {
		t.Fatalf("leadExit = %q", got)
	}
}

func TestTheModelCannotAcknowledgeTheExitRiskThroughToolArguments(t *testing.T) {
	f := withoutExit()
	cc := tooltest.Member()
	tool := NewCreateCampaignTool(f.deps)
	modelArgs := createCampaignArgsFor(map[string]interface{}{copilottools.ExitRiskChoiceKey: copilottools.ExitRiskAcknowledged})

	args, _ := proposed(t, tool, cc, modelArgs)
	res := tool.Execute(creating(cc), cc, args)
	if res.Status == copilot.StatusOK || f.create.got == nil || f.create.got.ExitRiskAcknowledgedBy != "" {
		t.Fatalf("the model's acknowledgement went through: %+v, created %+v", res, f.create.got)
	}
	if !strings.Contains(res.Message, "cartão") {
		t.Fatalf("the refusal does not point to the card: %q", res.Message)
	}

	withExit := newCampaignFixture()
	if err := NewCreateCampaignTool(withExit.deps).(copilot.Validator).Validate(creating(cc), cc, modelArgs); !errors.Is(err, copilottools.ErrInvalidArgs) {
		t.Fatalf("an acknowledgement nobody asked for = %v, want refused", err)
	}
}

func TestTheOperatorTickOnTheCardCreatesTheCampaignWithTheAcknowledgement(t *testing.T) {
	f := withoutExit()
	cc := tooltest.Member()
	tool := NewCreateCampaignTool(f.deps)

	args, choices := proposed(t, tool, cc, createCampaignArgsFor(nil))
	approved, err := copilot.WithChoices(args, choices, map[string]string{copilottools.ExitRiskChoiceKey: copilottools.ExitRiskAcknowledged})
	if err != nil {
		t.Fatal(err)
	}
	if err := tool.(copilot.Validator).Validate(creating(cc), cc, approved); err != nil {
		t.Fatalf("Validate = %v", err)
	}
	res := tool.Execute(creating(cc), cc, approved)
	if res.Status != copilot.StatusOK || f.create.got.ExitRiskAcknowledgedBy != cc.UserID {
		t.Fatalf("result = %+v, acknowledged by %q", res, f.create.got.ExitRiskAcknowledgedBy)
	}
}

func TestTheCardFailsClosedWhenTheVerdictCannotBeRead(t *testing.T) {
	f := newCampaignFixture()
	f.exit.err = errors.New("db down")
	cc := tooltest.Member()
	if _, err := NewCreateCampaignTool(f.deps).(copilot.ChoiceAsker).Choices(creating(cc), cc, createCampaignArgsFor(nil)); err == nil {
		t.Fatal("a proposal went out without knowing the exit verdict")
	}
}

func TestThePreviewReportsTheExitVerdict(t *testing.T) {
	f := withoutExit()
	res := NewPreviewCampaignImportTool(f.deps).Execute(context.Background(), tooltest.Member(), map[string]interface{}{"media_id": knownSheet, "template_id": knownTemplate})
	if res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	if data := res.Data.(map[string]interface{}); data["lead_exit"] != "none" || data["lead_exit_note"] == nil {
		t.Fatalf("data = %+v", data)
	}
}
