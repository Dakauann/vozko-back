package copilottools

import (
	"context"
	"log"

	"vozko/domain/copilot"
	"vozko/domain/readiness"
	"vozko/domain/tools"
	"vozko/domain/workspace"
)

type offerActionArgs struct {
	Kind string `json:"kind" req:"true" desc:"o cartão a mostrar"`
}

type offerActionTool struct{ readiness readiness.SnapshotUseCase }

func NewOfferActionTool(snapshot readiness.SnapshotUseCase) copilot.Tool {
	return &offerActionTool{readiness: snapshot}
}

func (t *offerActionTool) Meta() copilot.Meta {
	return copilot.Meta{Resource: workspace.ResourceAIChat, Action: workspace.ActionRead}
}

func (t *offerActionTool) Definition() tools.Definition {
	def := definition("offer_action",
		"Mostra na conversa um cartão com o próximo passo quando falta algo no workspace: conectar um número do WhatsApp "+
			"oficial ou não oficial, Instagram, Telegram, recarregar o saldo ou regularizar a assinatura. O cartão mostra os limites "+
			"do plano e a permissão do usuário e leva ao fluxo certo; não execute nada por conta própria. Depois de chamar, "+
			"explique em uma frase o que o cartão permite.", offerActionArgs{})
	param := def.Parameters["kind"]
	for _, k := range copilot.ActionKinds() {
		param.Enum = append(param.Enum, string(k))
	}
	def.Parameters["kind"] = param
	return def
}

func (t *offerActionTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a offerActionArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	kind := copilot.ActionKind(a.Kind)
	if !kind.Valid() {
		return copilot.Result{Status: copilot.StatusError, Message: "kind desconhecido"}
	}
	snap, err := t.readiness.Snapshot(ctx, readiness.Person{WorkspaceID: cc.WorkspaceID, UserID: cc.UserID, SystemAdmin: cc.SystemAdmin})
	if err != nil {
		log.Printf("[copilot] offer_action snapshot failed: %v", err)
		return copilot.Result{Status: copilot.StatusError, Message: "não foi possível ler o estado do workspace agora"}
	}
	card, ok := copilot.NewActionCard(kind, snap)
	if !ok {
		return copilot.Result{Status: copilot.StatusError, Message: "esse recurso não está disponível neste workspace"}
	}
	data := map[string]interface{}{"card_shown": string(kind), "can_act": true}
	if card.Status != nil {
		data["can_act"] = card.Status.CanAdd
		if card.Status.Blocker != "" {
			data["blocker"] = card.Status.Blocker
		}
		if card.Status.Usage != nil {
			data["used"], data["total"] = card.Status.Usage.Used, card.Status.Usage.Total
		}
	}
	return copilot.Result{Status: copilot.StatusOK, Data: data, Card: card}
}
