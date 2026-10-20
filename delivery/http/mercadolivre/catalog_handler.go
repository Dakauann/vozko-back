package mercadolivre

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/commentautomationhttp"
	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/conversation"
	mldomain "vozko/domain/mercadolivre"
	"vozko/domain/shared"
	"vozko/infra/http/middleware"
	mluc "vozko/usecases/mercadolivre"
)

type ListingsService interface {
	List(ctx context.Context, q mluc.ListingsQuery) (*mluc.ListingPage, error)
	ThumbnailURL(ctx context.Context, workspaceID, accountID, itemID string) (string, error)
}

type QuestionsService interface {
	List(ctx context.Context, q mluc.QuestionsQuery) (*mluc.QuestionPage, error)
	Get(ctx context.Context, workspaceID, accountID, questionID string) (*mluc.QuestionView, error)
	Answer(ctx context.Context, in mluc.AnswerInput) (*mldomain.Question, error)
	Delete(ctx context.Context, workspaceID, accountID, questionID string) error
	BlockAsker(ctx context.Context, workspaceID, accountID, questionID string) error
	UnblockAsker(ctx context.Context, workspaceID, accountID, questionID string) error
	ResponseTime(ctx context.Context, workspaceID, accountID string) (*mldomain.ResponseTime, error)
}

type ReasonsService interface {
	Options(ctx context.Context, q mluc.ConversationQuery) ([]mldomain.ReasonOption, error)
	Send(ctx context.Context, in mluc.SendReasonInput) (*mluc.SendReasonOutput, error)
}

type PacksService interface {
	Summary(ctx context.Context, q mluc.ConversationQuery) (*mluc.PackView, error)
}

type CatalogDeps struct {
	Listings  ListingsService
	Questions QuestionsService
	Reasons   ReasonsService
	Packs     PacksService
	Rules     commentautomationhttp.RuleManager
}

type CatalogHandler struct {
	listings  ListingsService
	questions QuestionsService
	reasons   ReasonsService
	packs     PacksService
	rules     *commentautomationhttp.RulesHandler
}

func NewCatalogHandler(d CatalogDeps) *CatalogHandler {
	if d.Listings == nil || d.Questions == nil || d.Reasons == nil || d.Packs == nil || d.Rules == nil {
		panic("mercadolivre http: the catalog handler requires listings, questions, reasons, pack summaries and the comment rule manager")
	}
	return &CatalogHandler{
		listings: d.Listings, questions: d.Questions, reasons: d.Reasons, packs: d.Packs,
		rules: commentautomationhttp.NewRulesHandler(shared.EntryTypeMercadoLivre, d.Rules, writeCatalogError),
	}
}

