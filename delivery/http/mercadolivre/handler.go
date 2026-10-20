package mercadolivre

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/oauthpopup"
	"vozko/delivery/http/response"
	mldomain "vozko/domain/mercadolivre"
	"vozko/domain/shared"
	"vozko/infra/http/middleware"
	mluc "vozko/usecases/mercadolivre"
	"vozko/usecases/shared/automationrefs"
)

const (
	maxRequestBody = 64 << 10
	popupSource    = "mercadolivre-login"
)

type Connector interface {
	Start(ctx context.Context, in mluc.StartConnectInput) (*mluc.StartConnectOutput, error)
	Complete(ctx context.Context, in mluc.CompleteConnectInput) (*mluc.CompleteConnectOutput, error)
}

type AccountsService interface {
	List(ctx context.Context, in mldomain.ListAccountsInput) (*shared.PaginatedResult[*mldomain.Account], error)
	Get(ctx context.Context, workspaceID, id string) (*mldomain.Account, error)
	Update(ctx context.Context, in mluc.UpdateAccountInput) (*mldomain.Account, error)
	Disconnect(ctx context.Context, workspaceID, id string) (*mluc.DisconnectOutput, error)
}

type HealthChecker interface {
	CheckInWorkspace(ctx context.Context, workspaceID, id string) (*mldomain.Account, error)
}

type HandlerDeps struct {
	Connect         Connector
	Accounts        AccountsService
	Health          HealthChecker
	FrontendBaseURL string
}

type Handler struct {
	connect         Connector
	accounts        AccountsService
	health          HealthChecker
	frontendBaseURL string
}

func NewHandler(d HandlerDeps) *Handler {
	if d.Connect == nil || d.Accounts == nil || d.Health == nil {
		panic("mercadolivre http: the handler requires connect, accounts and health")
	}
	return &Handler{connect: d.Connect, accounts: d.Accounts, health: d.Health, frontendBaseURL: strings.TrimRight(d.FrontendBaseURL, "/")}
}

// @Summary		Iniciar conexão de conta do Mercado Livre
// @Description	Retorna a URL de autorização do Mercado Livre (PKCE). Com redirect=1 redireciona direto. Só o administrador da conta do Mercado Livre consegue autorizar.
// @Tags			Mercado Livre
// @Produce		json
// @Param			returnPath	query		string	false	"Caminho do painel para voltar depois da conexão"
// @Param			popup		query		string	false	"1 quando a conexão roda em popup"
// @Param			redirect	query		string	false	"1 para redirecionar em vez de responder JSON"
// @Success		200			{object}	ConnectStartResponse
// @Success		302
// @Failure		400			{object}	response.ErrorResponse
// @Failure		500			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/oauth/mercadolivre/start [get]
func (h *Handler) StartConnect(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.GetWorkspaceID(r)
	if workspaceID == "" {
		response.WriteError(w, http.StatusBadRequest, "Workspace is required", nil)
		return
	}
	userID := ""
	if claims := middleware.GetClaims(r); claims != nil {
		userID = claims.UserID
	}
	q := r.URL.Query()
	out, err := h.connect.Start(r.Context(), mluc.StartConnectInput{
		WorkspaceID: workspaceID,
		UserID:      userID,
		ReturnPath:  q.Get("returnPath"),
		Popup:       q.Get("popup") == "1",
	})
	if err != nil {
		log.Printf("[mercadolivre] connect start failed: %v", err)
		response.WriteError(w, http.StatusInternalServerError, "Failed to start the Mercado Livre connection", nil)
		return
	}
	if q.Get("redirect") == "1" {
		http.Redirect(w, r, out.AuthorizeURL, http.StatusFound)
		return
	}
	response.WriteSuccess(w, http.StatusOK, ConnectStartResponse{AuthorizeURL: out.AuthorizeURL})
}

// @Summary		Callback de conexão do Mercado Livre
// @Description	Conclui o OAuth: devolve o resultado ao popup (postMessage com source, status, nickname, accountId e reason) ou redireciona ao painel com ?mercadolivre=<status>&reason=<motivo>.
// @Tags			Mercado Livre
// @Param			code				query	string	false	"Código de autorização"
// @Param			state				query	string	true	"Estado assinado emitido no início"
// @Param			error				query	string	false	"Erro devolvido pelo Mercado Livre"
// @Param			error_description	query	string	false	"Descrição do erro"
// @Success		200
// @Success		302
// @Router			/oauth/mercadolivre/callback [get]
func (h *Handler) HandleCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	out, err := h.connect.Complete(r.Context(), mluc.CompleteConnectInput{
		Code:             q.Get("code"),
		State:            q.Get("state"),
		Error:            q.Get("error"),
		ErrorDescription: q.Get("error_description"),
	})
	result := map[string]any{"source": popupSource}
	if err != nil {
		result["status"], result["reason"] = "error", connectErrorCode(err)
	} else {
		result["status"] = "connected"
		if out.Reconnected {
			result["status"] = "reconnected"
		}
		result["nickname"], result["accountId"] = out.Account.DisplayName(), out.Account.ID
	}
	if out != nil && out.Popup {
		oauthpopup.WriteResult(w, h.frontendBaseURL, result)
		return
	}
	returnPath := mluc.AccountsPath
	if out != nil && out.ReturnPath != "" {
		returnPath = out.ReturnPath
	}
	params := url.Values{"mercadolivre": {result["status"].(string)}}
	for _, key := range []string{"reason", "nickname", "accountId"} {
		if v, ok := result[key].(string); ok {
			params.Set(key, v)
		}
	}
	oauthpopup.Redirect(w, r, h.frontendBaseURL, returnPath, mluc.AccountsPath, params)
}

