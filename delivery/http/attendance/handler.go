package attendance

import (
	"net/http"
	"time"

	"vozko/delivery/http/response"
	"vozko/domain/agent_presence"
	attendancedomain "vozko/domain/attendance"
	"vozko/domain/queue_event"
	"vozko/infra/http/middleware"
)

type AttendanceHandler struct {
	getAttendanceStats          attendancedomain.GetAttendanceStatsUseCase
	getWindowStats              attendancedomain.GetWindowStatsUseCase
	getResponseTimeDistribution attendancedomain.GetResponseTimeDistributionUseCase
	getAIAgentStats             attendancedomain.GetAIAgentStatsUseCase
	getFRTStats                 attendancedomain.GetFRTStatsUseCase
	getOverview                 attendancedomain.GetOverviewUseCase
	queueRepo                   queue_event.Repository
	presenceRepo                agent_presence.Repository
}

func NewAttendanceHandler(
	getAttendanceStats attendancedomain.GetAttendanceStatsUseCase,
	getWindowStats attendancedomain.GetWindowStatsUseCase,
	getResponseTimeDistribution attendancedomain.GetResponseTimeDistributionUseCase,
	getAIAgentStats attendancedomain.GetAIAgentStatsUseCase,
) *AttendanceHandler {
	return &AttendanceHandler{
		getAttendanceStats:          getAttendanceStats,
		getWindowStats:              getWindowStats,
		getResponseTimeDistribution: getResponseTimeDistribution,
		getAIAgentStats:             getAIAgentStats,
	}
}

func (h *AttendanceHandler) SetFRTStats(uc attendancedomain.GetFRTStatsUseCase) {
	h.getFRTStats = uc
}

func (h *AttendanceHandler) SetOverview(uc attendancedomain.GetOverviewUseCase) {
	h.getOverview = uc
}

func (h *AttendanceHandler) SetQueueRepo(repo queue_event.Repository) {
	h.queueRepo = repo
}

func (h *AttendanceHandler) SetPresenceRepo(repo agent_presence.Repository) {
	h.presenceRepo = repo
}

func parseStatsFilter(r *http.Request) attendancedomain.StatsFilter {
	filter := attendancedomain.StatsFilter{}
	if v := r.URL.Query().Get("date_from"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			filter.DateFrom = &t
		}
	}
	if v := r.URL.Query().Get("date_to"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			end := t.Add(24*time.Hour - time.Second)
			filter.DateTo = &end
		}
	}
	filter.CampaignID = r.URL.Query().Get("campaign_id")
	filter.CampaignType = r.URL.Query().Get("campaign_type")
	return filter
}

// @Summary		Estatísticas de atendentes
// @Description	Retorna as métricas de atendimento por atendente do workspace (atribuídos, respondidos, taxa de resposta e tempo médio de resposta), com filtro opcional por período e campanha.
// @Tags			Atendimento
// @Produce		json
// @Param			date_from		query	string	false	"Data inicial (YYYY-MM-DD)"
// @Param			date_to			query	string	false	"Data final (YYYY-MM-DD)"
// @Param			campaign_id		query	string	false	"ID da campanha"
// @Param			campaign_type	query	string	false	"Tipo da campanha"
// @Success		200	{array}		attendance.AttendantStats
// @Failure		400	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/attendance/stats [get]
func (h *AttendanceHandler) GetAttendanceStats(w http.ResponseWriter, r *http.Request) {
	wsID := middleware.GetWorkspaceID(r)
	if wsID == "" {
		response.WriteError(w, http.StatusBadRequest, "workspace_id required", nil)
		return
	}

	filter := parseStatsFilter(r)

	stats, err := h.getAttendanceStats.Execute(wsID, filter)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to fetch attendance stats: "+err.Error(), nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"attendants": stats,
	})
}

// @Summary		Estatísticas de janelas de atendimento
// @Description	Retorna o total de conversas abertas do workspace agrupadas por faixas de tempo desde a última interação, com filtro opcional por período e campanha.
// @Tags			Atendimento
// @Produce		json
// @Param			date_from		query	string	false	"Data inicial (YYYY-MM-DD)"
// @Param			date_to			query	string	false	"Data final (YYYY-MM-DD)"
// @Param			campaign_id		query	string	false	"ID da campanha"
// @Param			campaign_type	query	string	false	"Tipo da campanha"
// @Success		200	{object}	attendance.WindowStats
// @Failure		400	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/attendance/windows [get]
func (h *AttendanceHandler) GetWindowStats(w http.ResponseWriter, r *http.Request) {
	wsID := middleware.GetWorkspaceID(r)
	if wsID == "" {
		response.WriteError(w, http.StatusBadRequest, "workspace_id required", nil)
		return
	}

	filter := parseStatsFilter(r)

	stats, err := h.getWindowStats.Execute(wsID, filter)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to fetch window stats: "+err.Error(), nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, stats)
}

