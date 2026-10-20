package unofficial_whatsapp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/shared"
	uwg "vozko/domain/unofficial_whatsapp_group"
	"vozko/infra/http/middleware"
	uwguc "vozko/usecases/unofficial_whatsapp_group"
)

type WorkspaceGroupHandler struct {
	groups      *uwguc.Service
	departments DepartmentScopeResolver
}

func NewWorkspaceGroupHandler(groups *uwguc.Service, departments DepartmentScopeResolver) *WorkspaceGroupHandler {
	return &WorkspaceGroupHandler{groups: groups, departments: departments}
}

func (h *WorkspaceGroupHandler) actor(w http.ResponseWriter, r *http.Request) (uwguc.Actor, bool) {
	workspaceID, scope, ok := requireRequestScope(w, r, h.departments)
	if !ok {
		return uwguc.Actor{}, false
	}
	return uwguc.Actor{WorkspaceID: workspaceID, UserID: middleware.GetClaims(r).UserID, Departments: scope}, true
}

func writeGroupPage[T any](w http.ResponseWriter, page *shared.PaginatedResult[T]) {
	items := page.Items
	if items == nil {
		items = []T{}
	}
	response.WritePaginated(w, http.StatusOK, items, response.PaginationMeta{
		Page: page.Page, PageSize: page.PageSize, TotalPages: page.TotalPages, TotalItems: page.TotalItems,
	})
}

func decodeGroupBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil && !errors.Is(err, io.EOF) {
		response.WriteError(w, http.StatusBadRequest, "invalid request body", nil)
		return false
	}
	return true
}

func isSet(values url.Values, key string) bool {
	v := httpx.ParseBoolQuery(values.Get(key))
	return v != nil && *v
}

func optionalText(values url.Values, key string) *string {
	if !values.Has(key) {
		return nil
	}
	v := strings.TrimSpace(values.Get(key))
	return &v
}

// @Summary		Painel dos grupos
// @Description	Números conectados, grupos acompanhados, pessoas, análises e ciclos, volume diário de mensagens, grupos mais ativos, sentimento geral e por grupo, tempo de resposta, crescimento, insights recentes e pessoas em vários grupos. Só entram grupos com um número do escopo de departamentos de quem consulta; datas no fuso do workspace.
// @Tags			Unofficial WhatsApp Groups
// @Produce		json
// @Success		200	{object}	unofficial_whatsapp_group_usecase.DashboardStats
// @Failure		403	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/unofficial-whatsapp/groups/dashboard [get]
func (h *WorkspaceGroupHandler) Dashboard(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	stats, err := h.groups.Dashboard(r.Context(), a)
	if err != nil {
		writeWorkspaceGroupError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, stats)
}

// @Summary		Entradas e saídas por grupo
// @Description	Membros, entradas, saídas, taxa de saída, saldo e mensagens de cada grupo acompanhado no período (`month` é o mês corrente), com o resumo de todos. Grupos sem base para a taxa de saída ficam por último em qualquer ordem.
// @Tags			Unofficial WhatsApp Groups
// @Produce		json
// @Param			period		query		string	false	"month, 7d, 30d ou 90d"	default(month)
// @Param			origin		query		string	false	"managed ou external"
// @Param			category	query		string	false	"Categoria; vazio filtra grupos sem categoria"
// @Param			search		query		string	false	"Nome ou descrição do grupo"
// @Param			sort		query		string	false	"name, members, joins, leaves, churn, growth, net ou messages"
// @Param			desc		query		bool	false	"Ordem decrescente"
// @Param			page		query		int		false	"Página"
// @Param			pageSize	query		int		false	"Itens por página"
// @Success		200			{object}	membershipResponse
// @Failure		400			{object}	response.ErrorResponse
// @Failure		403			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/unofficial-whatsapp/groups/membership [get]
func (h *WorkspaceGroupHandler) Membership(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	values := r.URL.Query()
	origin, ok := parseOrigin(w, values)
	if !ok {
		return
	}
	period := uwg.Period(strings.TrimSpace(values.Get("period")))
	if period == "" {
		period = uwg.PeriodMonth
	}
	page, err := h.groups.MembershipTable(r.Context(), a, uwguc.MembershipTableInput{
		Period: period, Origin: origin, Category: optionalText(values, "category"), Search: strings.TrimSpace(values.Get("search")),
		Sort: uwg.MembershipSort(values.Get("sort")), Desc: isSet(values, "desc"),
		Pagination: httpx.ParsePagination(values),
	})
	if err != nil {
		writeWorkspaceGroupError(w, err)
		return
	}
	rows := page.Page.Items
	if rows == nil {
		rows = []*uwg.MembershipStat{}
	}
	response.WriteSuccess(w, http.StatusOK, membershipResponse{
		Data:    rows,
		Meta:    response.PaginationMeta{Page: page.Page.Page, PageSize: page.Page.PageSize, TotalPages: page.Page.TotalPages, TotalItems: page.Page.TotalItems},
		Summary: page.Summary, From: page.From, To: page.To, Timezone: page.Timezone,
	})
}

