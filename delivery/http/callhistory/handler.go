package callhistory

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/calls/callhistory"
	"vozko/domain/calls/cdr"
	"vozko/domain/shared"
	"vozko/infra/http/middleware"
	callhistory_usecase "vozko/usecases/callhistory"
)

var errInvalidFilter = errors.New("invalid filter")

type History interface {
	List(ctx context.Context, input callhistory_usecase.ListInput) (*shared.PaginatedResult[callhistory.Summary], error)
	Get(ctx context.Context, viewer callhistory_usecase.Viewer, callID string) (*callhistory.Detail, error)
}

type Access = callhistory_usecase.Permissions

type Handler struct {
	history History
	access  Access
}

func NewHandler(history History, access Access) *Handler {
	return &Handler{history: history, access: access}
}

// @Summary		Listar chamadas
// @Description	Lista as ligações do workspace, das mais recentes para as mais antigas, por tronco SIP e WhatsApp: quem ligou, quem atendeu, quantas transferências houve, o resultado, o tempo de conversa e o valor cobrado. Sem a permissão `call_history:view_others`, a lista traz só as ligações de que você participou (fez, atendeu, transferiu ou recebeu por transferência) e `memberId` é ignorado. `amountMicros` é o valor debitado do saldo, em milionésimos de real.
// @Tags			Histórico de chamadas
// @Produce		json
// @Param			page		query		int		false	"Página (começa em 1)"
// @Param			pageSize	query		int		false	"Itens por página (máximo 100)"
// @Param			direction	query		string	false	"Direção"						Enums(inbound, outbound)
// @Param			channel		query		string	false	"Canal"							Enums(phone, whatsapp)
// @Param			result		query		string	false	"Atendida ou não"				Enums(answered, unanswered)
// @Param			memberId	query		string	false	"Ligações de que este membro participou (exige call_history:view_others)"
// @Param			from		query		string	false	"Início do período (YYYY-MM-DD ou RFC 3339)"
// @Param			to			query		string	false	"Fim do período (YYYY-MM-DD inclui o dia todo, ou RFC 3339)"
// @Param			number		query		string	false	"Parte do número do contato"
// @Success		200			{object}	CallListResponse
// @Failure		400			{object}	response.ErrorResponse
// @Failure		401			{object}	response.ErrorResponse
// @Failure		403			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/calls [get]
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	viewer, ok := h.viewer(w, r)
	if !ok {
		return
	}
	input, err := listInput(r.URL.Query(), viewer)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}
	page, err := h.history.List(r.Context(), input)
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toList(page))
}

// @Summary		Detalhar chamada
// @Description	Mostra uma ligação com quem participou, a linha do tempo (início, atendimento, cada transferência e o que aconteceu com ela, fim) e a gravação. A gravação só aparece para quem tem `call_recordings:read`. Uma ligação de que você não participou, sem `call_history:view_others`, responde 404.
// @Tags			Histórico de chamadas
// @Produce		json
// @Param			callId	path		string	true	"Identificador da ligação"
// @Success		200		{object}	CallDetailResponse
// @Failure		401		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/calls/{callId} [get]
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	viewer, ok := h.viewer(w, r)
	if !ok {
		return
	}
	detail, err := h.history.Get(r.Context(), viewer, mux.Vars(r)["callId"])
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toDetail(detail))
}

func (h *Handler) viewer(w http.ResponseWriter, r *http.Request) (callhistory_usecase.Viewer, bool) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return callhistory_usecase.Viewer{}, false
	}
	claims := middleware.GetClaims(r)
	if claims == nil || claims.UserID == "" {
		response.WriteError(w, http.StatusUnauthorized, "authentication required", nil)
		return callhistory_usecase.Viewer{}, false
	}
	return callhistory_usecase.ViewerFor(h.access, workspaceID, claims.UserID), true
}

func listInput(query url.Values, viewer callhistory_usecase.Viewer) (callhistory_usecase.ListInput, error) {
	input := callhistory_usecase.ListInput{
		Viewer:   viewer,
		MemberID: strings.TrimSpace(query.Get("memberId")),
		Number:   strings.TrimSpace(query.Get("number")),
	}
	input.Page, _ = strconv.Atoi(query.Get("page"))
	input.PageSize, _ = strconv.Atoi(query.Get("pageSize"))

	switch direction := cdr.Direction(query.Get("direction")); direction {
	case "":
	case cdr.DirectionInbound, cdr.DirectionOutbound:
		input.Direction = &direction
	default:
		return input, errInvalidFilter
	}
	switch channel := callhistory.Channel(query.Get("channel")); channel {
	case "":
	case callhistory.ChannelPhone, callhistory.ChannelWhatsApp:
		input.Channel = &channel
	default:
		return input, errInvalidFilter
	}
	switch query.Get("result") {
	case "":
	case "answered":
		input.Answered = ptr(true)
	case "unanswered":
		input.Answered = ptr(false)
	default:
		return input, errInvalidFilter
	}
	var err error
	if input.From, err = dateBound(query.Get("from"), false); err != nil {
		return input, err
	}
	if input.To, err = dateBound(query.Get("to"), true); err != nil {
		return input, err
	}
	return input, nil
}

func dateBound(raw string, endOfDay bool) (*time.Time, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	bound := httpx.ParseDateBound(raw, endOfDay)
	if bound == nil {
		return nil, errInvalidFilter
	}
	return bound, nil
}

func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, cdr.ErrCallNotFound):
		response.WriteError(w, http.StatusNotFound, "call not found", nil)
	case errors.Is(err, callhistory_usecase.ErrWorkspaceRequired):
		response.WriteError(w, http.StatusForbidden, err.Error(), nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, "internal server error", nil)
	}
}

func ptr[T any](value T) *T { return &value }
