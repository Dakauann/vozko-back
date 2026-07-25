package shortlink

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	shortlinkdomain "vozko/domain/shortlink"
	"vozko/infra/http/middleware"
)

type ShortLinkHandler struct {
	createUC       shortlinkdomain.CreateShortLinkUseCase
	updateUC       shortlinkdomain.UpdateShortLinkUseCase
	getUC          shortlinkdomain.GetShortLinkUseCase
	listUC         shortlinkdomain.ListShortLinksUseCase
	deleteUC       shortlinkdomain.DeleteShortLinkUseCase
	statsUC        shortlinkdomain.GetWorkspaceStatsUseCase
	resolveUC      shortlinkdomain.ResolveShortLinkUseCase
	unlockUC       shortlinkdomain.UnlockShortLinkUseCase
	publishUC      shortlinkdomain.PublishClickUseCase
	analyticsUC    shortlinkdomain.GetAnalyticsUseCase
	recentClicksUC shortlinkdomain.ListRecentClicksUseCase
	qrUC           shortlinkdomain.GenerateQRUseCase
	baseURL        string
}

func NewShortLinkHandler(
	createUC shortlinkdomain.CreateShortLinkUseCase,
	updateUC shortlinkdomain.UpdateShortLinkUseCase,
	getUC shortlinkdomain.GetShortLinkUseCase,
	listUC shortlinkdomain.ListShortLinksUseCase,
	deleteUC shortlinkdomain.DeleteShortLinkUseCase,
	statsUC shortlinkdomain.GetWorkspaceStatsUseCase,
	resolveUC shortlinkdomain.ResolveShortLinkUseCase,
	unlockUC shortlinkdomain.UnlockShortLinkUseCase,
	publishUC shortlinkdomain.PublishClickUseCase,
	analyticsUC shortlinkdomain.GetAnalyticsUseCase,
	recentClicksUC shortlinkdomain.ListRecentClicksUseCase,
	qrUC shortlinkdomain.GenerateQRUseCase,
	baseURL string,
) *ShortLinkHandler {
	return &ShortLinkHandler{
		createUC:       createUC,
		updateUC:       updateUC,
		getUC:          getUC,
		listUC:         listUC,
		deleteUC:       deleteUC,
		statsUC:        statsUC,
		resolveUC:      resolveUC,
		unlockUC:       unlockUC,
		publishUC:      publishUC,
		analyticsUC:    analyticsUC,
		recentClicksUC: recentClicksUC,
		qrUC:           qrUC,
		baseURL:        baseURL,
	}
}

// @Summary		Criar link curto
// @Description	Cria um link curto rastreável para a URL de destino informada. Suporta alias personalizado, expiração, limite de cliques, senha e tipo de redirecionamento.
// @Tags			Links Curtos
// @Accept			json
// @Produce		json
// @Param			request	body		createShortLinkRequest	true	"Dados do link a criar"
// @Success		201	{object}	shortLinkResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		409	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/short-links [post]
func (h *ShortLinkHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createShortLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{"targetUrl": "string (required)"})
		return
	}
	if errs := req.Validate(); errs != nil {
		response.WriteValidationError(w, errs)
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	link, err := h.createUC.Execute(r.Context(), req.toInput(middleware.GetWorkspaceID(r), claims.UserID))
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, newShortLinkResponse(h.decorate(link)))
}

// @Summary		Listar links curtos
// @Description	Lista os links curtos do workspace, com paginação e filtro opcional por departamento.
// @Tags			Links Curtos
// @Produce		json
// @Param			page		query	int		false	"Página (padrão 1)"
// @Param			pageSize	query	int		false	"Itens por página (padrão 20, máx 100)"
// @Param			departmentId	query	string	false	"Filtrar por departamento"
// @Success		200	{object}	response.PaginatedPayload
// @Failure		401	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/short-links [get]
func (h *ShortLinkHandler) List(w http.ResponseWriter, r *http.Request) {
	page, pageSize := paginationParams(r)
	departmentID := optionalQuery(r, "departmentId")

	result, err := h.listUC.Execute(r.Context(), middleware.GetWorkspaceID(r), departmentID, page, pageSize)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	items := make([]*shortlinkdomain.ShortLink, 0, len(result.Items))
	for _, link := range result.Items {
		items = append(items, h.decorate(link))
	}
	response.WritePaginated(w, http.StatusOK, newShortLinkResponses(items), response.PaginationMeta{
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalPages: result.TotalPages,
		TotalItems: result.TotalItems,
	})
}

