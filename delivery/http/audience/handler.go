package audience

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	ca "vozko/domain/audience"
	"vozko/domain/shared"
	"vozko/infra/http/middleware"
	cauc "vozko/usecases/audience"
)

const maxRequestBody = 256 << 10

type Handler struct {
	list              ca.ListUseCase
	stats             ca.StatsUseCase
	trends            ca.TrendsUseCase
	authors           ca.ListAuthorsUseCase
	author            ca.GetAuthorUseCase
	containers        ca.ListAuthorContainersUseCase
	escalate          ca.EscalateCommentUseCase
	recipients        cauc.ListEscalationRecipientsUseCase
	alerts            *cauc.ManageAlertRulesUseCase
	channels          *cauc.GetAlertChannelsUseCase
	testAlert         *cauc.TestAlertRuleUseCase
	suggest           ca.SuggestCommentReplyUseCase
	postReply         ca.PostCommentReplyUseCase
	moderate          ca.SetModerationStateUseCase
	getSet            ca.GetSettingsUseCase
	updateSet         ca.UpdateSettingsUseCase
	retry             ca.RetryUseCase
	spend             ca.SpendUseCase
	usage             ca.UsageUseCase
	workspaceSettings ca.WorkspaceSettingsUseCase
	estimate          ca.EstimateBackfillUseCase
	start             ca.StartBackfillUseCase
	backfill          ca.GetBackfillUseCase
	cancel            ca.CancelBackfillUseCase

	accounts     ca.ListAccountSettingsUseCase
	getContainer ca.GetContainerSettingsUseCase
	putContainer ca.PutContainerSettingsUseCase
	delContainer ca.DeleteContainerSettingsUseCase
}

type Deps struct {
	List              ca.ListUseCase
	Stats             ca.StatsUseCase
	Trends            ca.TrendsUseCase
	Authors           ca.ListAuthorsUseCase
	Author            ca.GetAuthorUseCase
	Containers        ca.ListAuthorContainersUseCase
	Escalate          ca.EscalateCommentUseCase
	Recipients        cauc.ListEscalationRecipientsUseCase
	Alerts            *cauc.ManageAlertRulesUseCase
	Channels          *cauc.GetAlertChannelsUseCase
	TestAlert         *cauc.TestAlertRuleUseCase
	Suggest           ca.SuggestCommentReplyUseCase
	PostReply         ca.PostCommentReplyUseCase
	Moderate          ca.SetModerationStateUseCase
	GetSet            ca.GetSettingsUseCase
	UpdateSet         ca.UpdateSettingsUseCase
	Retry             ca.RetryUseCase
	Spend             ca.SpendUseCase
	Usage             ca.UsageUseCase
	WorkspaceSettings ca.WorkspaceSettingsUseCase
	Estimate          ca.EstimateBackfillUseCase
	Start             ca.StartBackfillUseCase
	Backfill          ca.GetBackfillUseCase
	Cancel            ca.CancelBackfillUseCase

	Accounts     ca.ListAccountSettingsUseCase
	GetContainer ca.GetContainerSettingsUseCase
	PutContainer ca.PutContainerSettingsUseCase
	DelContainer ca.DeleteContainerSettingsUseCase
}

func NewHandler(d Deps) *Handler {
	return &Handler{
		list: d.List, stats: d.Stats, trends: d.Trends, authors: d.Authors, author: d.Author, containers: d.Containers, escalate: d.Escalate, recipients: d.Recipients,
		alerts: d.Alerts, testAlert: d.TestAlert, channels: d.Channels,
		suggest: d.Suggest, postReply: d.PostReply, moderate: d.Moderate,
		getSet: d.GetSet, updateSet: d.UpdateSet, retry: d.Retry, spend: d.Spend, usage: d.Usage,
		workspaceSettings: d.WorkspaceSettings,
		estimate:          d.Estimate, start: d.Start, backfill: d.Backfill, cancel: d.Cancel,
		accounts: d.Accounts, getContainer: d.GetContainer, putContainer: d.PutContainer, delContainer: d.DelContainer,
	}
}

