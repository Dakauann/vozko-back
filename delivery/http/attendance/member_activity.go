package attendance

import (
	"context"
	"errors"
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/infra/http/middleware"
	attendance_usecase "vozko/usecases/attendance"
)

type memberActivityReader interface {
	Execute(ctx context.Context, q attendance_usecase.MemberActivityQuery) (*attendance_usecase.MemberActivityReport, error)
}

func (h *AttendanceHandler) SetMemberActivity(reader memberActivityReader) {
	h.memberActivity = reader
}

// @Summary		Atividade de um membro
// @Description	Mostra quando um membro esteve conectado ao painel (sessões por dia no fuso local, tempo conectado, em ligação, horário habitual de início, sinais de entrada tarde, dia sem presença e possível aba esquecida aberta), um mapa de calor por dia da semana e hora, as conversas recebidas por gatilho, as recebidas estando offline e os números de atendimento do período. Exige acesso a Atendimento e que o membro seja de um departamento do usuário (donos e administradores veem todos). O fuso é o do horário de atendimento do workspace; sem ele, o enviado em timezone; sem os dois, UTC.
// @Tags			Atendimento
// @Produce		json
// @Param			id			path	string	true	"ID do usuário membro"
// @Param			date_from	query	string	false	"Primeiro dia (YYYY-MM-DD); padrão: 6 dias antes do último"
// @Param			date_to		query	string	false	"Último dia (YYYY-MM-DD); padrão: hoje"
// @Param			timezone	query	string	false	"Fuso IANA do navegador, ex.: America/Sao_Paulo"
// @Success		200	{object}	attendance_usecase.MemberActivityReport
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Failure		503	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/attendance/members/{id}/activity [get]
func (h *AttendanceHandler) GetMemberActivity(w http.ResponseWriter, r *http.Request) {
	h.writeMemberActivity(w, r, mux.Vars(r)["id"], false)
}

// @Summary		Minha atividade
// @Description	A mesma atividade de membro, sempre do próprio usuário. Qualquer membro do workspace pode ver a sua.
// @Tags			Atendimento
// @Produce		json
// @Param			date_from	query	string	false	"Primeiro dia (YYYY-MM-DD)"
// @Param			date_to		query	string	false	"Último dia (YYYY-MM-DD)"
// @Param			timezone	query	string	false	"Fuso IANA do navegador"
// @Success		200	{object}	attendance_usecase.MemberActivityReport
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Failure		503	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/attendance/members/me/activity [get]
func (h *AttendanceHandler) GetMyActivity(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	h.writeMemberActivity(w, r, claims.UserID, true)
}

func (h *AttendanceHandler) writeMemberActivity(w http.ResponseWriter, r *http.Request, memberID string, self bool) {
	if h.memberActivity == nil {
		response.WriteError(w, http.StatusServiceUnavailable, "Member activity is unavailable", nil)
		return
	}
	query := r.URL.Query()
	report, err := h.memberActivity.Execute(r.Context(), attendance_usecase.MemberActivityQuery{
		WorkspaceID: middleware.GetWorkspaceID(r),
		MemberID:    memberID,
		FromDay:     query.Get("date_from"),
		ToDay:       query.Get("date_to"),
		Timezone:    query.Get("timezone"),
		Self:        self,
		Departments: middleware.GetDepartmentFilter(r),
	})
	switch {
	case err == nil:
		response.WriteSuccess(w, http.StatusOK, report)
	case errors.Is(err, attendance_usecase.ErrMemberOutOfScope):
		response.WriteError(w, http.StatusNotFound, "Member not found in your departments", nil)
	case errors.Is(err, attendance_usecase.ErrActivityPeriod):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	case httpx.WriteAnalyticsLimit(w, err, "attendance"):
	default:
		response.WriteError(w, http.StatusInternalServerError, "Failed to load member activity", nil)
	}
}
