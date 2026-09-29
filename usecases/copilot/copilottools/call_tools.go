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
	TrunkID     string `json:"trunk_id" id:"true" desc:"trunk_id listado por uma resposta anterior desta ferramenta; omita para usar o único tronco disponível ou deixar o usuário escolher"`
}

type placeCallTool struct{ deps CallDeps }

func NewPlaceCallTool(deps CallDeps) copilot.Tool { return placeCallTool{deps: deps} }

func (placeCallTool) Meta() copilot.Meta {
	return copilot.Meta{Resource: workspace.ResourceSIPTrunks, Action: workspace.ActionCall}
}

func (placeCallTool) Definition() tools.Definition {
	return definition("place_call",
		"Mostra na conversa um cartão para o usuário ligar para um telefone por um tronco SIP do workspace. A ligação só começa "+
			"quando o usuário clica em Ligar no cartão, usando o microfone dele: nunca diga que a ligação foi feita ou atendida. "+
			"Com mais de um tronco disponível, o usuário escolhe no discador; se ele pedir um tronco pelo nome, chame de novo com o trunk_id listado.",
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
		"trunk":        "o usuário escolhe o tronco no discador",
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
		return copilot.Result{Status: copilot.StatusDenied, Message: "o usuário não tem permissão para ligar pelos troncos deste workspace"}
	case errors.Is(err, sip_trunk.ErrInvalidPhoneNumber):
		return copilot.Result{Status: copilot.StatusError, Message: "telefone inválido: use apenas dígitos, *, # e um + no início"}
	case errors.Is(err, sip_trunk.ErrNoDialableTrunk):
		return copilot.Result{Status: copilot.StatusError, Message: "nenhum tronco pode ligar agora: é preciso um tronco habilitado, de saída e registrado no provedor; ofereça abrir a tela de troncos SIP"}
	case errors.Is(err, sip_trunk.ErrTrunkNotFound):
		return copilot.Result{Status: copilot.StatusError, Message: "tronco não encontrado; omita trunk_id ou use um trunk_id listado por esta ferramenta"}
	case errors.Is(err, sip_trunk.ErrTrunkDisabled), errors.Is(err, sip_trunk.ErrTrunkCannotDial), errors.Is(err, sip_trunk.ErrTrunkNotRegistered):
		return copilot.Result{Status: copilot.StatusError, Message: "esse tronco não pode ligar agora (desligado, só de entrada ou sem registro no provedor); omita trunk_id para usar outro"}
	default:
		log.Printf("[copilot] place_call: %v", err)
		return copilot.Result{Status: copilot.StatusError, Message: "não foi possível preparar a ligação agora"}
	}
}
