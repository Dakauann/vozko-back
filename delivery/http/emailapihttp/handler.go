package emailapihttp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"

	"vozko/delivery/http/apikeys"
	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/emailaddress"
	"vozko/domain/emailapi"
	emailapi_usecase "vozko/usecases/emailapi"
	tx "vozko/usecases/transactionalemail"
)

const maxSendBody = 24 << 20

type Sender interface {
	Send(ctx context.Context, req emailapi.Request) (emailapi_usecase.Result, error)
}

type Log interface {
	List(ctx context.Context, q emailapi.LogQuery) (emailapi.Page[emailapi.LogEntry], error)
	Detail(ctx context.Context, workspaceID, id string) (emailapi.Detail, error)
}

type Handler struct {
	sender   Sender
	log      Log
	webhooks Webhooks
}

func NewHandler(sender Sender, log Log, webhooks Webhooks) *Handler {
	return &Handler{sender: sender, log: log, webhooks: webhooks}
}

func principalWorkspace(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	p, ok := apikeys.PrincipalOf(r)
	if !ok || p.WorkspaceID == "" {
		response.WriteErrorWithCode(w, http.StatusUnauthorized, apikeys.CodeMissing, "Envie a chave de API no cabeçalho Authorization: Bearer <chave>.", nil)
		return "", "", false
	}
	return p.WorkspaceID, p.KeyID, true
}

func decodeSend(w http.ResponseWriter, r *http.Request) (SendRequest, bool) {
	var req SendRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxSendBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			response.WriteRefusal(w, sendByCode[emailapi_usecase.CodeAttachmentTooLarge])
			return SendRequest{}, false
		}
		response.WriteErrorWithCode(w, http.StatusBadRequest, httpx.CodeInvalidBody, "O corpo da requisição não é um JSON válido para este envio: "+err.Error(), nil)
		return SendRequest{}, false
	}
	if strings.TrimSpace(req.IdempotencyKey) == "" {
		req.IdempotencyKey = r.Header.Get("Idempotency-Key")
	}
	return req, true
}

func (req SendRequest) domain(workspaceID, keyID string) (emailapi.Request, error) {
	attachments := make([]emailapi.AttachmentSource, len(req.Attachments))
	for i, a := range req.Attachments {
		src := emailapi.AttachmentSource{FileName: a.Filename, ContentType: a.ContentType, URL: a.Path}
		if a.Content != "" {
			data, err := base64.StdEncoding.DecodeString(a.Content)
			if err != nil {
				return emailapi.Request{}, tx.Refuse(emailapi_usecase.CodeAttachmentInvalid, emailapi.ErrAttachmentInvalid)
			}
			src.Data = data
		}
		attachments[i] = src
	}
	tags := make([]emailapi.Tag, len(req.Tags))
	for i, t := range req.Tags {
		tags[i] = emailapi.Tag{Name: t.Name, Value: t.Value}
	}
	return emailapi.Request{
		WorkspaceID: workspaceID, Origin: emailapi.OriginAPI, APIKeyID: keyID, From: req.From, To: req.To, Cc: req.Cc, Bcc: req.Bcc,
		ReplyTo: req.ReplyTo, Headers: req.Headers, Tags: tags, Attachments: attachments, IdempotencyKey: req.IdempotencyKey,
		Content: emailapi.Content{Subject: req.Subject, HTML: req.HTML, Text: req.Text, DesignID: req.DesignID, Variables: req.Variables},
	}, nil
}

