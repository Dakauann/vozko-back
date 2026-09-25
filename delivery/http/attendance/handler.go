package attendance

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/agent_presence"
	attendancedomain "vozko/domain/attendance"
	"vozko/domain/queue_event"
	"vozko/infra/http/middleware"
	attendance_usecase "vozko/usecases/attendance"
)

type AttendanceHandler struct {
	getAttendanceStats          attendancedomain.GetAttendanceStatsUseCase
	getWindowStats              attendancedomain.GetWindowStatsUseCase
	getResponseTimeDistribution attendancedomain.GetResponseTimeDistributionUseCase
	getAIAgentStats             attendancedomain.GetAIAgentStatsUseCase
	getFRTStats                 attendancedomain.GetFRTStatsUseCase
	sections                    attendancedomain.OverviewSectionsUseCase
	queueRepo                   queue_event.Repository
	presenceRepo                agent_presence.Repository
	targets                     *attendance_usecase.TargetsService
	targetScoper                targetScoper
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

func (h *AttendanceHandler) SetOverview(uc attendancedomain.OverviewSectionsUseCase) {
	h.sections = uc
}

func (h *AttendanceHandler) SetQueueRepo(repo queue_event.Repository) {
	h.queueRepo = repo
}

func (h *AttendanceHandler) SetPresenceRepo(repo agent_presence.Repository) {
	h.presenceRepo = repo
}

func parseStatsFilter(r *http.Request) attendancedomain.StatsFilter {
	filter := attendancedomain.StatsFilter{}
	filter.DateFrom, filter.DateTo = parseDayRange(r)
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
	filter.DateFrom, filter.DateTo = parseDayRange(r)
	filter.DepartmentID = r.URL.Query().Get("department_id")
	filter.MemberID = r.URL.Query().Get("member_id")
	filter.CampaignID = r.URL.Query().Get("campaign_id")
	filter.CampaignType = r.URL.Query().Get("campaign_type")
	filter.Channel = r.URL.Query().Get("channel")
	filter.RankMetric = r.URL.Query().Get("rank_metric")
	if v := r.URL.Query().Get("trend_buckets"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			filter.TrendBuckets = n
		}
	}
	ia := r.URL.Query().Get("include_ai")
	if ia == "" || ia == "1" || ia == "true" || ia == "yes" {
		filter.IncludeAI = true
	}
	return filter
}

// @Summary		Seção da visão geral de atendimento
// @Description	Retorna uma seção da visão geral de atendimento (summary, trend, stages, backlog, team, rework ou live) para que a página carregue cada bloco quando ele entra na tela, com filtros opcionais por período, departamento, membro, campanha e canal. As seções, exceto live, idênticas ficam em cache por 60 segundos; quando o limite de consultas analíticas simultâneas está ocupado, responde 503 com Retry-After.
// @Tags			Atendimento
// @Produce		json
// @Param			section			path	string	true	"Seção"	Enums(summary, trend, stages, backlog, team, rework, live)
// @Param			date_from		query	string	false	"Data inicial (YYYY-MM-DD)"
// @Param			date_to			query	string	false	"Data final (YYYY-MM-DD)"
// @Param			department_id	query	string	false	"ID do departamento"
// @Param			member_id		query	string	false	"ID do membro"
// @Param			campaign_id		query	string	false	"ID da campanha"
// @Param			campaign_type	query	string	false	"Tipo da campanha"
// @Param			channel			query	string	false	"Canal de atendimento"
// @Param			rank_metric		query	string	false	"Métrica de ordenação da equipe (apenas team)"
// @Param			trend_buckets	query	int		false	"Meses da série histórica (apenas trend)"
// @Param			include_ai		query	string	false	"Incluir agentes de IA (apenas team)"
// @Success		200	{object}	attendance.SummarySection
// @Failure		400	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		503	{object}	response.ErrorResponse
// @Failure		504	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/attendance/overview/{section} [get]
func (h *AttendanceHandler) GetOverviewSection(w http.ResponseWriter, r *http.Request) {
	wsID := middleware.GetWorkspaceID(r)
	if wsID == "" {
		response.WriteError(w, http.StatusBadRequest, "workspace_id required", nil)
		return
	}
	section, ok := attendancedomain.ParseSection(mux.Vars(r)["section"])
	if !ok {
		response.WriteError(w, http.StatusNotFound, "unknown overview section", nil)
		return
	}
	if h.sections == nil {
		response.WriteError(w, http.StatusServiceUnavailable, "attendance overview is not configured", nil)
		return
	}

	out, err := h.readSection(r.Context(), section, wsID, parseOverviewFilter(r))
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		writeOverviewError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, out)
}

func (h *AttendanceHandler) readSection(
	ctx context.Context,
	section attendancedomain.Section,
	workspaceID string,
	filter attendancedomain.OverviewFilter,
) (any, error) {
	switch section {
	case attendancedomain.SectionSummary:
		return h.sections.Summary(ctx, workspaceID, filter)
	case attendancedomain.SectionTrend:
		return h.sections.Trend(ctx, workspaceID, filter)
	case attendancedomain.SectionStages:
		return h.sections.Stages(ctx, workspaceID, filter)
	case attendancedomain.SectionBacklog:
		return h.sections.Backlog(ctx, workspaceID, filter)
	case attendancedomain.SectionTeam:
		return h.sections.Team(ctx, workspaceID, filter)
	case attendancedomain.SectionRework:
		return h.sections.Rework(ctx, workspaceID, filter)
	case attendancedomain.SectionLive:
		return h.sections.Live(ctx, workspaceID, filter)
	}
	return nil, errUnknownSection
}

var errUnknownSection = errors.New("unknown overview section")

func writeOverviewError(w http.ResponseWriter, err error) {
	if httpx.WriteAnalyticsLimit(w, err, "attendance") {
		return
	}
	response.WriteError(w, http.StatusInternalServerError, "Failed to fetch attendance overview: "+err.Error(), nil)
}

func parseDayRange(r *http.Request) (*time.Time, *time.Time) {
	var from, to *time.Time
	if day, err := attendancedomain.ParseDay(r.URL.Query().Get("date_from")); err == nil {
		from = &day
	}
	if day, err := attendancedomain.ParseDay(r.URL.Query().Get("date_to")); err == nil {
		end := attendancedomain.EndOfDay(day)
		to = &end
	}
	return from, to
}