// @Summary		Estatísticas de links curtos
// @Description	Retorna o total de links e o total de cliques do workspace, para os cartões de resumo.
// @Tags			Links Curtos
// @Produce		json
// @Success		200	{object}	workspaceStatsResponse
// @Failure		401	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/short-links/stats [get]
func (h *ShortLinkHandler) Stats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.statsUC.Execute(r.Context(), middleware.GetWorkspaceID(r))
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, newWorkspaceStatsResponse(stats))
}

// @Summary		Obter link curto
// @Description	Retorna um link curto específico do workspace.
// @Tags			Links Curtos
// @Produce		json
// @Param			id	path	string	true	"ID do link curto"
// @Success		200	{object}	shortLinkResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/short-links/{id} [get]
func (h *ShortLinkHandler) Get(w http.ResponseWriter, r *http.Request) {
	link, err := h.getUC.Execute(r.Context(), middleware.GetWorkspaceID(r), mux.Vars(r)["id"])
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, newShortLinkResponse(h.decorate(link)))
}

// @Summary		Atualizar link curto
// @Description	Atualiza os atributos de um link curto (destino, título, expiração, limite de cliques, senha, status ou tipo de redirecionamento).
// @Tags			Links Curtos
// @Accept			json
// @Produce		json
// @Param			id		path		string					true	"ID do link curto"
// @Param			request	body		updateShortLinkRequest	true	"Campos a atualizar"
// @Success		200	{object}	shortLinkResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/short-links/{id} [patch]
func (h *ShortLinkHandler) Update(w http.ResponseWriter, r *http.Request) {
	var req updateShortLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{"targetUrl": "string (optional)"})
		return
	}
	if errs := req.Validate(); errs != nil {
		response.WriteValidationError(w, errs)
		return
	}

	link, err := h.updateUC.Execute(r.Context(), middleware.GetWorkspaceID(r), mux.Vars(r)["id"], req.toInput())
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, newShortLinkResponse(h.decorate(link)))
}

// @Summary		Excluir link curto
// @Description	Exclui (soft delete) um link curto do workspace.
// @Tags			Links Curtos
// @Produce		json
// @Param			id	path	string	true	"ID do link curto"
// @Success		204	"Link removido"
// @Failure		401	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/short-links/{id} [delete]
func (h *ShortLinkHandler) Delete(w http.ResponseWriter, r *http.Request) {
	if err := h.deleteUC.Execute(r.Context(), middleware.GetWorkspaceID(r), mux.Vars(r)["id"]); err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusNoContent, nil)
}

// @Summary		Métricas de acesso do link
// @Description	Retorna as métricas agregadas de cliques do link: total, únicos, série temporal e distribuições por país, dispositivo, referenciador, navegador e sistema operacional.
// @Tags			Links Curtos
// @Produce		json
// @Param			id		path	string	true	"ID do link curto"
// @Param			from	query	string	false	"Início do período (RFC3339)"
// @Param			to		query	string	false	"Fim do período (RFC3339)"
// @Success		200	{object}	analyticsResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/short-links/{id}/analytics [get]
func (h *ShortLinkHandler) Analytics(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.GetWorkspaceID(r)
	id := mux.Vars(r)["id"]

	if _, err := h.getUC.Execute(r.Context(), workspaceID, id); err != nil {
		h.handleDomainError(w, err)
		return
	}

	analytics, err := h.analyticsUC.Execute(r.Context(), shortlinkdomain.AnalyticsInput{
		ShortLinkID: id,
		WorkspaceID: workspaceID,
		From:        parseTimeQuery(r, "from"),
		To:          parseTimeQuery(r, "to"),
	})
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, newAnalyticsResponse(analytics))
}

