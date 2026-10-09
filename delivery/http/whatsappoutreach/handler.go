package whatsappoutreach

import (
	"encoding/json"
	"net/http"

	"vozko/delivery/http/response"
	"vozko/domain/conversation"
	user_domain "vozko/domain/user"
	wo "vozko/domain/whatsapp_outreach"
	"vozko/infra/http/middleware"
)

const idempotencyHeader = "Idempotency-Key"

type DepartmentScopeResolver interface {
	GetDepartmentScope(userID, workspaceID string, isAdmin bool) (conversation.DepartmentAccessScope, bool)
}

type Handler struct {
	start       wo.StartOfficialConversationUseCase
	quote       wo.QuoteTemplateSendUseCase
	departments DepartmentScopeResolver
}

type HandlerDeps struct {
	Start       wo.StartOfficialConversationUseCase
	Quote       wo.QuoteTemplateSendUseCase
	Departments DepartmentScopeResolver
}

func NewHandler(d HandlerDeps) *Handler {
	return &Handler{start: d.Start, quote: d.Quote, departments: d.Departments}
}

func (h *Handler) scope(w http.ResponseWriter, r *http.Request) (workspaceID string, departmentIDs []string, isAdmin bool, ok bool) {
	workspaceID = middleware.GetWorkspaceID(r)
	if workspaceID == "" {
		response.WriteErrorWithCode(w, http.StatusForbidden, "workspace_required", "workspace is required", nil)
		return "", nil, false, false
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteErrorWithCode(w, http.StatusForbidden, "forbidden", "you may not send from this workspace", nil)
		return "", nil, false, false
	}
	isAdmin = claims.Role == string(user_domain.RoleAdmin)

	if h.departments == nil {
		response.WriteErrorWithCode(w, http.StatusInternalServerError, "scope_unavailable",
			"department scope is unavailable, refusing to send", nil)
		return "", nil, false, false
	}

	resolved, allowed := h.departments.GetDepartmentScope(claims.UserID, workspaceID, isAdmin)
	if !allowed {
		response.WriteErrorWithCode(w, http.StatusForbidden, "forbidden",
			"you do not have access to this workspace's conversations", nil)
		return "", nil, false, false
	}
	if resolved.Restrict {
		departmentIDs = resolved.DepartmentIDs
	}
	return workspaceID, departmentIDs, isAdmin, true
}

