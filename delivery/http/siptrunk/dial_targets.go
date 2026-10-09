package siptrunk

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/lead"
	lead_usecase "vozko/usecases/lead"
)

type DialTargetPlanner interface {
	Plan(ctx context.Context, actor lead_usecase.Actor, leadID string) (*lead_usecase.LeadDialPlan, error)
	Lines(ctx context.Context, actor lead_usecase.Actor) (*lead_usecase.LeadDialPlan, error)
}

func (h *Handler) WithDialTargets(planner DialTargetPlanner) *Handler {
	h.dialTargets = planner
	return h
}

// @Summary		Números e linhas para ligar a um lead
// @Description	Diz para quais números de um lead você pode ligar e por quais linhas, com o motivo quando não pode. É a mesma regra que a ligação segue no WebSocket de chamadas (`start_call` com `lead_id`), então o botão "Ligar" mostra o que vai acontecer antes de ligar.
// @Description
// @Description	- `numbers`: o WhatsApp do lead (`identity: true`) primeiro e depois os telefones de contato, já no formato de discagem. Um número com `refusal` não pode ser chamado: `blocked` (o lead está bloqueado, ou o número é o WhatsApp de outro lead bloqueado) ou `invalid_number` (nenhuma linha consegue discar esse número). Um telefone de contato traz o `phoneId` do telefone no cadastro do lead; o WhatsApp não traz.
// @Description	- `refusal`: o motivo quando nenhum número pode ser chamado: `no_number` (o lead não tem telefone) ou o motivo do primeiro número (`blocked`, `opted_out`, `invalid_number`).
// @Description	- `callable`: o número que o "Ligar" do lead chama, o primeiro sem `refusal`; as linhas em `trunks` foram conferidas para ele. Não vem quando nenhum número pode ser chamado.
// @Description	- `trunks`: as linhas conectadas que podem fazer a ligação. Só vêm quando algum número pode ser chamado. Com mais de uma, você escolhe; a escolhida vai em `trunk_id`.
// @Description	- `trunkRefusal`: `unauthorized` (falta `sip_trunks:call`) ou `no_dialable_trunk` (nenhuma linha conectada pode ligar agora).
// @Description
// @Description	Sem `leadId` (o discador com um número digitado), a resposta traz só as linhas: `numbers` vem vazio, `leadId` vem vazio e `trunks`/`trunkRefusal` seguem a mesma regra. Nesse caso `leads:read` não é exigido.
// @Description
// @Description	Com `leadId`, exige também `leads:read`, porque a resposta traz os telefones do lead.
// @Tags			Troncos SIP
// @Produce		json
// @Param			leadId	query		string	false	"ID do lead; sem ele, só as linhas"
// @Success		200		{object}	DialTargetsResponse
// @Failure		401		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Failure		503		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/dial-targets [get]
func (h *Handler) DialTargets(w http.ResponseWriter, r *http.Request) {
	if h.dialTargets == nil {
		response.WriteErrorWithCode(w, http.StatusServiceUnavailable, "not_configured", "Calls to leads are not configured on this server", nil)
		return
	}
	if _, ok := httpx.RequireWorkspace(w, r); !ok {
		return
	}
	actor, ok := httpx.LeadActor(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, "authentication required", nil)
		return
	}
	leadID := strings.TrimSpace(r.URL.Query().Get("leadId"))
	var plan *lead_usecase.LeadDialPlan
	var err error
	if leadID == "" {
		plan, err = h.dialTargets.Lines(r.Context(), actor)
	} else {
		plan, err = h.dialTargets.Plan(r.Context(), actor, leadID)
	}
	switch {
	case errors.Is(err, lead.ErrLeadForbidden):
		response.WriteErrorWithCode(w, http.StatusForbidden, lead.ErrorCode(err), "You need leads:read to see the numbers of a lead", nil)
	case errors.Is(err, lead.ErrLeadNotFound):
		response.WriteErrorWithCode(w, http.StatusNotFound, lead.ErrorCode(err), "Lead not found", nil)
	case err != nil || plan == nil:
		response.WriteError(w, http.StatusInternalServerError, "internal server error", nil)
	default:
		response.WriteSuccess(w, http.StatusOK, toDialTargetsResponse(plan))
	}
}

func toDialTargetsResponse(plan *lead_usecase.LeadDialPlan) DialTargetsResponse {
	out := DialTargetsResponse{
		LeadID:       plan.LeadID,
		Refusal:      string(plan.Refusal),
		Callable:     plan.Callable,
		Numbers:      make([]DialTargetNumberResponse, 0, len(plan.Numbers)),
		Trunks:       make([]DialTargetTrunkResponse, 0, len(plan.Trunks)),
		TrunkRefusal: string(plan.TrunkRefusal),
	}
	for _, number := range plan.Numbers {
		out.Numbers = append(out.Numbers, DialTargetNumberResponse{
			Number: number.Number, Identity: number.Identity, Label: string(number.Label), PhoneID: number.PhoneID, Refusal: string(number.Refusal),
		})
	}
	for _, trunk := range plan.Trunks {
		out.Trunks = append(out.Trunks, DialTargetTrunkResponse{ID: trunk.ID, Name: trunk.Name})
	}
	return out
}
