package emailapihttp

import (
	"context"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	wh "vozko/domain/emailwebhook"
	"vozko/infra/http/middleware"
	emailwebhook_usecase "vozko/usecases/emailwebhook"
)

type Webhooks interface {
	Create(ctx context.Context, in emailwebhook_usecase.CreateInput) (emailwebhook_usecase.WithSecret, error)
	Update(ctx context.Context, in emailwebhook_usecase.UpdateInput) (wh.Endpoint, error)
	Delete(ctx context.Context, workspaceID, id string) error
	RotateSecret(ctx context.Context, workspaceID, id string) (emailwebhook_usecase.WithSecret, error)
	List(ctx context.Context, workspaceID string) ([]wh.Endpoint, error)
	Deliveries(ctx context.Context, workspaceID, endpointID, cursor string, limit int) (emailwebhook_usecase.DeliveryPage, error)
	Retry(ctx context.Context, workspaceID, endpointID, deliveryID string) (wh.Delivery, error)
}

func ids(w http.ResponseWriter, r *http.Request, names ...string) ([]string, bool) {
	out := make([]string, len(names))
	for i, name := range names {
		value := mux.Vars(r)[name]
		if _, err := uuid.Parse(value); err != nil {
			target := wh.ErrEndpointNotFound
			if name == "deliveryId" {
				target = wh.ErrDeliveryNotFound
			}
			writeWebhookRefusal(w, "", target)
			return nil, false
		}
		out[i] = value
	}
	return out, true
}

func eventCatalog() []string {
	out := make([]string, len(wh.EventTypes))
	for i, t := range wh.EventTypes {
		out[i] = string(t)
	}
	return out
}

// @Summary		Listar os webhooks da API de e-mail
// @Description	Os endereços que recebem os eventos dos e-mails enviados pela API, com os eventos escolhidos, se estão ativos, os quatro últimos caracteres do segredo e a saúde (falhando desde, último sucesso, última falha). `events` lista todos os eventos que um webhook pode receber e `limit` o máximo de webhooks por workspace.
// @Tags			API de e-mail
// @Produce		json
// @Success		200	{object}	EndpointListResponse
// @Failure		403	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/email/transactional/webhooks [get]
func (h *Handler) ListWebhooks(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	endpoints, err := h.webhooks.List(r.Context(), workspaceID)
	if err != nil {
		writeWebhookRefusal(w, workspaceID, err)
		return
	}
	out := EndpointListResponse{Endpoints: make([]EndpointResponse, len(endpoints)), Events: eventCatalog(), Limit: wh.MaxEndpointsPerWorkspace}
	for i, e := range endpoints {
		out.Endpoints[i] = endpointResponse(e)
	}
	response.WriteSuccess(w, http.StatusOK, out)
}

