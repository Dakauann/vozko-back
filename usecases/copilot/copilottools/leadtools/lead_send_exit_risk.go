package leadtools

import (
	"context"
	"strings"

	"vozko/domain/copilot"
	"vozko/domain/leadaction"
	"vozko/usecases/copilot/copilottools"
)

func (t *prepareLeadActionTool) Choices(ctx context.Context, cc copilot.Context, args map[string]interface{}) ([]copilot.ChoiceField, error) {
	templateID, sends := templateOfSend(args)
	if !sends {
		return nil, nil
	}
	verdict, err := copilottools.ExitVerdictOf(ctx, t.deps.Exit, cc, templateID)
	if err != nil {
		return nil, err
	}
	return copilottools.ExitRiskChoices(verdict), nil
}

func (t *prepareLeadActionTool) refuseUnaskedExitRisk(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	choices, err := t.Choices(ctx, cc, args)
	if err != nil {
		return refusalOf(err)
	}
	return copilottools.RefuseUnaskedExitRisk(args, choices)
}

func (t *prepareLeadActionTool) exitFields(ctx context.Context, cc copilot.Context, p leadActionPlan) []copilot.Field {
	if p.request.Action != leadaction.ActionSendTemplate || p.request.Params.Send == nil {
		return nil
	}
	verdict, err := copilottools.ExitVerdictOf(ctx, t.deps.Exit, cc, p.request.Params.Send.TemplateID)
	if err != nil {
		return []copilot.Field{{Key: "risks", Value: "não foi possível ler a saída do lead deste modelo"}}
	}
	return []copilot.Field{copilottools.ExitVerdictField(verdict)}
}

func templateOfSend(args map[string]interface{}) (string, bool) {
	var a prepareLeadActionArgs
	copilottools.BindArgs(args, &a)
	if leadaction.Action(a.Action) != leadaction.ActionSendTemplate {
		return "", false
	}
	return strings.TrimSpace(a.TemplateID), true
}

func withoutCardChoices(args map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(args))
	for key, value := range args {
		if key != copilottools.ExitRiskChoiceKey {
			out[key] = value
		}
	}
	return out
}
