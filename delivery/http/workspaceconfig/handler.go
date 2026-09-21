package workspaceconfig

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	"vozko/domain/working_hours"
	workspaceconfigdomain "vozko/domain/workspace_config"
	"vozko/infra/http/middleware"
)

const maxConfigBodyBytes = 64 << 10

type WorkspaceConfigHandler struct {
	getConfig         workspaceconfigdomain.GetWorkspaceConfigUseCase
	updateConfig      workspaceconfigdomain.UpdateWorkspaceConfigUseCase
	updateOwnerConfig workspaceconfigdomain.UpdateWorkspaceConfigOwnerUseCase
}

func NewWorkspaceConfigHandler(getConfig workspaceconfigdomain.GetWorkspaceConfigUseCase, updateConfig workspaceconfigdomain.UpdateWorkspaceConfigUseCase, updateOwnerConfig workspaceconfigdomain.UpdateWorkspaceConfigOwnerUseCase) *WorkspaceConfigHandler {
	return &WorkspaceConfigHandler{
		getConfig:         getConfig,
		updateConfig:      updateConfig,
		updateOwnerConfig: updateOwnerConfig,
	}
}

// @Summary		Obter configuração do workspace
// @Description	Retorna as configurações do workspace: proteção contra spam de campanha, atribuição de administradores, modo da roleta, música de espera, política da fila de atendimento e encerramento automático de conversas.
// @Tags			Configuração do workspace
// @Produce		json
// @Param			workspaceId	path		string	true	"ID do workspace"
// @Success		200			{object}	WorkspaceConfigResponse
// @Failure		400			{object}	response.ErrorResponse
// @Failure		500			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/workspaces/{workspaceId}/config [get]
func (h *WorkspaceConfigHandler) Get(w http.ResponseWriter, r *http.Request) {
	workspaceID := mux.Vars(r)["workspaceId"]
	if workspaceID == "" {
		response.WriteError(w, http.StatusBadRequest, "Missing workspaceId", nil)
		return
	}

	cfg, err := h.getConfig.Execute(r.Context(), workspaceID)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to get workspace configuration", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, toWorkspaceConfigResponse(cfg))
}

// @Summary		Atualizar configuração do workspace
// @Description	Atualiza as preferências do workspace: atribuição de administradores, modo da roleta (online ou última vez online, com janela e resgate), música de espera, política da fila de atendimento e encerramento automático de conversas.
// @Tags			Configuração do workspace
// @Accept			json
// @Produce		json
// @Param			workspaceId	path		string												true	"ID do workspace"
// @Param			request		body		workspace_config.UpdateWorkspaceConfigOwnerInput	true	"Campos de configuração a atualizar"
// @Success		200			{object}	WorkspaceConfigResponse
// @Failure		400			{object}	response.ErrorResponse
// @Failure		401			{object}	response.ErrorResponse
// @Failure		403			{object}	response.ErrorResponse
// @Failure		500			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/workspaces/{workspaceId}/config [put]
func (h *WorkspaceConfigHandler) Update(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	workspaceID := mux.Vars(r)["workspaceId"]
	if workspaceID == "" {
		response.WriteError(w, http.StatusBadRequest, "Missing workspaceId", nil)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxConfigBodyBytes))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "Could not read request body", nil)
		return
	}

	var input workspaceconfigdomain.UpdateWorkspaceConfigOwnerInput
	if err := json.Unmarshal(body, &input); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"skipAdminAssignment":         "boolean (optional)",
			"rouletteMode":                "string (optional): online | last_seen",
			"rouletteLastSeenWindowHours": "number (optional, 1..168)",
			"rouletteRescueEnabled":       "boolean (optional)",
			"rouletteRescueAfterMinutes":  "number (optional, 1..1440)",
			"workingHours":                "object (optional), or null to clear",
		})
		return
	}

	hours, clearHours, err := working_hours.DecodePatch(body, "workingHours")
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}
	input.WorkingHours = hours
	input.ClearWorkingHours = clearHours

	cfg, err := h.updateOwnerConfig.Execute(r.Context(), workspaceID, claims.UserID, claims.Role, input)
	if err != nil {
		if errors.Is(err, workspaceconfigdomain.ErrForbidden) || errors.Is(err, workspaceconfigdomain.ErrUnauthorized) {
			response.WriteError(w, http.StatusForbidden, "Insufficient permissions to update workspace configuration", nil)
			return
		}
		if working_hours.IsPolicyError(err) {
			response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "Failed to update workspace configuration", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, toWorkspaceConfigResponse(cfg))
}

func (h *WorkspaceConfigHandler) UpdateSensitive(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	workspaceID := mux.Vars(r)["workspaceId"]
	if workspaceID == "" {
		response.WriteError(w, http.StatusBadRequest, "Missing workspaceId", nil)
		return
	}

	var input workspaceconfigdomain.UpdateWorkspaceConfigInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"campaignSpamProtectionDays": "number (optional, min: 0)",
		})
		return
	}

	cfg, err := h.updateConfig.Execute(r.Context(), workspaceID, claims.UserID, claims.Role, input)
	if err != nil {
		if errors.Is(err, workspaceconfigdomain.ErrUnauthorized) {
			response.WriteError(w, http.StatusForbidden, "Only administrators can modify workspace configuration", nil)
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "Failed to update workspace configuration", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, toWorkspaceConfigResponse(cfg))
}