// @Summary		Pessoas em vários grupos
// @Description	Pessoas que estão em pelo menos `minGroups` grupos acompanhados ao mesmo tempo, com os grupos de cada uma e o lead vinculado. Nossos próprios números não entram.
// @Tags			Unofficial WhatsApp Groups
// @Produce		json
// @Param			search		query		string	false	"Nome ou telefone"
// @Param			minGroups	query		int		false	"Mínimo de grupos"	default(2)
// @Param			page		query		int		false	"Página"
// @Param			pageSize	query		int		false	"Itens por página"
// @Success		200			{object}	response.PaginatedPayload{data=[]unofficial_whatsapp_group.Person}
// @Failure		403			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/unofficial-whatsapp/groups/people [get]
func (h *WorkspaceGroupHandler) People(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	values := r.URL.Query()
	minGroups := 0
	if v := httpx.ParseIntQuery(values.Get("minGroups")); v != nil {
		minGroups = *v
	}
	page, err := h.groups.People(r.Context(), a, strings.TrimSpace(values.Get("search")), minGroups, httpx.ParsePagination(values))
	if err != nil {
		writeWorkspaceGroupError(w, err)
		return
	}
	writeGroupPage(w, page)
}

// @Summary		Temas das análises
// @Description	Temas que as análises de IA encontraram nos grupos nos últimos `days` dias, com quantas vezes e em quantos grupos apareceram.
// @Tags			Unofficial WhatsApp Groups
// @Produce		json
// @Param			days		query		int		false	"Janela em dias"	default(30)
// @Param			search		query		string	false	"Texto do tema"
// @Param			page		query		int		false	"Página"
// @Param			pageSize	query		int		false	"Itens por página"
// @Success		200			{object}	response.PaginatedPayload{data=[]unofficial_whatsapp_group.ThemeAggregate}
// @Failure		403			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/unofficial-whatsapp/groups/themes [get]
func (h *WorkspaceGroupHandler) Themes(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	values := r.URL.Query()
	days := 30
	if v := httpx.ParseIntQuery(values.Get("days")); v != nil {
		days = *v
	}
	page, err := h.groups.Themes(r.Context(), a, days, strings.TrimSpace(values.Get("search")), httpx.ParsePagination(values))
	if err != nil {
		writeWorkspaceGroupError(w, err)
		return
	}
	writeGroupPage(w, page)
}

// @Summary		Categorias dos grupos
// @Tags			Unofficial WhatsApp Groups
// @Produce		json
// @Success		200	{array}		string
// @Failure		403	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/unofficial-whatsapp/groups/categories [get]
func (h *WorkspaceGroupHandler) Categories(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	categories, err := h.groups.Categories(r.Context(), a)
	if err != nil {
		writeWorkspaceGroupError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, categories)
}

// @Summary		Listar os grupos
// @Description	Grupos dos números do escopo de quem consulta, com os números presentes em cada um (conectado, admin, pode enviar), atividade, resumo da análise e campanhas em andamento.
// @Tags			Unofficial WhatsApp Groups
// @Produce		json
// @Param			search		query		string	false	"Nome do grupo"
// @Param			followed	query		bool	false	"Somente acompanhados ou não acompanhados"
// @Param			origin		query		string	false	"managed ou external"
// @Param			category	query		string	false	"Categoria; vazio filtra grupos sem categoria"
// @Param			instanceId	query		string	false	"Número"
// @Param			adminsOnly	query		bool	false	"Permissão de envio: true para grupos em que só admins enviam mensagens, false para grupos abertos em que todos enviam; ausente traz os dois"
// @Param			hasAnalysis	query		bool	false	"Com ou sem análise"
// @Param			sort		query		string	false	"recent, oldest, name, name_desc ou members"
// @Param			page		query		int		false	"Página"
// @Param			pageSize	query		int		false	"Itens por página"
// @Success		200			{object}	response.PaginatedPayload{data=[]unofficial_whatsapp_group_usecase.GroupRow}
// @Failure		400			{object}	response.ErrorResponse
// @Failure		403			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/unofficial-whatsapp/groups [get]
func (h *WorkspaceGroupHandler) List(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	values := r.URL.Query()
	origin, ok := parseOrigin(w, values)
	if !ok {
		return
	}
	page, err := h.groups.ListGroups(r.Context(), a, uwguc.ListGroupsInput{
		Search:      strings.TrimSpace(values.Get("search")),
		Followed:    httpx.ParseBoolQuery(values.Get("followed")),
		Origin:      origin,
		Category:    optionalText(values, "category"),
		InstanceID:  strings.TrimSpace(values.Get("instanceId")),
		AdminsOnly:  httpx.ParseBoolQuery(values.Get("adminsOnly")),
		HasAnalysis: httpx.ParseBoolQuery(values.Get("hasAnalysis")),
		Sort:        uwg.GroupSort(values.Get("sort")),
		Pagination:  httpx.ParsePagination(values),
	})
	if err != nil {
		writeWorkspaceGroupError(w, err)
		return
	}
	writeGroupPage(w, page)
}