// @Summary		Cliques recentes do link
// @Description	Lista os cliques recentes do link com paginação (dispositivo, localização, referenciador e horário).
// @Tags			Links Curtos
// @Produce		json
// @Param			id			path	string	true	"ID do link curto"
// @Param			page		query	int		false	"Página (padrão 1)"
// @Param			pageSize	query	int		false	"Itens por página (padrão 20, máx 100)"
// @Success		200	{object}	response.PaginatedPayload
// @Failure		401	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/short-links/{id}/clicks [get]
func (h *ShortLinkHandler) RecentClicks(w http.ResponseWriter, r *http.Request) {
	page, pageSize := paginationParams(r)
	result, err := h.recentClicksUC.Execute(r.Context(), middleware.GetWorkspaceID(r), mux.Vars(r)["id"], page, pageSize)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WritePaginated(w, http.StatusOK, newClickResponses(result.Items), response.PaginationMeta{
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalPages: result.TotalPages,
		TotalItems: result.TotalItems,
	})
}

// @Summary		QR code do link
// @Description	Gera o QR code (PNG) que aponta para a URL curta do link.
// @Tags			Links Curtos
// @Produce		png
// @Param			id		path	string	true	"ID do link curto"
// @Param			size	query	int		false	"Tamanho em pixels (128 a 1024, padrão 320)"
// @Success		200	{file}	binary
// @Failure		401	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/short-links/{id}/qr [get]
func (h *ShortLinkHandler) QR(w http.ResponseWriter, r *http.Request) {
	size, _ := strconv.Atoi(r.URL.Query().Get("size"))
	png, err := h.qrUC.Execute(r.Context(), middleware.GetWorkspaceID(r), mux.Vars(r)["id"], size)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(png)
}

func (h *ShortLinkHandler) decorate(link *shortlinkdomain.ShortLink) *shortlinkdomain.ShortLink {
	link.ShortURL = shortlinkdomain.ShortURL(h.baseURL, link.Code)
	return link
}

func (h *ShortLinkHandler) handleDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, shortlinkdomain.ErrShortLinkNotFound):
		response.WriteError(w, http.StatusNotFound, err.Error(), nil)
	case errors.Is(err, shortlinkdomain.ErrCodeTaken),
		errors.Is(err, shortlinkdomain.ErrReservedAlias):
		response.WriteError(w, http.StatusConflict, err.Error(), nil)
	case errors.Is(err, shortlinkdomain.ErrMaxShortLinksReached):
		response.WriteError(w, http.StatusUnprocessableEntity, err.Error(), nil)
	case errors.Is(err, shortlinkdomain.ErrThreatDetected),
		errors.Is(err, shortlinkdomain.ErrTargetURLBlocked),
		errors.Is(err, shortlinkdomain.ErrTargetURLLoop),
		errors.Is(err, shortlinkdomain.ErrTargetURLRequired),
		errors.Is(err, shortlinkdomain.ErrTargetURLTooLong),
		errors.Is(err, shortlinkdomain.ErrTargetURLInvalid),
		errors.Is(err, shortlinkdomain.ErrTargetURLScheme),
		errors.Is(err, shortlinkdomain.ErrTargetURLCredentials),
		errors.Is(err, shortlinkdomain.ErrInvalidAliasLength),
		errors.Is(err, shortlinkdomain.ErrInvalidAliasChar),
		errors.Is(err, shortlinkdomain.ErrInvalidRedirectType),
		errors.Is(err, shortlinkdomain.ErrInvalidStatus),
		errors.Is(err, shortlinkdomain.ErrInvalidMaxClicks),
		errors.Is(err, shortlinkdomain.ErrTitleTooLong),
		errors.Is(err, shortlinkdomain.ErrCodeRequired):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
	}
}

func paginationParams(r *http.Request) (int, int) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("pageSize"))
	return page, pageSize
}

func optionalQuery(r *http.Request, key string) *string {
	value := strings.TrimSpace(r.URL.Query().Get(key))
	if value == "" {
		return nil
	}
	return &value
}

func parseTimeQuery(r *http.Request, key string) time.Time {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}
	}
	return parsed
}
