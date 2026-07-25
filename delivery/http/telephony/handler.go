package telephony

import (
	"net/http"
	"time"

	"vozko/delivery/http/response"
	telephonydomain "vozko/domain/telephony"
	"vozko/infra/http/middleware"
)

type TelephonyHandler struct {
	getOverview telephonydomain.GetOverviewUseCase
	getBoard    telephonydomain.GetBoardUseCase
}

func NewTelephonyHandler(getOverview telephonydomain.GetOverviewUseCase) *TelephonyHandler {
	return &TelephonyHandler{getOverview: getOverview}
}

func (h *TelephonyHandler) SetBoard(uc telephonydomain.GetBoardUseCase) {
	if h != nil {
		h.getBoard = uc
	}
}

func parseTelephonyFilter(r *http.Request) telephonydomain.OverviewFilter {
	f := telephonydomain.OverviewFilter{}
	if v := r.URL.Query().Get("date_from"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			f.DateFrom = &t
		}
	}
	if v := r.URL.Query().Get("date_to"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			end := t.Add(24*time.Hour - time.Second)
			f.DateTo = &end
		}
	}
	f.Direction = r.URL.Query().Get("direction")
	f.CallType = r.URL.Query().Get("call_type")
	f.AgentID = r.URL.Query().Get("agent_id")
	f.MemberID = r.URL.Query().Get("member_id")
	if v := r.URL.Query().Get("service_level_seconds"); v != "" {
		var n int
		for _, c := range v {
			if c >= '0' && c <= '9' {
				n = n*10 + int(c-'0')
			}
		}
		if n > 0 && n <= 300 {
			f.ServiceLevelSeconds = n
		}
	}
	return f
}

// @Summary		Painel de telefonia (VoIP / discador)
// @Description	Retorna os indicadores do painel operacional de telefonia: volume de chamadas, fila, ocupação, nível de serviço e voz por IA, agregados a partir do CDR. Aceita filtros opcionais por período, direção, tipo, campanha, agente e membro.
// @Tags			Telefonia
// @Produce		json
// @Param			date_from				query		string	false	"Data inicial no formato AAAA-MM-DD"
// @Param			date_to					query		string	false	"Data final no formato AAAA-MM-DD"
// @Param			direction				query		string	false	"Direção da chamada (inbound ou outbound)"
// @Param			call_type				query		string	false	"Tipo de chamada (crm, trunk_inbound, trunk_outbound)"
// @Param			campaign_id				query		string	false	"Filtrar por campanha"
// @Param			agent_id				query		string	false	"Filtrar por agente (humano ou IA)"
// @Param			member_id				query		string	false	"Filtrar por membro (usuário do discador)"
// @Param			service_level_seconds	query		int		false	"Limiar de nível de serviço em segundos (1 a 300)"
// @Success		200	{object}	OverviewResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/telephony/overview [get]
func (h *TelephonyHandler) GetOverview(w http.ResponseWriter, r *http.Request) {
	wsID := middleware.GetWorkspaceID(r)
	if wsID == "" {
		response.WriteError(w, http.StatusBadRequest, "workspace_id required", nil)
		return
	}
	if h.getOverview == nil {
		response.WriteSuccess(w, http.StatusOK, toOverviewResponse(&telephonydomain.Overview{
			Hourly:      make([]telephonydomain.HourlyPoint, 24),
			Definitions: telephonydomain.DefaultDefinitions(),
		}))
		return
	}
	out, err := h.getOverview.Execute(wsID, parseTelephonyFilter(r))
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to fetch telephony overview: "+err.Error(), nil)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toOverviewResponse(out))
}

// @Summary		Painel de concorrência ao vivo
// @Description	Retorna a foto ao vivo das posições de atendimento (humanos e IA), capacidade de canais simultâneos e profundidade de fila. Lido apenas do Redis, sem consulta ao banco.
// @Tags			Telefonia
// @Produce		json
// @Success		200	{object}	BoardSnapshotResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/telephony/board [get]
func (h *TelephonyHandler) GetBoard(w http.ResponseWriter, r *http.Request) {
	wsID := middleware.GetWorkspaceID(r)
	if wsID == "" {
		response.WriteError(w, http.StatusBadRequest, "workspace_id required", nil)
		return
	}
	if h.getBoard == nil {
		response.WriteSuccess(w, http.StatusOK, toBoardSnapshotResponse(&telephonydomain.BoardSnapshot{
			WorkspaceID: wsID,
			AsOf:        time.Now().UTC(),
			Humans:      []telephonydomain.HumanSeat{},
		}))
		return
	}
	out, err := h.getBoard.Execute(wsID)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to fetch telephony board: "+err.Error(), nil)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toBoardSnapshotResponse(out))
}
