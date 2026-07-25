package systemconfig

import (
	"encoding/json"
	"errors"
	"net/http"

	"vozko/delivery/http/response"
	"vozko/domain/config"
	"vozko/infra/http/middleware"
)

type SystemConfigHandler struct {
	getConfig    config.GetSystemConfigUseCase
	updateConfig config.UpdateSystemConfigUseCase
}

func NewSystemConfigHandler(getConfig config.GetSystemConfigUseCase, updateConfig config.UpdateSystemConfigUseCase) *SystemConfigHandler {
	return &SystemConfigHandler{
		getConfig:    getConfig,
		updateConfig: updateConfig,
	}
}

func (h *SystemConfigHandler) Get(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.getConfig.Execute(r.Context())
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to get system configuration", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, toSystemConfigResponse(cfg))
}

func (h *SystemConfigHandler) Update(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	var input config.UpdateSystemConfigInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"baseSystemPrompt":   "string (optional)",
			"maxConcurrentCalls": "number (optional, min: 1)",
			"workTimeEnabled":    "boolean (optional)",
			"workTimeStart":      "string HH:MM format (optional, e.g., '08:00')",
			"workTimeEnd":        "string HH:MM format (optional, e.g., '20:00')",
		})
		return
	}

	cfg, err := h.updateConfig.Execute(r.Context(), claims.UserID, claims.Role, input)
	if err != nil {
		if errors.Is(err, config.ErrUnauthorized) {
			response.WriteError(w, http.StatusForbidden, "Only administrators can modify system configuration", nil)
			return
		}
		if errors.Is(err, config.ErrInvalidWorkTimeSlot) {
			response.WriteError(w, http.StatusBadRequest, "Work time start must be before work time end", nil)
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "Failed to update system configuration", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, toSystemConfigResponse(cfg))
}