// @Summary		Distribuição de tempo de resposta
// @Description	Retorna a distribuição das conversas do workspace por faixa de tempo de resposta, com filtro opcional por período e campanha.
// @Tags			Atendimento
// @Produce		json
// @Param			date_from		query	string	false	"Data inicial (YYYY-MM-DD)"
// @Param			date_to			query	string	false	"Data final (YYYY-MM-DD)"
// @Param			campaign_id		query	string	false	"ID da campanha"
// @Param			campaign_type	query	string	false	"Tipo da campanha"
// @Success		200	{object}	attendance.ResponseTimeDistribution
// @Failure		400	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/attendance/response-times [get]
func (h *AttendanceHandler) GetResponseTimeDistribution(w http.ResponseWriter, r *http.Request) {
	wsID := middleware.GetWorkspaceID(r)
	if wsID == "" {
		response.WriteError(w, http.StatusBadRequest, "workspace_id required", nil)
		return
	}

	filter := parseStatsFilter(r)

	dist, err := h.getResponseTimeDistribution.Execute(wsID, filter)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to fetch response time distribution: "+err.Error(), nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, dist)
}

// @Summary		Estatísticas de agentes de IA
// @Description	Retorna as métricas de atendimento dos agentes de IA do workspace (sessões, contenção, transbordo e abandono), com filtro opcional por período e campanha.
// @Tags			Atendimento
// @Produce		json
// @Param			date_from		query	string	false	"Data inicial (YYYY-MM-DD)"
// @Param			date_to			query	string	false	"Data final (YYYY-MM-DD)"
// @Param			campaign_id		query	string	false	"ID da campanha"
// @Param			campaign_type	query	string	false	"Tipo da campanha"
// @Success		200	{array}		attendance.AIAgentStats
// @Failure		400	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/attendance/ai-stats [get]
func (h *AttendanceHandler) GetAIAgentStats(w http.ResponseWriter, r *http.Request) {
	wsID := middleware.GetWorkspaceID(r)
	if wsID == "" {
		response.WriteError(w, http.StatusBadRequest, "workspace_id required", nil)
		return
	}
	if h.getAIAgentStats == nil {
		response.WriteSuccess(w, http.StatusOK, map[string]interface{}{"agents": []interface{}{}})
		return
	}
	filter := parseStatsFilter(r)
	stats, err := h.getAIAgentStats.Execute(wsID, filter)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to fetch AI agent stats: "+err.Error(), nil)
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"agents": stats,
	})
}

// @Summary		Tempo de primeira resposta (FRT)
// @Description	Retorna as métricas de tempo de primeira resposta do workspace (médio, mediano e por origem humana ou IA), com filtro opcional por período e campanha.
// @Tags			Atendimento
// @Produce		json
// @Param			date_from		query	string	false	"Data inicial (YYYY-MM-DD)"
// @Param			date_to			query	string	false	"Data final (YYYY-MM-DD)"
// @Param			campaign_id		query	string	false	"ID da campanha"
// @Param			campaign_type	query	string	false	"Tipo da campanha"
// @Success		200	{object}	attendance.FRTStats
// @Failure		400	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/attendance/frt [get]
func (h *AttendanceHandler) GetFRTStats(w http.ResponseWriter, r *http.Request) {
	wsID := middleware.GetWorkspaceID(r)
	if wsID == "" {
		response.WriteError(w, http.StatusBadRequest, "workspace_id required", nil)
		return
	}
	if h.getFRTStats == nil {
		response.WriteSuccess(w, http.StatusOK, &attendancedomain.FRTStats{})
		return
	}
	filter := parseStatsFilter(r)
	stats, err := h.getFRTStats.Execute(wsID, filter)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to fetch FRT stats: "+err.Error(), nil)
		return
	}
	response.WriteSuccess(w, http.StatusOK, stats)
}

