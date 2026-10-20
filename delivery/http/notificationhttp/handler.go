package notificationhttp

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/delivery/notificationview"
	"vozko/domain/notification"
	"vozko/infra/http/middleware"
	notification_usecase "vozko/usecases/notification"
)

type InboxService interface {
	List(ctx context.Context, v notification.Viewer, q notification.ListQuery) (notification.Page, error)
	Summary(ctx context.Context, v notification.Viewer) (notification.Summary, error)
	MarkRead(ctx context.Context, v notification.Viewer, ids []string) (int, error)
	MarkUnread(ctx context.Context, v notification.Viewer, ids []string) (int, error)
	MarkAllRead(ctx context.Context, v notification.Viewer, category string) (int, error)
	Preferences(ctx context.Context, v notification.Viewer) (notification.Preferences, error)
	SavePreferences(ctx context.Context, v notification.Viewer, change notification_usecase.PreferencesChange) (notification.Preferences, error)
}

type Handler struct {
	inbox InboxService
}

func NewHandler(inbox InboxService) *Handler {
	return &Handler{inbox: inbox}
}

func (h *Handler) viewer(w http.ResponseWriter, r *http.Request) (notification.Viewer, bool) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return notification.Viewer{}, false
	}
	claims := middleware.GetClaims(r)
	if claims == nil || strings.TrimSpace(claims.UserID) == "" {
		writeError(w, notification.ErrViewerRequired)
		return notification.Viewer{}, false
	}
	return notification.Viewer{WorkspaceID: workspaceID, UserID: strings.TrimSpace(claims.UserID)}, true
}

// @Summary		Notificações da pessoa no workspace
// @Description	Lista as notificações de quem está logado no workspace do cabeçalho `X-Workspace-ID`, da mais nova para a mais antiga. Cada pessoa só vê as próprias notificações; nenhuma permissão extra é exigida.
// @Description
// @Description	Filtros: `category` (uma das categorias abaixo), `unread=true` (só não lidas), `limit` (1 a 50, padrão 20) e `cursor` (o `next` da página anterior; vazio quando não há mais páginas).
// @Description
// @Description	| Categoria | Assunto |
// @Description	|---|---|
// @Description	| `announcements` | Comunicados e novidades da plataforma |
// @Description	| `campaigns` | Campanhas |
// @Description	| `templates` | Modelos de mensagem |
// @Description	| `channels` | Números e canais |
// @Description	| `billing` | Financeiro e saldo |
// @Description	| `advertising` | Anúncios |
// @Description	| `team` | Equipe |
// @Description	| `support` | Suporte |
// @Description	| `system` | Sistema |
// @Description
// @Description	`severity` é `info`, `success`, `warning` ou `critical`. `actionUrl` é um caminho do app (`/dashboard/...`) ou um endereço `https`. Códigos: 400 `notification_category_unknown`, `notification_cursor_invalid`, `notification_limit_invalid`, `notification_unread_invalid`.
// @Tags			Notificações
// @Produce		json
// @Param			category	query		string	false	"categoria"
// @Param			unread		query		bool	false	"só não lidas"
// @Param			limit		query		int		false	"itens por página (1 a 50)"
// @Param			cursor		query		string	false	"next da página anterior"
// @Success		200			{object}	ListResponse
// @Failure		400			{object}	response.ErrorResponse
// @Failure		401			{object}	response.ErrorResponse
// @Failure		403			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/notifications [get]
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	v, ok := h.viewer(w, r)
	if !ok {
		return
	}
	params := r.URL.Query()
	limit, ok := optionalInt(w, params.Get("limit"))
	if !ok {
		return
	}
	unread, ok := optionalBool(w, params.Get("unread"))
	if !ok {
		return
	}
	q, err := notification.ParseListQuery(params.Get("category"), unread, params.Get("cursor"), limit)
	if err != nil {
		writeError(w, err)
		return
	}
	page, err := h.inbox.List(r.Context(), v, q)
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, ListResponse{Items: notificationview.ItemsOf(page.Items), Next: page.Next})
}

// @Summary		Resumo das notificações não lidas
// @Description	Quantas notificações não lidas a pessoa tem no workspace, no total e por categoria. A contagem para em 1000: com `capped` verdadeiro, mostre "1000+".
// @Tags			Notificações
// @Produce		json
// @Success		200	{object}	SummaryResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/notifications/summary [get]
func (h *Handler) Summary(w http.ResponseWriter, r *http.Request) {
	v, ok := h.viewer(w, r)
	if !ok {
		return
	}
	summary, err := h.inbox.Summary(r.Context(), v)
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentSummary(summary))
}

// @Summary		Marcar notificações como lidas
// @Description	Marca como lidas as notificações informadas (1 a 100 ids) da pessoa logada. Ids de outras pessoas ou de outro workspace são ignorados. `updated` diz quantas mudaram. Código: 400 `notification_ids_invalid`.
// @Tags			Notificações
// @Accept			json
// @Produce		json
// @Param			body	body		IDsRequest	true	"ids"
// @Success		200		{object}	ChangeResponse
// @Failure		400		{object}	response.ErrorResponse
// @Failure		401		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/notifications/read [post]
func (h *Handler) MarkRead(w http.ResponseWriter, r *http.Request) {
	h.changeIDs(w, r, h.inbox.MarkRead)
}