func parseOrigin(w http.ResponseWriter, values url.Values) (uwg.Origin, bool) {
	origin := uwg.Origin(strings.TrimSpace(values.Get("origin")))
	if origin != "" && !origin.Valid() {
		response.WriteError(w, http.StatusBadRequest, uwg.ErrInvalidOrigin.Error(), nil)
		return "", false
	}
	return origin, true
}

// @Summary		Buscar os grupos dos números
// @Description	Lê no WhatsApp os grupos de cada número conectado do escopo e registra os que ainda não existem, sem acompanhar nenhum. Um número que falhar entra em `failed` e não impede os outros.
// @Tags			Unofficial WhatsApp Groups
// @Produce		json
// @Success		200	{object}	unofficial_whatsapp_group_usecase.DiscoverResult
// @Failure		403	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/unofficial-whatsapp/groups/discover [post]
func (h *WorkspaceGroupHandler) Discover(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	result, err := h.groups.Discover(r.Context(), a)
	if err != nil {
		writeWorkspaceGroupError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, result)
}

// @Summary		Detalhe de um grupo
// @Description	Grupo com os números presentes, contagem de membros, crescimento em 30 dias, visão geral das análises, último ciclo e as análises do grupo. `conversationId` é a conversa do grupo que quem consulta pode abrir.
// @Tags			Unofficial WhatsApp Groups
// @Produce		json
// @Param			id	path		string	true	"Grupo"
// @Success		200	{object}	unofficial_whatsapp_group_usecase.GroupDetail
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/unofficial-whatsapp/groups/{id} [get]
func (h *WorkspaceGroupHandler) Get(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	detail, err := h.groups.GetGroup(r.Context(), a, mux.Vars(r)["id"])
	if err != nil {
		writeWorkspaceGroupError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, detail)
}

// @Summary		Editar um grupo
// @Description	Com `applyToSeries`, a mudança vale para todos os grupos da série que quem pede vê, e o nome vira o nome base de cada um ("Nome #N"); se algum falhar responde 409 com os nomes. A categoria fica só na plataforma. Nome, descrição, foto e configurações mudam no WhatsApp pelo número admin do escopo; sem um, responde 403.
// @Tags			Unofficial WhatsApp Groups
// @Accept			json
// @Produce		json
// @Param			id		path		string						true	"Grupo"
// @Param			body	body		updateWorkspaceGroupRequest	true	"Campos a alterar"
// @Success		200		{object}	unofficial_whatsapp_group.Group
// @Failure		400		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Failure		409		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/unofficial-whatsapp/groups/{id} [patch]
func (h *WorkspaceGroupHandler) Update(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req updateWorkspaceGroupRequest
	if !decodeGroupBody(w, r, &req) {
		return
	}
	g, err := h.groups.UpdateGroup(r.Context(), a, mux.Vars(r)["id"], uwguc.UpdateGroupInput{
		ApplyToSeries: req.ApplyToSeries, Category: req.Category, Subject: req.Subject, Description: req.Description, ImageURL: req.ImageURL,
		AdminsOnlyMessages: req.AdminsOnlyMessages, AdminsOnlyEdit: req.AdminsOnlyEdit,
	})
	if err != nil {
		writeWorkspaceGroupError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, g)
}

// @Summary		Acompanhar um grupo
// @Description	Passa a ler os membros, as entradas e saídas e a contar mensagens do grupo. A primeira leitura dos membros vira a base e não conta como entrada.
// @Tags			Unofficial WhatsApp Groups
// @Produce		json
// @Param			id	path		string	true	"Grupo"
// @Success		200	{object}	unofficial_whatsapp_group.Group
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/unofficial-whatsapp/groups/{id}/follow [post]
func (h *WorkspaceGroupHandler) Follow(w http.ResponseWriter, r *http.Request) {
	h.groupAction(w, r, h.groups.Follow)
}