func listInput(r *http.Request) ca.ListInput {
	v := r.URL.Query()
	in := ca.ListInput{
		WorkspaceID:      middleware.GetWorkspaceID(r),
		Source:           ca.Source(strings.TrimSpace(v.Get("source"))),
		AccountID:        strings.TrimSpace(v.Get("accountId")),
		ContainerID:      strings.TrimSpace(v.Get("containerId")),
		TopicKey:         strings.TrimSpace(v.Get("topic")),
		Stance:           ca.Stance(strings.TrimSpace(v.Get("stance"))),
		Sentiment:        shared.Sentiment(strings.TrimSpace(v.Get("sentiment"))),
		Intent:           ca.Intent(strings.TrimSpace(v.Get("intent"))),
		AuthorExternalID: strings.TrimSpace(v.Get("authorExternalId")),
		Options: shared.QueryOptions{
			Pagination: httpx.ParsePagination(v),
			Sorts:      httpx.ParseSort(v, map[string]string{"severity": "severity", "commentedat": "occurred_at"}),
		},
	}
	for _, s := range strings.Split(v.Get("status"), ",") {
		if s = strings.TrimSpace(s); s != "" {
			in.Statuses = append(in.Statuses, ca.Status(s))
		}
	}
	in.SubjectKinds = parseSubjectKinds(v.Get("subjectKind"))
	in.Interest = ca.Interest(strings.TrimSpace(v.Get("interest")))
	in.Disposition = ca.Disposition(strings.TrimSpace(v.Get("disposition")))
	in.Qualification = ca.Qualification(strings.TrimSpace(v.Get("qualification")))
	in.NextAction = ca.NextAction(strings.TrimSpace(v.Get("nextAction")))
	in.SubjectID = strings.TrimSpace(v.Get("subjectId"))
	in.LatestOnly = v.Get("latestOnly") == "true"

	in.SeverityMin = intParam(v, "severityMin")
	in.SeverityMax = intParam(v, "severityMax")
	in.RequiresAction = boolParam(v, "requiresAction")
	in.From = timeParam(v, "from")
	in.To = timeParam(v, "to")
	return in
}

// @Summary	Listar comentários analisados
// @Tags		Analysis
// @Produce	json
// @Success	200	{object}	response.PaginatedPayload
// @Security	BearerAuth
// @Router		/comment-analysis [get]
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	h.listFiltered(w, r, listInput(r))
}

func (h *Handler) listFiltered(w http.ResponseWriter, r *http.Request, in ca.ListInput) {
	result, err := h.list.Execute(r.Context(), in)
	if err != nil {
		writeDomainError(w, err, "Failed to list analysed comments")
		return
	}
	items := make([]CommentResponse, 0, len(result.Items))
	for _, a := range result.Items {
		items = append(items, toCommentResponse(a))
	}
	response.WritePaginated(w, http.StatusOK, items, response.PaginationMeta{
		Page: result.Page, PageSize: result.PageSize, TotalPages: result.TotalPages, TotalItems: result.TotalItems,
	})
}

// @Summary	Estatísticas ao vivo da análise de comentários
// @Tags		Analysis
// @Produce	json
// @Success	200	{object}	StatsResponse
// @Security	BearerAuth
// @Router		/comment-analysis/stats [get]
func (h *Handler) Stats(w http.ResponseWriter, r *http.Request) {
	h.statsFiltered(w, r, listInput(r))
}

func (h *Handler) statsFiltered(w http.ResponseWriter, r *http.Request, in ca.ListInput) {
	stats, err := h.stats.Execute(r.Context(), in)
	if err != nil {
		writeDomainError(w, err, "Failed to compute stats")
		return
	}
	response.WriteSuccess(w, http.StatusOK, toStatsResponse(stats))
}

// @Summary	Série diária (rollups) da análise de comentários
// @Tags		Analysis
// @Produce	json
// @Param		scope	query	string	true	"account|container|topic"
// @Param		scopeId	query	string	true	"ID do escopo"
// @Success	200	{array}	TrendPointResponse
// @Security	BearerAuth
// @Router		/comment-analysis/trends [get]
func (h *Handler) Trends(w http.ResponseWriter, r *http.Request) {
	v := r.URL.Query()
	now := time.Now().UTC()
	from, to := now.Add(-30*24*time.Hour), now
	if t := timeParam(v, "from"); t != nil {
		from = *t
	}
	if t := timeParam(v, "to"); t != nil {
		to = *t
	}
	scope, scopeID := strings.TrimSpace(v.Get("scope")), strings.TrimSpace(v.Get("scopeId"))
	var rows []*ca.Rollup
	var err error
	if scope != "" || scopeID != "" {
		rows, err = h.trends.Execute(r.Context(), ca.TrendInput{
			WorkspaceID: middleware.GetWorkspaceID(r),
			Scope:       ca.RollupScope(scope), ScopeID: scopeID,
			From: from, To: to,
		})
	} else {
		in := listInput(r)
		in.From, in.To = &from, &to
		rows, err = h.trends.ExecuteFiltered(r.Context(), in)
	}
	if err != nil {
		writeDomainError(w, err, "Failed to load trends")
		return
	}
	out := make([]TrendPointResponse, 0, len(rows))
	for _, r := range rows {
		out = append(out, toTrendPoint(r))
	}
	response.WriteSuccess(w, http.StatusOK, out)
}

