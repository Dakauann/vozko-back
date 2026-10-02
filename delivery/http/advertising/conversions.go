package advertisinghttp

import (
	"net/http"

	"vozko/delivery/http/response"
	"vozko/domain/advertising"
)

type ConnectDatasetRequest struct {
	BusinessPhoneID string `json:"businessPhoneId"`
}

type DatasetResponse struct {
	DatasetID string `json:"datasetId"`
}

// @Summary		Configuração de conversões
// @Description	Para onde o Vozko envia os eventos do CRM (negócio criado vira LeadSubmitted, negócio ganho vira Purchase): conjunto de dados do WhatsApp ou pixel.
// @Tags			Anúncios
// @Produce		json
// @Success		200	{object}	advertising.ConversionSettings
// @Security		BearerAuth
// @Router			/ads/conversions/settings [get]
func (h *Handler) ConversionSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := h.d.Conversions.Settings(r.Context(), workspaceOf(r))
	if err != nil {
		writeError(w, err, "Failed to load the conversion settings")
		return
	}
	response.WriteSuccess(w, http.StatusOK, settings)
}

// @Summary		Salvar configuração de conversões
// @Tags			Anúncios
// @Accept			json
// @Produce		json
// @Param			body	body		advertising.ConversionSettings	true	"configuração"
// @Success		200		{object}	advertising.ConversionSettings
// @Failure		422		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/conversions/settings [put]
func (h *Handler) SaveConversionSettings(w http.ResponseWriter, r *http.Request) {
	var settings advertising.ConversionSettings
	if !decodeJSON(w, r, &settings) {
		return
	}
	saved, err := h.d.Conversions.Save(r.Context(), workspaceOf(r), settings)
	if err != nil {
		writeError(w, err, "Failed to save the conversion settings")
		return
	}
	response.WriteSuccess(w, http.StatusOK, saved)
}

// @Summary		Conectar conjunto de dados do WhatsApp
// @Description	Usa o conjunto de dados da conta do WhatsApp Business do número oficial escolhido para enviar as conversões. Precisa de uma conta de anúncios na configuração.
// @Tags			Anúncios
// @Accept			json
// @Produce		json
// @Param			body	body		ConnectDatasetRequest	true	"número oficial do WhatsApp"
// @Success		200		{object}	DatasetResponse
// @Failure		404		{object}	response.ErrorResponse
// @Failure		422		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/conversions/dataset [post]
func (h *Handler) ConnectDataset(w http.ResponseWriter, r *http.Request) {
	var req ConnectDatasetRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	settings, err := h.d.Conversions.ConnectDataset(r.Context(), workspaceOf(r), req.BusinessPhoneID)
	if err != nil {
		writeError(w, err, "Failed to connect the dataset")
		return
	}
	response.WriteSuccess(w, http.StatusOK, DatasetResponse{DatasetID: settings.DatasetID})
}

// @Summary		Conversões enviadas recentemente
// @Tags			Anúncios
// @Produce		json
// @Success		200	{array}	advertising.ConversionRecord
// @Security		BearerAuth
// @Router			/ads/conversions/recent [get]
func (h *Handler) RecentConversions(w http.ResponseWriter, r *http.Request) {
	records, err := h.d.Conversions.Recent(r.Context(), workspaceOf(r))
	if err != nil {
		writeError(w, err, "Failed to list recent conversions")
		return
	}
	response.WriteSuccess(w, http.StatusOK, nonNil(records))
}