func parseLimit(w http.ResponseWriter, raw string) (int, bool) {
	if strings.TrimSpace(raw) == "" {
		return 0, true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 {
		response.WriteErrorWithCode(w, http.StatusBadRequest, "invalid_limit", "limit must be a positive number", nil)
		return 0, false
	}
	return limit, true
}

// @Summary		Listar anúncios da conta do Mercado Livre
// @Description	Espelho dos anúncios do vendedor, atualizado a cada 6 horas e a cada notificação de anúncio. Paginação por cursor.
// @Tags			Mercado Livre
// @Produce		json
// @Param			id		path		string	true	"ID da conta"
// @Param			search	query		string	false	"Busca no título ou pelo código MLB"
// @Param			status	query		string	false	"Status do anúncio no Mercado Livre (active, paused, closed)"
// @Param			cursor	query		string	false	"Cursor devolvido em nextCursor"
// @Param			limit	query		int		false	"Itens por página (até 100)"
// @Success		200		{object}	ListingListResponse
// @Failure		400		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/mercadolivre/accounts/{id}/listings [get]
func (h *CatalogHandler) ListListings(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, ok := parseLimit(w, q.Get("limit"))
	if !ok {
		return
	}
	page, err := h.listings.List(r.Context(), mluc.ListingsQuery{
		WorkspaceID: middleware.GetWorkspaceID(r), AccountID: mux.Vars(r)["id"],
		Search: strings.TrimSpace(q.Get("search")), Status: strings.TrimSpace(q.Get("status")), Cursor: q.Get("cursor"), Limit: limit,
	})
	if err != nil {
		writeCatalogError(w, err, "Failed to list Mercado Livre listings")
		return
	}
	out := ListingListResponse{Data: make([]ListingResponse, 0, len(page.Items)), NextCursor: page.NextCursor}
	for _, item := range page.Items {
		out.Data = append(out.Data, toListingResponse(item))
	}
	response.WriteSuccess(w, http.StatusOK, out)
}

// @Summary		Miniatura do anúncio
// @Description	Redireciona para a miniatura do anúncio guardada no armazenamento próprio.
// @Tags			Mercado Livre
// @Param			id		path	string	true	"ID da conta"
// @Param			itemId	path	string	true	"Código do anúncio (MLB...)"
// @Success		302
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/mercadolivre/accounts/{id}/listings/{itemId}/thumbnail [get]
func (h *CatalogHandler) ListingThumbnail(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	url, err := h.listings.ThumbnailURL(r.Context(), middleware.GetWorkspaceID(r), vars["id"], vars["itemId"])
	if err != nil {
		writeCatalogError(w, err, "Failed to load the listing thumbnail")
		return
	}
	http.Redirect(w, r, url, http.StatusFound)
}

func (h *CatalogHandler) listQuestions(w http.ResponseWriter, r *http.Request, itemID string) {
	q := r.URL.Query()
	limit, ok := parseLimit(w, q.Get("limit"))
	if !ok {
		return
	}
	query := mluc.QuestionsQuery{
		WorkspaceID: middleware.GetWorkspaceID(r), AccountID: mux.Vars(r)["id"], ItemID: itemID, Cursor: q.Get("cursor"), Limit: limit,
	}
	if raw := strings.TrimSpace(q.Get("status")); raw != "" {
		status, err := mldomain.ParseQuestionStatus(strings.ToUpper(raw))
		if err != nil {
			response.WriteErrorWithCode(w, http.StatusBadRequest, "invalid_status", err.Error(), nil)
			return
		}
		query.Status = &status
	}
	page, err := h.questions.List(r.Context(), query)
	if err != nil {
		writeCatalogError(w, err, "Failed to list Mercado Livre questions")
		return
	}
	out := QuestionListResponse{Data: make([]QuestionResponse, 0, len(page.Items)), NextCursor: page.NextCursor}
	for _, item := range page.Items {
		out.Data = append(out.Data, toQuestionView(item))
	}
	response.WriteSuccess(w, http.StatusOK, out)
}

// @Summary		Listar perguntas da conta do Mercado Livre
// @Description	Perguntas de todos os anúncios, as sem resposta primeiro e depois as mais recentes. Paginação por cursor.
// @Tags			Mercado Livre
// @Produce		json
// @Param			id		path		string	true	"ID da conta"
// @Param			status	query		string	false	"UNANSWERED, ANSWERED, CLOSED_UNANSWERED, UNDER_REVIEW, BANNED, DELETED ou DISABLED"
// @Param			itemId	query		string	false	"Código do anúncio (MLB...)"
// @Param			cursor	query		string	false	"Cursor devolvido em nextCursor"
// @Param			limit	query		int		false	"Itens por página (até 100)"
// @Success		200		{object}	QuestionListResponse
// @Failure		400		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/mercadolivre/accounts/{id}/questions [get]
func (h *CatalogHandler) ListQuestions(w http.ResponseWriter, r *http.Request) {
	h.listQuestions(w, r, strings.TrimSpace(r.URL.Query().Get("itemId")))
}

// @Summary		Listar perguntas de um anúncio
// @Tags			Mercado Livre
// @Produce		json
// @Param			id		path		string	true	"ID da conta"
// @Param			itemId	path		string	true	"Código do anúncio (MLB...)"
// @Param			status	query		string	false	"Status da pergunta"
// @Param			cursor	query		string	false	"Cursor devolvido em nextCursor"
// @Param			limit	query		int		false	"Itens por página (até 100)"
// @Success		200		{object}	QuestionListResponse
// @Failure		400		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/mercadolivre/accounts/{id}/listings/{itemId}/questions [get]
func (h *CatalogHandler) ListListingQuestions(w http.ResponseWriter, r *http.Request) {
	h.listQuestions(w, r, mux.Vars(r)["itemId"])
}

// @Summary		Ler uma pergunta
// @Description	A pergunta como está no espelho da plataforma, com quem perguntou e o anúncio, para atualizar uma linha (por exemplo depois de um 409). Não consulta o Mercado Livre. asker.blocked diz se quem perguntou está na lista de bloqueio de perguntas da conta: bloqueios feitos aqui aparecem na hora e os feitos no Mercado Livre aparecem na verificação diária da conta.
// @Tags			Mercado Livre
// @Produce		json
// @Param			id			path		string	true	"ID da conta"
// @Param			questionId	path		string	true	"ID da pergunta no Mercado Livre"
// @Success		200			{object}	QuestionResponse
// @Failure		404			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/mercadolivre/accounts/{id}/questions/{questionId} [get]
func (h *CatalogHandler) GetQuestion(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	view, err := h.questions.Get(r.Context(), middleware.GetWorkspaceID(r), vars["id"], vars["questionId"])
	if err != nil {
		writeCatalogError(w, err, "Failed to load the question")
		return
	}
	response.WriteSuccess(w, http.StatusOK, toQuestionView(*view))
}

// @Summary		Responder uma pergunta
// @Description	A resposta é pública e definitiva no Mercado Livre. A pergunta é relida antes; se já não estiver sem resposta, nada é enviado (409).
// @Tags			Mercado Livre
// @Accept			json
// @Produce		json
// @Param			id			path		string					true	"ID da conta"
// @Param			questionId	path		string					true	"ID da pergunta no Mercado Livre"
// @Param			body		body		AnswerQuestionRequest	true	"Texto da resposta (até 2.000 caracteres)"
// @Success		200			{object}	QuestionResponse
// @Failure		400			{object}	response.ErrorResponse
// @Failure		401			{object}	response.ErrorResponse
// @Failure		404			{object}	response.ErrorResponse
// @Failure		409			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/mercadolivre/accounts/{id}/questions/{questionId}/answer [post]
func (h *CatalogHandler) AnswerQuestion(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpx.WorkspaceActor(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	var req AnswerQuestionRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxRequestBody)).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, nil)
		return
	}
	vars := mux.Vars(r)
	userID := actor.UserID
	question, err := h.questions.Answer(r.Context(), mluc.AnswerInput{
		WorkspaceID: actor.WorkspaceID, AccountID: vars["id"], QuestionID: vars["questionId"], Text: req.Text,
		Source: mldomain.AnsweredByOperator, UserID: &userID,
	})
	if err != nil {
		writeCatalogError(w, err, "Failed to answer the question")
		return
	}
	response.WriteSuccess(w, http.StatusOK, toQuestionResponse(question))
}