// @Summary	Autores ranqueados (quem comenta coisas ruins)
// @Tags		Analysis
// @Produce	json
// @Success	200	{object}	response.PaginatedPayload
// @Security	BearerAuth
// @Router		/comment-analysis/authors [get]
func (h *Handler) ListAuthors(w http.ResponseWriter, r *http.Request) {
	v := r.URL.Query()
	in := ca.AuthorsInput{
		WorkspaceID:      middleware.GetWorkspaceID(r),
		Source:           ca.Source(strings.TrimSpace(v.Get("source"))),
		AccountID:        strings.TrimSpace(v.Get("accountId")),
		FlaggedOnly:      v.Get("flagged") == "true",
		Stance:           ca.Stance(strings.TrimSpace(v.Get("stance"))),
		ModerationState:  ca.ModerationState(strings.TrimSpace(v.Get("moderation"))),
		AuthorExternalID: strings.TrimSpace(v.Get("authorExternalId")),
		Options:          shared.QueryOptions{Pagination: httpx.ParsePagination(v)},
	}
	if n := intParam(v, "minComments"); n != nil {
		in.MinComments = *n
	}
	in.From = timeParam(v, "from")
	in.To = timeParam(v, "to")
	if raw := strings.TrimSpace(v.Get("sort")); raw != "" {
		key, ok := ca.ParseAuthorSortKey(raw)
		if !ok {
			response.WriteError(w, http.StatusBadRequest, "Unknown sort key", map[string]string{
				"sort": strings.Join(authorSortKeyNames(), " | "),
			})
			return
		}
		in.Sort = ca.Sort{Key: key, Ascending: strings.EqualFold(strings.TrimSpace(v.Get("order")), "asc")}
	}
	result, err := h.authors.Execute(r.Context(), in)
	if err != nil {
		writeDomainError(w, err, "Failed to list authors")
		return
	}
	items := make([]AuthorResponse, 0, len(result.Items))
	for _, a := range result.Items {
		items = append(items, toAuthorResponse(a))
	}
	response.WritePaginated(w, http.StatusOK, items, response.PaginationMeta{
		Page: result.Page, PageSize: result.PageSize, TotalPages: result.TotalPages, TotalItems: result.TotalItems,
	})
}

// @Summary	Um autor e seus comentários
// @Tags		Analysis
// @Produce	json
// @Param		id	path	string	true	"ID do autor"
// @Success	200	{object}	AuthorDetailResponse
// @Security	BearerAuth
// @Router		/comment-analysis/authors/{id} [get]
func (h *Handler) GetAuthor(w http.ResponseWriter, r *http.Request) {
	detail, err := h.author.Execute(r.Context(), middleware.GetWorkspaceID(r), mux.Vars(r)["id"], httpx.ParsePagination(r.URL.Query()))
	if err != nil {
		writeDomainError(w, err, "Failed to load author")
		return
	}
	comments := make([]CommentResponse, 0, len(detail.Comments.Items))
	for _, c := range detail.Comments.Items {
		comments = append(comments, toCommentResponse(c))
	}
	response.WriteSuccess(w, http.StatusOK, AuthorDetailResponse{
		Author: toAuthorResponse(detail.Author), Comments: comments,
		Page: detail.Comments.Page, PageSize: detail.Comments.PageSize, Total: detail.Comments.TotalItems,
	})
}

// @Summary	Posts em que um autor comentou
// @Tags		Analysis
// @Produce	json
// @Param		id	path	string	true	"ID do autor"
// @Success	200	{object}	AuthorContainersResponse
// @Security	BearerAuth
// @Router		/comment-analysis/authors/{id}/containers [get]
func (h *Handler) ListAuthorContainers(w http.ResponseWriter, r *http.Request) {
	v := r.URL.Query()
	out, err := h.containers.Execute(r.Context(), ca.AuthorContainersRequest{
		WorkspaceID: middleware.GetWorkspaceID(r),
		AuthorID:    mux.Vars(r)["id"],
		From:        timeParam(v, "from"),
		To:          timeParam(v, "to"),
		Page:        httpx.ParsePagination(v),
	})
	if err != nil {
		writeDomainError(w, err, "Failed to load author posts")
		return
	}
	containers := make([]AuthorContainerResponse, 0, len(out.Containers.Items))
	for _, c := range out.Containers.Items {
		containers = append(containers, toAuthorContainerResponse(c))
	}
	response.WriteSuccess(w, http.StatusOK, AuthorContainersResponse{
		Author: toAuthorResponse(out.Author), Containers: containers,
		Page: out.Containers.Page, PageSize: out.Containers.PageSize, Total: out.Containers.TotalItems,
	})
}

