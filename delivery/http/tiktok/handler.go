package tiktok

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/commentautomationhttp"
	"vozko/delivery/http/httpx"
	"vozko/delivery/http/oauthpopup"
	"vozko/delivery/http/response"
	"vozko/domain/shared"
	ttdomain "vozko/domain/tiktok"
	"vozko/infra/http/middleware"
	ttuc "vozko/usecases/tiktok"
)

const (
	OAuthStartPath     = "/oauth/tiktok/start"
	OAuthCallbackPath  = "/oauth/tiktok/callback/"
	PopupMessageSource = "tiktok-login"
	maxRequestBody     = 64 << 10
)

type HandlerDeps struct {
	Connect         *ttuc.ConnectUseCase
	Accounts        *ttuc.Accounts
	Health          *ttuc.HealthCheckJob
	Comments        *ttuc.Comments
	Rules           commentautomationhttp.RuleManager
	Images          *ttuc.ImageStore
	RateLimited     RateLimitDetector
	FrontendBaseURL string
}

type Handler struct {
	d        HandlerDeps
	rules    *commentautomationhttp.RulesHandler
	frontend string
}

func NewHandler(d HandlerDeps) *Handler {
	if d.Connect == nil || d.Accounts == nil || d.Health == nil || d.Comments == nil || d.Rules == nil || d.Images == nil || d.RateLimited == nil || strings.TrimSpace(d.FrontendBaseURL) == "" {
		panic("tiktok http: connect, accounts, health, comments, rules, images, a rate limit detector and the frontend url are required")
	}
	h := &Handler{d: d, frontend: strings.TrimRight(d.FrontendBaseURL, "/")}
	h.rules = commentautomationhttp.NewRulesHandler(shared.EntryTypeTikTok, d.Rules, h.fail)
	return h
}

func (h *Handler) fail(w http.ResponseWriter, err error, fallback string) {
	writeDomainError(w, err, fallback, h.d.RateLimited)
}

// @Summary		Iniciar conexão de conta do TikTok
// @Description	Gera a URL de autorização do TikTok para conectar uma conta comercial. Com `redirect=1` responde com um redirecionamento; com `popup=1` o resultado volta ao painel por mensagem de janela.
// @Tags			TikTok
// @Produce		json
// @Param			redirect	query	string	false	"1 para redirecionar direto ao TikTok"
// @Param			popup		query	string	false	"1 quando aberto em janela popup"
// @Param			returnPath	query	string	false	"Caminho do painel para voltar"
// @Success		200	{object}	ConnectStartResponse
// @Security		BearerAuth
// @Router			/oauth/tiktok/start [get]
func (h *Handler) StartConnect(w http.ResponseWriter, r *http.Request) {
	userID := ""
	if claims := middleware.GetClaims(r); claims != nil {
		userID = claims.UserID
	}
	out, err := h.d.Connect.Start(r.Context(), ttuc.StartConnectInput{
		WorkspaceID: middleware.GetWorkspaceID(r),
		UserID:      userID,
		ReturnPath:  r.URL.Query().Get("returnPath"),
		Popup:       r.URL.Query().Get("popup") == "1",
	})
	if err != nil {
		h.fail(w, err, "Não foi possível iniciar a conexão com o TikTok")
		return
	}
	if r.URL.Query().Get("redirect") == "1" {
		http.Redirect(w, r, out.AuthorizeURL, http.StatusFound)
		return
	}
	response.WriteSuccess(w, http.StatusOK, ConnectStartResponse{AuthorizeURL: out.AuthorizeURL})
}

