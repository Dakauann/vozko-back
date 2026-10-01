package copilottools

import (
	"context"
	"errors"
	"log"
	"strings"

	"vozko/domain/copilot"
	"vozko/domain/sip_trunk"
	"vozko/domain/tools"
	"vozko/domain/workspace"
)

type CallDeps struct {
	Planner sip_trunk.CallPlanner
}

type placeCallArgs struct {
	PhoneNumber string `json:"phone_number" req:"true" desc:"telefone exato a ligar, como veio de get_lead ou read_conversation; nunca invente"`
	TrunkID     string `json:"trunk_id" id:"true" desc:"trunk_id listado por esta ferramenta (é o mesmo line_id de list_phone_lines); omita para usar a única linha disponível ou deixar o usuário escolher"`
}

type placeCallTool struct{ deps CallDeps }

func NewPlaceCallTool(deps CallDeps) copilot.Tool { return placeCallTool{deps: deps} }

func (placeCallTool) Meta() copilot.Meta {
	return copilot.Meta{Resource: workspace.ResourceSIPTrunks, Action: workspace.ActionCall}
}

func (placeCallTool) Definition() tools.Definition {
	return definition("place_call",
		"Mostra na conversa um cartão para o usuário ligar para um telefone por uma linha telefônica do workspace. A ligação só começa "+
			"quando o usuário clica em Ligar no cartão, usando o microfone dele: nunca diga que a ligação foi feita ou atendida. "+
			"Com mais de uma linha disponível, o usuário escolhe no discador; se ele pedir uma linha pelo nome, chame de novo com o trunk_id listado.",
		placeCallArgs{})
}

func (t placeCallTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a placeCallArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	plan, err := t.deps.Planner.Plan(ctx, sip_trunk.CallPlanInput{
		WorkspaceID: cc.WorkspaceID,
		UserID:      cc.UserID,
		IsAdmin:     cc.SystemAdmin,
		TrunkID:     strings.TrimSpace(a.TrunkID),
		PhoneNumber: a.PhoneNumber,
	})
	if err != nil {
		return callRefusal(err)
	}
	data := map[string]interface{}{
		"card_shown":   "place_call",
		"phone_number": plan.PhoneNumber,
		"trunks":       trunkChoices(plan.Trunks),
		"trunk":        "o usuário escolhe a linha no discador",
	}
	if trunk, ok := plan.Chosen(); ok {
		data["trunk"] = trunk.Name
	}
	return copilot.Result{Status: copilot.StatusOK, Card: copilot.NewCallCard(*plan), Data: data}
}

func trunkChoices(choices []sip_trunk.TrunkChoice) []map[string]string {
	out := make([]map[string]string, 0, len(choices))
	for _, c := range choices {
		out = append(out, map[string]string{"trunk_id": c.ID, "name": c.Name})
	}
	return out
}

func callRefusal(err error) copilot.Result {
	switch {
	case errors.Is(err, sip_trunk.ErrCallNotPermitted):
		return copilot.Result{Status: copilot.StatusDenied, Message: "o usuário não tem permissão para ligar pelas linhas telefônicas deste workspace"}
	case errors.Is(err, sip_trunk.ErrInvalidPhoneNumber):
		return copilot.Result{Status: copilot.StatusError, Message: "telefone inválido: use apenas dígitos, *, # e um + no início"}
	case errors.Is(err, sip_trunk.ErrNoDialableTrunk):
		return copilot.Result{Status: copilot.StatusError, Message: "nenhuma linha pode ligar agora: é preciso uma linha ligada, que faça ligações e esteja conectada à operadora; confira com list_phone_lines ou ofereça abrir a tela de linhas telefônicas"}
	case errors.Is(err, sip_trunk.ErrTrunkNotFound):
		return copilot.Result{Status: copilot.StatusError, Message: "linha não encontrada; omita trunk_id ou use um trunk_id listado por esta ferramenta"}
	case errors.Is(err, sip_trunk.ErrTrunkDisabled), errors.Is(err, sip_trunk.ErrTrunkCannotDial), errors.Is(err, sip_trunk.ErrTrunkNotRegistered):
		return copilot.Result{Status: copilot.StatusError, Message: "essa linha não pode ligar agora (desligada, só recebe ligações ou não está conectada à operadora); omita trunk_id para usar outra"}
	default:
		log.Printf("[copilot] place_call: %v", err)
		return copilot.Result{Status: copilot.StatusError, Message: "não foi possível preparar a ligação agora"}
	}
}
