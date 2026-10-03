package advertisinghttp

import (
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	"vozko/domain/advertising"
)

type AudienceListResponse struct {
	TermsAccepted bool                   `json:"termsAccepted"`
	TermsURL      string                 `json:"termsUrl"`
	Audiences     []advertising.Audience `json:"audiences"`
}

type CustomerListResponse struct {
	Audience advertising.Audience `json:"audience"`
	Matched  int                  `json:"matched"`
	Skipped  int                  `json:"skipped"`
}

// @Summary		Públicos personalizados da conta
// @Description	Públicos da conta na Meta e se os termos de públicos personalizados foram aceitos (sem eles, não dá para criar listas nem semelhantes).
// @Tags			Anúncios
// @Produce		json
// @Param			id	path		string	true	"ID da conta de anúncios"
// @Success		200	{object}	AudienceListResponse
// @Failure		409	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/accounts/{id}/audiences [get]
func (h *Handler) Audiences(w http.ResponseWriter, r *http.Request) {
	list, err := h.d.Audience.List(r.Context(), workspaceOf(r), mux.Vars(r)["id"])
	if err != nil {
		writeError(w, err, "Failed to list audiences")
		return
	}
	response.WriteSuccess(w, http.StatusOK, AudienceListResponse{TermsAccepted: list.TermsAccepted, TermsURL: list.TermsURL, Audiences: nonNil(list.Audiences)})
}

// @Summary		Criar lista de clientes
// @Description	Cria um público com clientes do CRM (por filtro) ou de um arquivo CSV da biblioteca de mídia. Os dados são normalizados e enviados com hash SHA-256.
// @Tags			Anúncios
// @Accept			json
// @Produce		json
// @Param			body	body		advertising.CustomerListDraft	true	"lista de clientes"
// @Success		201		{object}	CustomerListResponse
// @Failure		409		{object}	response.ErrorResponse
// @Failure		422		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/audiences/customer-list [post]
func (h *Handler) CreateCustomerList(w http.ResponseWriter, r *http.Request) {
	var draft advertising.CustomerListDraft
	if !decodeJSON(w, r, &draft) {
		return
	}
	result, err := h.d.Audience.CreateCustomerList(r.Context(), workspaceOf(r), draft)
	if err != nil {
		writeError(w, err, "Failed to create the customer list")
		return
	}
	response.WriteSuccess(w, http.StatusCreated, CustomerListResponse{Audience: result.Audience, Matched: result.Matched, Skipped: result.Skipped})
}

// @Summary		Criar público semelhante
// @Tags			Anúncios
// @Accept			json
// @Produce		json
// @Param			body	body		advertising.LookalikeDraft	true	"público semelhante (1 a 10 por cento)"
// @Success		201		{object}	advertising.Audience
// @Failure		409		{object}	response.ErrorResponse
// @Failure		422		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/audiences/lookalike [post]
func (h *Handler) CreateLookalike(w http.ResponseWriter, r *http.Request) {
	var draft advertising.LookalikeDraft
	if !decodeJSON(w, r, &draft) {
		return
	}
	audience, err := h.d.Audience.CreateLookalike(r.Context(), workspaceOf(r), draft)
	if err != nil {
		writeError(w, err, "Failed to create the lookalike audience")
		return
	}
	response.WriteSuccess(w, http.StatusCreated, audience)
}

// @Summary		Excluir público personalizado
// @Tags			Anúncios
// @Param			metaId		path	string	true	"ID do público na Meta"
// @Param			accountId	query	string	true	"ID da conta de anúncios"
// @Success		204
// @Failure		404	{object}	response.ErrorResponse
// @Failure		422	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/audiences/{metaId} [delete]
func (h *Handler) DeleteAudience(w http.ResponseWriter, r *http.Request) {
	accountID, err := requiredQuery(r, "accountId")
	if err != nil {
		writeError(w, err, "Failed to delete the audience")
		return
	}
	if err := h.d.Audience.Delete(r.Context(), workspaceOf(r), accountID, mux.Vars(r)["metaId"]); err != nil {
		writeError(w, err, "Failed to delete the audience")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// @Summary		Públicos salvos
// @Description	Combinações de público e posicionamentos salvas no Vozko para reaproveitar ao criar anúncios.
// @Tags			Anúncios
// @Produce		json
// @Success		200	{array}	advertising.SavedAudience
// @Security		BearerAuth
// @Router			/ads/saved-audiences [get]
func (h *Handler) SavedAudiences(w http.ResponseWriter, r *http.Request) {
	saved, err := h.d.Audience.SavedList(r.Context(), workspaceOf(r))
	if err != nil {
		writeError(w, err, "Failed to list saved audiences")
		return
	}
	response.WriteSuccess(w, http.StatusOK, nonNil(saved))
}

func (h *Handler) saveAudience(w http.ResponseWriter, r *http.Request, id string, status int) {
	var saved advertising.SavedAudience
	if !decodeJSON(w, r, &saved) {
		return
	}
	saved.ID = id
	out, err := h.d.Audience.Save(r.Context(), workspaceOf(r), personOf(r).UserID, saved)
	if err != nil {
		writeError(w, err, "Failed to save the audience")
		return
	}
	response.WriteSuccess(w, status, out)
}

// @Summary		Salvar público
// @Tags			Anúncios
// @Accept			json
// @Produce		json
// @Param			body	body		advertising.SavedAudience	true	"público salvo"
// @Success		201		{object}	advertising.SavedAudience
// @Failure		422		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/saved-audiences [post]
func (h *Handler) CreateSavedAudience(w http.ResponseWriter, r *http.Request) {
	h.saveAudience(w, r, "", http.StatusCreated)
}

// @Summary		Atualizar público salvo
// @Tags			Anúncios
// @Accept			json
// @Produce		json
// @Param			id		path		string						true	"ID do público salvo"
// @Param			body	body		advertising.SavedAudience	true	"público salvo"
// @Success		200		{object}	advertising.SavedAudience
// @Failure		404		{object}	response.ErrorResponse
// @Failure		422		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/saved-audiences/{id} [put]
func (h *Handler) UpdateSavedAudience(w http.ResponseWriter, r *http.Request) {
	h.saveAudience(w, r, mux.Vars(r)["id"], http.StatusOK)
}

// @Summary		Excluir público salvo
// @Tags			Anúncios
// @Param			id	path	string	true	"ID do público salvo"
// @Success		204
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/saved-audiences/{id} [delete]
func (h *Handler) DeleteSavedAudience(w http.ResponseWriter, r *http.Request) {
	if err := h.d.Audience.DeleteSaved(r.Context(), workspaceOf(r), mux.Vars(r)["id"]); err != nil {
		writeError(w, err, "Failed to delete the saved audience")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
