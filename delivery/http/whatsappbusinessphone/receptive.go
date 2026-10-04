package whatsappbusinessphone

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	wc "vozko/domain/whatsapp_campaign"
	"vozko/infra/http/middleware"
)

type ReceptiveUseCase interface {
	Get(workspaceID, phoneID string) (wc.ReceptiveSettings, error)
	Update(workspaceID, phoneID string, settings wc.ReceptiveSettings) (wc.ReceptiveSettings, error)
}

func (h *WhatsAppBusinessPhoneHandler) SetReceptive(receptive ReceptiveUseCase) {
	h.receptive = receptive
}

// @Summary		Consultar a automação do número
// @Description	Retorna a automação de atendimento receptivo do número (agente ou fluxo, funil e análises), no mesmo formato dos outros canais. Só o workspace dono do número pode consultar e configurar; workspaces com acesso concedido recebem 403.
// @Tags			Telefones do WhatsApp Business
// @Produce		json
// @Param			id	path		string	true	"ID do telefone"
// @Success		200	{object}	NumberAutomationResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Failure		503	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/whatsapp/business-phones/{id}/automation [get]
func (h *WhatsAppBusinessPhoneHandler) GetNumberAutomation(w http.ResponseWriter, r *http.Request) {
	if h.receptive == nil {
		response.WriteError(w, http.StatusServiceUnavailable, "Number automation is unavailable", nil)
		return
	}
	id := mux.Vars(r)["id"]
	settings, err := h.receptive.Get(middleware.GetWorkspaceID(r), id)
	if err != nil {
		writeReceptiveError(w, err, "Failed to load the number automation")
		return
	}
	response.WriteSuccess(w, http.StatusOK, numberAutomationResponse(id, settings))
}

// @Summary		Configurar a automação do número
// @Description	Define quem atende as conversas receptivas do número (agente ou fluxo), o funil e as análises. Vale para todas as conversas receptivas do número; conversas de campanhas seguem a própria campanha. Só o workspace dono do número pode configurar.
// @Tags			Telefones do WhatsApp Business
// @Accept			json
// @Produce		json
// @Param			id		path		string					true	"ID do telefone"
// @Param			request	body		NumberAutomationRequest	true	"Automação do número"
// @Success		200	{object}	NumberAutomationResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Failure		503	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/whatsapp/business-phones/{id}/automation [put]
func (h *WhatsAppBusinessPhoneHandler) UpdateNumberAutomation(w http.ResponseWriter, r *http.Request) {
	if h.receptive == nil {
		response.WriteError(w, http.StatusServiceUnavailable, "Number automation is unavailable", nil)
		return
	}
	var body NumberAutomationRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}
	id := mux.Vars(r)["id"]
	settings, err := h.receptive.Update(middleware.GetWorkspaceID(r), id, body.settings())
	if err != nil {
		writeReceptiveError(w, err, "Failed to save the number automation")
		return
	}
	response.WriteSuccess(w, http.StatusOK, numberAutomationResponse(id, settings))
}

func writeReceptiveError(w http.ResponseWriter, err error, fallback string) {
	switch {
	case errors.Is(err, wc.ErrReceptiveNotOwner):
		response.WriteError(w, http.StatusForbidden, "Only the workspace that owns this number configures its automation", nil)
	case errors.Is(err, wc.ErrCampaignBusinessPhoneNotFound):
		response.WriteError(w, http.StatusNotFound, "WhatsApp Business phone number not found", nil)
	default:
		log.Printf("[business-phone] number automation: %v", err)
		response.WriteError(w, http.StatusInternalServerError, fallback, nil)
	}
}
