package dialerringchannels

import (
	"encoding/json"
	"errors"
	"net/http"

	"vozko/delivery/http/response"
	"vozko/domain/workspace"
	"vozko/infra/http/middleware"
	dialer_usecase "vozko/usecases/dialer"
)

type DialerRingChannelsHandler struct {
	uc *dialer_usecase.SetRingChannelsUseCase
}

func NewDialerRingChannelsHandler(uc *dialer_usecase.SetRingChannelsUseCase) *DialerRingChannelsHandler {
	return &DialerRingChannelsHandler{uc: uc}
}

// @Summary		Consultar os canais de toque do próprio membro
// @Description	Retorna a seleção de canais de dispositivo que tocam nas chamadas recebidas pelo membro autenticado (dialer no navegador, ramal SIP, ...).
// @Tags			Dialer
// @Produce		json
// @Success		200	{object}	RingChannelsResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/dialer/ring-channels [get]
func (h *DialerRingChannelsHandler) GetMine(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	wsID := middleware.GetWorkspaceID(r)
	if claims == nil || wsID == "" {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	channels, err := h.uc.Get(wsID, claims.UserID)
	if err != nil {
		if errors.Is(err, workspace.ErrMemberNotFound) {
			response.WriteError(w, http.StatusNotFound, "Member not found", nil)
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "Failed to read ring channels", nil)
		return
	}
	response.WriteSuccess(w, http.StatusOK, RingChannelsResponse{Channels: ringChannelsToStrings(channels)})
}

// @Summary		Atualizar os canais de toque do próprio membro
// @Description	Define quais canais de dispositivo tocam nas chamadas recebidas pelo membro autenticado.
// @Tags			Dialer
// @Accept			json
// @Produce		json
// @Param			request	body		SetRingChannelsRequest	true	"Canais que devem tocar"
// @Success		200		{object}	RingChannelsResponse
// @Failure		400		{object}	response.ErrorResponse
// @Failure		401		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Failure		500		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/dialer/ring-channels [put]
func (h *DialerRingChannelsHandler) SetMine(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	wsID := middleware.GetWorkspaceID(r)
	if claims == nil || wsID == "" {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	var req SetRingChannelsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}
	if errs := req.Validate(); errs != nil {
		response.WriteValidationError(w, errs)
		return
	}
	channels := make([]workspace.RingChannel, len(req.Channels))
	for i, c := range req.Channels {
		channels[i] = workspace.RingChannel(c)
	}
	if err := h.uc.Execute(wsID, claims.UserID, channels); err != nil {
		switch {
		case errors.Is(err, workspace.ErrInvalidRingChannel):
			response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
		case errors.Is(err, workspace.ErrMemberNotFound):
			response.WriteError(w, http.StatusNotFound, "Member not found", nil)
		default:
			response.WriteError(w, http.StatusInternalServerError, "Failed to update ring channels", nil)
		}
		return
	}
	response.WriteSuccess(w, http.StatusOK, RingChannelsResponse{Channels: req.Channels})
}

func ringChannelsToStrings(channels []workspace.RingChannel) []string {
	out := make([]string, len(channels))
	for i, c := range channels {
		out[i] = string(c)
	}
	return out
}