// @Summary		Apagar uma pergunta
// @Tags			Mercado Livre
// @Produce		json
// @Param			id			path		string	true	"ID da conta"
// @Param			questionId	path		string	true	"ID da pergunta no Mercado Livre"
// @Success		200			{object}	QuestionActionResponse
// @Failure		404			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/mercadolivre/accounts/{id}/questions/{questionId} [delete]
func (h *CatalogHandler) DeleteQuestion(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	if err := h.questions.Delete(r.Context(), middleware.GetWorkspaceID(r), vars["id"], vars["questionId"]); err != nil {
		writeCatalogError(w, err, "Failed to delete the question")
		return
	}
	response.WriteSuccess(w, http.StatusOK, QuestionActionResponse{Status: "deleted"})
}

// @Summary		Bloquear perguntas do autor
// @Description	Coloca quem perguntou na lista de bloqueio de perguntas do vendedor.
// @Tags			Mercado Livre
// @Produce		json
// @Param			id			path		string	true	"ID da conta"
// @Param			questionId	path		string	true	"ID da pergunta no Mercado Livre"
// @Success		200			{object}	QuestionActionResponse
// @Failure		404			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/mercadolivre/accounts/{id}/questions/{questionId}/block-author [post]
func (h *CatalogHandler) BlockAuthor(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	if err := h.questions.BlockAsker(r.Context(), middleware.GetWorkspaceID(r), vars["id"], vars["questionId"]); err != nil {
		writeCatalogError(w, err, "Failed to block the author")
		return
	}
	response.WriteSuccess(w, http.StatusOK, QuestionActionResponse{Status: "blocked"})
}

