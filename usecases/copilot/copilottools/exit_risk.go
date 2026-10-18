package copilottools

import (
	"context"
	"fmt"
	"strings"

	"vozko/domain/buttonaction"
	"vozko/domain/copilot"
	buttonaction_usecase "vozko/usecases/buttonaction"
)

type ExitVerdicts interface {
	ExitVerdict(ctx context.Context, a buttonaction_usecase.Actor, templateID, campaignID string) (*buttonaction_usecase.VerdictView, error)
}

func ButtonActor(cc copilot.Context) buttonaction_usecase.Actor {
	return buttonaction_usecase.Actor{WorkspaceID: cc.WorkspaceID, UserID: cc.UserID, IsAdmin: cc.SystemAdmin, Departments: cc.Departments}
}

func ExitVerdictOf(ctx context.Context, exits ExitVerdicts, cc copilot.Context, templateID string) (buttonaction.Verdict, error) {
	if exits == nil {
		return buttonaction.Verdict{}, buttonaction.ErrUnavailable
	}
	view, err := exits.ExitVerdict(ctx, ButtonActor(cc), templateID, "")
	if err != nil {
		return buttonaction.Verdict{}, err
	}
	return view.Verdict, nil
}

const (
	ExitRiskChoiceKey                       = "exit_risk_acknowledgement"
	ExitRiskChoiceKind   copilot.ChoiceKind = "exit_risk_acknowledgement"
	ExitRiskAcknowledged                    = "acknowledged"
)

func ExitRiskChoices(verdict buttonaction.Verdict) []copilot.ChoiceField {
	if buttonaction.RequireExitRiskAcknowledged(verdict, nil) == nil {
		return nil
	}
	return []copilot.ChoiceField{{Key: ExitRiskChoiceKey, Kind: ExitRiskChoiceKind}}
}

func ExitRiskAcknowledgedBy(cc copilot.Context, args map[string]interface{}) string {
	if value, ok := args[ExitRiskChoiceKey].(string); ok && value == ExitRiskAcknowledged {
		return cc.UserID
	}
	return ""
}

func RefuseUnaskedExitRisk(args map[string]interface{}, asked []copilot.ChoiceField) error {
	if _, present := args[ExitRiskChoiceKey]; !present {
		return nil
	}
	for _, choice := range asked {
		if choice.Key == ExitRiskChoiceKey {
			return nil
		}
	}
	return fmt.Errorf("%w: %s só é marcado pelo usuário no cartão de aprovação; nunca o envie", ErrInvalidArgs, ExitRiskChoiceKey)
}

func ExitVerdictField(verdict buttonaction.Verdict) copilot.Field {
	if buttonaction.RequireExitRiskAcknowledged(verdict, nil) != nil {
		return copilot.Field{Key: "risks", Value: "O modelo não mostra ao lead como parar de receber mensagens. Para seguir, o usuário marca no cartão que entende os riscos."}
	}
	return copilot.Field{Key: "leadExit", Value: strings.Join(exitsOf(verdict), ", ")}
}

func exitsOf(verdict buttonaction.Verdict) []string {
	var exits []string
	for _, button := range verdict.Buttons {
		if button.LeadsOut() {
			exits = append(exits, fmt.Sprintf("%s (%s)", button.Text, button.Action.Kind))
		}
	}
	for _, word := range verdict.ExitWords {
		exits = append(exits, buttonaction.ExitCommandPrefix+word)
	}
	return exits
}