// @Summary	Definir estado de moderação de um autor
// @Tags		Analysis
// @Accept		json
// @Produce	json
// @Param		id	path	string	true	"ID do autor"
// @Success	200	{object}	AuthorResponse
// @Security	BearerAuth
// @Router		/comment-analysis/authors/{id} [patch]
func (h *Handler) SetModeration(w http.ResponseWriter, r *http.Request) {
	var req ModerationRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	author, err := h.moderate.Execute(r.Context(), ca.SetModerationStateInput{
		WorkspaceID: middleware.GetWorkspaceID(r), AuthorID: mux.Vars(r)["id"], State: ca.ModerationState(strings.TrimSpace(req.State)),
	})
	if err != nil {
		writeDomainError(w, err, "Failed to update moderation state")
		return
	}
	response.WriteSuccess(w, http.StatusOK, toAuthorResponse(author))
}

func accountRef(r *http.Request) (ca.Source, string) {
	vars := mux.Vars(r)
	source := ca.Source(strings.TrimSpace(vars["source"]))
	if source == "" {
		source = ca.SourceInstagram
	}
	return source, strings.TrimSpace(vars["accountId"])
}

// @Summary	Configuração da análise de comentários de uma conta
// @Tags		Analysis
// @Produce	json
// @Param		source	path	string	true	"Canal (instagram)"
// @Param		accountId	path	string	true	"ID da conta"
// @Success	200	{object}	SettingsResponse
// @Security	BearerAuth
// @Router		/comment-analysis/settings/{source}/{accountId} [get]
func (h *Handler) GetSettings(w http.ResponseWriter, r *http.Request) {
	source, accountID := accountRef(r)
	s, err := h.getSet.Execute(r.Context(), middleware.GetWorkspaceID(r), source, accountID)
	if err != nil {
		writeDomainError(w, err, "Failed to load settings")
		return
	}
	response.WriteSuccess(w, http.StatusOK, toSettingsResponse(s))
}

// @Summary	Atualizar configuração da análise de comentários
// @Tags		Analysis
// @Accept		json
// @Produce	json
// @Param		source	path	string	true	"Canal (instagram)"
// @Param		accountId	path	string	true	"ID da conta"
// @Success	200	{object}	SettingsResponse
// @Security	BearerAuth
// @Router		/comment-analysis/settings/{source}/{accountId} [patch]
func (h *Handler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	var req SettingsRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	source, accountID := accountRef(r)
	in := ca.UpdateSettingsInput{
		WorkspaceID: middleware.GetWorkspaceID(r), Source: source, AccountID: accountID,
		Enabled: req.Enabled, Model: req.Model, DailyCap: req.DailyCap, Instructions: req.Instructions,
	}
	if req.Vertical != nil {
		v := ca.Vertical(strings.TrimSpace(*req.Vertical))
		in.Vertical = &v
	}
	if req.Topics != nil {
		t := ca.TopicSet(*req.Topics)
		in.Topics = &t
	}
	if req.SeverityThreshold != nil {
		in.ActionPolicy = &ca.ActionPolicy{SeverityThreshold: *req.SeverityThreshold}
	}
	if req.ReplyPolicy != nil {
		in.ReplyPolicy = req.ReplyPolicy
	}
	s, err := h.updateSet.Execute(r.Context(), in)
	if err != nil {
		writeDomainError(w, err, "Failed to update settings")
		return
	}
	response.WriteSuccess(w, http.StatusOK, toSettingsResponse(s))
}

// @Summary	Reprocessar um comentário com falha
// @Tags		Analysis
// @Produce	json
// @Param		id	path	string	true	"ID da análise"
// @Success	200	{object}	CommentResponse
// @Security	BearerAuth
// @Router		/comment-analysis/{id}/retry [post]
func (h *Handler) Retry(w http.ResponseWriter, r *http.Request) {
	row, err := h.retry.Execute(r.Context(), middleware.GetWorkspaceID(r), mux.Vars(r)["id"])
	if err != nil {
		writeDomainError(w, err, "Failed to retry")
		return
	}
	response.WriteSuccess(w, http.StatusOK, toCommentResponse(row))
}

// @Summary	Encaminhar um comentário por WhatsApp
// @Tags		Analysis
// @Accept		json
// @Produce	json
// @Param		id	path	string	true	"ID da análise"
// @Success	200	{object}	EscalationResponse
// @Security	BearerAuth
// @Router		/comment-analysis/{id}/escalate [post]
func (h *Handler) Escalate(w http.ResponseWriter, r *http.Request) {
	var req EscalateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	var userID string
	if claims := middleware.GetClaims(r); claims != nil {
		userID = claims.UserID
	}
	escalation, err := h.escalate.Execute(r.Context(), ca.EscalateCommentInput{
		WorkspaceID:   middleware.GetWorkspaceID(r),
		UserID:        userID,
		CommentID:     mux.Vars(r)["id"],
		RecipientID:   strings.TrimSpace(req.RecipientID),
		RecipientKind: strings.TrimSpace(req.RecipientKind),
		Note:          req.Note,
	})
	if err != nil {
		writeDomainError(w, err, "Failed to forward the comment")
		return
	}
	response.WriteSuccess(w, http.StatusOK, toEscalationResponse(*escalation))
}