// @Summary		Enviar e-mail transacional
// @Description	Envia um e-mail transacional (redefinição de senha, recibo, aviso) pelo domínio verificado do workspace. Autenticação por chave de API com a permissão `email:send` (`Authorization: Bearer wsk_...`). Nada é registrado no inbox e nenhum lead ou contato é criado; cada destinatário aparece no registro de envios e nos webhooks.
// @Description
// @Description	**Destinatários.** `to` (ao menos um), `cc` e `bcc`, até 50 no total; endereços repetidos contam uma vez. Cada destinatário recebe a própria cópia, endereçada a ele, e é cobrado como um e-mail (mesmo preço de `GET /email/pricing`, mais os blocos de 256 KB dos anexos). Nenhum destinatário vê os outros.
// @Description
// @Description	**Conteúdo.** `subject` mais `html` e/ou `text`, ou `design_id` (um modelo salvo) com `variables` (usadas como `{{field.chave}}` no modelo; `{{email}}` vira o destinatário). `from` é o id ou o endereço de um remetente verificado do workspace. `reply_to` (até 5) vira o Reply-To; sem ele, as respostas vão para o endereço de `from`. `headers` aceita `X-*` (exceto `X-SES-*`), `List-Unsubscribe`, `List-Unsubscribe-Post`, `List-Id`, `Auto-Submitted`, `Precedence` e `Entity-Ref-ID`, até 20. `tags` (até 10, `name` e `value` com letras, números, `_` e `-`) servem de filtro no registro e voltam nos webhooks. `attachments`: `filename` mais `content` em base64 ou `path` https, até 16 MB no total; executáveis e scripts são recusados.
// @Description
// @Description	**Idempotência.** `idempotency_key` é obrigatória (ou no cabeçalho `Idempotency-Key`). A mesma chave com a mesma requisição devolve o mesmo resultado (200, `repeated: true`) sem enviar nem cobrar de novo; destinatários que não saíram são tentados outra vez. A mesma chave com outra requisição responde 409 `email_idempotency_key_reused`. Uma chamada simultânea com a mesma chave responde 409 `email_send_in_progress`.
// @Description
// @Description	**Resposta.** 201 quando ao menos um destinatário saiu, com o resultado de cada um (`status` `sent`, ou `refused` com `error_code`). Quando nenhum saiu, a recusa do primeiro destinatário. Antes de cobrar: 400 `email_idempotency_key_required`, `email_idempotency_key_invalid`, `email_from_required`, `email_recipients_invalid`, `email_address_invalid`, `email_reply_to_invalid`, `email_tags_invalid`, `invalid_body`; 404 `email_design_not_found`; 409 `email_idempotency_key_reused`, `email_send_in_progress`, `email_sender_not_verified`, `email_recipient_suppressed`; 413 `email_attachment_too_large`; 422 `email_sender_unknown`, `email_header_not_allowed`, `email_message_invalid`, `email_subject_invalid`, `email_attachment_invalid`, `email_attachment_blocked`, `email_attachment_unavailable`; 402 `email_balance_insufficient` (`details.missing_micros`); 503 `email_charge_failed`. Depois de cobrar, com reembolso de quem não saiu: 409 `email_sender_missing`, `email_account_paused`; 429 `email_daily_cap_reached`; 429 `email_send_throttled` (cabeçalho `Retry-After` e `details.retry_after_seconds`; repita com a mesma chave); 503 `email_account_reactivation_failed`; 502 `email_send_failed`. Autenticação: 401 `api_key_missing`, `api_key_invalid`, `api_key_revoked`; 403 `api_key_scope_missing`; 429 `api_key_rate_limited`; 503 `api_key_unavailable`.
// @Tags			API de e-mail
// @Accept			json
// @Produce		json
// @Param			request	body		SendRequest	true	"Remetente, destinatários, conteúdo e chave de idempotência"
// @Success		201		{object}	SendResponse
// @Success		200		{object}	SendResponse	"Mesma idempotency_key; nada foi enviado nem cobrado de novo"
// @Failure		400		{object}	response.ErrorResponse
// @Failure		401		{object}	response.ErrorResponse
// @Failure		402		{object}	response.ErrorResponse	"email_balance_insufficient (details.missing_micros)"
// @Failure		403		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Failure		409		{object}	response.ErrorResponse
// @Failure		413		{object}	response.ErrorResponse
// @Failure		422		{object}	response.ErrorResponse
// @Failure		429		{object}	response.ErrorResponse	"email_send_throttled, email_daily_cap_reached ou api_key_rate_limited"
// @Header			429		{integer}	Retry-After	"Segundos até tentar de novo"
// @Failure		502		{object}	response.ErrorResponse
// @Failure		503		{object}	response.ErrorResponse
// @Security		WorkspaceKey
// @Router			/email/transactional/messages [post]
func (h *Handler) Send(w http.ResponseWriter, r *http.Request) {
	workspaceID, keyID, ok := principalWorkspace(w, r)
	if !ok {
		return
	}
	body, ok := decodeSend(w, r)
	if !ok {
		return
	}
	req, err := body.domain(workspaceID, keyID)
	if err != nil {
		writeSendRefusal(w, workspaceID, err)
		return
	}
	result, err := h.sender.Send(r.Context(), req)
	if err != nil {
		writeSendRefusal(w, workspaceID, err)
		return
	}
	status := http.StatusCreated
	if result.Repeated {
		status = http.StatusOK
	}
	if result.RetryAfterSeconds > 0 {
		w.Header().Set("Retry-After", strconv.FormatInt(result.RetryAfterSeconds, 10))
	}
	response.WriteSuccess(w, status, sendResponse(result))
}