// @Summary		Criar um webhook da API de e-mail
// @Description	Cadastra um endereço https que recebe os eventos escolhidos dos e-mails enviados pela API: `email.sent`, `email.delivered`, `email.delivery_delayed`, `email.bounced`, `email.complained`, `email.opened`, `email.clicked`, `email.failed` (rejeitado pelo provedor). O endereço precisa resolver para um endereço público; privados, locais e de metadados são recusados agora e de novo a cada entrega. O segredo de assinatura (`secret`) aparece só nesta resposta e na rotação.
// @Description
// @Description	**Entrega.** `POST` com `Content-Type: application/json` e os cabeçalhos `X-Webhook-Id` (o id do evento, igual em todas as tentativas), `X-Webhook-Attempt` e `X-Webhook-Signature: t=<segundos unix>,v1=<hex>`, em que `v1` é o HMAC-SHA256 com o segredo sobre `<t>.<corpo bruto>`. Para verificar: recalcule o HMAC, compare em tempo constante e recuse `t` com mais de 5 minutos. Qualquer resposta 2xx confirma; outra resposta, um tempo esgotado (10 s) ou um endereço recusado contam como falha, e a entrega é repetida após 30 s, 2 min, 10 min, 30 min, 1 h, 3 h e 6 h (8 tentativas). Um webhook que falha por mais de uma hora gera um aviso para quem configura o e-mail; ele nunca é desativado sozinho.
// @Description
// @Description	**Corpo.** `{"id": "evt_...", "type": "email.bounced", "created_at": "...", "data": {"message_id", "send_id", "idempotency_key", "from", "to", "role", "subject", "tags", "occurred_at", "bounce": {"type", "sub_type", "diagnostic"}, "complaint": {"feedback_type"}, "click": {"link"}}}`. Recusas: 400 `email_webhook_url_invalid`, `email_webhook_events_invalid`; 422 `email_webhook_url_blocked`; 409 `email_webhook_limit_reached` (até 10 por workspace).
// @Tags			API de e-mail
// @Accept			json
// @Produce		json
// @Param			request	body		EndpointRequest	true	"Endereço, eventos e se começa ativo (padrão ativo)"
// @Success		201		{object}	EndpointWithSecretResponse
// @Failure		400		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Failure		409		{object}	response.ErrorResponse
// @Failure		422		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/email/transactional/webhooks [post]
func (h *Handler) CreateWebhook(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	var req EndpointRequest
	if !httpx.DecodeStrictJSON(w, r, &req) {
		return
	}
	enabled := req.Enabled == nil || *req.Enabled
	userID := ""
	if claims := middleware.GetClaims(r); claims != nil {
		userID = claims.UserID
	}
	created, err := h.webhooks.Create(r.Context(), emailwebhook_usecase.CreateInput{WorkspaceID: workspaceID, UserID: userID, URL: req.URL, Events: req.Events, Enabled: enabled})
	if err != nil {
		writeWebhookRefusal(w, workspaceID, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, EndpointWithSecretResponse{EndpointResponse: endpointResponse(created.Endpoint), Secret: created.Secret})
}

// @Summary		Alterar um webhook da API de e-mail
// @Description	Muda o endereço, os eventos ou se o webhook está ativo. Pausado, ele não recebe eventos novos e as entregas na fila são puladas (podem ser reenviadas depois). Recusas: as mesmas da criação e 404 `email_webhook_not_found`.
// @Tags			API de e-mail
// @Accept			json
// @Produce		json
// @Param			id		path		string					true	"Id do webhook"
// @Param			request	body		EndpointUpdateRequest	true	"Campos a mudar"
// @Success		200		{object}	EndpointResponse
// @Failure		400		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Failure		422		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/email/transactional/webhooks/{id} [patch]
func (h *Handler) UpdateWebhook(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	path, ok := ids(w, r, "id")
	if !ok {
		return
	}
	var req EndpointUpdateRequest
	if !httpx.DecodeStrictJSON(w, r, &req) {
		return
	}
	updated, err := h.webhooks.Update(r.Context(), emailwebhook_usecase.UpdateInput{WorkspaceID: workspaceID, ID: path[0], URL: req.URL, Events: req.Events, Enabled: req.Enabled})
	if err != nil {
		writeWebhookRefusal(w, workspaceID, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, endpointResponse(updated))
}

// @Summary		Excluir um webhook da API de e-mail
// @Description	Remove o webhook; as entregas pendentes dele deixam de sair. Recusa: 404 `email_webhook_not_found`.
// @Tags			API de e-mail
// @Param			id	path	string	true	"Id do webhook"
// @Success		204
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/email/transactional/webhooks/{id} [delete]
func (h *Handler) DeleteWebhook(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	path, ok := ids(w, r, "id")
	if !ok {
		return
	}
	if err := h.webhooks.Delete(r.Context(), workspaceID, path[0]); err != nil {
		writeWebhookRefusal(w, workspaceID, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// @Summary		Trocar o segredo de um webhook
// @Description	Gera um segredo novo e o devolve uma única vez; o antigo deixa de assinar na hora. Recusa: 404 `email_webhook_not_found`.
// @Tags			API de e-mail
// @Produce		json
// @Param			id	path		string	true	"Id do webhook"
// @Success		200	{object}	EndpointWithSecretResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/email/transactional/webhooks/{id}/secret [post]
func (h *Handler) RotateWebhookSecret(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	path, ok := ids(w, r, "id")
	if !ok {
		return
	}
	rotated, err := h.webhooks.RotateSecret(r.Context(), workspaceID, path[0])
	if err != nil {
		writeWebhookRefusal(w, workspaceID, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, EndpointWithSecretResponse{EndpointResponse: endpointResponse(rotated.Endpoint), Secret: rotated.Secret})
}

// @Summary		Entregas de um webhook
// @Description	As entregas do webhook, da mais nova para a mais antiga, paginadas por cursor, cada uma com as tentativas (código HTTP, duração, erro e próxima tentativa). Guardadas por 30 dias. Recusas: 404 `email_webhook_not_found`; 400 `email_log_cursor_invalid`.
// @Tags			API de e-mail
// @Produce		json
// @Param			id		path		string	true	"Id do webhook"
// @Param			cursor	query		string	false	"Cursor da página seguinte"
// @Param			limit	query		int		false	"Itens por página, 1 a 100"
// @Success		200		{object}	DeliveryPageResponse
// @Failure		400		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/email/transactional/webhooks/{id}/deliveries [get]
func (h *Handler) WebhookDeliveries(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	path, ok := ids(w, r, "id")
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	page, err := h.webhooks.Deliveries(r.Context(), workspaceID, path[0], r.URL.Query().Get("cursor"), limit)
	if err != nil {
		writeWebhookRefusal(w, workspaceID, err)
		return
	}
	out := DeliveryPageResponse{NextCursor: page.Next, Data: make([]DeliveryResponse, len(page.Items))}
	for i, v := range page.Items {
		out.Data[i] = deliveryResponse(v)
	}
	response.WriteSuccess(w, http.StatusOK, out)
}

// @Summary		Reenviar uma entrega de webhook
// @Description	Coloca de novo na fila uma entrega que falhou de vez ou foi pulada; ela sai uma vez, com uma tentativa a mais. Recusas: 404 `email_webhook_not_found`, `email_webhook_delivery_not_found`; 409 `email_webhook_delivery_not_retryable`.
// @Tags			API de e-mail
// @Produce		json
// @Param			id			path		string	true	"Id do webhook"
// @Param			deliveryId	path		string	true	"Id da entrega"
// @Success		202			{object}	DeliveryResponse
// @Failure		403			{object}	response.ErrorResponse
// @Failure		404			{object}	response.ErrorResponse
// @Failure		409			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/email/transactional/webhooks/{id}/deliveries/{deliveryId}/retry [post]
func (h *Handler) RetryWebhookDelivery(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	path, ok := ids(w, r, "id", "deliveryId")
	if !ok {
		return
	}
	d, err := h.webhooks.Retry(r.Context(), workspaceID, path[0], path[1])
	if err != nil {
		writeWebhookRefusal(w, workspaceID, err)
		return
	}
	response.WriteSuccess(w, http.StatusAccepted, deliveryResponse(emailwebhook_usecase.DeliveryView{Delivery: d}))
}