// @Summary		Desbloquear perguntas do autor
// @Tags			Mercado Livre
// @Produce		json
// @Param			id			path		string	true	"ID da conta"
// @Param			questionId	path		string	true	"ID da pergunta no Mercado Livre"
// @Success		200			{object}	QuestionActionResponse
// @Failure		404			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/mercadolivre/accounts/{id}/questions/{questionId}/block-author [delete]
func (h *CatalogHandler) UnblockAuthor(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	if err := h.questions.UnblockAsker(r.Context(), middleware.GetWorkspaceID(r), vars["id"], vars["questionId"]); err != nil {
		writeCatalogError(w, err, "Failed to unblock the author")
		return
	}
	response.WriteSuccess(w, http.StatusOK, QuestionActionResponse{Status: "unblocked"})
}

// @Summary		Tempo de resposta às perguntas
// @Description	Tempo de resposta medido pelo Mercado Livre nos últimos 14 dias, guardado por 1 hora. Sem perguntas no período, available é false.
// @Tags			Mercado Livre
// @Produce		json
// @Param			id	path		string	true	"ID da conta"
// @Success		200	{object}	ResponseTimeResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/mercadolivre/accounts/{id}/questions/response-time [get]
func (h *CatalogHandler) ResponseTime(w http.ResponseWriter, r *http.Request) {
	rt, err := h.questions.ResponseTime(r.Context(), middleware.GetWorkspaceID(r), mux.Vars(r)["id"])
	if err != nil {
		writeCatalogError(w, err, "Failed to load the response time")
		return
	}
	response.WriteSuccess(w, http.StatusOK, toResponseTime(rt))
}

// @Summary		Listar regras de perguntas
// @Tags			Mercado Livre
// @Produce		json
// @Param			id	path	string	true	"ID da conta"
// @Success		200	{array}	commentautomationhttp.RuleResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/mercadolivre/accounts/{id}/comment-rules [get]
func (h *CatalogHandler) ListCommentRules(w http.ResponseWriter, r *http.Request) {
	h.rules.List(w, r, middleware.GetWorkspaceID(r), mux.Vars(r)["id"])
}

// @Summary		Criar regra de perguntas
// @Description	Ações aceitas no Mercado Livre: public_reply (a resposta, até 2.000 caracteres) e delete. containerId é o código do anúncio; vazio vale para todos.
// @Tags			Mercado Livre
// @Accept			json
// @Produce		json
// @Param			id		path		string								true	"ID da conta"
// @Param			body	body		commentautomationhttp.RuleRequest	true	"Regra"
// @Success		201		{object}	commentautomationhttp.RuleResponse
// @Failure		400		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/mercadolivre/accounts/{id}/comment-rules [post]
func (h *CatalogHandler) CreateCommentRule(w http.ResponseWriter, r *http.Request) {
	h.rules.Create(w, r, middleware.GetWorkspaceID(r), mux.Vars(r)["id"])
}

