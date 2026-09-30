package ws

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"vozko/domain/shared"
	"vozko/domain/user"
	workspace_domain "vozko/domain/workspace"
	"vozko/infra/http/middleware"
)

const (
	conversationWriteWait      = 10 * time.Second
	conversationPongWait       = 60 * time.Second
	conversationPingPeriod     = (conversationPongWait * 9) / 10
	conversationMaxMessageSize = 512 * 1024
)

type ConversationWSHandler struct {
	hub    *ConversationHub
	logger *log.Logger
}

func NewConversationWSHandler(hub *ConversationHub, logger *log.Logger) *ConversationWSHandler {
	if hub == nil {
		return nil
	}
	if logger == nil {
		logger = log.Default()
	}
	return &ConversationWSHandler{
		hub:    hub,
		logger: logger,
	}
}

func resolveConnectionDepartmentID(r *http.Request) string {
	if r == nil {
		return ""
	}

	if departmentID := strings.TrimSpace(r.URL.Query().Get("departmentId")); departmentID != "" {
		return departmentID
	}

	filter := middleware.GetDepartmentFilter(r)
	if filter == nil {
		return ""
	}

	departmentID, err := filter.DepartmentIDForCreation()
	if err != nil {
		return ""
	}

	return strings.TrimSpace(departmentID)
}

