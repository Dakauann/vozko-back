package attendance

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	attendancedomain "vozko/domain/attendance"
	"vozko/domain/attendance_target"
	"vozko/domain/conversation"
	"vozko/infra/http/middleware"
	attendance_usecase "vozko/usecases/attendance"
)

type targetScoper interface {
	GetDepartmentScope(userID, workspaceID string, isAdmin bool) (conversation.DepartmentAccessScope, bool)
}

type TargetRequest struct {
	Scope     string  `json:"scope" example:"workspace"`
	ScopeID   string  `json:"scopeId,omitempty"`
	MetricKey string  `json:"metricKey" example:"finished"`
	Period    string  `json:"period" example:"2026-09"`
	Value     float64 `json:"value" example:"1786"`
	Currency  string  `json:"currency,omitempty" example:"BRL"`
}

func (h *AttendanceHandler) SetTargets(svc *attendance_usecase.TargetsService, scoper targetScoper) {
	h.targets = svc
	h.targetScoper = scoper
}

func (h *AttendanceHandler) targetAccess(r *http.Request, workspaceID string) (attendance_usecase.TargetAccess, bool) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		return attendance_usecase.TargetAccess{}, false
	}
	access := attendance_usecase.TargetAccess{
		UserID:  claims.UserID,
		IsAdmin: claims.Role == "admin",
	}
	if h.targetScoper == nil {
		return access, true
	}
	scope, ok := h.targetScoper.GetDepartmentScope(claims.UserID, workspaceID, access.IsAdmin)
	if !ok {
		return attendance_usecase.TargetAccess{}, false
	}
	access.DepartmentIDs = scope.DepartmentIDs
	access.Restrict = scope.Restrict
	return access, true
}

func parseTargetPeriod(raw string) (attendance_target.Month, bool) {
	return attendance_target.ParseMonth(raw)
}

func writeTargetError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, attendance_usecase.ErrTargetsUnavailable):
		response.WriteError(w, http.StatusServiceUnavailable, err.Error(), nil)
	case errors.Is(err, attendance_usecase.ErrTargetForbidden):
		response.WriteError(w, http.StatusForbidden, err.Error(), nil)
	case errors.Is(err, attendance_target.ErrNotFound):
		response.WriteError(w, http.StatusNotFound, err.Error(), nil)
	case errors.Is(err, attendance_target.ErrPeriodClosed):
		response.WriteError(w, http.StatusConflict, err.Error(), nil)
	case errors.Is(err, attendance_target.ErrUnknownMetric),
		errors.Is(err, attendance_target.ErrMetricNotTargetab),
		errors.Is(err, attendance_target.ErrInvalidScope),
		errors.Is(err, attendance_target.ErrScopeIDRequired),
		errors.Is(err, attendance_target.ErrScopeIDForbidden),
		errors.Is(err, attendance_target.ErrInvalidValue),
		errors.Is(err, attendance_target.ErrCurrencyRequired),
		errors.Is(err, attendance_target.ErrCurrencyForbidden),
		errors.Is(err, attendance_target.ErrPeriodRequired):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, "Failed to handle attendance target: "+err.Error(), nil)
	}
}

// @Summary		Métricas que aceitam meta
// @Description	Lista as métricas de atendimento que podem receber uma meta, com o tipo de valor e a direção (maior ou menor é melhor).
// @Tags			Atendimento
// @Produce		json
// @Success		200	{object}	map[string][]attendance.MetricSpec
// @Security		BearerAuth
// @Router			/attendance/metrics [get]
func (h *AttendanceHandler) GetTargetableMetrics(w http.ResponseWriter, r *http.Request) {
	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"metrics": attendancedomain.TargetableMetrics(),
	})
}

// @Summary		Listar metas de atendimento
// @Description	Lista as metas do workspace para o período informado (mês). Usuários restritos a departamentos veem apenas as metas do próprio escopo.
// @Tags			Atendimento
// @Produce		json
// @Param			period	query	string	false	"Período no formato YYYY-MM (padrão: mês atual)"
// @Success		200	{object}	map[string][]attendance_target.Target
// @Failure		400	{object}	response.ErrorResponse
// @Failure		503	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/attendance/targets [get]
func (h *AttendanceHandler) ListTargets(w http.ResponseWriter, r *http.Request) {
	wsID := middleware.GetWorkspaceID(r)
	if wsID == "" {
		response.WriteError(w, http.StatusBadRequest, "workspace_id required", nil)
		return
	}
	period, ok := parseTargetPeriod(r.URL.Query().Get("period"))
	if !ok {
		response.WriteError(w, http.StatusBadRequest, "period must be YYYY-MM", nil)
		return
	}
	access, allowed := h.targetAccess(r, wsID)
	if !allowed {
		response.WriteError(w, http.StatusForbidden, "Forbidden", nil)
		return
	}

	targets, err := h.targets.List(r.Context(), wsID, period, access)
	if err != nil {
		writeTargetError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{"targets": targets})
}

// @Summary		Definir meta de atendimento
// @Description	Cria ou atualiza a meta de uma métrica para um período, no escopo do workspace, de um departamento ou de um membro. Períodos já encerrados são recusados.
// @Tags			Atendimento
// @Accept			json
// @Produce		json
// @Param			request	body		TargetRequest	true	"Meta a gravar"
// @Success		200	{object}	attendance_target.Target
// @Failure		400	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		409	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/attendance/targets [put]
func (h *AttendanceHandler) UpsertTarget(w http.ResponseWriter, r *http.Request) {
	wsID := middleware.GetWorkspaceID(r)
	if wsID == "" {
		response.WriteError(w, http.StatusBadRequest, "workspace_id required", nil)
		return
	}
	var req TargetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}
	period, ok := parseTargetPeriod(req.Period)
	if !ok {
		response.WriteError(w, http.StatusBadRequest, "period must be YYYY-MM", nil)
		return
	}
	access, allowed := h.targetAccess(r, wsID)
	if !allowed {
		response.WriteError(w, http.StatusForbidden, "Forbidden", nil)
		return
	}

	stored, err := h.targets.Upsert(r.Context(), wsID, attendance_usecase.UpsertTargetInput{
		Scope:     attendance_target.Scope(strings.TrimSpace(req.Scope)),
		ScopeID:   req.ScopeID,
		MetricKey: req.MetricKey,
		Period:    period,
		Value:     req.Value,
		Currency:  req.Currency,
	}, access)
	if err != nil {
		writeTargetError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, stored)
}

// @Summary		Remover meta de atendimento
// @Description	Remove a meta informada. Metas de períodos já encerrados não podem ser removidas.
// @Tags			Atendimento
// @Produce		json
// @Param			id	path	string	true	"ID da meta"
// @Success		204	{object}	nil
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		409	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/attendance/targets/{id} [delete]
func (h *AttendanceHandler) DeleteTarget(w http.ResponseWriter, r *http.Request) {
	wsID := middleware.GetWorkspaceID(r)
	if wsID == "" {
		response.WriteError(w, http.StatusBadRequest, "workspace_id required", nil)
		return
	}
	id := strings.TrimSpace(mux.Vars(r)["id"])
	if id == "" {
		response.WriteError(w, http.StatusBadRequest, "id required", nil)
		return
	}
	access, allowed := h.targetAccess(r, wsID)
	if !allowed {
		response.WriteError(w, http.StatusForbidden, "Forbidden", nil)
		return
	}

	if err := h.targets.Delete(r.Context(), wsID, id, access); err != nil {
		writeTargetError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusNoContent, nil)
}