// @Summary		Atualizar regra de perguntas
// @Description	Recebe a regra inteira; cada campo substitui o guardado.
// @Tags			Mercado Livre
// @Accept			json
// @Produce		json
// @Param			id		path		string								true	"ID da conta"
// @Param			ruleId	path		string								true	"ID da regra"
// @Param			body	body		commentautomationhttp.RuleRequest	true	"Regra"
// @Success		200		{object}	commentautomationhttp.RuleResponse
// @Failure		400		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/mercadolivre/accounts/{id}/comment-rules/{ruleId} [patch]
func (h *CatalogHandler) UpdateCommentRule(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	h.rules.Update(w, r, middleware.GetWorkspaceID(r), vars["id"], vars["ruleId"])
}

// @Summary		Remover regra de perguntas
// @Tags			Mercado Livre
// @Produce		json
// @Param			id		path		string	true	"ID da conta"
// @Param			ruleId	path		string	true	"ID da regra"
// @Success		200		{object}	commentautomationhttp.RuleDeletedResponse
// @Failure		404		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/mercadolivre/accounts/{id}/comment-rules/{ruleId} [delete]
func (h *CatalogHandler) DeleteCommentRule(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	h.rules.Delete(w, r, middleware.GetWorkspaceID(r), vars["id"], vars["ruleId"])
}

func conversationQuery(r *http.Request) (mluc.ConversationQuery, bool) {
	actor, ok := httpx.WorkspaceActor(r)
	if !ok {
		return mluc.ConversationQuery{}, false
	}
	return mluc.ConversationQuery{
		Actor: conversation.SentByPerson(actor.UserID), SystemAdmin: actor.IsAdmin,
		WorkspaceID: actor.WorkspaceID, EntryID: mux.Vars(r)["entryId"],
	}, true
}

// @Summary		Motivos para iniciar a conversa
// @Description	Opções que o Mercado Livre oferece para o vendedor escrever primeiro nesta venda, com o saldo de usos de cada uma. Ação só de pessoas. description é o internal_description do Mercado Livre e templates[].text é o texto do modelo no site da conta, com os marcadores (%s, %d) como vieram; ambos são omitidos quando o Mercado Livre não os envia. As variáveis não têm rótulo no Mercado Livre.
// @Tags			Mercado Livre
// @Produce		json
// @Param			entryId	path		string	true	"ID da conversa"
// @Success		200		{object}	ReasonListResponse
// @Failure		401		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/mercadolivre/conversations/{entryId}/reasons [get]
func (h *CatalogHandler) ListReasons(w http.ResponseWriter, r *http.Request) {
	q, ok := conversationQuery(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	options, err := h.reasons.Options(r.Context(), q)
	if err != nil {
		writeCatalogError(w, err, "Failed to load the reasons")
		return
	}
	out := ReasonListResponse{Data: make([]ReasonOptionResponse, 0, len(options))}
	for _, o := range options {
		out.Data = append(out.Data, toReasonOption(o))
	}
	response.WriteSuccess(w, http.StatusOK, out)
}

// @Summary		Iniciar a conversa com um motivo
// @Description	Envia uma das opções oferecidas (modelo com variáveis, ou texto livre em OTHER). Ação manual de quem pode enviar mensagens; agentes e fluxos nunca usam motivos.
// @Tags			Mercado Livre
// @Accept			json
// @Produce		json
// @Param			entryId	path		string				true	"ID da conversa"
// @Param			body	body		SendReasonRequest	true	"Motivo escolhido"
// @Success		201		{object}	SendReasonResponse
// @Failure		400		{object}	response.ErrorResponse
// @Failure		401		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Failure		422		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/mercadolivre/conversations/{entryId}/reasons [post]
func (h *CatalogHandler) SendReason(w http.ResponseWriter, r *http.Request) {
	q, ok := conversationQuery(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	var req SendReasonRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxRequestBody)).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, nil)
		return
	}
	vars := make([]mldomain.ReasonVarValue, 0, len(req.Vars))
	for _, v := range req.Vars {
		vars = append(vars, mldomain.ReasonVarValue{ID: v.ID, Value: v.Value})
	}
	out, err := h.reasons.Send(r.Context(), mluc.SendReasonInput{
		ConversationQuery: q, OptionID: req.OptionID, TemplateID: req.TemplateID, Text: req.Text, Vars: vars,
	})
	if err != nil {
		writeCatalogError(w, err, "Failed to send the reason")
		return
	}
	response.WriteSuccess(w, http.StatusCreated, SendReasonResponse{ProviderMessageID: out.ProviderMessageID})
}