// @Summary		Callback de conexão do TikTok
// @Description	Recebe o código de autorização do TikTok, conecta a conta e volta ao painel (ou ao popup) com o resultado.
// @Tags			TikTok
// @Success		302
// @Router			/oauth/tiktok/callback/ [get]
func (h *Handler) HandleCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	out, err := h.d.Connect.Complete(r.Context(), ttuc.CompleteConnectInput{
		Code: q.Get("code"), State: q.Get("state"), Error: q.Get("error"), ErrorDescription: q.Get("error_description"),
	})
	popup := out != nil && out.Popup
	returnPath := ttuc.AccountsPath
	if out != nil && out.ReturnPath != "" {
		returnPath = out.ReturnPath
	}
	if err != nil {
		status, reason := "error", connectErrorCode(err)
		if reason == "declined" {
			status = "cancelled"
		}
		if popup {
			oauthpopup.WriteResult(w, h.frontend, map[string]any{"source": PopupMessageSource, "status": status, "reason": reason})
			return
		}
		oauthpopup.Redirect(w, r, h.frontend, returnPath, ttuc.AccountsPath, url.Values{"tiktok": {status}, "reason": {reason}})
		return
	}
	status := "connected"
	if out.Reconnected {
		status = "reconnected"
	}
	if popup {
		oauthpopup.WriteResult(w, h.frontend, map[string]any{"source": PopupMessageSource, "status": status, "accountId": out.Account.ID, "username": out.Account.Username})
		return
	}
	oauthpopup.Redirect(w, r, h.frontend, returnPath, ttuc.AccountsPath, url.Values{"tiktok": {status}, "username": {out.Account.Username}})
}

func (h *Handler) present(a *ttdomain.Account) AccountResponse {
	out := AccountResponse{
		ID: a.ID, WorkspaceID: a.WorkspaceID, DepartmentID: a.DepartmentID, OpenID: a.OpenID, Username: a.Username, DisplayName: a.DisplayName,
		AvatarURL: h.d.Images.URL(a.AvatarKey), HasAdvancedAccess: a.HasAdvancedAccess, IsBusinessAccount: a.IsBusinessAccount,
		Status: string(a.Status), StatusReason: a.StatusReason,
		Capabilities: CapabilitiesResponse{
			CanReadVideos: a.Capabilities.CanReadVideos, CanReadComments: a.Capabilities.CanReadComments, CanManageComments: a.Capabilities.CanManageComments,
			CanReceiveMessages: a.Capabilities.CanReceiveMessages, CanSendMessages: a.Capabilities.CanSendMessages, CanManageInboxProfile: a.Capabilities.CanManageInboxProfile,
		},
		GrantedScopes: a.GrantedScopes, WebhooksHealthy: a.WebhooksHealthy, ReconnectBy: a.RefreshExpiresAt, PurgeAfter: a.PurgeAfter,
		LastHealthCheckAt: a.LastHealthCheckAt, AgentID: a.AgentID, WorkflowID: a.WorkflowID, PipelineID: a.PipelineID,
		EnableAgentResponses: a.EnableAgentResponses, EnableWorkflow: a.EnableWorkflow, EnableAnalysis: a.EnableAnalysis,
		EnableAutoStaging: a.EnableAutoStaging, EnableAutoMemory: a.EnableAutoMemory, AutomationDisclosure: a.AutomationDisclosure,
		CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt,
	}
	if out.GrantedScopes == nil {
		out.GrantedScopes = []string{}
	}
	if a.RefreshExpiresAt != nil {
		out.ReconnectSoon = a.ReconnectSoon(shared.SystemClock{}.Now())
	}
	return out
}