// @Summary	Conversas para as quais um comentário pode ser encaminhado
// @Tags		Analysis
// @Produce	json
// @Param		query	query	string	false	"Busca por nome ou número"
// @Success	200	{array}	audience_usecase.EscalationRecipient
// @Security	BearerAuth
// @Router		/comment-analysis/escalation-recipients [get]
func (h *Handler) EscalationRecipients(w http.ResponseWriter, r *http.Request) {
	var userID string
	if claims := middleware.GetClaims(r); claims != nil {
		userID = claims.UserID
	}
	rows, err := h.recipients.Execute(r.Context(), cauc.EscalationRecipientQuery{
		WorkspaceID: middleware.GetWorkspaceID(r),
		UserID:      userID,
		Query:       strings.TrimSpace(r.URL.Query().Get("query")),
		Limit:       intOrZero(intParam(r.URL.Query(), "limit")),
	})
	if err != nil {
		writeDomainError(w, err, "Failed to list recipients")
		return
	}
	response.WriteSuccess(w, http.StatusOK, rows)
}

// @Summary	Sugerir uma resposta ao comentário com IA
// @Tags		Analysis
// @Produce	json
// @Param		id	path	string	true	"ID da análise"
// @Success	200	{object}	audience.ReplySuggestion
// @Security	BearerAuth
// @Router		/comment-analysis/{id}/reply/suggest [post]
func (h *Handler) SuggestReply(w http.ResponseWriter, r *http.Request) {
	suggestion, err := h.suggest.Execute(r.Context(), ca.SuggestReplyInput{
		WorkspaceID: middleware.GetWorkspaceID(r),
		CommentID:   mux.Vars(r)["id"],
	})
	if err != nil {
		writeDomainError(w, err, "Failed to draft a reply")
		return
	}
	response.WriteSuccess(w, http.StatusOK, suggestion)
}

// @Summary	Publicar uma resposta ao comentário
// @Tags		Analysis
// @Accept		json
// @Produce	json
// @Param		id	path	string	true	"ID da análise"
// @Success	200	{object}	audience.ReplySuggestion
// @Security	BearerAuth
// @Router		/comment-analysis/{id}/reply [post]
func (h *Handler) PostReply(w http.ResponseWriter, r *http.Request) {
	var req ReplyRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	var userID string
	if claims := middleware.GetClaims(r); claims != nil {
		userID = claims.UserID
	}
	posted, err := h.postReply.Execute(r.Context(), ca.PostReplyInput{
		WorkspaceID: middleware.GetWorkspaceID(r),
		UserID:      userID,
		CommentID:   mux.Vars(r)["id"],
		Text:        req.Text,
	})
	if err != nil {
		writeDomainError(w, err, "Failed to publish the reply")
		return
	}
	response.WriteSuccess(w, http.StatusOK, posted)
}

func intOrZero(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}

// @Summary	Gasto com análise de comentários no período
// @Tags		Analysis
// @Produce	json
// @Success	200	{object}	SpendResponse
// @Security	BearerAuth
// @Router		/comment-analysis/spend [get]
func (h *Handler) Spend(w http.ResponseWriter, r *http.Request) {
	v := r.URL.Query()
	in := ca.SpendInput{Source: ca.Source(strings.TrimSpace(v.Get("source"))), AccountID: strings.TrimSpace(v.Get("accountId"))}
	if d := intParam(v, "days"); d != nil {
		in.Days = *d
	}
	totals, err := h.spend.Execute(r.Context(), middleware.GetWorkspaceID(r), in)
	if err != nil {
		writeDomainError(w, err, "Failed to load spend")
		return
	}
	response.WriteSuccess(w, http.StatusOK, SpendResponse{BatchTotals: *totals})
}

// @Summary	Estimar um reprocessamento de comentários antigos
// @Tags		Analysis
// @Produce	json
// @Param		source	path	string	true	"Canal (instagram)"
// @Param		accountId	path	string	true	"ID da conta"
// @Success	200	{object}	BackfillEstimateResponse
// @Security	BearerAuth
// @Router		/comment-analysis/backfill/{source}/{accountId}/estimate [get]
func (h *Handler) EstimateBackfill(w http.ResponseWriter, r *http.Request) {
	source, accountID := accountRef(r)
	est, err := h.estimate.Execute(r.Context(), middleware.GetWorkspaceID(r), source, accountID, strings.TrimSpace(r.URL.Query().Get("containerId")))
	if err != nil {
		writeDomainError(w, err, "Failed to estimate")
		return
	}
	response.WriteSuccess(w, http.StatusOK, BackfillEstimateResponse{
		Containers: est.Containers, EstimatedComments: est.EstimatedComments,
	})
}