// @Summary		Estatísticas de fila
// @Description	Retorna as métricas de fila de atendimento do workspace (aguardando, atendidas e tempo de espera), com filtro opcional por período.
// @Tags			Atendimento
// @Produce		json
// @Param			date_from	query	string	false	"Data inicial (YYYY-MM-DD)"
// @Param			date_to		query	string	false	"Data final (YYYY-MM-DD)"
// @Success		200	{object}	queue_event.Stats
// @Failure		400	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/attendance/queue-stats [get]
func (h *AttendanceHandler) GetQueueStats(w http.ResponseWriter, r *http.Request) {
	wsID := middleware.GetWorkspaceID(r)
	if wsID == "" {
		response.WriteError(w, http.StatusBadRequest, "workspace_id required", nil)
		return
	}
	if h.queueRepo == nil {
		response.WriteSuccess(w, http.StatusOK, &queue_event.Stats{})
		return
	}
	filter := parseStatsFilter(r)
	stats, err := h.queueRepo.Stats(wsID, filter.DateFrom, filter.DateTo)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to fetch queue stats: "+err.Error(), nil)
		return
	}
	response.WriteSuccess(w, http.StatusOK, stats)
}

// @Summary		Ocupação dos atendentes
// @Description	Retorna a ocupação dos atendentes do workspace (tempo em chamada sobre tempo online) no período informado.
// @Tags			Atendimento
// @Produce		json
// @Param			date_from	query	string	false	"Data inicial (YYYY-MM-DD)"
// @Param			date_to		query	string	false	"Data final (YYYY-MM-DD)"
// @Success		200	{array}		agent_presence.OccupancyRow
// @Failure		400	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/attendance/occupancy [get]
func (h *AttendanceHandler) GetOccupancy(w http.ResponseWriter, r *http.Request) {
	wsID := middleware.GetWorkspaceID(r)
	if wsID == "" {
		response.WriteError(w, http.StatusBadRequest, "workspace_id required", nil)
		return
	}
	if h.presenceRepo == nil {
		response.WriteSuccess(w, http.StatusOK, map[string]interface{}{"occupancy": []interface{}{}})
		return
	}
	filter := parseStatsFilter(r)
	rows, err := h.presenceRepo.Occupancy(wsID, filter.DateFrom, filter.DateTo)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to fetch occupancy: "+err.Error(), nil)
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{"occupancy": rows})
}

func parseOverviewFilter(r *http.Request) attendancedomain.OverviewFilter {
	filter := attendancedomain.OverviewFilter{}
	if v := r.URL.Query().Get("date_from"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			filter.DateFrom = &t
		}
	}
	if v := r.URL.Query().Get("date_to"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			end := t.Add(24*time.Hour - time.Second)
			filter.DateTo = &end
		}
	}
	filter.DepartmentID = r.URL.Query().Get("department_id")
	filter.MemberID = r.URL.Query().Get("member_id")
	filter.CampaignID = r.URL.Query().Get("campaign_id")
	filter.CampaignType = r.URL.Query().Get("campaign_type")
	filter.Channel = r.URL.Query().Get("channel")
	ia := r.URL.Query().Get("include_ai")
	if ia == "" || ia == "1" || ia == "true" || ia == "yes" {
		filter.IncludeAI = true
	}
	return filter
}

// @Summary		Visão geral de atendimento
// @Description	Retorna o painel operacional filtrável do workspace (KPIs, distribuição por hora, por departamento e por equipe), com filtros opcionais por período, departamento, membro, campanha e canal.
// @Tags			Atendimento
// @Produce		json
// @Param			date_from		query	string	false	"Data inicial (YYYY-MM-DD)"
// @Param			date_to			query	string	false	"Data final (YYYY-MM-DD)"
// @Param			department_id	query	string	false	"ID do departamento"
// @Param			member_id		query	string	false	"ID do membro"
// @Param			campaign_id		query	string	false	"ID da campanha"
// @Param			campaign_type	query	string	false	"Tipo da campanha"
// @Param			channel			query	string	false	"Canal de atendimento"
// @Param			include_ai		query	string	false	"Incluir agentes de IA (padrão: true)"
// @Success		200	{object}	attendance.Overview
// @Failure		400	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/attendance/overview [get]
func (h *AttendanceHandler) GetOverview(w http.ResponseWriter, r *http.Request) {
	wsID := middleware.GetWorkspaceID(r)
	if wsID == "" {
		response.WriteError(w, http.StatusBadRequest, "workspace_id required", nil)
		return
	}
	if h.getOverview == nil {
		response.WriteSuccess(w, http.StatusOK, &attendancedomain.Overview{
			Hourly:      make([]attendancedomain.HourlyPoint, 24),
			Definitions: attendancedomain.DefaultDefinitions(),
			KPIs: attendancedomain.OverviewKPIs{
				CSATAvailable: false,
				SLAAvailable:  false,
			},
		})
		return
	}
	filter := parseOverviewFilter(r)
	out, err := h.getOverview.Execute(wsID, filter)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to fetch attendance overview: "+err.Error(), nil)
		return
	}
	response.WriteSuccess(w, http.StatusOK, out)
}