// @Summary		Listar contas do TikTok
// @Tags			TikTok
// @Produce		json
// @Param			search		query	string	false	"Busca por nome ou usuário"
// @Param			status		query	string	false	"CONNECTED, NEEDS_RECONNECT, REVOKED ou DISCONNECTED"
// @Param			page		query	int		false	"Página"
// @Param			pageSize	query	int		false	"Itens por página"
// @Success		200	{array}	AccountResponse
// @Security		BearerAuth
// @Router			/tiktok/accounts [get]
func (h *Handler) ListAccounts(w http.ResponseWriter, r *http.Request) {
	in := ttdomain.ListAccountsInput{
		WorkspaceID: middleware.GetWorkspaceID(r),
		Search:      strings.TrimSpace(r.URL.Query().Get("search")),
		Options:     shared.QueryOptions{Pagination: httpx.ParsePagination(r.URL.Query())},
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("status")); raw != "" {
		if status, err := ttdomain.ParseStatus(strings.ToUpper(raw)); err == nil {
			in.Status = &status
		}
	}
	result, err := h.d.Accounts.List(r.Context(), in)
	if err != nil {
		h.fail(w, err, "Não foi possível listar as contas do TikTok")
		return
	}
	items := make([]AccountResponse, 0, len(result.Items))
	for _, a := range result.Items {
		items = append(items, h.present(a))
	}
	response.WritePaginated(w, http.StatusOK, items, response.PaginationMeta{Page: result.Page, PageSize: result.PageSize, TotalItems: result.TotalItems, TotalPages: result.TotalPages})
}

// @Summary		Detalhar conta do TikTok
// @Tags			TikTok
// @Produce		json
// @Param			id	path	string	true	"ID da conta"
// @Success		200	{object}	AccountResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/tiktok/accounts/{id} [get]
func (h *Handler) GetAccount(w http.ResponseWriter, r *http.Request) {
	a, err := h.d.Accounts.Get(r.Context(), middleware.GetWorkspaceID(r), mux.Vars(r)["id"])
	if err != nil {
		h.fail(w, err, "Não foi possível carregar a conta do TikTok")
		return
	}
	response.WriteSuccess(w, http.StatusOK, h.present(a))
}

// @Summary		Atualizar automação da conta do TikTok
// @Description	Define departamento, funil, agente, fluxo, as chaves de automação e o aviso de atendimento automático. Agente, fluxo, funil e departamento precisam ser do mesmo workspace.
// @Tags			TikTok
// @Accept			json
// @Produce		json
// @Param			id		path	string					true	"ID da conta"
// @Param			body	body	UpdateAccountRequest	true	"Campos a alterar"
// @Success		200	{object}	AccountResponse
// @Failure		422	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/tiktok/accounts/{id} [put]
func (h *Handler) UpdateAccount(w http.ResponseWriter, r *http.Request) {
	var req UpdateAccountRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxRequestBody)).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, nil)
		return
	}
	a, err := h.d.Accounts.Update(r.Context(), ttuc.UpdateAccountInput{
		WorkspaceID: middleware.GetWorkspaceID(r), ID: mux.Vars(r)["id"],
		DepartmentID: req.DepartmentID, AgentID: req.AgentID, WorkflowID: req.WorkflowID, PipelineID: req.PipelineID,
		EnableAgentResponses: req.EnableAgentResponses, EnableWorkflow: req.EnableWorkflow, EnableAnalysis: req.EnableAnalysis,
		EnableAutoStaging: req.EnableAutoStaging, EnableAutoMemory: req.EnableAutoMemory, AutomationDisclosure: req.AutomationDisclosure,
	})
	if err != nil {
		h.fail(w, err, "Não foi possível atualizar a conta do TikTok")
		return
	}
	response.WriteSuccess(w, http.StatusOK, h.present(a))
}

// @Summary		Desconectar conta do TikTok
// @Description	Revoga o acesso no TikTok, apaga os tokens na hora e agenda a exclusão das conversas, contatos, comentários e análises da conta para daqui a 30 dias. Conectar de novo antes disso cancela a exclusão.
// @Tags			TikTok
// @Produce		json
// @Param			id	path	string	true	"ID da conta"
// @Success		200	{object}	DisconnectResponse
// @Security		BearerAuth
// @Router			/tiktok/accounts/{id} [delete]
func (h *Handler) DisconnectAccount(w http.ResponseWriter, r *http.Request) {
	out, err := h.d.Accounts.Disconnect(r.Context(), middleware.GetWorkspaceID(r), mux.Vars(r)["id"])
	if err != nil {
		h.fail(w, err, "Não foi possível desconectar a conta do TikTok")
		return
	}
	response.WriteSuccess(w, http.StatusOK, DisconnectResponse{PurgeAfter: out.PurgeAfter, Warning: out.Warning})
}