// @Summary	Iniciar um reprocessamento de comentários antigos
// @Tags		Analysis
// @Accept		json
// @Produce	json
// @Param		source	path	string	true	"Canal (instagram)"
// @Param		accountId	path	string	true	"ID da conta"
// @Success	201	{object}	BackfillResponse
// @Security	BearerAuth
// @Router		/comment-analysis/backfill/{source}/{accountId} [post]
func (h *Handler) StartBackfill(w http.ResponseWriter, r *http.Request) {
	var req BackfillRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	source, accountID := accountRef(r)
	b, err := h.start.Execute(r.Context(), ca.StartBackfillInput{
		WorkspaceID: middleware.GetWorkspaceID(r), Source: source, AccountID: accountID,
		ContainerID: strings.TrimSpace(req.ContainerID), RequestedByUserID: r.Header.Get("X-User-ID"),
		ConfirmedEstimate: req.ConfirmedEstimate,
	})
	if err != nil {
		writeDomainError(w, err, "Failed to start backfill")
		return
	}
	response.WriteSuccess(w, http.StatusCreated, toBackfillResponse(b))
}

// @Summary	Progresso de um reprocessamento
// @Tags		Analysis
// @Produce	json
// @Param		id	path	string	true	"ID do reprocessamento"
// @Success	200	{object}	BackfillResponse
// @Security	BearerAuth
// @Router		/comment-analysis/backfill/{id} [get]
func (h *Handler) GetBackfill(w http.ResponseWriter, r *http.Request) {
	b, err := h.backfill.Execute(r.Context(), middleware.GetWorkspaceID(r), mux.Vars(r)["id"])
	if err != nil {
		writeDomainError(w, err, "Failed to load backfill")
		return
	}
	response.WriteSuccess(w, http.StatusOK, toBackfillResponse(b))
}

// @Summary	Cancelar um reprocessamento
// @Tags		Analysis
// @Produce	json
// @Param		id	path	string	true	"ID do reprocessamento"
// @Success	200	{object}	BackfillResponse
// @Security	BearerAuth
// @Router		/comment-analysis/backfill/{id}/cancel [post]
func (h *Handler) CancelBackfill(w http.ResponseWriter, r *http.Request) {
	b, err := h.cancel.Execute(r.Context(), middleware.GetWorkspaceID(r), mux.Vars(r)["id"])
	if err != nil {
		writeDomainError(w, err, "Failed to cancel backfill")
		return
	}
	response.WriteSuccess(w, http.StatusOK, toBackfillResponse(b))
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	if err := json.NewDecoder(io.LimitReader(r.Body, maxRequestBody)).Decode(target); err != nil {
		response.WriteInvalidBodyError(w, nil)
		return false
	}
	return true
}

func intParam(v url.Values, key string) *int {
	raw := strings.TrimSpace(v.Get(key))
	if raw == "" {
		return nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return nil
	}
	return &n
}

func boolParam(v url.Values, key string) *bool {
	switch strings.ToLower(strings.TrimSpace(v.Get(key))) {
	case "true", "1":
		b := true
		return &b
	case "false", "0":
		b := false
		return &b
	}
	return nil
}

func timeParam(v url.Values, key string) *time.Time {
	raw := strings.TrimSpace(v.Get(key))
	if raw == "" {
		return nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return &t
	}
	if t, err := time.Parse("2006-01-02", raw); err == nil {
		t = t.UTC()
		return &t
	}
	return nil
}

func writeDomainError(w http.ResponseWriter, err error, fallback string) {
	switch {
	case errors.Is(err, ca.ErrNotFound):
		response.WriteError(w, http.StatusNotFound, "Not found", nil)
	case errors.Is(err, ca.ErrInvalidFilter), errors.Is(err, ca.ErrContainerInvalid),
		errors.Is(err, ca.ErrTooManyTopics), errors.Is(err, ca.ErrTopicKeyInvalid), errors.Is(err, ca.ErrTopicLabelTooLong),
		errors.Is(err, ca.ErrWorkspaceRequired):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	case errors.Is(err, ca.ErrStatusTransition):
		response.WriteErrorWithCode(w, http.StatusConflict, "invalid_state", err.Error(), nil)
	case errors.Is(err, ca.ErrChannelUnavailable):
		response.WriteErrorWithCode(w, http.StatusUnprocessableEntity, "channel_unavailable", err.Error(), nil)
	case errors.Is(err, cauc.ErrBackfillAlreadyActive):
		response.WriteErrorWithCode(w, http.StatusConflict, "backfill_active", err.Error(), nil)
	case errors.Is(err, cauc.ErrBackfillEstimateStale):
		response.WriteErrorWithCode(w, http.StatusConflict, "estimate_stale", err.Error(), nil)
	case errors.Is(err, cauc.ErrBackfillNotCancelable):
		response.WriteErrorWithCode(w, http.StatusConflict, "not_cancelable", err.Error(), nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, fallback, nil)
	}
}

