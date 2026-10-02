package advertisinghttp

import (
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	"vozko/domain/advertising"
)

type RuleStatusRequest struct {
	Enabled *bool `json:"enabled"`
}

// @Summary		Regras automáticas da conta
// @Tags			Anúncios
// @Produce		json
// @Param			id	path		string	true	"ID da conta de anúncios"
// @Success		200	{array}		advertising.AutomatedRule
// @Failure		409	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/accounts/{id}/rules [get]
func (h *Handler) Rules(w http.ResponseWriter, r *http.Request) {
	rules, err := h.d.Rules.List(r.Context(), workspaceOf(r), mux.Vars(r)["id"])
	if err != nil {
		writeError(w, err, "Failed to list automated rules")
		return
	}
	response.WriteSuccess(w, http.StatusOK, nonNil(rules))
}

// @Summary		Criar regra automática
// @Description	Regra da Meta que verifica condições (métrica, operador, valor) numa janela e pausa, liga, muda o orçamento por porcentagem ou avisa.
// @Tags			Anúncios
// @Accept			json
// @Produce		json
// @Param			body	body		advertising.AutomatedRule	true	"regra"
// @Success		201		{object}	MetaIDResponse
// @Failure		422		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/rules [post]
func (h *Handler) CreateRule(w http.ResponseWriter, r *http.Request) {
	var rule advertising.AutomatedRule
	if !decodeJSON(w, r, &rule) {
		return
	}
	id, err := h.d.Rules.Create(r.Context(), workspaceOf(r), rule)
	if err != nil {
		writeError(w, err, "Failed to create the automated rule")
		return
	}
	response.WriteSuccess(w, http.StatusCreated, MetaIDResponse{MetaID: id})
}

// @Summary		Ligar ou desligar regra automática
// @Tags			Anúncios
// @Accept			json
// @Param			ruleId		path	string				true	"ID da regra na Meta"
// @Param			accountId	query	string				true	"ID da conta de anúncios"
// @Param			body		body	RuleStatusRequest	true	"enabled obrigatório"
// @Success		204
// @Failure		404	{object}	response.ErrorResponse
// @Failure		422	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/rules/{ruleId}/status [post]
func (h *Handler) SetRuleStatus(w http.ResponseWriter, r *http.Request) {
	var req RuleStatusRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Enabled == nil {
		writeError(w, advertising.FieldError("enabled", "required"), "Failed to change the automated rule")
		return
	}
	accountID, err := requiredQuery(r, "accountId")
	if err != nil {
		writeError(w, err, "Failed to change the automated rule")
		return
	}
	if err := h.d.Rules.SetEnabled(r.Context(), workspaceOf(r), accountID, mux.Vars(r)["ruleId"], *req.Enabled); err != nil {
		writeError(w, err, "Failed to change the automated rule")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// @Summary		Excluir regra automática
// @Tags			Anúncios
// @Param			ruleId		path	string	true	"ID da regra na Meta"
// @Param			accountId	query	string	true	"ID da conta de anúncios"
// @Success		204
// @Failure		404	{object}	response.ErrorResponse
// @Failure		422	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/rules/{ruleId} [delete]
func (h *Handler) DeleteRule(w http.ResponseWriter, r *http.Request) {
	accountID, err := requiredQuery(r, "accountId")
	if err != nil {
		writeError(w, err, "Failed to delete the automated rule")
		return
	}
	if err := h.d.Rules.Delete(r.Context(), workspaceOf(r), accountID, mux.Vars(r)["ruleId"]); err != nil {
		writeError(w, err, "Failed to delete the automated rule")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// @Summary		Histórico da regra automática
// @Tags			Anúncios
// @Produce		json
// @Param			ruleId		path		string	true	"ID da regra na Meta"
// @Param			accountId	query		string	true	"ID da conta de anúncios"
// @Success		200			{array}		advertising.RuleRun
// @Failure		404			{object}	response.ErrorResponse
// @Failure		422			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/rules/{ruleId}/history [get]
func (h *Handler) RuleHistory(w http.ResponseWriter, r *http.Request) {
	accountID, err := requiredQuery(r, "accountId")
	if err != nil {
		writeError(w, err, "Failed to load the rule history")
		return
	}
	runs, err := h.d.Rules.History(r.Context(), workspaceOf(r), accountID, mux.Vars(r)["ruleId"])
	if err != nil {
		writeError(w, err, "Failed to load the rule history")
		return
	}
	response.WriteSuccess(w, http.StatusOK, nonNil(runs))
}
