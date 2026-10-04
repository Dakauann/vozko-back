package advertisinghttp

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	"vozko/domain/advertising"
)

type ConnectStartResponse struct {
	AuthorizeURL string `json:"authorizeUrl"`
}

type SpendCapRequest struct {
	Amount nullableAmount `json:"amount" swaggertype:"integer" extensions:"x-nullable"`
}

type nullableAmount struct {
	present bool
	value   *int64
}

func (a *nullableAmount) UnmarshalJSON(raw []byte) error {
	a.present = true
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		a.value = nil
		return nil
	}
	var amount int64
	if err := json.Unmarshal(raw, &amount); err != nil {
		return err
	}
	a.value = &amount
	return nil
}

// @Summary		Listar contas de anúncios
// @Tags			Anúncios
// @Produce		json
// @Success		200	{array}		AccountResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/accounts [get]
func (h *Handler) ListAccounts(w http.ResponseWriter, r *http.Request) {
	accounts, err := h.d.Accounts.List(r.Context(), workspaceOf(r))
	if err != nil {
		writeError(w, err, "Failed to list ad accounts")
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentAll(accounts, presentAccount))
}

// @Summary		Sincronizar conta de anúncios
// @Description	Atualiza a conta, as campanhas, os conjuntos, os anúncios e os resultados dos últimos dias a partir da Meta.
// @Tags			Anúncios
// @Produce		json
// @Param			id	path		string	true	"ID da conta de anúncios"
// @Success		200	{object}	AccountResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		409	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/accounts/{id}/sync [post]
func (h *Handler) SyncAccount(w http.ResponseWriter, r *http.Request) {
	account, err := h.d.Sync.Sync(r.Context(), workspaceOf(r), mux.Vars(r)["id"])
	if err != nil {
		writeError(w, err, "Failed to sync the ad account")
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentAccount(account))
}

// @Summary		Desconectar conta de anúncios
// @Tags			Anúncios
// @Param			id	path	string	true	"ID da conta de anúncios"
// @Success		204
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/accounts/{id} [delete]
func (h *Handler) DisconnectAccount(w http.ResponseWriter, r *http.Request) {
	if err := h.d.Accounts.Disconnect(r.Context(), workspaceOf(r), mux.Vars(r)["id"]); err != nil {
		writeError(w, err, "Failed to disconnect the ad account")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// @Summary		Definir limite de gastos da conta
// @Description	Valor em unidades mínimas da moeda da conta (centavos para BRL), acima do que a conta já gastou. amount null remove o limite; o campo é obrigatório. Conta pré-paga não aceita limite manual (409 prepaid_spend_cap): a Meta usa os fundos adicionados como limite. Conta cuja cobrança não foi lida também recusa (409 billing_unknown) até a próxima sincronização.
// @Tags			Anúncios
// @Accept			json
// @Produce		json
// @Param			id		path		string			true	"ID da conta de anúncios"
// @Param			body	body		SpendCapRequest	true	"novo limite ou null"
// @Success		200		{object}	AccountResponse
// @Failure		400		{object}	response.ErrorResponse
// @Failure		409		{object}	response.ErrorResponse
// @Failure		422		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/accounts/{id}/spend-cap [put]
func (h *Handler) SetSpendCap(w http.ResponseWriter, r *http.Request) {
	var req SpendCapRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if !req.Amount.present {
		writeError(w, advertising.FieldError("amount", "required"), "Failed to change the spend cap")
		return
	}
	account, err := h.d.Manage.SetSpendCap(r.Context(), workspaceOf(r), mux.Vars(r)["id"], req.Amount.value)
	if err != nil {
		writeError(w, err, "Failed to change the spend cap")
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentAccount(account))
}