// @Summary	Contas com análise de comentários configurada no workspace
// @Tags		Analysis
// @Produce	json
// @Success	200	{array}	SettingsResponse
// @Security	BearerAuth
// @Router		/comment-analysis/settings [get]
func (h *Handler) ListAccountSettings(w http.ResponseWriter, r *http.Request) {
	rows, err := h.accounts.Execute(r.Context(), middleware.GetWorkspaceID(r))
	if err != nil {
		writeDomainError(w, err, "Failed to list accounts")
		return
	}
	out := make([]SettingsResponse, 0, len(rows))
	for _, s := range rows {
		out = append(out, toSettingsResponse(s))
	}
	response.WriteSuccess(w, http.StatusOK, out)
}

func containerRef(r *http.Request) ca.ContainerRef {
	source, accountID := accountRef(r)
	return ca.ContainerRef{Source: source, AccountID: accountID, ContainerID: strings.TrimSpace(mux.Vars(r)["containerId"])}
}

// @Summary	Configuração de análise de uma publicação (override e efetiva)
// @Tags		Analysis
// @Produce	json
// @Param		source	path	string	true	"Canal (instagram)"
// @Param		accountId	path	string	true	"ID da conta"
// @Param		containerId	path	string	true	"ID da publicação"
// @Success	200	{object}	ContainerSettingsResponse
// @Security	BearerAuth
// @Router		/comment-analysis/settings/{source}/{accountId}/containers/{containerId} [get]
func (h *Handler) GetContainerSettings(w http.ResponseWriter, r *http.Request) {
	cs, err := h.getContainer.Execute(r.Context(), middleware.GetWorkspaceID(r), containerRef(r))
	if err != nil {
		writeDomainError(w, err, "Failed to load post settings")
		return
	}
	response.WriteSuccess(w, http.StatusOK, toContainerSettingsResponse(cs))
}

// @Summary	Substituir a configuração de análise de uma publicação
// @Tags		Analysis
// @Accept		json
// @Produce	json
// @Param		source	path	string	true	"Canal (instagram)"
// @Param		accountId	path	string	true	"ID da conta"
// @Param		containerId	path	string	true	"ID da publicação"
// @Success	200	{object}	ContainerSettingsResponse
// @Security	BearerAuth
// @Router		/comment-analysis/settings/{source}/{accountId}/containers/{containerId} [put]
func (h *Handler) PutContainerSettings(w http.ResponseWriter, r *http.Request) {
	var req ContainerOverrideRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	ref := containerRef(r)
	o := ca.ContainerOverride{
		WorkspaceID: middleware.GetWorkspaceID(r), Source: ref.Source, AccountID: ref.AccountID, ContainerID: ref.ContainerID,
		Enabled: req.Enabled, Model: req.Model, SeverityThreshold: req.SeverityThreshold, Instructions: req.Instructions,
	}
	if req.Topics != nil {
		t := ca.TopicSet(*req.Topics)
		o.Topics = &t
	}
	cs, err := h.putContainer.Execute(r.Context(), o)
	if err != nil {
		writeDomainError(w, err, "Failed to save post settings")
		return
	}
	response.WriteSuccess(w, http.StatusOK, toContainerSettingsResponse(cs))
}

// @Summary	Remover a configuração própria de uma publicação (volta a herdar)
// @Tags		Analysis
// @Produce	json
// @Param		source	path	string	true	"Canal (instagram)"
// @Param		accountId	path	string	true	"ID da conta"
// @Param		containerId	path	string	true	"ID da publicação"
// @Success	200	{object}	ContainerSettingsResponse
// @Security	BearerAuth
// @Router		/comment-analysis/settings/{source}/{accountId}/containers/{containerId} [delete]
func (h *Handler) DeleteContainerSettings(w http.ResponseWriter, r *http.Request) {
	cs, err := h.delContainer.Execute(r.Context(), middleware.GetWorkspaceID(r), containerRef(r))
	if err != nil {
		writeDomainError(w, err, "Failed to reset post settings")
		return
	}
	response.WriteSuccess(w, http.StatusOK, toContainerSettingsResponse(cs))
}

func authorSortKeyNames() []string {
	keys := ca.AllAuthorSortKeys()
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, string(k))
	}
	return out
}

