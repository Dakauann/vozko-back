package leadtools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vozko/domain/buttonaction"
	"vozko/domain/campaign"
	"vozko/domain/copilot"
	buttonaction_usecase "vozko/usecases/buttonaction"
	"vozko/usecases/copilot/copilottools"
	"vozko/usecases/copilot/copilottools/tooltest"
	leadaction_usecase "vozko/usecases/leadaction"
)

type fakeExitVerdicts struct {
	state      buttonaction.VerdictState
	templateID string
	calls      int
}

func (f *fakeExitVerdicts) ExitVerdict(_ context.Context, _ buttonaction_usecase.Actor, templateID, _ string) (*buttonaction_usecase.VerdictView, error) {
	f.calls++
	f.templateID = templateID
	return &buttonaction_usecase.VerdictView{Verdict: buttonaction.Verdict{State: f.state}}, nil
}

func templateSendHarness(state buttonaction.VerdictState) *actionHarness {
	preview := counted(5, 5, nil)
	preview.Send = &campaign.SendQuote{Count: 5, Parts: 1, CostMicros: 400000, BalanceMicros: 10000000, Affordable: true, Currency: "USD"}
	h := newActionHarness(preview)
	h.exit.state = state
	h.actions.outcome = leadaction_usecase.Outcome{Send: &campaign.SendReview{Channel: campaign.ChannelOfficial}}
	return h
}

func templateSendArgs(extra map[string]interface{}) map[string]interface{} {
	args := map[string]interface{}{"action": "send_template", "name": "Convite", "business_phone_id": somePhone, "template_id": someTmpl, "cidade": "sp:campinas"}
	for k, v := range extra {
		args[k] = v
	}
	return args
}

func proposedSend(t *testing.T, tool copilot.Tool, cc copilot.Context, args map[string]interface{}) (map[string]interface{}, []copilot.ChoiceField) {
	t.Helper()
	choices, err := tool.(copilot.ChoiceAsker).Choices(context.Background(), cc, args)
	if err != nil {
		t.Fatal(err)
	}
	stripped := copilot.StripChoices(args, choices)
	fields := propose(t, tool, cc, stripped)
	if len(choices) > 0 && tooltest.FieldValue(fields, "risks") == "" {
		t.Fatalf("the card does not show the risk: %+v", fields)
	}
	return stripped, choices
}

func TestATemplateSendWithoutAnExitAsksTheOperatorOnTheCard(t *testing.T) {
	h := templateSendHarness(buttonaction.VerdictNone)
	tool := NewPrepareLeadActionTool(h.deps)

	_, choices := proposedSend(t, tool, manager(), templateSendArgs(nil))
	if len(choices) != 1 || choices[0].Kind != copilottools.ExitRiskChoiceKind || h.exit.templateID != someTmpl {
		t.Fatalf("choices = %+v, verdict of %q", choices, h.exit.templateID)
	}

	other := templateSendHarness(buttonaction.VerdictNone)
	block, err := NewPrepareLeadActionTool(other.deps).(copilot.ChoiceAsker).Choices(context.Background(), manager(), map[string]interface{}{"action": "block", "blocked": true, "cidade": "sp:campinas"})
	if err != nil || len(block) != 0 || other.exit.calls != 0 {
		t.Fatalf("an action that sends nothing asked about the exit: %+v %v", block, err)
	}
}

func TestTheModelCannotAcknowledgeTheExitRiskOfALeadSend(t *testing.T) {
	h := templateSendHarness(buttonaction.VerdictNone)
	tool := NewPrepareLeadActionTool(h.deps)

	args, _ := proposedSend(t, tool, manager(), templateSendArgs(map[string]interface{}{copilottools.ExitRiskChoiceKey: copilottools.ExitRiskAcknowledged}))
	tool.Execute(context.Background(), manager(), args)
	if len(h.actions.starts) != 1 || h.actions.starts[0].Params.Send.ExitRiskAcknowledged {
		t.Fatalf("the model's acknowledgement reached the send: %+v", h.actions.starts)
	}

	ok := templateSendHarness(buttonaction.VerdictOK)
	err := NewPrepareLeadActionTool(ok.deps).(copilot.Validator).Validate(context.Background(), manager(), templateSendArgs(map[string]interface{}{copilottools.ExitRiskChoiceKey: copilottools.ExitRiskAcknowledged}))
	if !errors.Is(err, copilottools.ErrInvalidArgs) || !strings.Contains(err.Error(), "cartão") {
		t.Fatalf("an acknowledgement nobody asked for = %v", err)
	}
}

func TestTheOperatorTickReachesTheLeadSend(t *testing.T) {
	h := templateSendHarness(buttonaction.VerdictNone)
	tool := NewPrepareLeadActionTool(h.deps)

	args, choices := proposedSend(t, tool, manager(), templateSendArgs(nil))
	approved, err := copilot.WithChoices(args, choices, map[string]string{copilottools.ExitRiskChoiceKey: copilottools.ExitRiskAcknowledged})
	if err != nil {
		t.Fatal(err)
	}
	res := approve(t, tool, manager(), approved)
	if res.Status != copilot.StatusOK || len(h.actions.starts) != 1 || !h.actions.starts[0].Params.Send.ExitRiskAcknowledged {
		t.Fatalf("result = %+v, starts %+v", res, h.actions.starts)
	}
}

func TestARefusedLeadSendPointsToTheCard(t *testing.T) {
	h := templateSendHarness(buttonaction.VerdictNone)
	h.actions.startErr = buttonaction.ErrExitRiskNotAcknowledged
	tool := NewPrepareLeadActionTool(h.deps)
	args, _ := proposedSend(t, tool, manager(), templateSendArgs(nil))
	res := tool.Execute(context.Background(), manager(), args)
	if res.Status == copilot.StatusOK || !strings.Contains(res.Message, "cartão") {
		t.Fatalf("result = %+v", res)
	}
}