// @Summary		WebSocket de conversas
// @Description	Mantém a caixa de entrada e as conversas abertas atualizadas em tempo real: novas mensagens, leituras, digitação, mudanças de etapa, etiquetas, responsável e status. Também é por ele que o atendente envia mensagens e muda o status da conversa.
// @Description
// @Description	## Conectar
// @Description
// @Description	```text
// @Description	wss://SUA_URL_BASE/ws/conversations?token=SEU_ACCESS_TOKEN&workspaceId=SEU_WORKSPACE_ID
// @Description	```
// @Description
// @Description	| Parâmetro | Obrigatório | Descrição |
// @Description	|---|---|---|
// @Description	| `workspaceId` | não | Workspace da conexão. Sem ele, vale o workspace padrão do usuário. |
// @Description	| `departmentId` | não | Departamento selecionado; filtra a caixa de entrada. |
// @Description	| `campaignId` + `campaignType` | não | Abre a conexão já na caixa de uma campanha. `campaignType`: `whatsapp`, `unofficial_whatsapp`, `instagram`, `facebook` ou `telegram`. |
// @Description
// @Description	- Permissão: `conversations:read` no workspace.
// @Description	- O servidor envia um ping a cada 54 segundos; navegadores respondem sozinhos. Sem resposta em 60 segundos, a conexão é encerrada.
// @Description	- Mensagens do cliente podem ter até 512 KiB.
// @Description
// @Description	Ao conectar, você recebe `conversation:connected`, depois `conversation:connected_users` e a primeira página da caixa de entrada em `conversation:inbox`.
// @Description
// @Description	## Mensagens que você envia
// @Description
// @Description	Formato: `{"type": "...", "payload": {...}}`. Tipos desconhecidos recebem `conversation:error` com `code: "unknown_event"`.
// @Description
// @Description	| type | Permissão | payload | Resposta |
// @Description	|---|---|---|---|
// @Description	| `subscribe` | `conversations:read` | `{"entry_id": string, "entry_type": string, "page_size"?: number}` | `conversation:subscribed` e `conversation:history`. A partir daí você recebe os eventos dessa conversa. `page_size` padrão 50, máximo 100. |
// @Description	| `unsubscribe` | | `{"entry_id": string, "entry_type": string}` | `conversation:unsubscribed` |
// @Description	| `send` | `conversations:send` | `{"entry_id": string, "entry_type": string, "text"?: string, "media_id"?: string, "media_type"?: "image" \| "video" \| "audio" \| "document" \| "sticker", "signed"?: boolean, "request_id": string, "reply_to_message_id"?: string}` | `conversation:message_sent` para todos os inscritos, ou `conversation:message_error`. Informe `text` ou `media_id`. |
// @Description	| `send_button` | `conversations:send` | `{"entry_id": string, "entry_type": string, "body_text": string, "buttons": [{"id": string, "title": string}], "header_type"?: string, "header_text"?: string, "footer_text"?: string, "request_id": string, "reply_to_message_id"?: string}` | Igual a `send`. De 1 a 3 botões. |
// @Description	| `mark_read` | `conversations:read` | `{"entry_id": string, "entry_type": string, "message_ids": string[]}` | `conversation:read` para todos os inscritos. |
// @Description	| `typing` | `conversations:read` | `{"entry_id": string, "entry_type": string, "is_typing"?: boolean}` | `conversation:typing` para os outros inscritos. Sem erros. |
// @Description	| `request_connected_users` | `conversations:read` | `{}` | `conversation:connected_users` |
// @Description	| `load_history` | `conversations:read` | `{"entry_id": string, "entry_type": string, "before": string, "page_size"?: number}` | `conversation:history` com mensagens anteriores a `before` (RFC 3339). |
// @Description	| `load_around` | `conversations:read` | `{"entry_id": string, "entry_type": string, "timestamp": string, "page_size"?: number}` | `conversation:history` em torno de `timestamp` (RFC 3339). |
// @Description	| `request_inbox_page` | `conversations:read` | `{"page"?: number, "page_size"?: number}` | `conversation:inbox`. `page_size` padrão 20, máximo 50. |
// @Description	| `search_inbox` | `conversations:read` | Veja Busca na caixa de entrada | `conversation:search_results` |
// @Description	| `search_messages` | `conversations:read` | `{"entry_id": string, "entry_type": string, "query": string, "page"?: number, "page_size"?: number}` | `conversation:search_messages_results` |
// @Description	| `load_entry_matches` | `conversations:read` | Igual a `search_messages` (`page_size` padrão 10, máximo 50) | `conversation:entry_matches_results` |
// @Description	| `reopen_window` | `conversations:reopen` | `{"entry_id": string, "entry_type": string, "template_id": string, "parameters"?: string[], "request_id"?: string}` | Envia um template para reabrir a janela de atendimento. Resposta `conversation:window_reopened`. |
// @Description	| `assign_to` | `conversations:assign` | `{"entry_id": string, "entry_type": string, "user_id": string}` | Sem resposta direta: você recebe `conversation:entry_update`. |
// @Description	| `request_funnel_column` | `conversations:read` | `{"stage_id": string, "page"?: number, "page_size"?: number}` | `conversation:funnel_column` |
// @Description	| `request_funnel_summary` | `conversations:read` | `{"stage_ids": string[]}` | `conversation:funnel_summary` |
// @Description	| `switch_view` | | `{"campaign_id"?: string, "campaign_type"?: string, "whatsapp_campaign_type"?: "standard" \| "organic", "container_kind"?: "" \| "campaign", "conversation_status"?: "new" \| "ongoing" \| "finished"}` | `conversation:view_switched` e a primeira página em `conversation:inbox`. Cancela todas as inscrições em conversas. |
// @Description	| `set_conversation_status` | `conversations:send` | `{"entry_id": string, "entry_type": string, "status": "ongoing" \| "finished", "outcome_code"?: string}` | Sem resposta direta: você recebe `conversation:conversation_status_update`. |
// @Description
// @Description	`entry_type` é o canal da conversa: `whatsapp`, `unofficial_whatsapp`, `instagram`, `facebook` ou `telegram`.
// @Description
// @Description	Regras de `set_conversation_status`: uma conversa volta a `new` só quando o contato manda uma mensagem nova; uma conversa finalizada só reabre da mesma forma. Se o workspace exige um desfecho, envie `outcome_code`; sem ele você recebe `outcome_required` com a lista em `outcomes`.
// @Description
// @Description	Em `switch_view` com `campaign_type: "whatsapp"`, é preciso `whatsapp_campaigns:read`; com `unofficial_whatsapp` e `container_kind: "campaign"`, `unofficial_whatsapp_campaigns:read`.
// @Description
// @Description	### Busca na caixa de entrada
// @Description
// @Description	Todos os campos de `search_inbox` são opcionais:
// @Description
// @Description	```json
// @Description	{
// @Description	  "query": "string",
// @Description	  "stage_id": "string",
// @Description	  "stage_name": "string",
// @Description	  "min_message_count": 0,
// @Description	  "max_message_count": 0,
// @Description	  "message_search": "string",
// @Description	  "window_open": true,
// @Description	  "has_unread": true,
// @Description	  "channel": "string",
// @Description	  "date_from": "RFC 3339",
// @Description	  "date_to": "RFC 3339",
// @Description	  "conversation_status": "new | ongoing | finished",
// @Description	  "responsible_user_id": "string",
// @Description	  "responsible_unassigned": true,
// @Description	  "responsible_kind": "ai | workflow",
// @Description	  "page": 1,
// @Description	  "page_size": 20
// @Description	}
// @Description	```
// @Description
// @Description	## Mensagens que você recebe
// @Description
// @Description	Formato: `{"type": "...", "payload": {...}}`.
// @Description
// @Description	### Respostas aos seus pedidos
// @Description
// @Description	| type | payload |
// @Description	|---|---|
// @Description	| `conversation:connected` | `{"user_id": string, "connection_id": string}` |
// @Description	| `conversation:subscribed` | `{"entry_id", "entry_type", "lead_name"?, "lead_number"?, "lead_picture"?, "lead_metadata"?: object, "entry_variables"?: string[], "unread_count": number, "automation_enabled": boolean, "window_open": boolean, "window_expires_at"?: string, "window_closed_reason"?: string, "window_tier"?: "standard" \| "human_agent"}` |
// @Description	| `conversation:unsubscribed` | `{"entry_id": string, "entry_type": string}` |
// @Description	| `conversation:history` | `{"entry_id", "entry_type", "messages": Message[], "has_more": boolean, "total": number, "page_size": number}` |
// @Description	| `conversation:inbox` | `{"entries": InboxEntry[], "page", "page_size", "total_items", "total_pages", "stage_counts"?: object, "conversation_status_counts"?: object, "available_labels"?: Label[]}` |
// @Description	| `conversation:search_results` | `{"query"?, "filters": object, "entries": InboxEntry[], "page", "page_size", "total_items", "total_pages"}` |
// @Description	| `conversation:search_messages_results` | `{"entry_id", "entry_type", "query", "messages": Message[], "page", "page_size", "total_items", "total_pages"}` |
// @Description	| `conversation:entry_matches_results` | `{"entry_id", "entry_type", "query", "matches": MatchedMessage[], "page", "page_size", "total_items", "total_pages"}` |
// @Description	| `conversation:window_reopened` | `{"entry_id", "entry_type", "request_id"?, "template_id", "message_id"}` |
// @Description	| `conversation:funnel_column` | `{"stage_id", "entries": InboxEntry[], "page", "page_size", "total_items", "total_pages"}` |
// @Description	| `conversation:funnel_summary` | `{"columns": [{"stage_id": string, "total_items": number}]}` |
// @Description	| `conversation:view_switched` | `{"view_mode": "global" \| "campaign", "campaign_id"?, "campaign_type"?, "whatsapp_campaign_type"?, "container_kind"?, "conversation_status"?}` |
// @Description	| `conversation:connected_users` | `{"users": [{"user_id", "workspace_id", "campaign_id", "campaign_type", "campaign_name"?, "view_mode", "username"?, "email"?, "connected_at"}]}`. Também chega sozinho quando alguém entra, sai ou troca de visão. Você só vê colegas dos seus departamentos, salvo se tiver acesso a todo o workspace. |
// @Description	| `conversation:message_error` | `{"request_id", "entry_id", "entry_type", "error": string, "code"?: string}`. Falha de `send` ou `send_button`. |
// @Description	| `conversation:error` | Veja Erros. |
// @Description
// @Description	### Eventos das conversas em que você se inscreveu
// @Description
// @Description	| type | payload | Quando |
// @Description	|---|---|---|
// @Description	| `conversation:message` | `{"entry_id", "entry_type", "message": Message}` | Chegou ou foi enviada uma mensagem (contato, IA, fluxo, agendamento, chamadas). |
// @Description	| `conversation:message_sent` | `{"request_id", "entry_id", "entry_type", "message": Message}` | Um atendente enviou uma mensagem. Chega para todos os inscritos. |
// @Description	| `conversation:read` | `{"entry_id", "entry_type", "message_ids": string[], "read_by": string, "read_at": string}` | Mensagens foram marcadas como lidas. |
// @Description	| `conversation:typing` | `{"entry_id", "entry_type", "user_id"?, "is_typing": boolean}` | Um colega está digitando. |
// @Description	| `conversation:message_status` | `{"entry_id", "entry_type", "message_id", "status": "sent" \| "delivered" \| "read" \| "failed"}` | O canal confirmou a entrega. |
// @Description	| `conversation:analysis_update` | `{"entry_id", "entry_type", "analysis": Analysis \| null, "pending": boolean}` | A análise da conversa mudou. |
// @Description	| `conversation:conversation_status_update` | `{"entry_id", "entry_type", "status", "close_source"?, "close_reason"?, "close_outcome"?, "closed_at"?}` | O status da conversa mudou. |
// @Description
// @Description	Uma mesma mensagem chega uma vez só por conexão: como `conversation:message` ou como `conversation:message_sent`.
// @Description
// @Description	### Eventos do workspace
// @Description
// @Description	Chegam mesmo sem inscrição, para quem tem acesso à conversa e está na visão correspondente:
// @Description
// @Description	| type | payload | Quando |
// @Description	|---|---|---|
// @Description	| `conversation:entry_update` | `{"entry": InboxEntry, "silent"?: boolean}` | Nova mensagem, envio, troca de responsável, status ou etapa. `silent: true` indica atualização sem novidade para o atendente. |
// @Description	| `conversation:conversation_status_counts_update` | `{"counts": {"new": number, "ongoing": number, "finished": number}}` | Os totais por status mudaram. |
// @Description	| `conversation:stage_update` | `{"entry_id", "entry_type", "stage": Stage \| null}` | A etapa da conversa mudou. |
// @Description	| `conversation:label_update` | `{"entry_id", "entry_type", "labels": Label[]}` | As etiquetas mudaram. |
// @Description	| `conversation:entry_removed` | `{"entry_id", "entry_type", "reason": "assigned"}` | A conversa foi para outra pessoa e você não a vê mais. |
// @Description	| `audience:analyzed` | `{"workspaceId", "source", "accountId", "containerId", "items": CommentAnalyzed[], "more": number}` | Um lote de comentários foi analisado. Só para quem tem `audience:read`. |
// @Description
// @Description	## Tipos usados nas mensagens
// @Description
// @Description	**InboxEntry**: `entry_id`, `entry_type`, `campaign_id`?, `campaign_name`?, `lead_id`?, `lead_name`?, `lead_number`?, `blocked`, `lead_picture`?, `is_group`?, `lead_metadata`?, `entry_variables`?, `unread_count`, `last_message_preview`?, `last_message_at`, `last_message_type`?, `last_message_sender`?, `last_message_sender_avatar`?, `window_open`, `window_expires_at`?, `window_closed_reason`?, `business_phone_id`?, `assigned_user_id`?, `assigned_username`?, `automation_enabled`, `stage`?: Stage, `labels`?: Label[], `available_stages`?: Stage[], `matched_messages`?: MatchedMessage[], `total_matches`?, `latest_analysis`?: Analysis, `analysis_phase`?: `"awaiting" | "queued"`, `live_read`?: LiveRead, `conversation_status`?: `new | ongoing | finished`, `close_source`?: `human | ai | system`, `close_reason`?: `manual | customer_idle | ai_resolved | max_age | workflow`, `closed_at`?, `close_outcome`?, `ai_handler`?: AIHandler.
// @Description
// @Description	**Message** (chaves em camelCase): `id`, `entryId`, `entryType`, `channel`, `messageType`, `direction`?: `INBOUND | OUTBOUND`, `from`, `to`, `text`, `mediaId`?, `mediaType`?, `read`, `readAt`?, `readBy`?, `whatsappMessageId`?, `externalMessageId`?, `replyToMessageId`?, `deliveryStatus`?: `sent | delivered | read | failed`, `senderName`?, `senderAvatar`?, `sentVia`?: `business_app`, `sentBy`: `{"kind": "contact" | "human" | "ai" | "workflow" | "campaign" | "external" | "system", "id": string}`, `metadata`?, `createdAt`, `updatedAt`.
// @Description
// @Description	Valores de `messageType`: `user_message`, `ai_response`, `tool_call`, `tool_result`, `audio`, `system`, `media`, `operator`, `template`, `call_permission_request`, `call_permission_granted`, `call_permission_rejected`, `call_received`, `call_answered`, `call_missed`, `call_ended`, `story_reply`, `story_mention`, `reaction`, `unsupported`, `post_share`, `sticker`, `link_share`.
// @Description
// @Description	**Stage**: `stage_id`, `name`, `color`?. **Label**: `label_id`, `name`, `color`?.
// @Description
// @Description	**MatchedMessage**: `message_id`, `text`, `from`, `message_type`, `channel`, `created_at`, `position`, `page`.
// @Description
// @Description	**AIHandler**: `kind`, `agent_id`?, `agent_name`?, `agent_avatar`?, `agent_active`, `workflow_id`?, `workflow_name`?, `workflow_run_id`?, `run_status`?, `current_node_id`?, `current_node_type`?.
// @Description
// @Description	**LiveRead** (camelCase): `interest`?, `disposition`?, `sentiment`?, `qualification`?, `nextAction`?, `language`?, `attendanceQuality`, `certainty`?: object, `decidedAt`.
// @Description
// @Description	**Analysis** (camelCase): análise da conversa com `id`, `status`, `sentiment`?, `intent`?, `interest`?, `productInterest`?, `disposition`?, `qualification`?, `nextAction`?, `summary`?, `attendanceQuality`?, `messageCount`?, `requiresAction`, `excerpt`, `analyzedAt`? e demais campos de identificação (`workspaceId`, `source`, `accountId`, `containerId`, `subjectId`, `createdAt`, `updatedAt`).
// @Description
// @Description	**CommentAnalyzed** (camelCase): `commentId`, `subjectKind`, `authorExternalId`, `authorHandle`?, `stance`?, `sentiment`?, `intent`?, `topicKey`?, `severity`, `requiresAction`, `isSpam`, `excerpt`, `interest`?, `productInterest`?, `disposition`?, `qualification`?, `nextAction`?, `summary`?, `occurredAt`, `analyzedAt`.
// @Description
// @Description	Valores de `window_closed_reason`: `expired`, `no_inbound`, `contact_blocked`, `session_down`, `account_restricted`, `reply_revoked` e `channel_unavailable`.
// @Description
// @Description	Datas são strings RFC 3339.
// @Description
// @Description	## Erros
// @Description
// @Description	`conversation:error` traz `{"code": string, "message": string}` e, em mudanças de status, também `entry_id`, `entry_type`, `status`, `previous_status` e `outcomes` (`[{"code", "label", "isDurable", "position"}]`).
// @Description
// @Description	| code | Significado |
// @Description	|---|---|
// @Description	| `forbidden` | Falta permissão, a campanha é de outro workspace ou a regra de status não permite a mudança. Se o acesso ao workspace for revogado, a conexão é encerrada logo depois. |
// @Description	| `unauthorized` | Você não tem acesso a essa conversa, ou o responsável escolhido não pode recebê-la. |
// @Description	| `unknown_event` | `type` desconhecido. |
// @Description	| `invalid_payload` | O `payload` não tem o formato esperado. |
// @Description	| `missing_fields` | Falta um campo obrigatório. |
// @Description	| `missing_content` | Mensagem sem texto nem mídia. |
// @Description	| `invalid_buttons` | É preciso de 1 a 3 botões. |
// @Description	| `invalid_entry_type` | `entry_type` inválido. |
// @Description	| `invalid_timestamp` | Data fora do formato RFC 3339. |
// @Description	| `invalid_status` | Status diferente de `new`, `ongoing` ou `finished`. |
// @Description	| `invalid_campaign_type`, `invalid_container_kind`, `invalid_whatsapp_campaign_type` | Filtro de visão inválido. |
// @Description	| `not_found` | Campanha não encontrada. |
// @Description	| `not_configured` | O recurso não está disponível neste servidor. |
// @Description	| `outcome_required` | Escolha um desfecho (lista em `outcomes`) para finalizar. |
// @Description	| `outcome_unknown` | O desfecho não existe no workspace (lista em `outcomes`). |
// @Description	| `history_error`, `inbox_error`, `search_error`, `fetch_error`, `template_send_failed`, `assign_failed`, `internal_error` | Falha ao processar o pedido; tente de novo. |
// @Description
// @Description	Códigos de `conversation:message_error`: `unauthorized`, `not_configured`, `send_failed` e `internal_error`.
// @Description
// @Description	## Autenticação
// @Description
// @Description	Use o mesmo access token da API HTTP. Navegadores não permitem cabeçalhos em WebSockets, então o token pode ir no cabeçalho `Authorization: Bearer SEU_ACCESS_TOKEN` (clientes fora do navegador), no parâmetro `token` da URL ou no cookie `accessToken` do login em modo cookie. Com o cookie, a conexão só é aceita quando o `Origin` é o próprio painel ou uma origem confiável; o token no cabeçalho ou na URL vale de qualquer origem.
// @Description
// @Description	## Workspace
// @Description
// @Description	Informe o workspace em `workspace_id` e em `workspaceId`, com o mesmo valor (ou o cabeçalho `X-Workspace-ID` junto com `workspaceId`). O primeiro é usado na checagem de permissão da conexão; o segundo define o workspace da sessão.
// @Description
// @Description	## Formato das mensagens
// @Description
// @Description	Cada quadro de texto carrega um único objeto JSON com o tipo da mensagem. Datas são strings RFC 3339. Antes de abrir o WebSocket, erros de autenticação, workspace e permissão voltam como HTTP comum (401, 400, 403, 429, 501).
// @Tags			WebSockets
// @Param			token		query	string	false	"Access token (alternativa ao cabeçalho Authorization e ao cookie accessToken)"
// @Param			workspace_id	query	string	false	"Workspace usado na checagem de permissão da conexão"
// @Param			workspaceId	query	string	false	"Workspace da sessão"
// @Param			departmentId	query	string	false	"Departamento selecionado; filtra a caixa de entrada"
// @Param			campaignId	query	string	false	"Abre a conexão na caixa de uma campanha (junto com campaignType)"
// @Param			campaignType	query	string	false	"Canal da campanha" Enums(whatsapp, unofficial_whatsapp, instagram, facebook, telegram)
// @Success		101	{string}	string	"Switching Protocols: a conexão vira WebSocket"
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ws/conversations [get]
func (h *ConversationWSHandler) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.hub == nil {
		http.Error(w, "Conversation WebSocket not configured", http.StatusNotImplemented)
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	query := r.URL.Query()
	campaignID := query.Get("campaignId")
	campaignType := query.Get("campaignType")

	if campaignType != "" && !shared.EntryType(campaignType).SupportsInboxScope() {
		http.Error(w, "campaignType must be "+shared.FormatEntryTypes(shared.InboxScopableEntryTypes()), http.StatusBadRequest)
		return
	}

	workspaceID := middleware.GetWorkspaceID(r)
	if qsWS := query.Get("workspaceId"); qsWS != "" {
		workspaceID = qsWS
	}
	if workspaceID == "" {
		http.Error(w, "workspace is required", http.StatusForbidden)
		return
	}

	departmentID := resolveConnectionDepartmentID(r)

	isSystemAdmin := claims.Role == string(user.RoleAdmin)
	if !isSystemAdmin && h.hub.authorizer != nil {
		isSystemAdmin := claims.Role == string(user.RoleAdmin)
		allowed := h.hub.authorizer.HasWorkspacePermission(
			claims.UserID,
			workspaceID,
			string(workspace_domain.ResourceConversations),
			string(workspace_domain.ActionRead),
			isSystemAdmin,
		)
		if !allowed {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	}

	viewMode := "global"
	if campaignID != "" && campaignType != "" {
		viewMode = "campaign"
		if h.hub.workspaceResolver != nil {
			campaignWorkspaceID, err := h.hub.workspaceResolver.GetCampaignWorkspaceID(campaignID, campaignType)
			if err != nil || campaignWorkspaceID == "" {
				http.Error(w, "campaign not found", http.StatusNotFound)
				return
			}
			if campaignWorkspaceID != workspaceID {
				http.Error(w, "campaign does not belong to workspace", http.StatusForbidden)
				return
			}
		}
		if h.hub.authorizer != nil && !h.hub.authorizer.CanAccessCampaign(claims.UserID, workspaceID, campaignID, campaignType, isSystemAdmin) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	} else {
		campaignID = ""
		campaignType = ""
	}

	ws, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Printf("[ConversationWS] Upgrade error: %v", err)
		return
	}

	conn := &WSConnection{
		ID:           uuid.New().String(),
		UserID:       claims.UserID,
		WorkspaceID:  workspaceID,
		DepartmentID: departmentID,
		IsAdmin:      claims.Role == string(user.RoleAdmin),
		Conn:         ws,
		Send:         make(chan []byte, 256),
		CampaignID:   campaignID,
		CampaignType: campaignType,
		ViewMode:     viewMode,
		Done:         make(chan struct{}),
		connectedAt:  time.Now(),
	}

	h.hub.RegisterConnection(conn)

	go h.writePump(conn)
	go h.readPump(conn)
}