// @Summary		Resumo da venda da conversa
// @Description	Itens, situação do pedido e do envio e total da venda desta conversa, guardados na plataforma e atualizados pelas notificações de pedido e envio e no máximo a cada 24 horas. Um resumo mais antigo é devolvido como está e uma atualização é pedida em segundo plano. As miniaturas vêm do armazenamento próprio, nunca do Mercado Livre. Sem resumo ainda, responde 404 com o código pack_summary_pending.
// @Tags			Mercado Livre
// @Produce		json
// @Param			entryId	path		string	true	"ID da conversa"
// @Success		200		{object}	PackSummaryResponse
// @Failure		401		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/mercadolivre/conversations/{entryId}/pack [get]
func (h *CatalogHandler) PackSummary(w http.ResponseWriter, r *http.Request) {
	q, ok := conversationQuery(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	view, err := h.packs.Summary(r.Context(), q)
	if err != nil {
		writeCatalogError(w, err, "Failed to load the sale summary")
		return
	}
	response.WriteSuccess(w, http.StatusOK, toPackSummary(view))
}

func toPackSummary(v *mluc.PackView) PackSummaryResponse {
	out := PackSummaryResponse{
		Items:       make([]PackItemResponse, 0, len(v.Items)),
		OrderStatus: v.OrderStatus, ShippingStatus: v.ShippingStatus, ShippingSubstatus: v.ShippingSubstatus,
		Total: v.TotalAmount, Currency: v.CurrencyID, RefreshedAt: v.RefreshedAt,
	}
	for _, item := range v.Items {
		out.Items = append(out.Items, PackItemResponse{
			ItemID: item.ItemID, Title: item.Title, Quantity: item.Quantity, UnitPrice: item.UnitPrice,
			Currency: item.CurrencyID, ThumbnailURL: item.ThumbnailURL, Permalink: item.Permalink,
		})
	}
	return out
}

func writeCatalogError(w http.ResponseWriter, err error, fallback string) {
	switch {
	case errors.Is(err, mldomain.ErrPackSummaryPending):
		response.WriteErrorWithCode(w, http.StatusNotFound, "pack_summary_pending", "The sale summary is being loaded", nil)
	case errors.Is(err, mldomain.ErrQuestionNotOwned), errors.Is(err, mldomain.ErrQuestionNotFound),
		errors.Is(err, mldomain.ErrListingNotFound), errors.Is(err, mluc.ErrResourceGone):
		response.WriteError(w, http.StatusNotFound, "Not found", nil)
	case errors.Is(err, mldomain.ErrQuestionNotOpen):
		response.WriteErrorWithCode(w, http.StatusConflict, "question_not_open", "This question is no longer waiting for an answer", nil)
	case errors.Is(err, mldomain.ErrAnswerEmpty), errors.Is(err, mldomain.ErrAnswerTooLong):
		response.WriteErrorWithCode(w, http.StatusBadRequest, "answer_invalid", err.Error(), nil)
	case errors.Is(err, shared.ErrKeysetInvalid):
		response.WriteErrorWithCode(w, http.StatusBadRequest, "invalid_cursor", err.Error(), nil)
	case errors.Is(err, mldomain.ErrInvalidResource):
		response.WriteErrorWithCode(w, http.StatusBadRequest, "invalid_resource", err.Error(), nil)
	case errors.Is(err, mluc.ErrAccountUnavailable):
		response.WriteErrorWithCode(w, http.StatusConflict, "account_unavailable", "This Mercado Livre account cannot do this right now", nil)
	case errors.Is(err, mldomain.ErrPersonOnly), errors.Is(err, conversation.ErrUnauthorized):
		response.WriteErrorWithCode(w, http.StatusForbidden, "forbidden", "You cannot act on this conversation", nil)
	case errors.Is(err, mldomain.ErrReasonUnknown):
		response.WriteErrorWithCode(w, http.StatusUnprocessableEntity, "reason_unknown", err.Error(), nil)
	case errors.Is(err, mldomain.ErrReasonUnavailable):
		response.WriteErrorWithCode(w, http.StatusUnprocessableEntity, "reason_unavailable", err.Error(), nil)
	case errors.Is(err, mldomain.ErrReasonCapReached):
		response.WriteErrorWithCode(w, http.StatusUnprocessableEntity, "reason_cap_reached", err.Error(), nil)
	case errors.Is(err, mldomain.ErrReasonContent):
		response.WriteErrorWithCode(w, http.StatusUnprocessableEntity, "reason_content", err.Error(), nil)
	default:
		writeError(w, err, fallback)
	}
}

func toListingResponse(v mluc.ListingView) ListingResponse {
	l := v.Listing
	return ListingResponse{
		ID: l.ID, AccountID: l.AccountID, ItemID: l.ItemID, Title: l.Title, Permalink: l.Permalink, Status: l.Status,
		ThumbnailURL: v.ThumbnailURL, SyncedAt: l.SyncedAt, CreatedAt: l.CreatedAt, UpdatedAt: l.UpdatedAt,
	}
}

func toQuestionResponse(q *mldomain.Question) QuestionResponse {
	return QuestionResponse{
		ID: q.ID, AccountID: q.AccountID, ItemID: q.ItemID, QuestionID: q.QuestionID, Text: q.Text, Status: string(q.Status),
		AnswerText: q.AnswerText, AnswerStatus: string(q.AnswerStatus), AnsweredBy: string(q.AnsweredBy),
		AnsweredByUserID: q.AnsweredByUserID, RuleID: q.RuleID, AskedAt: q.AskedAt, AnsweredAt: q.AnsweredAt, SuspectedSpam: q.SuspectedSpam,
	}
}

func toQuestionView(v mluc.QuestionView) QuestionResponse {
	out := toQuestionResponse(v.Question)
	if v.Asker != nil {
		out.Asker = &QuestionAskerResponse{ContactID: v.Asker.ID, MLUserID: v.Asker.MLUserID, Nickname: v.Asker.Nickname, Blocked: v.Asker.QuestionsBlocked()}
	}
	if v.Listing != nil {
		listing := toListingResponse(*v.Listing)
		out.Listing = &listing
	}
	return out
}

func toResponseSlot(s *mldomain.ResponseTimeSlot) *ResponseTimeSlotResponse {
	if s == nil {
		return nil
	}
	return &ResponseTimeSlotResponse{Minutes: s.Minutes, SalesPercentIncrease: s.SalesPercentIncrease}
}

func toResponseTime(rt *mldomain.ResponseTime) ResponseTimeResponse {
	return ResponseTimeResponse{
		Available: rt.Available, Total: toResponseSlot(rt.Total), WeekdaysWorkingHours: toResponseSlot(rt.WeekdaysWorkingHours),
		WeekdaysExtraHours: toResponseSlot(rt.WeekdaysExtraHours), Weekend: toResponseSlot(rt.Weekend),
	}
}

func toReasonOption(o mldomain.ReasonOption) ReasonOptionResponse {
	out := ReasonOptionResponse{
		ID: o.ID, Description: o.Description, Type: o.Type, Enabled: o.Enabled, Actionable: o.Actionable, CharLimit: o.CharLimit, CapAvailable: o.CapAvailable,
		Templates: make([]ReasonTemplateResponse, 0, len(o.Templates)),
	}
	for _, t := range o.Templates {
		tpl := ReasonTemplateResponse{ID: t.ID, Text: t.Text, Vars: make([]ReasonVarResponse, 0, len(t.Vars))}
		for _, v := range t.Vars {
			tpl.Vars = append(tpl.Vars, ReasonVarResponse{ID: v.ID, Type: v.Type})
		}
		out.Templates = append(out.Templates, tpl)
	}
	return out
}