// @Summary		Verificar a saúde da conta do TikTok
// @Description	Confere agora permissões, perfil, mensagens diretas e a assinatura dos webhooks, sem esperar a verificação diária.
// @Tags			TikTok
// @Produce		json
// @Param			id	path	string	true	"ID da conta"
// @Success		200	{object}	AccountResponse
// @Security		BearerAuth
// @Router			/tiktok/accounts/{id}/health-check [post]
func (h *Handler) CheckHealth(w http.ResponseWriter, r *http.Request) {
	a, err := h.d.Health.CheckAccount(r.Context(), middleware.GetWorkspaceID(r), mux.Vars(r)["id"])
	if err != nil {
		h.fail(w, err, "Não foi possível verificar a conta do TikTok")
		return
	}
	response.WriteSuccess(w, http.StatusOK, h.present(a))
}

func cursorParam(r *http.Request) (int64, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("cursor"))
	if raw == "" {
		return 0, true
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	return n, err == nil && n >= 0
}

func nextCursor(n int64, hasMore bool) string {
	if !hasMore {
		return ""
	}
	return strconv.FormatInt(n, 10)
}

// @Summary		Listar publicações da conta do TikTok
// @Description	Lê uma página das publicações da conta direto do TikTok (até 20) e guarda uma cópia com as miniaturas.
// @Tags			TikTok
// @Produce		json
// @Param			id		path	string	true	"ID da conta"
// @Param			cursor	query	string	false	"Cursor da próxima página"
// @Success		200	{object}	VideoPageResponse
// @Security		BearerAuth
// @Router			/tiktok/accounts/{id}/videos [get]
func (h *Handler) ListVideos(w http.ResponseWriter, r *http.Request) {
	cursor, ok := cursorParam(r)
	if !ok {
		response.WriteError(w, http.StatusBadRequest, "cursor inválido", nil)
		return
	}
	listing, err := h.d.Comments.ListVideos(r.Context(), middleware.GetWorkspaceID(r), mux.Vars(r)["id"], cursor)
	if err != nil {
		h.fail(w, err, "Não foi possível listar as publicações do TikTok")
		return
	}
	out := VideoPageResponse{Videos: make([]VideoResponse, 0, len(listing.Videos)), NextCursor: nextCursor(listing.Cursor, listing.HasMore), HasMore: listing.HasMore}
	for _, v := range listing.Videos {
		out.Videos = append(out.Videos, VideoResponse{
			ItemID: v.ItemID, MediaType: string(v.MediaType), Caption: v.Caption, ShareURL: v.ShareURL, EmbedURL: v.EmbedURL,
			ThumbnailURL: h.d.Images.URL(v.ThumbnailKey), IsAd: v.IsAd, Likes: v.Likes, Comments: v.Comments, Shares: v.Shares, Views: v.Views, PostedAt: v.PostedAt,
		})
	}
	response.WriteSuccess(w, http.StatusOK, out)
}