// @Summary		Iniciar conversa no WhatsApp oficial enviando um modelo
// @Description	Envia um modelo aprovado para um número que nunca escreveu para a empresa e abre a conversa no CRM. Consome saldo. Envie o cabeçalho `Idempotency-Key` para que um reenvio da requisição não cobre nem envie duas vezes. Recusas antes de qualquer cobrança: 403 `lead_blocked` (contato bloqueado); 409 `lead_opted_out` (o contato pediu para não receber mensagens); 409 `within_spam_window` (o contato recebeu mensagem deste número dentro da proteção contra spam do workspace, ou outro envio para ele a partir deste número está em andamento); 409 `window_already_open`. Depois do envio: 409 `send_outcome_unknown` quando a resposta do WhatsApp se perdeu e o modelo pode ter sido entregue e cobrado (confira a conversa antes de enviar de novo; não repita automaticamente).
// @Tags			whatsapp-outreach
// @Accept			json
// @Produce		json
// @Param			Idempotency-Key	header		string							true	"Chave que torna o envio idempotente"
// @Param			request			body		StartConversationRequest		true	"Destinatário e modelo"
// @Success		201				{object}	StartedConversationResponse
// @Success		200				{object}	StartedConversationResponse	"Envio idêntico já realizado; nada foi cobrado novamente"
// @Failure		400				{object}	response.ErrorResponse
// @Failure		402				{object}	response.ErrorResponse
// @Failure		403				{object}	response.ErrorResponse
// @Failure		404				{object}	response.ErrorResponse
// @Failure		409				{object}	WindowOpenResponse
// @Failure		422				{object}	response.ErrorResponse
// @Failure		429				{object}	response.ErrorResponse
// @Failure		502				{object}	response.ErrorResponse
// @Failure		503				{object}	response.ErrorResponse
// @Router			/whatsapp/outreach/conversations [post]
func (h *Handler) StartConversation(w http.ResponseWriter, r *http.Request) {
	workspaceID, departmentIDs, isAdmin, ok := h.scope(w, r)
	if !ok {
		return
	}

	idempotencyKey := trimHeader(r, idempotencyHeader)
	if idempotencyKey == "" {
		response.WriteErrorWithCode(w, http.StatusBadRequest, "idempotency_key_required",
			"envie o cabeçalho Idempotency-Key para evitar cobranças duplicadas", nil)
		return
	}

	var req StartConversationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"businessPhoneId": "número comercial de origem",
			"templateId":      "modelo aprovado a enviar",
			"phoneNumber":     "número de destino",
		})
		return
	}

	claims := middleware.GetClaims(r)
	userID := ""
	if claims != nil {
		userID = claims.UserID
	}

	result, err := h.start.Execute(r.Context(), wo.StartConversationInput{
		WorkspaceID:     workspaceID,
		UserID:          userID,
		IsAdmin:         isAdmin,
		BusinessPhoneID: req.BusinessPhoneID,
		TemplateID:      req.TemplateID,
		PhoneNumber:     req.PhoneNumber,
		Name:            req.Name,
		BodyParams:      req.Parameters,
		HeaderParams:    req.HeaderParameters,
		IdempotencyKey:  idempotencyKey,
		DepartmentIDs:   departmentIDs,
	})
	if err != nil {
		h.writeDomainError(w, err, result)
		return
	}

	status := http.StatusCreated
	if result.Replayed {
		status = http.StatusOK
	}
	response.WriteSuccess(w, status, toStartedResponse(result))
}

// @Summary		Consultar o custo de um envio de modelo
// @Description	Retorna o preço do modelo para o workspace e se o saldo atual cobre o envio. Só modelos liberados para o workspace são cotados: um modelo de outro workspace responde 403 (`forbidden`), um modelo inexistente responde 404 (`not_found`),, um custo fora da faixa calculável responde 422 (`quote_out_of_range`), e uma falha ao ler as liberações ou o preço responde 503 (`quote_unavailable`), sem nada enviado.
// @Tags			whatsapp-outreach
// @Produce		json
// @Param			templateId		query		string	true	"ID do modelo"
// @Param			businessPhoneId	query		string	false	"ID do número comercial"
// @Success		200				{object}	SendQuoteResponse
// @Failure		403				{object}	response.ErrorResponse
// @Failure		404				{object}	response.ErrorResponse
// @Failure		422				{object}	response.ErrorResponse
// @Router			/whatsapp/outreach/quote [get]
func (h *Handler) Quote(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, _, ok := h.scope(w, r)
	if !ok {
		return
	}
	if h.quote == nil {
		response.WriteErrorWithCode(w, http.StatusInternalServerError, "quote_unavailable", "pricing is unavailable", nil)
		return
	}

	quote, err := h.quote.Execute(r.Context(), workspaceID,
		r.URL.Query().Get("templateId"), r.URL.Query().Get("businessPhoneId"))
	if err != nil && wo.ErrorCode(err) == "" {
		response.WriteErrorWithCode(w, http.StatusServiceUnavailable, "quote_unavailable", "não foi possível calcular o custo agora, tente novamente em instantes", nil)
		return
	}
	if err != nil {
		h.writeDomainError(w, err, nil)
		return
	}
	response.WriteSuccess(w, http.StatusOK, SendQuoteResponse{
		Category:      quote.Category,
		PriceMicros:   quote.PriceMicros,
		BalanceMicros: quote.BalanceMicros,
		Affordable:    quote.Affordable,
	})
}

