package siptrunk

import (
	"errors"
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/sip_trunk"
	sip_trunk_usecase "vozko/usecases/sip_trunk"
)

type HandlerDeps struct {
	Create    *sip_trunk_usecase.CreateTrunkUseCase
	Update    *sip_trunk_usecase.UpdateTrunkUseCase
	Delete    *sip_trunk_usecase.DeleteTrunkUseCase
	List      *sip_trunk_usecase.ListTrunksUseCase
	Get       *sip_trunk_usecase.GetTrunkUseCase
	Hangup    *sip_trunk_usecase.HangupCallUseCase
	ListCalls *sip_trunk_usecase.ListCallsUseCase
}

type Handler struct {
	create    *sip_trunk_usecase.CreateTrunkUseCase
	update    *sip_trunk_usecase.UpdateTrunkUseCase
	delete    *sip_trunk_usecase.DeleteTrunkUseCase
	list      *sip_trunk_usecase.ListTrunksUseCase
	get       *sip_trunk_usecase.GetTrunkUseCase
	hangup    *sip_trunk_usecase.HangupCallUseCase
	listCalls *sip_trunk_usecase.ListCallsUseCase
}

func NewHandler(d HandlerDeps) *Handler {
	return &Handler{
		create:    d.Create,
		update:    d.Update,
		delete:    d.Delete,
		list:      d.List,
		get:       d.Get,
		hangup:    d.Hangup,
		listCalls: d.ListCalls,
	}
}