// @Summary		Listar comentários de uma publicação do TikTok
// @Tags			TikTok
// @Produce		json
// @Param			id				path	string	true	"ID da conta"
// @Param			videoId			path	string	true	"ID da publicação no TikTok"
// @Param			cursor			query	string	false	"Cursor da próxima página"
// @Param			includeHidden	query	bool	false	"Inclui comentários ocultos"
// @Success		200	{object}	CommentPageResponse
// @Security		BearerAuth
// @Router			/tiktok/accounts/{id}/videos/{videoId}/comments [get]
func (h *Handler) ListComments(w http.ResponseWriter, r *http.Request) {
	cursor, ok := cursorParam(r)
	if !ok {
		response.WriteError(w, http.StatusBadRequest, "cursor inválido", nil)
		return
	}
	vars := mux.Vars(r)
	workspaceID := middleware.GetWorkspaceID(r)
	listing, err := h.d.Comments.ListComments(r.Context(), workspaceID, vars["id"], vars["videoId"], cursor, r.URL.Query().Get("includeHidden") == "true")
	if err != nil {
		h.fail(w, err, "Não foi possível listar os comentários do TikTok")
		return
	}
	canManage := false
	if a, err := h.d.Accounts.Get(r.Context(), workspaceID, vars["id"]); err == nil {
		canManage = a.CanManageCommentsNow()
	}
	out := CommentPageResponse{Comments: make([]CommentResponse, 0, len(listing.Comments)), NextCursor: nextCursor(listing.Cursor, listing.HasMore), HasMore: listing.HasMore}
	for _, c := range listing.Comments {
		out.Comments = append(out.Comments, presentComment(c, canManage))
	}
	response.WriteSuccess(w, http.StatusOK, out)
}

// @Summary		Responder comentário do TikTok
// @Description	Publica uma resposta pública (até 1.200 caracteres) na conversa do comentário. Só vale para comentários das publicações da própria conta.
// @Tags			TikTok
// @Accept			json
// @Produce		json
// @Param			id			path	string			true	"ID da conta"
// @Param			commentId	path	string			true	"ID do comentário no TikTok"
// @Param			body		body	ReplyRequest	true	"Texto da resposta"
// @Success		201	{object}	CommentResponse
// @Security		BearerAuth
// @Router			/tiktok/accounts/{id}/comments/{commentId}/replies [post]
func (h *Handler) ReplyComment(w http.ResponseWriter, r *http.Request) {
	var req ReplyRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxRequestBody)).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, nil)
		return
	}
	vars := mux.Vars(r)
	reply, err := h.d.Comments.Reply(r.Context(), middleware.GetWorkspaceID(r), vars["id"], vars["commentId"], req.Text)
	if err != nil {
		h.fail(w, err, "Não foi possível responder o comentário no TikTok")
		return
	}
	response.WriteSuccess(w, http.StatusCreated, presentComment(reply, true))
}

func (h *Handler) moderate(w http.ResponseWriter, r *http.Request, action func(workspaceID, accountID, commentID string) (*ttdomain.Comment, error)) {
	vars := mux.Vars(r)
	c, err := action(middleware.GetWorkspaceID(r), vars["id"], vars["commentId"])
	if err != nil {
		h.fail(w, err, "Não foi possível alterar o comentário no TikTok")
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentComment(c, true))
}

// @Summary		Ocultar comentário do TikTok
// @Tags			TikTok
// @Produce		json
// @Param			id			path	string	true	"ID da conta"
// @Param			commentId	path	string	true	"ID do comentário no TikTok"
// @Success		200	{object}	CommentResponse
// @Security		BearerAuth
// @Router			/tiktok/accounts/{id}/comments/{commentId}/hide [post]
func (h *Handler) HideComment(w http.ResponseWriter, r *http.Request) {
	h.moderate(w, r, func(ws, acc, id string) (*ttdomain.Comment, error) {
		return h.d.Comments.SetHidden(r.Context(), ws, acc, id, true)
	})
}

// @Summary		Reexibir comentário do TikTok
// @Description	Pede ao TikTok para mostrar o comentário de novo e confere o resultado, porque a moderação do TikTok pode manter o comentário oculto.
// @Tags			TikTok
// @Produce		json
// @Param			id			path	string	true	"ID da conta"
// @Param			commentId	path	string	true	"ID do comentário no TikTok"
// @Success		200	{object}	CommentResponse
// @Security		BearerAuth
// @Router			/tiktok/accounts/{id}/comments/{commentId}/unhide [post]
func (h *Handler) UnhideComment(w http.ResponseWriter, r *http.Request) {
	h.moderate(w, r, func(ws, acc, id string) (*ttdomain.Comment, error) {
		return h.d.Comments.SetHidden(r.Context(), ws, acc, id, false)
	})
}