// @Summary		Uso da análise contra o teto do workspace
// @Description	Quanto o workspace analisou na janela móvel de 24 horas e o teto em vigor. É a resposta a "por que a cobertura está baixa": ao atingir o teto a análise pausa até que as horas mais antigas saiam da janela.
// @Tags			Analysis
// @Produce		json
// @Success		200	{object}	audience.Usage
// @Security		BearerAuth
// @Router			/audience/usage [get]
func (h *Handler) Usage(w http.ResponseWriter, r *http.Request) {
	if h.usage == nil {
		response.WriteSuccess(w, http.StatusOK, ca.Usage{})
		return
	}
	usage, err := h.usage.Execute(r.Context(), middleware.GetWorkspaceID(r))
	if err != nil {
		writeDomainError(w, err, "Failed to load usage")
		return
	}
	response.WriteSuccess(w, http.StatusOK, usage)
}

type WorkspaceSettingsResponse struct {
	DailyCap                 int `json:"dailyCap"`
	DebounceMinutes          int `json:"debounceMinutes"`
	EffectiveDailyCap        int `json:"effectiveDailyCap"`
	EffectiveDebounceMinutes int `json:"effectiveDebounceMinutes"`
	MinDebounceMinutes       int `json:"minDebounceMinutes"`
	MaxDebounceMinutes       int `json:"maxDebounceMinutes"`
}

func workspaceSettingsResponse(s ca.WorkspaceSettings, effectiveDailyCap int) WorkspaceSettingsResponse {
	return WorkspaceSettingsResponse{
		DailyCap:                 s.DailyCap,
		DebounceMinutes:          s.DebounceMinutes,
		EffectiveDailyCap:        effectiveDailyCap,
		EffectiveDebounceMinutes: ca.ClampDebounceMinutes(s.DebounceMinutes),
		MinDebounceMinutes:       ca.MinDebounceMinutes,
		MaxDebounceMinutes:       ca.MaxDebounceMinutes,
	}
}

func (h *Handler) effectiveDailyCap(r *http.Request) int {
	if h.usage == nil {
		return 0
	}
	usage, err := h.usage.Execute(r.Context(), middleware.GetWorkspaceID(r))
	if err != nil {
		return 0
	}
	return usage.Limit
}

// @Summary		Configuração de análise do workspace
// @Description	Teto de análises e tempo de silêncio antes de analisar uma conversa. Zero significa "não definido", e o valor efetivo vem junto.
// @Tags			Analysis
// @Produce		json
// @Success		200	{object}	WorkspaceSettingsResponse
// @Security		BearerAuth
// @Router			/audience/workspace-settings [get]
func (h *Handler) WorkspaceSettings(w http.ResponseWriter, r *http.Request) {
	if h.workspaceSettings == nil {
		response.WriteSuccess(w, http.StatusOK, workspaceSettingsResponse(ca.WorkspaceSettings{}, 0))
		return
	}
	settings, err := h.workspaceSettings.Execute(r.Context(), middleware.GetWorkspaceID(r))
	if err != nil {
		writeDomainError(w, err, "Failed to load the analysis settings")
		return
	}
	response.WriteSuccess(w, http.StatusOK, workspaceSettingsResponse(settings, h.effectiveDailyCap(r)))
}

type UpdateWorkspaceSettingsRequest struct {
	DailyCap        *int `json:"dailyCap,omitempty"`
	DebounceMinutes *int `json:"debounceMinutes,omitempty"`
}

// @Summary		Alterar a configuração de análise do workspace
// @Description	Define o teto de análises na janela móvel de 24 horas e/ou quantos minutos a conversa precisa ficar sem mensagens antes de ser analisada. Vale para o workspace inteiro, em todos os canais.
// @Tags			Analysis
// @Accept			json
// @Produce		json
// @Param			request	body		UpdateWorkspaceSettingsRequest	true	"Campos a alterar"
// @Success		200	{object}	WorkspaceSettingsResponse
// @Failure		400	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/audience/workspace-settings [put]
func (h *Handler) UpdateWorkspaceSettings(w http.ResponseWriter, r *http.Request) {
	if h.workspaceSettings == nil {
		response.WriteError(w, http.StatusNotFound, "Analysis settings are not available", nil)
		return
	}
	var req UpdateWorkspaceSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}
	settings, err := h.workspaceSettings.Update(r.Context(), middleware.GetWorkspaceID(r), ca.UpdateWorkspaceSettingsInput{
		DailyCap:        req.DailyCap,
		DebounceMinutes: req.DebounceMinutes,
	})
	if err != nil {
		writeDomainError(w, err, "Failed to update the analysis settings")
		return
	}
	response.WriteSuccess(w, http.StatusOK, workspaceSettingsResponse(settings, h.effectiveDailyCap(r)))
}