func connectErrorCode(err error) string {
	if code, ok := oauthpopup.StateErrorCode(err); ok {
		return code
	}
	switch {
	case errors.Is(err, mldomain.ErrNotAccountAdmin):
		return "not_account_admin"
	case errors.Is(err, mldomain.ErrSiteNotSupported):
		return "site_not_supported"
	case errors.Is(err, mldomain.ErrPermissionMissing):
		return "missing_permission"
	case errors.Is(err, mldomain.ErrAccountAlreadyLinked):
		return "already_linked"
	case errors.Is(err, mluc.ErrAuthorizationDeclined):
		return "authorization_declined"
	case errors.Is(err, mluc.ErrProbeInconclusive):
		return "mercadolivre_unreachable"
	}
	return "connect_failed"
}

// @Summary		Listar contas do Mercado Livre
// @Tags			Mercado Livre
// @Produce		json
// @Param			search		query		string	false	"Busca por apelido ou ID do vendedor"
// @Param			status		query		string	false	"CONNECTED, NEEDS_RECONNECT, REVOKED ou DISCONNECTED"
// @Param			page		query		int		false	"Página"
// @Param			pageSize	query		int		false	"Tamanho da página"
// @Success		200			{object}	AccountListResponse
// @Failure		400			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/mercadolivre/accounts [get]
func (h *Handler) ListAccounts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	in := mldomain.ListAccountsInput{
		WorkspaceID: middleware.GetWorkspaceID(r),
		Search:      strings.TrimSpace(q.Get("search")),
		Options:     shared.QueryOptions{Pagination: httpx.ParsePagination(q)},
	}
	if raw := strings.TrimSpace(q.Get("status")); raw != "" {
		status, err := mldomain.ParseStatus(strings.ToUpper(raw))
		if err != nil {
			response.WriteErrorWithCode(w, http.StatusBadRequest, "invalid_status", err.Error(), nil)
			return
		}
		in.Status = &status
	}
	result, err := h.accounts.List(r.Context(), in)
	if err != nil {
		writeError(w, err, "Failed to list Mercado Livre accounts")
		return
	}
	items := make([]AccountResponse, 0, len(result.Items))
	for _, a := range result.Items {
		items = append(items, toAccountResponse(a))
	}
	response.WriteSuccess(w, http.StatusOK, AccountListResponse{Data: items, Meta: PaginationMeta{
		Page: result.Page, PageSize: result.PageSize, TotalPages: result.TotalPages, TotalItems: result.TotalItems,
	}})
}

// @Summary		Obter conta do Mercado Livre
// @Tags			Mercado Livre
// @Produce		json
// @Param			id	path		string	true	"ID da conta"
// @Success		200	{object}	AccountResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/mercadolivre/accounts/{id} [get]
func (h *Handler) GetAccount(w http.ResponseWriter, r *http.Request) {
	account, err := h.accounts.Get(r.Context(), middleware.GetWorkspaceID(r), mux.Vars(r)["id"])
	if err != nil {
		writeError(w, err, "Failed to load the Mercado Livre account")
		return
	}
	response.WriteSuccess(w, http.StatusOK, toAccountResponse(account))
}

// @Summary		Atualizar a conta do Mercado Livre
// @Description	Altera departamento, funil, agente, fluxo, interruptores de automação e o aviso de automação. Campos ausentes ficam como estão; string vazia limpa a referência.
// @Tags			Mercado Livre
// @Accept			json
// @Produce		json
// @Param			id		path		string					true	"ID da conta"
// @Param			body	body		UpdateAccountRequest	true	"Campos a alterar"
// @Success		200		{object}	AccountResponse
// @Failure		400		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Failure		422		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/mercadolivre/accounts/{id} [patch]
func (h *Handler) UpdateAccount(w http.ResponseWriter, r *http.Request) {
	var req UpdateAccountRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxRequestBody)).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, nil)
		return
	}
	account, err := h.accounts.Update(r.Context(), mluc.UpdateAccountInput{
		WorkspaceID:          middleware.GetWorkspaceID(r),
		ID:                   mux.Vars(r)["id"],
		DepartmentID:         req.DepartmentID,
		AgentID:              req.AgentID,
		WorkflowID:           req.WorkflowID,
		PipelineID:           req.PipelineID,
		EnableAgentResponses: req.EnableAgentResponses,
		EnableWorkflow:       req.EnableWorkflow,
		AutomationDisclosure: req.AutomationDisclosure,
	})
	if err != nil {
		writeError(w, err, "Failed to update the Mercado Livre account")
		return
	}
	response.WriteSuccess(w, http.StatusOK, toAccountResponse(account))
}