func toStartedResponse(r *wo.StartedConversation) StartedConversationResponse {
	return StartedConversationResponse{
		EntryID:             r.EntryID,
		EntryType:           r.EntryType,
		LeadID:              r.LeadID,
		AttemptID:           r.AttemptID,
		MessageID:           r.MessageID,
		ConversationExisted: r.ConversationExisted,
		Replayed:            r.Replayed,
		ChargedMicros:       r.ChargedMicros,
		Recorded:            r.Recorded,
	}
}

type refusalReply struct {
	status  int
	message string
}

var refusalReplies = map[string]refusalReply{
	wo.CodeSendInProgress:         {http.StatusConflict, "este envio já está em andamento"},
	wo.CodeWithinSpamWindow:       {http.StatusConflict, "este contato já recebeu uma mensagem deste número recentemente"},
	wo.CodeLeadOptedOut:           {http.StatusConflict, "este contato pediu para não receber mensagens"},
	wo.CodeInsufficientBalance:    {http.StatusPaymentRequired, "saldo insuficiente para enviar este modelo"},
	wo.CodeMonthlySendCapReached:  {http.StatusForbidden, "o limite mensal de envios deste workspace foi atingido, fale com a administração"},
	wo.CodeSendWindowClosed:       {http.StatusForbidden, "fora da janela de envios deste workspace, fale com a administração"},
	wo.CodePricingUnavailable:     {http.StatusUnprocessableEntity, "não há preço configurado para esta categoria de modelo"},
	wo.CodeQuoteOutOfRange:        {http.StatusUnprocessableEntity, "não foi possível calcular o custo deste envio"},
	wo.CodeTemplateNotSendable:    {http.StatusUnprocessableEntity, "este modelo não está pronto para envio"},
	wo.CodeTemplatePhoneMismatch:  {http.StatusUnprocessableEntity, "este modelo não pertence à conta do número selecionado"},
	wo.CodeInvalidPhone:           {http.StatusBadRequest, "número de destino inválido"},
	wo.CodeLeadBlocked:            {http.StatusForbidden, "este contato está bloqueado"},
	wo.CodeForbidden:              {http.StatusForbidden, "você não tem acesso a este número ou modelo"},
	wo.CodePhoneNotConnected:      {http.StatusUnprocessableEntity, "este número não está conectado"},
	wo.CodeNotFound:               {http.StatusNotFound, "número ou modelo não encontrado"},
	wo.CodeIdempotencyKeyRequired: {http.StatusBadRequest, "envie o cabeçalho Idempotency-Key"},
	wo.CodeBillingUnavailable:     {http.StatusInternalServerError, "cobrança indisponível, envio recusado"},
	wo.CodeSendOutcomeUnknown:     {http.StatusConflict, "o WhatsApp pode ter entregue o modelo, confira a conversa antes de enviar de novo"},
	wo.CodeConversationNotFound:   {http.StatusNotFound, "conversa não encontrada"},
}

func (h *Handler) writeDomainError(w http.ResponseWriter, err error, result *wo.StartedConversation) {
	code := wo.ErrorCode(err)
	if code == wo.CodeWindowAlreadyOpen {
		payload := WindowOpenResponse{
			Error:   true,
			Code:    code,
			Message: "esta conversa já está aberta, responda pelo chat sem custo",
		}
		if result != nil {
			payload.EntryID = result.EntryID
			payload.EntryType = result.EntryType
		}
		writeJSON(w, http.StatusConflict, payload)
		return
	}
	reply, known := refusalReplies[code]
	if !known {
		response.WriteErrorWithCode(w, http.StatusBadGateway, "send_failed", "não foi possível enviar o modelo", nil)
		return
	}
	response.WriteErrorWithCode(w, reply.status, code, reply.message, nil)
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func trimHeader(r *http.Request, name string) string {
	value := r.Header.Get(name)
	for len(value) > 0 && (value[0] == ' ' || value[0] == '\t') {
		value = value[1:]
	}
	for len(value) > 0 && (value[len(value)-1] == ' ' || value[len(value)-1] == '\t') {
		value = value[:len(value)-1]
	}
	return value
}