// @Summary		Deixar de acompanhar um grupo
// @Description	Para de registrar entradas, saídas e mensagens do grupo. O histórico já registrado continua.
// @Tags			Unofficial WhatsApp Groups
// @Produce		json
// @Param			id	path		string	true	"Grupo"
// @Success		200	{object}	unofficial_whatsapp_group.Group
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/unofficial-whatsapp/groups/{id}/unfollow [post]
func (h *WorkspaceGroupHandler) Unfollow(w http.ResponseWriter, r *http.Request) {
	h.groupAction(w, r, h.groups.Unfollow)
}

// @Summary		Atualizar um grupo pelo WhatsApp
// @Description	Relê no WhatsApp os dados e os membros do grupo por um número conectado do escopo.
// @Tags			Unofficial WhatsApp Groups
// @Produce		json
// @Param			id	path		string	true	"Grupo"
// @Success		200	{object}	unofficial_whatsapp_group.Group
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		409	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/unofficial-whatsapp/groups/{id}/refresh [post]
func (h *WorkspaceGroupHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	h.groupAction(w, r, h.groups.Refresh)
}

// @Summary		Próximo grupo da série
// @Description	Cria o próximo grupo de uma série com o mesmo nome numerado, os mesmos números e as mesmas configurações. Só grupos criados aqui podem abrir uma série.
// @Tags			Unofficial WhatsApp Groups
// @Produce		json
// @Param			id	path		string	true	"Grupo da série"
// @Success		201	{object}	unofficial_whatsapp_group_usecase.ManagedGroupResult
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		409	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/unofficial-whatsapp/groups/{id}/series [post]
func (h *WorkspaceGroupHandler) NextInSeries(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	result, err := h.groups.NextInSeries(r.Context(), a, mux.Vars(r)["id"])
	if err != nil {
		writeWorkspaceGroupError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, result)
}

func (h *WorkspaceGroupHandler) groupAction(w http.ResponseWriter, r *http.Request, act func(context.Context, uwguc.Actor, string) (*uwg.Group, error)) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	g, err := act(r.Context(), a, mux.Vars(r)["id"])
	if err != nil {
		writeWorkspaceGroupError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, g)
}

func writeWorkspaceGroupError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, uwg.ErrGroupNotFound), errors.Is(err, uwg.ErrAnalysisNotFound), errors.Is(err, uwg.ErrJoinNotFound):
		response.WriteError(w, http.StatusNotFound, err.Error(), nil)

	case errors.Is(err, uwg.ErrNumberOutOfScope):
		response.WriteError(w, http.StatusNotFound, "number not found", nil)

	case errors.Is(err, uwg.ErrNoConnectedNumber):
		response.WriteError(w, http.StatusForbidden, err.Error(), nil)

	case errors.Is(err, uwg.ErrGroupNotFollowed), errors.Is(err, uwg.ErrSeriesNeedsManagedGroup),
		errors.Is(err, uwg.ErrSeriesPartlyUpdated), errors.Is(err, uwg.ErrNoInviteLink), errors.Is(err, uwg.ErrNumberAlreadyInGroup),
		errors.Is(err, uwg.ErrAnalysisBusy):
		response.WriteError(w, http.StatusConflict, err.Error(), nil)

	case errors.Is(err, uwg.ErrCategoryTooLong), errors.Is(err, uwg.ErrInvalidOrigin),
		errors.Is(err, uwg.ErrGroupSubjectNeeded), errors.Is(err, uwg.ErrInviteInvalid),
		errors.Is(err, uwg.ErrNoNumbersSelected), errors.Is(err, uwg.ErrPeriodInvalid), errors.Is(err, uwg.ErrNoGroupsToExport),
		errors.Is(err, uwg.ErrAnalysisNameRequired), errors.Is(err, uwg.ErrAnalysisPromptRequired),
		errors.Is(err, uwg.ErrAnalysisGroupRequired), errors.Is(err, uwg.ErrMessagesPerCycleRange),
		errors.Is(err, uwg.ErrScheduleInvalid), errors.Is(err, uwg.ErrTriggerInvalid),
		errors.Is(err, uwg.ErrAnalysisStatusInvalid), errors.Is(err, uwg.ErrPromptTooLong),
		errors.Is(err, uwg.ErrTrackedFieldKey), errors.Is(err, uwg.ErrTrackedFieldDuplicate),
		errors.Is(err, uwg.ErrTrackedFieldType), errors.Is(err, uwg.ErrTrackedFieldsTooMany):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)

	default:
		writeDomainError(w, err)
	}
}
