package lead

import (
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	leaddomain "vozko/domain/lead"
	lead_usecase "vozko/usecases/lead"
)

// @Summary		Cadastrar um familiar do lead
// @Description	Cria um novo lead (o familiar) e o liga a este lead numa só gravação. `kind` diz o que o novo lead é deste lead: `spouse`, `partner`, `parent`, `child`, `sibling`, `grandparent`, `grandchild`, `uncle_aunt`, `nephew_niece`, `cousin`, `in_law`, `relative` (família) ou `referred` (indicado por este lead) e `referred_by` (quem indicou este lead). `relative` aceita os mesmos campos de POST /leads, telefones e endereços inclusive. Com `copyPrimaryAddress`, o familiar recebe uma cópia do endereço principal deste lead, com a posição no mapa; sem endereço principal a resposta é 400 `lead_no_primary_address`. Exige `leads:update` e `leads:create`. A resposta traz este lead com a contagem atualizada, o familiar, a relação vista deste lead e, em `duplicates`, leads que podem ser a mesma pessoa (mesmo telefone, ou mesmo nome no mesmo endereço; este lead nunca aparece). Se o número de WhatsApp do familiar já é de outro lead, a resposta é 409 `lead_identity_taken` com `expected.leadId` (só para quem lê leads) para ligar aquele lead por POST /leads/{id}/relations. Códigos: 400 `invalid_body`, `lead_relation_kind_invalid`, `lead_identity_required`, `lead_phone_*`, `lead_address_*`; 404 `lead_not_found`; 409 `lead_identity_taken`.
// @Tags			Leads
// @Accept			json
// @Produce		json
// @Param			id		path		string				true	"ID do lead (UUID)"
// @Param			request	body		AddRelativeRequest	true	"Familiar e parentesco"
// @Success		201		{object}	lead.AddRelativeResponse
// @Failure		400		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Failure		409		{object}	response.ErrorResponse
// @Failure		500		{object}	response.ErrorResponse
// @Failure		503		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/{id}/relatives [post]
func (h *LeadHandler) AddRelative(w http.ResponseWriter, r *http.Request) {
	a, ok := h.commandActor(w, r)
	if !ok {
		return
	}
	var req AddRelativeRequest
	if !httpx.DecodeStrictJSON(w, r, &req) {
		return
	}
	if req.Kind == "" || req.Relative == nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"kind":     "string (required)",
			"relative": "object (required; the fields of POST /leads)",
		})
		return
	}
	result, err := h.commands.AddRelative(r.Context(), a, mux.Vars(r)["id"], lead_usecase.AddRelativeInput{
		Kind:               leaddomain.RelationKind(req.Kind),
		Relative:           req.Relative.toDomain(),
		CopyPrimaryAddress: req.CopyPrimaryAddress,
	})
	if err != nil {
		h.writeCommandError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, toRelativeResponse(result, h.now()))
}

// @Summary		Ligar dois leads
// @Description	Liga este lead a outro lead do workspace. `kind` diz o que o outro lead é deste lead (os mesmos valores de POST /leads/{id}/relatives). Cada par tem no máximo uma relação de família e uma de indicação: um irmão que também indicou o lead são duas relações. As contagens `relativesCount` e `referredCount` dos dois leads mudam na mesma gravação, e os dois avançam a `version`. Não exige `If-Match`. Códigos: 400 `invalid_body`, `lead_relation_kind_invalid`, `lead_relation_self`; 404 `lead_not_found`, `lead_relative_not_found` (o outro lead não existe neste workspace); 409 `lead_relation_exists`.
// @Tags			Leads
// @Accept			json
// @Produce		json
// @Param			id		path		string				true	"ID do lead (UUID)"
// @Param			request	body		LinkRelationRequest	true	"Outro lead e parentesco"
// @Success		201		{object}	lead.LinkRelationResponse
// @Failure		400		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Failure		409		{object}	response.ErrorResponse
// @Failure		500		{object}	response.ErrorResponse
// @Failure		503		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/{id}/relations [post]
func (h *LeadHandler) LinkRelation(w http.ResponseWriter, r *http.Request) {
	a, ok := h.commandActor(w, r)
	if !ok {
		return
	}
	var req LinkRelationRequest
	if !httpx.DecodeStrictJSON(w, r, &req) {
		return
	}
	if req.OtherLeadID == "" || req.Kind == "" {
		response.WriteInvalidBodyError(w, map[string]string{
			"otherLeadId": "string (required; a lead of this workspace)",
			"kind":        "string (required)",
		})
		return
	}
	id := mux.Vars(r)["id"]
	result, err := h.commands.LinkRelation(r.Context(), a, id, req.OtherLeadID, leaddomain.RelationKind(req.Kind))
	if err != nil {
		h.writeCommandError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, LinkRelationResponse{
		Lead:     toLeadRecord(result.Lead, h.now()),
		Relation: relationResponse(result.Relation, id),
	})
}

// @Summary		Desfazer uma relação entre leads
// @Description	Remove a relação de família ou de indicação. As contagens dos dois leads mudam na mesma gravação e os dois avançam a `version`. Exige `leads:update`. Códigos: 404 `lead_relation_not_found`.
// @Tags			Leads
// @Param			id	path	string	true	"ID da relação (UUID)"
// @Success		204
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Failure		503	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/lead-relations/{id} [delete]
func (h *LeadHandler) RemoveRelation(w http.ResponseWriter, r *http.Request) {
	a, ok := h.commandActor(w, r)
	if !ok {
		return
	}
	if _, err := h.commands.RemoveRelation(r.Context(), a, mux.Vars(r)["id"]); err != nil {
		h.writeCommandError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