func (h *ConversationWSHandler) readPump(conn *WSConnection) {
	defer func() {
		h.hub.UnregisterConnection(conn)
		conn.Conn.Close()
	}()

	conn.Conn.SetReadLimit(conversationMaxMessageSize)
	conn.Conn.SetReadDeadline(time.Now().Add(conversationPongWait))
	conn.Conn.SetPongHandler(func(string) error {
		conn.Conn.SetReadDeadline(time.Now().Add(conversationPongWait))
		return nil
	})

	for {
		_, message, err := conn.Conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure, websocket.CloseNormalClosure, websocket.CloseNoStatusReceived) {
				h.logger.Printf("[ConversationWS] User %s connection closed: %v", conn.UserID, err)
			} else if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				h.logger.Printf("[ConversationWS] User %s unexpected error: %v", conn.UserID, err)
			} else {
				h.logger.Printf("[ConversationWS] User %s read error: %v", conn.UserID, err)
			}
			break
		}

		var incoming WSIncomingMessage
		if err := json.Unmarshal(message, &incoming); err != nil {
			h.logger.Printf("[ConversationWS] Invalid message from user %s: %v", conn.UserID, err)
			continue
		}

		h.hub.HandleMessage(conn, &incoming)
	}
}

func (h *ConversationWSHandler) writePump(conn *WSConnection) {
	ticker := time.NewTicker(conversationPingPeriod)
	defer func() {
		ticker.Stop()
		conn.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-conn.Send:
			conn.Conn.SetWriteDeadline(time.Now().Add(conversationWriteWait))
			if !ok {
				h.logger.Printf("[ConversationWS] User %s send channel closed", conn.UserID)
				conn.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := conn.Conn.WriteMessage(websocket.TextMessage, message); err != nil {
				h.logger.Printf("[ConversationWS] User %s write error: %v", conn.UserID, err)
				return
			}

			n := len(conn.Send)
			for i := 0; i < n; i++ {
				queued := <-conn.Send
				if err := conn.Conn.WriteMessage(websocket.TextMessage, queued); err != nil {
					h.logger.Printf("[ConversationWS] User %s write error (batch): %v", conn.UserID, err)
					return
				}
			}

		case <-ticker.C:
			conn.Conn.SetWriteDeadline(time.Now().Add(conversationWriteWait))
			if err := conn.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				h.logger.Printf("[ConversationWS] User %s ping error: %v", conn.UserID, err)
				return
			}
		}
	}
}