// @Summary		Curtir comentário do TikTok
// @Tags			TikTok
// @Produce		json
// @Param			id			path	string	true	"ID da conta"
// @Param			commentId	path	string	true	"ID do comentário no TikTok"
// @Success		200	{object}	CommentResponse
// @Security		BearerAuth
// @Router			/tiktok/accounts/{id}/comments/{commentId}/like [post]
func (h *Handler) LikeComment(w http.ResponseWriter, r *http.Request) {
	h.moderate(w, r, func(ws, acc, id string) (*ttdomain.Comment, error) {
		return h.d.Comments.SetLiked(r.Context(), ws, acc, id, true)
	})
}

// @Summary		Descurtir comentário do TikTok
// @Tags			TikTok
// @Produce		json
// @Param			id			path	string	true	"ID da conta"
// @Param			commentId	path	string	true	"ID do comentário no TikTok"
// @Success		200	{object}	CommentResponse
// @Security		BearerAuth
// @Router			/tiktok/accounts/{id}/comments/{commentId}/unlike [post]
func (h *Handler) UnlikeComment(w http.ResponseWriter, r *http.Request) {
	h.moderate(w, r, func(ws, acc, id string) (*ttdomain.Comment, error) {
		return h.d.Comments.SetLiked(r.Context(), ws, acc, id, false)
	})
}

// @Summary		Listar regras de comentário da conta do TikTok
// @Description	Regras que respondem, ocultam ou curtem comentários automaticamente. No TikTok as ações são resposta pública, ocultar e curtir.
// @Tags			TikTok
// @Produce		json
// @Param			id	path	string	true	"ID da conta"
// @Success		200	{array}	commentautomationhttp.RuleResponse
// @Security		BearerAuth
// @Router			/tiktok/accounts/{id}/comment-rules [get]
func (h *Handler) ListCommentRules(w http.ResponseWriter, r *http.Request) {
	h.rules.List(w, r, middleware.GetWorkspaceID(r), mux.Vars(r)["id"])
}

// @Summary		Criar regra de comentário no TikTok
// @Tags			TikTok
// @Accept			json
// @Produce		json
// @Param			id		path	string								true	"ID da conta"
// @Param			body	body	commentautomationhttp.RuleRequest	true	"Regra"
// @Success		201	{object}	commentautomationhttp.RuleResponse
// @Security		BearerAuth
// @Router			/tiktok/accounts/{id}/comment-rules [post]
func (h *Handler) CreateCommentRule(w http.ResponseWriter, r *http.Request) {
	h.rules.Create(w, r, middleware.GetWorkspaceID(r), mux.Vars(r)["id"])
}

// @Summary		Atualizar regra de comentário no TikTok
// @Tags			TikTok
// @Accept			json
// @Produce		json
// @Param			id		path	string								true	"ID da conta"
// @Param			ruleId	path	string								true	"ID da regra"
// @Param			body	body	commentautomationhttp.RuleRequest	true	"Regra"
// @Success		200	{object}	commentautomationhttp.RuleResponse
// @Security		BearerAuth
// @Router			/tiktok/accounts/{id}/comment-rules/{ruleId} [put]
func (h *Handler) UpdateCommentRule(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	h.rules.Update(w, r, middleware.GetWorkspaceID(r), vars["id"], vars["ruleId"])
}

// @Summary		Excluir regra de comentário no TikTok
// @Tags			TikTok
// @Param			id		path	string	true	"ID da conta"
// @Param			ruleId	path	string	true	"ID da regra"
// @Success		204
// @Security		BearerAuth
// @Router			/tiktok/accounts/{id}/comment-rules/{ruleId} [delete]
func (h *Handler) DeleteCommentRule(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	h.rules.Delete(w, r, middleware.GetWorkspaceID(r), vars["id"], vars["ruleId"])
}