// @Summary		Marcar notificações como não lidas
// @Description	Volta para não lidas as notificações informadas (1 a 100 ids) da pessoa logada. Código: 400 `notification_ids_invalid`.
// @Tags			Notificações
// @Accept			json
// @Produce		json
// @Param			body	body		IDsRequest	true	"ids"
// @Success		200		{object}	ChangeResponse
// @Failure		400		{object}	response.ErrorResponse
// @Failure		401		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/notifications/unread [post]
func (h *Handler) MarkUnread(w http.ResponseWriter, r *http.Request) {
	h.changeIDs(w, r, h.inbox.MarkUnread)
}

func (h *Handler) changeIDs(w http.ResponseWriter, r *http.Request, change func(context.Context, notification.Viewer, []string) (int, error)) {
	v, ok := h.viewer(w, r)
	if !ok {
		return
	}
	var body IDsRequest
	if !httpx.DecodeStrictJSON(w, r, &body) {
		return
	}
	updated, err := change(r.Context(), v, body.IDs)
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, ChangeResponse{Updated: updated})
}

// @Summary		Marcar todas como lidas
// @Description	Marca como lidas todas as notificações não lidas da pessoa no workspace, ou só as de uma `category`. O corpo é opcional. Código: 400 `notification_category_unknown`.
// @Tags			Notificações
// @Accept			json
// @Produce		json
// @Param			body	body		ReadAllRequest	false	"categoria opcional"
// @Success		200		{object}	ChangeResponse
// @Failure		400		{object}	response.ErrorResponse
// @Failure		401		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/notifications/read-all [post]
func (h *Handler) MarkAllRead(w http.ResponseWriter, r *http.Request) {
	v, ok := h.viewer(w, r)
	if !ok {
		return
	}
	var body ReadAllRequest
	if !httpx.DecodeOptionalStrictJSON(w, r, &body) {
		return
	}
	updated, err := h.inbox.MarkAllRead(r.Context(), v, body.Category)
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, ChangeResponse{Updated: updated})
}

// @Summary		Preferências de email das notificações
// @Description	Mostra as categorias (`categories`) e quais delas a pessoa silenciou por email (`emailMuted`) neste workspace. Silenciar não esconde nada no app. Notificações `critical` sempre chegam por email. Com `emailDigest` verdadeiro, os emails que não são `critical` chegam juntos em um resumo diário às 08:00 (horário de Brasília), só com o que ainda não foi lido.
// @Tags			Notificações
// @Produce		json
// @Success		200	{object}	PreferencesResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/notifications/preferences [get]
func (h *Handler) Preferences(w http.ResponseWriter, r *http.Request) {
	v, ok := h.viewer(w, r)
	if !ok {
		return
	}
	prefs, err := h.inbox.Preferences(r.Context(), v)
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentPreferences(prefs))
}

// @Summary		Salvar preferências de email das notificações
// @Description	Substitui a lista de categorias silenciadas por email (`emailMuted`, vazia para receber todas) da pessoa neste workspace. `emailDigest` escolhe entre receber cada email na hora (falso) ou um resumo diário às 08:00 (verdadeiro); `critical` sempre chega na hora. Código: 400 `notification_category_unknown`.
// @Tags			Notificações
// @Accept			json
// @Produce		json
// @Param			body	body		PreferencesRequest	true	"categorias silenciadas"
// @Success		200		{object}	PreferencesResponse
// @Failure		400		{object}	response.ErrorResponse
// @Failure		401		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/notifications/preferences [put]
func (h *Handler) SavePreferences(w http.ResponseWriter, r *http.Request) {
	v, ok := h.viewer(w, r)
	if !ok {
		return
	}
	var body PreferencesRequest
	if !httpx.DecodeStrictJSON(w, r, &body) {
		return
	}
	prefs, err := h.inbox.SavePreferences(r.Context(), v, notification_usecase.PreferencesChange{EmailMuted: body.EmailMuted, EmailDigest: body.EmailDigest})
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentPreferences(prefs))
}

func optionalInt(w http.ResponseWriter, raw string) (int, bool) {
	if raw == "" {
		return 0, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		response.WriteErrorWithCode(w, http.StatusBadRequest, codeLimitInvalid, "limit must be a number", nil)
		return 0, false
	}
	return n, true
}

func optionalBool(w http.ResponseWriter, raw string) (bool, bool) {
	if raw == "" {
		return false, true
	}
	b, err := strconv.ParseBool(raw)
	if err != nil {
		response.WriteErrorWithCode(w, http.StatusBadRequest, codeUnreadInvalid, "unread must be true or false", nil)
		return false, false
	}
	return b, true
}