// @Summary		Desconectar a conta do Mercado Livre
// @Description	Apaga os tokens na hora, revoga a autorização no Mercado Livre quando possível e agenda a remoção dos dados da conta para daqui a 30 dias.
// @Tags			Mercado Livre
// @Produce		json
// @Param			id	path		string	true	"ID da conta"
// @Success		200	{object}	DisconnectResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/mercadolivre/accounts/{id} [delete]
func (h *Handler) DisconnectAccount(w http.ResponseWriter, r *http.Request) {
	out, err := h.accounts.Disconnect(r.Context(), middleware.GetWorkspaceID(r), mux.Vars(r)["id"])
	if err != nil {
		writeError(w, err, "Failed to disconnect the Mercado Livre account")
		return
	}
	response.WriteSuccess(w, http.StatusOK, DisconnectResponse{Status: "disconnected", PurgeAfter: out.PurgeAfter, Warning: out.Warning})
}

// @Summary		Verificar a conexão da conta do Mercado Livre agora
// @Description	Refaz as sondagens de permissão, atualiza o apelido e devolve a conta.
// @Tags			Mercado Livre
// @Produce		json
// @Param			id	path		string	true	"ID da conta"
// @Success		200	{object}	AccountResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		502	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/mercadolivre/accounts/{id}/health-check [post]
func (h *Handler) HealthCheck(w http.ResponseWriter, r *http.Request) {
	account, err := h.health.CheckInWorkspace(r.Context(), middleware.GetWorkspaceID(r), mux.Vars(r)["id"])
	if err != nil {
		writeError(w, err, "Failed to check the Mercado Livre account")
		return
	}
	response.WriteSuccess(w, http.StatusOK, toAccountResponse(account))
}

func writeError(w http.ResponseWriter, err error, fallback string) {
	switch {
	case errors.Is(err, mldomain.ErrAccountNotFound), errors.Is(err, mldomain.ErrConversationNotFound):
		response.WriteError(w, http.StatusNotFound, "Not found", nil)
	case errors.Is(err, automationrefs.ErrNotInWorkspace):
		response.WriteErrorWithCode(w, http.StatusUnprocessableEntity, "reference_not_in_workspace", err.Error(), nil)
	case errors.Is(err, mldomain.ErrTextCharset):
		response.WriteErrorWithCode(w, http.StatusBadRequest, "text_charset", err.Error(), nil)
	case errors.Is(err, mldomain.ErrTextTooLong):
		response.WriteErrorWithCode(w, http.StatusBadRequest, "text_too_long", err.Error(), nil)
	case errors.Is(err, mldomain.ErrWorkspaceIDRequired):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	case errors.Is(err, mldomain.ErrNeedsReconnect):
		response.WriteErrorWithCode(w, http.StatusConflict, "reconnect_required", "This Mercado Livre account needs to be reconnected", nil)
	case errors.Is(err, mluc.ErrProbeInconclusive):
		response.WriteErrorWithCode(w, http.StatusBadGateway, "mercadolivre_unreachable", "Mercado Livre did not answer the check", nil)
	default:
		log.Printf("[mercadolivre] %s: %v", fallback, err)
		response.WriteError(w, http.StatusInternalServerError, fallback, nil)
	}
}

func toAccountResponse(a *mldomain.Account) AccountResponse {
	scopes := a.GrantedScopes
	if scopes == nil {
		scopes = []string{}
	}
	return AccountResponse{
		ID:                   a.ID,
		WorkspaceID:          a.WorkspaceID,
		DepartmentID:         a.DepartmentID,
		MLUserID:             a.MLUserID,
		SiteID:               a.SiteID,
		Nickname:             a.Nickname,
		Status:               string(a.Status),
		StatusReason:         a.StatusReason,
		NeedsReconnect:       a.Status == mldomain.StatusNeedsReconnect || a.Status == mldomain.StatusRevoked,
		GrantedScopes:        scopes,
		CanReadQuestions:     a.CanReadQuestions,
		CanAnswerQuestions:   a.CanAnswerQuestions,
		CanReadMessages:      a.CanReadMessages,
		CanSendMessages:      a.CanSendMessages,
		AccessExpiresAt:      a.AccessExpiresAt,
		LastReconciledAt:     a.LastReconciledAt,
		LastHealthCheckAt:    a.LastHealthCheckAt,
		PurgeAfter:           a.PurgeAfter,
		AgentID:              a.AgentID,
		WorkflowID:           a.WorkflowID,
		PipelineID:           a.PipelineID,
		EnableAgentResponses: a.EnableAgentResponses,
		EnableWorkflow:       a.EnableWorkflow,
		AutomationDisclosure: a.AutomationDisclosure,
		CreatedAt:            a.CreatedAt,
		UpdatedAt:            a.UpdatedAt,
	}
}