// @Summary		Criar tronco SIP
// @Description	Cadastra um tronco SIP no workspace. Troncos habilitados passam a se registrar no provedor e a registração é mantida e renovada antes de expirar, sem depender de chamadas. É preciso ao menos um codec G.711 (PCMU ou PCMA).
// @Tags			Troncos SIP
// @Accept			json
// @Produce		json
// @Param			request	body		CreateTrunkRequest	true	"Dados do tronco a cadastrar"
// @Success		201		{object}	TrunkResponse
// @Failure		400		{object}	response.ErrorResponse
// @Failure		401		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/sip-trunks [post]
func (h *Handler) CreateTrunk(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	var req CreateTrunkRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	enabled := req.Enabled == nil || *req.Enabled
	trunk, err := h.create.Execute(r.Context(), sip_trunk_usecase.CreateTrunkInput{
		WorkspaceID: workspaceID,
		Name:        req.Name,
		TrunkType:   sip_trunk.TrunkType(req.TrunkType),
		Host:        req.Host,
		Port:        req.Port,
		Domain:      req.Domain,
		Transport:   sip_trunk.Transport(req.Transport),
		Username:    req.Username,
		Password:    req.Password,
		Enabled:     enabled,
		Settings:    req.Settings.toDomain(),
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, toTrunkDTO(trunk))
}

// @Summary		Listar troncos SIP
// @Description	Lista os troncos SIP do workspace com o estado atual da registração no provedor. A senha nunca é devolvida, apenas se há uma cadastrada.
// @Tags			Troncos SIP
// @Produce		json
// @Success		200	{array}		TrunkResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/sip-trunks [get]
func (h *Handler) ListTrunks(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	trunks, err := h.list.Execute(r.Context(), workspaceID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	items := make([]TrunkResponse, 0, len(trunks))
	for _, trunk := range trunks {
		items = append(items, toTrunkDTO(trunk))
	}
	response.WriteSuccess(w, http.StatusOK, items)
}

// @Summary		Obter tronco SIP
// @Description	Retorna um tronco SIP do workspace com o estado atual da registração.
// @Tags			Troncos SIP
// @Produce		json
// @Param			id	path		string	true	"ID do tronco"
// @Success		200	{object}	TrunkResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/sip-trunks/{id} [get]
func (h *Handler) GetTrunk(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	trunk, err := h.get.Execute(r.Context(), workspaceID, mux.Vars(r)["id"])
	if err != nil {
		writeDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toTrunkDTO(trunk))
}

// @Summary		Atualizar tronco SIP
// @Description	Atualiza um tronco SIP do workspace. Todos os campos são opcionais e apenas os informados são alterados; omitir a senha mantém a atual. O tronco se registra de novo com a configuração nova.
// @Tags			Troncos SIP
// @Accept			json
// @Produce		json
// @Param			id		path		string				true	"ID do tronco"
// @Param			request	body		UpdateTrunkRequest	true	"Campos do tronco a atualizar"
// @Success		200		{object}	TrunkResponse
// @Failure		400		{object}	response.ErrorResponse
// @Failure		401		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/sip-trunks/{id} [put]
func (h *Handler) UpdateTrunk(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	var req UpdateTrunkRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	trunk, err := h.update.Execute(r.Context(), sip_trunk_usecase.UpdateTrunkInput{
		WorkspaceID: workspaceID,
		ID:          mux.Vars(r)["id"],
		Name:        req.Name,
		TrunkType:   optionalTrunkType(req.TrunkType),
		Host:        req.Host,
		Port:        req.Port,
		Domain:      req.Domain,
		Transport:   optionalTransport(req.Transport),
		Username:    req.Username,
		Password:    req.Password,
		Enabled:     req.Enabled,
		Settings:    optionalSettings(req.Settings),
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toTrunkDTO(trunk))
}

// @Summary		Remover tronco SIP
// @Description	Remove um tronco SIP do workspace, cancela a registração no provedor e encerra as chamadas em andamento por ele.
// @Tags			Troncos SIP
// @Produce		json
// @Param			id	path		string	true	"ID do tronco"
// @Success		200	{object}	StatusResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/sip-trunks/{id} [delete]
func (h *Handler) DeleteTrunk(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	if err := h.delete.Execute(r.Context(), workspaceID, mux.Vars(r)["id"]); err != nil {
		writeDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, StatusResponse{Status: "deleted"})
}

// @Summary		Listar chamadas ativas do tronco
// @Description	Lista as chamadas em andamento por um tronco SIP do workspace. Chamadas são iniciadas pelo WebSocket de sessão de chamada (evento call:start com trunk_id), que aplica a reserva de saldo e a cobrança por minuto; não há rota HTTP para iniciar chamadas.
// @Tags			Troncos SIP
// @Produce		json
// @Param			id	path		string	true	"ID do tronco"
// @Success		200	{array}		ActiveCallResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/sip-trunks/{id}/calls [get]
func (h *Handler) ListCalls(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	calls, err := h.listCalls.Execute(r.Context(), workspaceID, mux.Vars(r)["id"])
	if err != nil {
		writeDomainError(w, err)
		return
	}
	items := make([]ActiveCallResponse, 0, len(calls))
	for _, call := range calls {
		items = append(items, toCallDTO(call))
	}
	response.WriteSuccess(w, http.StatusOK, items)
}

// @Summary		Encerrar chamada do tronco
// @Description	Encerra uma chamada em andamento por um tronco SIP do workspace. A chamada é cobrada pelos minutos falados até o encerramento.
// @Tags			Troncos SIP
// @Produce		json
// @Param			id		path		string	true	"ID do tronco"
// @Param			callId	path		string	true	"ID da chamada"
// @Success		200		{object}	StatusResponse
// @Failure		401		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Failure		503		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/sip-trunks/{id}/calls/{callId} [delete]
func (h *Handler) HangupCall(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	vars := mux.Vars(r)
	if err := h.hangup.Execute(r.Context(), workspaceID, vars["id"], vars["callId"]); err != nil {
		writeDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, StatusResponse{Status: "hung_up"})
}

func writeDomainError(w http.ResponseWriter, err error) {
	var rejected *sip_trunk.CallRejectedError
	switch {
	case errors.Is(err, sip_trunk.ErrTrunkNotFound), errors.Is(err, sip_trunk.ErrCallNotFound):
		response.WriteError(w, http.StatusNotFound, err.Error(), nil)
	case sip_trunk.IsInvalidInput(err):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	case errors.Is(err, sip_trunk.ErrTrunkDisabled), errors.Is(err, sip_trunk.ErrTrunkCannotDial), errors.Is(err, sip_trunk.ErrTrunkNotRegistered):
		response.WriteError(w, http.StatusConflict, err.Error(), nil)
	case errors.As(err, &rejected), errors.Is(err, sip_trunk.ErrMediaNotEncrypted):
		response.WriteError(w, http.StatusBadGateway, err.Error(), nil)
	case errors.Is(err, sip_trunk.ErrEngineNotRunning):
		response.WriteError(w, http.StatusServiceUnavailable, err.Error(), nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, "internal server error", nil)
	}
}