func logQuery(r *http.Request, workspaceID string) (emailapi.LogQuery, error) {
	q := r.URL.Query()
	out := emailapi.LogQuery{
		WorkspaceID: workspaceID,
		Status:      emailapi.MessageStatus(q.Get("status")),
		Recipient:   emailaddress.Address(q.Get("recipient")),
		TagName:     q.Get("tag_name"),
		TagValue:    q.Get("tag_value"),
	}
	cursor, err := emailapi.ParseCursor(q.Get("cursor"))
	if err != nil {
		return emailapi.LogQuery{}, tx.Refuse(emailapi_usecase.CodeLogCursorInvalid, err)
	}
	out.Cursor = cursor
	for param, target := range map[string]*time.Time{"after": &out.From, "before": &out.To} {
		if raw := q.Get(param); raw != "" {
			parsed, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				return emailapi.LogQuery{}, tx.Refuse(emailapi_usecase.CodeLogFilterInvalid, emailapi.ErrFilterInvalid)
			}
			*target = parsed
		}
	}
	if raw := q.Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil {
			return emailapi.LogQuery{}, tx.Refuse(emailapi_usecase.CodeLogFilterInvalid, emailapi.ErrFilterInvalid)
		}
		out.Limit = limit
	}
	return out, nil
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request, workspaceID string) {
	q, err := logQuery(r, workspaceID)
	if err != nil {
		writeLogRefusal(w, workspaceID, err)
		return
	}
	page, err := h.log.List(r.Context(), q)
	if err != nil {
		writeLogRefusal(w, workspaceID, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, logPage(page))
}

func (h *Handler) detail(w http.ResponseWriter, r *http.Request, workspaceID string) {
	id := mux.Vars(r)["id"]
	if _, err := uuid.Parse(id); err != nil {
		writeLogRefusal(w, workspaceID, tx.Refuse(emailapi_usecase.CodeMessageNotFound, emailapi.ErrMessageNotFound))
		return
	}
	detail, err := h.log.Detail(r.Context(), workspaceID, id)
	if err != nil {
		writeLogRefusal(w, workspaceID, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, logDetail(detail))
}

// @Summary		Listar os e-mails enviados pela API
// @Description	Registro de envios, um item por destinatário, do mais novo para o mais antigo, paginado por cursor (`next_cursor` vai em `cursor` da próxima página; não há contagem total). Filtros: `status` (`pending`, `refused`, `queued`, `sent`, `delivered`, `bounced`, `suppressed`, `failed`), `recipient` (endereço exato), `tag_name` com `tag_value`, `after` e `before` (RFC 3339). `limit` de 1 a 100, padrão 50. Chave de API com a permissão `email:read`. Recusas: 400 `email_log_cursor_invalid`, `email_log_filter_invalid`.
// @Tags			API de e-mail
// @Produce		json
// @Param			status		query		string	false	"Status do e-mail"
// @Param			recipient	query		string	false	"Endereço do destinatário"
// @Param			tag_name	query		string	false	"Nome da tag"
// @Param			tag_value	query		string	false	"Valor da tag"
// @Param			after		query		string	false	"Criados a partir deste instante (RFC 3339)"
// @Param			before		query		string	false	"Criados antes deste instante (RFC 3339)"
// @Param			cursor		query		string	false	"Cursor da página seguinte"
// @Param			limit		query		int		false	"Itens por página, 1 a 100"
// @Success		200			{object}	LogPageResponse
// @Failure		400			{object}	response.ErrorResponse
// @Failure		401			{object}	response.ErrorResponse
// @Failure		403			{object}	response.ErrorResponse
// @Security		WorkspaceKey
// @Router			/email/transactional/messages [get]
func (h *Handler) APIList(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, ok := principalWorkspace(w, r)
	if !ok {
		return
	}
	h.list(w, r, workspaceID)
}

// @Summary		Consultar um e-mail enviado pela API
// @Description	Um destinatário do registro de envios com a linha do tempo de eventos do provedor (envio, entrega, atraso, devolução, reclamação, abertura, clique, rejeição). Chave de API com a permissão `email:read`. Recusa: 404 `email_message_not_found`.
// @Tags			API de e-mail
// @Produce		json
// @Param			id	path		string	true	"Id do e-mail (o id de messages na resposta do envio)"
// @Success		200	{object}	LogDetailResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse	"email_message_not_found"
// @Security		WorkspaceKey
// @Router			/email/transactional/messages/{id} [get]
func (h *Handler) APIDetail(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, ok := principalWorkspace(w, r)
	if !ok {
		return
	}
	h.detail(w, r, workspaceID)
}

// @Summary		Registro de envios da API de e-mail
// @Description	O mesmo registro de `GET /email/transactional/messages`, lido pela tela com o login do usuário (permissão de leitura do e-mail marketing). Mesmos filtros, cursor e recusas.
// @Tags			API de e-mail
// @Produce		json
// @Param			status		query		string	false	"Status do e-mail"
// @Param			recipient	query		string	false	"Endereço do destinatário"
// @Param			tag_name	query		string	false	"Nome da tag"
// @Param			tag_value	query		string	false	"Valor da tag"
// @Param			after		query		string	false	"Criados a partir deste instante (RFC 3339)"
// @Param			before		query		string	false	"Criados antes deste instante (RFC 3339)"
// @Param			cursor		query		string	false	"Cursor da página seguinte"
// @Param			limit		query		int		false	"Itens por página, 1 a 100"
// @Success		200			{object}	LogPageResponse
// @Failure		400			{object}	response.ErrorResponse
// @Failure		403			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/email/transactional/log [get]
func (h *Handler) LogList(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	h.list(w, r, workspaceID)
}

// @Summary		Detalhe de um e-mail da API
// @Description	O mesmo detalhe de `GET /email/transactional/messages/{id}`, lido pela tela com o login do usuário.
// @Tags			API de e-mail
// @Produce		json
// @Param			id	path		string	true	"Id do e-mail"
// @Success		200	{object}	LogDetailResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse	"email_message_not_found"
// @Security		BearerAuth
// @Router			/email/transactional/log/{id} [get]
func (h *Handler) LogDetail(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	h.detail(w, r, workspaceID)
}
