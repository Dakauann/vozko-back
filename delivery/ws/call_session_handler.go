package ws

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"vozko/domain/calls/calllist"
	cdr "vozko/domain/calls/cdr"
	callsession_domain "vozko/domain/callsession"
	"vozko/domain/conversation"
	"vozko/domain/lead"
	"vozko/domain/metrics"
	"vozko/domain/sip_trunk"
	"vozko/domain/telephony"
	"vozko/infra/http/middleware"
	calls_usecase "vozko/usecases/calls"
	callsession_usecase "vozko/usecases/callsession"
)

type CallSessionWSHandler struct {
	startUseCase callsession_domain.StartOutboundCallUseCase
	endUseCase   callsession_domain.EndOutboundCallUseCase
	lifecycle    *callsession_usecase.OutboundCallLifecycleRunner
	authorizer   conversation.ConversationAuthorizer
	logger       *log.Logger
	wsMetrics    metrics.WSMetricsRecorder

	sessionRegistry callsession_domain.CallSessionRegistry
	callRegistry    callsession_domain.CallRegistry
	inboundOffers   callsession_domain.InboundOfferResponder
	recordingPool   *calls_usecase.RecordingUploadPool
	channels        *CallChannels
	transfers       CallTransfers
	reconnects      CallReconnects

	userResolver TransferUsernameResolver

	presenceTelemetry func(workspaceID, userID, state, source string)

	boardSync      telephony.BoardSync
	capacityReader telephony.CapacityReader

	presenceMu      sync.Mutex
	presencePending map[string]bool
}

const presenceBroadcastDebounce = 150 * time.Millisecond

type TransferUsernameResolver interface {
	ResolveUsernames(userIDs []string) map[string]string
}

var callSessionForcedShutdownTimeout = 3 * time.Second

func NewCallSessionWSHandler(
	startUseCase callsession_domain.StartOutboundCallUseCase,
	endUseCase callsession_domain.EndOutboundCallUseCase,
	lifecycle *callsession_usecase.OutboundCallLifecycleRunner,
	authorizer conversation.ConversationAuthorizer,
	logger *log.Logger,
	wsMetrics metrics.WSMetricsRecorder,
) *CallSessionWSHandler {
	if logger == nil {
		logger = log.Default()
	}
	return &CallSessionWSHandler{
		startUseCase: startUseCase,
		endUseCase:   endUseCase,
		lifecycle:    lifecycle,
		authorizer:   authorizer,
		logger:       logger,
		wsMetrics:    wsMetrics,
	}
}

func (h *CallSessionWSHandler) WithRegistries(
	sessions callsession_domain.CallSessionRegistry,
	calls callsession_domain.CallRegistry,
) *CallSessionWSHandler {
	if sessions == nil || calls == nil {
		panic("CallSessionWSHandler.WithRegistries: both registries are required")
	}
	h.sessionRegistry = sessions
	h.callRegistry = calls
	sessions.SetPresenceListener(h)
	return h
}

func (h *CallSessionWSHandler) WithUserResolver(resolver TransferUsernameResolver) *CallSessionWSHandler {
	h.userResolver = resolver
	return h
}

func (h *CallSessionWSHandler) WithPresenceTelemetry(fn func(workspaceID, userID, state, source string)) *CallSessionWSHandler {
	h.presenceTelemetry = fn
	return h
}

func (h *CallSessionWSHandler) WithLiveBoard(sync telephony.BoardSync, capacity telephony.CapacityReader) *CallSessionWSHandler {
	h.boardSync = sync
	h.capacityReader = capacity
	return h
}

func (h *CallSessionWSHandler) WithInboundCalls(offers callsession_domain.InboundOfferResponder) *CallSessionWSHandler {
	h.inboundOffers = offers
	return h
}

func (h *CallSessionWSHandler) WithRecording(pool *calls_usecase.RecordingUploadPool) *CallSessionWSHandler {
	h.recordingPool = pool
	return h
}

func (h *CallSessionWSHandler) WithChannels(channels *CallChannels) *CallSessionWSHandler {
	h.channels = channels
	return h
}

// @Summary		WebSocket de chamadas
// @Description	Leva o áudio das ligações entre o navegador e a Vozko: faz ligações por troncos SIP e pelo WhatsApp, recebe chamadas, transfere para colegas e filas e mostra quem está disponível.
// @Description
// @Description	## Conectar
// @Description
// @Description	```text
// @Description	wss://SUA_URL_BASE/ws/call-session?token=SEU_ACCESS_TOKEN&workspaceId=SEU_WORKSPACE_ID&workspace_id=SEU_WORKSPACE_ID
// @Description	```
// @Description
// @Description	- Permissão: `call_session:use` no workspace.
// @Description
// @Description	Ao conectar você recebe `conversation:connected` com `{"feature": "call-session", "workspace_id": string, "user_id": string}` e, em seguida, a presença da equipe em `call-session:presence`.
// @Description
// @Description	## Áudio
// @Description
// @Description	Nos dois sentidos o áudio é PCM de 16 bits little-endian, mono, codificado em base64.
// @Description
// @Description	- Você envia em `call_audio`, informando `sample_rate`. O servidor converte 16000, 22050, 24000, 32000, 44100 e 48000 Hz para 8000 Hz; outras taxas são descartadas.
// @Description	- Você recebe em `call:audio`, sempre a 8000 Hz.
// @Description
// @Description	## Mensagens que você envia
// @Description
// @Description	Formato: `{"type": "...", "payload": {...}}`. Tipos desconhecidos são ignorados.
// @Description
// @Description	| type | payload | O que faz |
// @Description	|---|---|---|
// @Description	| `start_call` | `{"phone_number": string, "trunk_id"?: string, "whatsapp_phone_id"?: string, "lead_id"?: string, "call_list_item_id"?: string, "request_id"?: string}` | Faz uma ligação. Com `trunk_id` sai pelo tronco SIP (exige `sip_trunks:call`); sem ele, pelo número de WhatsApp em `whatsapp_phone_id`. O andamento chega em `call:status`. Veja Ligar para um lead. |
// @Description	| `end_call` | `{}` | Desliga a ligação atual. |
// @Description	| `call_audio` | `{"audio": string, "sample_rate"?: number}` | Áudio do microfone. Sem resposta. |
// @Description	| `call:incoming_accept` | `{"offer_id": string}` | Atende uma chamada oferecida em `call:incoming`. Precisa vir da mesma conexão que recebeu a oferta. |
// @Description	| `call:incoming_decline` | `{"offer_id": string, "reason"?: string}` | Recusa a chamada; ela segue para o próximo atendente. |
// @Description	| `call:transfer` | `{"target_kind": "member" \| "queue", "user_id"?: string, "queue_id"?: string, "notes"?: string}` | Transfere a ligação atual para um colega (`user_id`) ou uma fila (`queue_id`). `notes` chega a quem atender, até 500 caracteres. O andamento chega em `call:transfer_status`. |
// @Description	| `call:transfer_cancel` | `{"call_id": string}` | Cancela uma transferência para colega ainda tocando; a ligação volta para você. |
// @Description
// @Description	Para transferir é preciso `call_session:transfer` e poder atender aquele tipo de ligação. Colegas e filas disponíveis vêm de `call-session:presence` e de `GET /call-queues/transfer-targets`.
// @Description
// @Description	### Ligar para um lead
// @Description
// @Description	- Com `lead_id`, o lead precisa ser deste workspace, não pode estar bloqueado (`lead_blocked`) e `phone_number` precisa ser o WhatsApp dele ou um dos telefones de contato, com ou sem o nono dígito (`lead_not_dialable`). Uma ligação direta para um lead que pediu para não receber mensagens é permitida; só as listas de ligação o deixam de fora. Se o número for o WhatsApp de outro lead, as regras desse lead também valem: bloqueado dá `lead_blocked`. A ligação fica registrada no lead: no histórico de chamadas, na gravação e na cobrança.
// @Description	- Sem `lead_id`, o número é procurado entre os leads do workspace e, se for o WhatsApp de um lead, valem as mesmas regras: bloqueado dá `lead_blocked`. Se a ligação seguir, ela fica registrada nesse lead.
// @Description	- `call_list_item_id` liga o item de uma lista de ligações que você está trabalhando e exige `lead_id`, o lead do item. A ligação fica registrada como a última do item e renova a reserva dele. O item precisa estar reservado por você (veja POST /call-lists/{listId}/next), ser desse lead e `phone_number` precisa ser o número do item, senão `call_list_item_unavailable`. As regras da lista também valem: quem pediu para não receber mensagens não é chamado.
// @Description	- Os números e as linhas que podem ligar para um lead vêm de `GET /dial-targets?leadId=`.
// @Description
// @Description	## Mensagens que você recebe
// @Description
// @Description	| type | payload | Quando |
// @Description	|---|---|---|
// @Description	| `conversation:connected` | `{"feature": "call-session", "workspace_id", "user_id"}` | Ao conectar. |
// @Description	| `call:status` | `{"status": "ringing" \| "answered", "reason"?, "call_id", "phone_number"?, "request_id"?}` | A ligação está chamando ou foi atendida. Também chega com `answered` quando você assume uma ligação transferida ou retomada. |
// @Description	| `call:audio` | `{"audio": string, "sample_rate": 8000}` | Áudio de quem está do outro lado. Em ligações feitas por você, também traz os tons de progresso padrão brasileiros (ITU-T E.180, 425 Hz): o tom de chamada enquanto o número toca, ou o áudio da própria operadora quando ela envia; e, por 3 segundos antes de `call:ended`, o tom de ocupado (número ocupado ou recusa) ou de congestionamento (falha). |
// @Description	| `call:ended` | `{"call_id", "phone_number"?, "reason"?, "duration_seconds": number, "request_id"?}` | A ligação terminou. `reason`: `ended`, `failed`, `busy`, `no_answer`, `declined`, `cancelled`, `insufficient_balance` ou `balance_check_error`. |
// @Description	| `call:waiting_slot` | `{"reason": string}` | Todas as linhas estão ocupadas; a ligação começa quando uma liberar. |
// @Description	| `call:incoming` | Veja Chamada oferecida | Uma chamada foi oferecida a você. |
// @Description	| `call:incoming_withdrawn` | `{"offer_id": string, "reason": "no_answer" \| "caller_hung_up"}` | A oferta expirou ou quem ligou desistiu. |
// @Description	| `call:transfer_status` | Veja Transferências | Andamento da sua transferência. |
// @Description	| `call-session:presence` | `{"users": [{"user_id", "username"?, "busy": boolean, "on_call"?: boolean, "ringing"?: boolean, "has_browser": boolean}]}` | Alguém conectou, desconectou, entrou ou saiu de uma ligação. Sem `call_session:list_members`, a lista traz só você. |
// @Description	| `telephony:board` | Veja Painel de telefonia | Junto com a presença, para quem tem `call_session:list_members`. |
// @Description	| `conversation:error` | `{"code": string, "message": string, "entry_id"?: string}` | Veja Erros. |
// @Description
// @Description	### Chamada oferecida
// @Description
// @Description	```json
// @Description	{
// @Description	  "offer_id": "string",
// @Description	  "call_id": "string",
// @Description	  "workspace_id": "string",
// @Description	  "from_number": "string",
// @Description	  "to_number": "string (opcional)",
// @Description	  "channel": "sip | whatsapp",
// @Description	  "expires_at": "RFC 3339",
// @Description	  "transfer": {
// @Description	    "from_user_id": "string (opcional)",
// @Description	    "from_name": "string (opcional)",
// @Description	    "queue_id": "string (opcional)",
// @Description	    "queue_name": "string (opcional)",
// @Description	    "notes": "string (opcional)"
// @Description	  },
// @Description	  "resume": true
// @Description	}
// @Description	```
// @Description
// @Description	- `transfer` aparece quando a chamada vem de um colega, de uma fila ou de um fluxo de voz.
// @Description	- `resume: true` indica que é a sua própria ligação voltando depois de a conexão cair (veja abaixo).
// @Description
// @Description	### Transferências
// @Description
// @Description	`call:transfer_status` chega apenas para quem transferiu:
// @Description
// @Description	```json
// @Description	{
// @Description	  "transfer_id": "string",
// @Description	  "call_id": "string",
// @Description	  "status": "ringing | queued | connected | returned | ended",
// @Description	  "target_user_id": "string (opcional)",
// @Description	  "target_name": "string (opcional)",
// @Description	  "queue_id": "string (opcional)",
// @Description	  "queue_name": "string (opcional)",
// @Description	  "reason": "string (opcional)"
// @Description	}
// @Description	```
// @Description
// @Description	- **Para um colega**: quem ligou ouve a música de espera e você recebe `ringing`. Se o colega atender, `connected` e a ligação sai do seu discador. Se recusar, não atender (20 segundos) ou você cancelar, a ligação volta para você com `call:status` `answered` e `returned` com `reason` `declined`, `no_answer` ou `cancelled`. Se quem ligou desistir, `ended` com `reason: "caller_hung_up"`.
// @Description	- **Para uma fila**: você recebe `queued` e a ligação sai do seu discador na hora. Se ninguém da fila atender até a espera máxima, a ligação toca de novo para você como uma nova `call:incoming`.
// @Description	- **Ligações do WhatsApp**: a conversa do contato vai junto e passa a ser de quem atender, com as mesmas regras da atribuição manual. Por isso só tocam colegas que podem receber essa conversa; para um colega que não pode, a transferência é recusada com `conversation_out_of_reach`.
// @Description
// @Description	### Conexão perdida durante uma ligação
// @Description
// @Description	Se a sua conexão cair com uma ligação atendida, quem ligou ouve a música de espera por até 30 segundos. Ao reconectar, você recebe `call:incoming` com `resume: true`: aceite para continuar a mesma ligação ou recuse para encerrá-la. Sem reconexão nesse tempo, a ligação é encerrada. Ligações ainda chamando terminam na hora.
// @Description
// @Description	### Painel de telefonia
// @Description
// @Description	```json
// @Description	{
// @Description	  "workspace_id": "string",
// @Description	  "rev": 0,
// @Description	  "as_of": "RFC 3339",
// @Description	  "capacity": {"used": 0, "max": 0, "pct": 0},
// @Description	  "humans": [{"user_id": "string", "username": "string (opcional)", "state": "free | ringing | on_call", "has_browser": true, "since": "RFC 3339"}],
// @Description	  "queue": {"depth": 0, "available": true},
// @Description	  "online": 0,
// @Description	  "free": 0,
// @Description	  "in_call": 0,
// @Description	  "ringing": 0,
// @Description	  "idle_pct": 0
// @Description	}
// @Description	```
// @Description
// @Description	## Erros
// @Description
// @Description	`conversation:error` traz `code` e `message`. Em erros de chamada oferecida, `entry_id` é o `offer_id`; em erros de transferência, é o `call_id`.
// @Description
// @Description	| code | Significado |
// @Description	|---|---|
// @Description	| `invalid_payload` | Mensagem ou `payload` fora do formato. |
// @Description	| `missing_fields` | Falta `phone_number`, `offer_id` ou `call_id`, ou `lead_id` junto com `call_list_item_id`. |
// @Description	| `unauthorized` | Falta permissão para ligar, para usar o tronco ou para transferir esse tipo de ligação. |
// @Description	| `already_in_call` | Você já está em uma ligação ou atendendo uma chamada. |
// @Description	| `no_active_call` | Não há ligação para desligar. |
// @Description	| `dial_failed` | Não foi possível iniciar a ligação. |
// @Description	| `no_call_slots` | Todas as linhas ocupadas; tente em instantes. |
// @Description	| `insufficient_balance` | Saldo insuficiente para ligar. |
// @Description	| `not_configured` | Origem de ligação, ligações para leads ou listas de ligação não configuradas neste servidor. |
// @Description	| `lead_not_dialable` | O lead não existe neste workspace, o número não é dele, ou as regras do workspace ou da lista não permitem ligar para ele. |
// @Description	| `lead_blocked` | O lead está bloqueado e não recebe ligações. |
// @Description	| `call_list_item_unavailable` | O item da lista não está reservado por você, não é do lead ou do número enviados, ou a lista não está ativa ou você não liga nela. Peça o próximo em POST /call-lists/{listId}/next. |
// @Description	| `whatsapp_permission_required` | O contato não autorizou receber ligações pelo WhatsApp. |
// @Description	| `trunk_unavailable` | O tronco não existe, está desativado ou não faz ligações. |
// @Description	| `trunk_not_registered` | O tronco não está registrado no provedor. |
// @Description	| `invalid_number` | O número só pode ter dígitos, `*`, `#` e um `+` no início. |
// @Description	| `inbound_unavailable` | Chamadas recebidas não estão disponíveis neste servidor. |
// @Description	| `offer_not_found` | A oferta não está mais disponível. |
// @Description	| `not_for_user` | A oferta não é sua ou foi enviada para outra conexão. |
// @Description	| `offer_resolved` | A oferta já foi atendida ou recusada. |
// @Description	| `inbound_failed` | Falha ao atender. |
// @Description	| `transfer_unavailable` | Transferências não estão disponíveis neste servidor. |
// @Description	| `no_call_to_transfer` | Não há ligação em andamento para transferir. |
// @Description	| `call_not_found` | A ligação já terminou. |
// @Description	| `not_call_owner` | Só quem está na ligação pode transferi-la. |
// @Description	| `target_unavailable` | O colega não está livre ou não pode atender esse tipo de ligação. |
// @Description	| `conversation_out_of_reach` | Em ligações do WhatsApp, a conversa do contato não pode ser passada para esse colega. |
// @Description	| `transfer_to_self` | Não é possível transferir para você mesmo. |
// @Description	| `transfer_in_progress` | Essa ligação já está sendo transferida. |
// @Description	| `no_transfer` | Não há transferência para cancelar. |
// @Description	| `queue_not_found` | A fila não existe neste workspace. |
// @Description	| `notes_too_long` | A nota pode ter até 500 caracteres. |
// @Description	| `invalid_target` | Escolha um colega ou uma fila. |
// @Description	| `transfer_failed` | A transferência não pôde ser iniciada. |
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
// @Success		101	{string}	string	"Switching Protocols: a conexão vira WebSocket"
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ws/call-session [get]
func (h *CallSessionWSHandler) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.startUseCase == nil || h.endUseCase == nil {
		http.Error(w, "Call session websocket not configured", http.StatusNotImplemented)
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	workspaceID := middleware.GetWorkspaceID(r)
	if qsWS := r.URL.Query().Get("workspaceId"); qsWS != "" {
		workspaceID = qsWS
	}
	if workspaceID == "" {
		http.Error(w, "workspace is required", http.StatusForbidden)
		return
	}

	isAdmin := strings.TrimSpace(claims.Role) == "admin"
	if h.authorizer == nil || !h.authorizer.HasWorkspacePermission(claims.UserID, workspaceID, "call_session", "use", isAdmin) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	ws, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Printf("[CallSessionWS] upgrade error: %v", err)
		return
	}
	defer ws.Close()

	h.wsMetrics.IncWSConnections(metrics.WSEndpointCallSession)
	defer h.wsMetrics.DecWSConnections(metrics.WSEndpointCallSession)

	var writeMu sync.Mutex
	send := func(msg *WSOutgoingMessage) {
		if msg == nil {
			return
		}
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = ws.WriteJSON(msg)
	}

	send(&WSOutgoingMessage{
		Type: WSEventConnected,
		Payload: map[string]string{
			"feature":      "call-session",
			"workspace_id": workspaceID,
			"user_id":      claims.UserID,
		},
	})

	session := newCallSession(
		uuid.New().String(),
		claims.UserID,
		workspaceID,
		send,
		h.endUseCase,
		h.logger,
		0,
	)
	if h.presenceTelemetry != nil {
		session.SetPresenceTelemetry(h.presenceTelemetry)
	}
	if h.channels != nil {
		session.SetActivity(h.channels.activity)
	}
	if h.reconnects != nil {
		session.SetReconnects(h.reconnects)
	}

	leave := func() {}
	if h.sessionRegistry != nil {

		registry := h.sessionRegistry
		ws := workspaceID
		session.SetPresenceCallback(func() { registry.NotifyPresenceChanged(ws) })
		deregister, err := h.sessionRegistry.Register(session)
		if err != nil {
			h.logger.Printf("[CallSessionWS] session registry rejected session: %v", err)
		} else {
			var once sync.Once
			leave = func() { once.Do(deregister) }
			defer leave()
		}
	}

	for {
		_, msgBytes, err := ws.ReadMessage()
		if err != nil {
			break
		}
		var in WSIncomingMessage
		if err := json.Unmarshal(msgBytes, &in); err != nil {
			send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "invalid_payload", Message: "Invalid websocket message"}})
			continue
		}

		switch in.Type {
		case WSEventStartCall:
			var p StartCallPayload
			if err := json.Unmarshal(in.Payload, &p); err != nil {
				send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "invalid_payload", Message: "Invalid start_call payload"}})
				continue
			}
			if strings.TrimSpace(p.PhoneNumber) == "" {
				send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "missing_fields", Message: "phone_number is required"}})
				continue
			}

			if h.authorizer != nil && !h.authorizer.HasWorkspacePermission(claims.UserID, workspaceID, "call_session", "use", false) {
				send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "unauthorized", Message: "You don't have permission to place calls"}})
				continue
			}

			if session.HasActiveCall() {
				send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "already_in_call", Message: "You already have an active call. End it first."}})
				continue
			}

			started, err := h.startUseCase.Execute(context.Background(), callsession_domain.StartOutboundCallInput{
				WorkspaceID:     workspaceID,
				UserID:          claims.UserID,
				IsAdmin:         isAdmin,
				TargetPhone:     p.PhoneNumber,
				WhatsAppPhoneID: p.WhatsAppPhoneID,
				TrunkID:         p.TrunkID,
				LeadID:          p.LeadID,
				CallListItemID:  p.CallListItemID,
				OnWaitingForSlot: func() {
					send(&WSOutgoingMessage{Type: WSEventWaitingCallSlot, Payload: WaitingCallSlotPayload{Reason: "All call slots in use, waiting for one to free up"}})
				},
			})
			if err != nil {
				h.sendStartCallError(send, err)
				continue
			}

			if _, err := attachCall(context.Background(), callAttachInput{
				Session:        session,
				Call:           started.Call,
				Admission:      started.Admission,
				Phone:          started.PhoneNumber,
				LeadID:         started.LeadID,
				TrunkID:        started.TrunkID,
				CallListItemID: started.CallListItemID,
				RequestID:      p.RequestID,
				WorkspaceID:    workspaceID,
				OwnerUserID:    claims.UserID,
				StartedAt:      time.Now(),
				Direction:      cdr.DirectionOutbound,
				CallRegistry:   h.callRegistry,
				EndUseCase:     h.endUseCase,
				Lifecycle:      h.lifecycle,
				RecordingPool:  h.recordingPool,
				Channels:       h.channels,
				Logger:         h.logger,
			}); err != nil {
				h.logger.Printf("[CallSessionWS] attach error: %v", err)
				send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "dial_failed", Message: "Failed to initiate call"}})
				continue
			}
		case WSEventEndCall:
			lc := session.Current()
			if lc == nil {
				send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "no_active_call", Message: "No active call to end"}})
				continue
			}
			_ = h.endUseCase.Execute(context.Background(), callsession_domain.EndOutboundCallInput{Call: lc.call, Hangup: true})
		case WSEventCallAudio:
			var p CallAudioPayload
			if err := json.Unmarshal(in.Payload, &p); err != nil {
				continue
			}
			pcm, err := base64.StdEncoding.DecodeString(p.Audio)
			if err != nil {
				continue
			}
			if p.SampleRate != 0 && p.SampleRate != sipDefaultSampleRate {
				converted, ok := func() ([]byte, bool) {
					if lc := session.Current(); lc != nil {
						return lc.inboundConverter.Convert(pcm, p.SampleRate)
					}
					return nil, false
				}()
				if !ok {
					if h.logger != nil {
						h.logger.Printf("[CallSessionWS] dropping call_audio with unsupported sample_rate=%d", p.SampleRate)
					}
					continue
				}
				pcm = converted
			}

			lc := session.Current()
			if lc == nil {
				continue
			}
			lc.enqueueAudio(pcm)
		case WSEventInboundCallAccept:
			h.handleInboundCallAction(session, in.Payload, true)
		case WSEventInboundCallDecline:
			h.handleInboundCallAction(session, in.Payload, false)
		case WSEventCallTransfer:
			h.handleTransfer(session, in.Payload)
		case WSEventCallTransferCancel:
			h.handleTransferCancel(session, in.Payload)
		}
	}

	leave()
	session.Shutdown(context.Background())
}

func (h *CallSessionWSHandler) sendStartCallError(send func(*WSOutgoingMessage), err error) {
	if send == nil {
		return
	}
	switch {
	case err == nil:
		return
	case strings.Contains(err.Error(), callsession_domain.ErrNoCallSlotsAvailable.Error()):
		send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "no_call_slots", Message: "No call slots available, please try again shortly"}})
	case strings.Contains(err.Error(), callsession_domain.ErrInsufficientBalance.Error()):
		send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "insufficient_balance", Message: "Insufficient balance to start a call"}})
	case strings.Contains(err.Error(), callsession_domain.ErrTargetPhoneRequired.Error()):
		send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "missing_fields", Message: "phone_number is required"}})
	case strings.Contains(err.Error(), callsession_domain.ErrCallSourceNotConfigured.Error()):
		send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "not_configured", Message: "Call source not configured"}})
	case errors.Is(err, callsession_domain.ErrLeadDialTargetsNotConfigured), errors.Is(err, callsession_domain.ErrCallListsNotConfigured):
		send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "not_configured", Message: "Calls to leads are not configured on this server"}})
	case calllist.ErrorCode(err) != "":
		send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "call_list_item_unavailable", Message: "This call list item is not reserved by you for this lead and number, or its list is not active"}})
	case errors.Is(err, callsession_domain.ErrCallListItemNeedsLead):
		send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "missing_fields", Message: "lead_id is required with call_list_item_id"}})
	case errors.Is(err, lead.ErrLeadDialBlocked):
		send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "lead_blocked", Message: "This lead is blocked and cannot be called"}})
	case errors.Is(err, lead.ErrLeadNotDialable):
		send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "lead_not_dialable", Message: "This number cannot be called for this lead"}})
	case errors.Is(err, conversation.ErrWhatsAppCallNoPermission):
		send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "whatsapp_permission_required", Message: "The customer hasn't granted permission to receive WhatsApp calls"}})
	case errors.Is(err, sip_trunk.ErrCallNotPermitted):
		send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "unauthorized", Message: "You don't have permission to call through SIP trunks"}})
	case errors.Is(err, sip_trunk.ErrTrunkNotFound), errors.Is(err, sip_trunk.ErrTrunkDisabled), errors.Is(err, sip_trunk.ErrTrunkCannotDial):
		send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "trunk_unavailable", Message: "This trunk cannot place calls"}})
	case errors.Is(err, sip_trunk.ErrTrunkNotRegistered):
		send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "trunk_not_registered", Message: "This trunk is not registered with its provider"}})
	case errors.Is(err, sip_trunk.ErrInvalidPhoneNumber):
		send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "invalid_number", Message: "The number can only contain digits, *, # and a leading +"}})
	default:
		h.logger.Printf("[CallSessionWS] start call error: %v", err)
		send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "dial_failed", Message: "Failed to initiate call"}})
	}
}

func (h *CallSessionWSHandler) handleInboundCallAction(session *callSession, raw json.RawMessage, accept bool) {
	if h.inboundOffers == nil {
		session.send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "inbound_unavailable", Message: "Inbound calls are not enabled on this server"}})
		return
	}
	var payload InboundCallActionPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		session.send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "invalid_payload", Message: "Invalid incoming call payload"}})
		return
	}
	offerID := strings.TrimSpace(payload.OfferID)
	if offerID == "" {
		session.send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "missing_fields", Message: "offer_id is required"}})
		return
	}

	var err error
	if accept {
		err = h.inboundOffers.Accept(context.Background(), callsession_domain.AcceptInboundCallInput{
			OfferID:     offerID,
			WorkspaceID: session.workspaceID,
			UserID:      session.userID,
			SessionID:   session.ID(),
		})
	} else {
		err = h.inboundOffers.Decline(context.Background(), callsession_domain.DeclineInboundCallInput{
			OfferID:     offerID,
			WorkspaceID: session.workspaceID,
			UserID:      session.userID,
			SessionID:   session.ID(),
			Reason:      payload.Reason,
		})
	}
	if err != nil {
		h.sendInboundCallError(session, offerID, err)
	}
}

func (h *CallSessionWSHandler) sendInboundCallError(session *callSession, offerID string, err error) {
	if err == nil {
		return
	}
	code := "inbound_failed"
	message := "Incoming call failed"
	switch {
	case errors.Is(err, callsession_domain.ErrInboundOfferNotFound):
		code, message = "offer_not_found", "Incoming call offer is no longer available"
	case errors.Is(err, callsession_domain.ErrInboundOfferNotForUser):
		code, message = "not_for_user", "This incoming call is not addressed to you"
	case errors.Is(err, callsession_domain.ErrInboundOfferAlreadyResolved):
		code, message = "offer_resolved", "Incoming call offer was already answered"
	case errors.Is(err, callsession_domain.ErrSessionBusy):
		code, message = "already_in_call", "You already have an active call"
	default:
		h.logger.Printf("[CallSessionWS] inbound call error: %v", err)
	}
	session.send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: code, Message: message, EntryID: offerID}})
}

func (h *CallSessionWSHandler) OnPresenceChanged(workspaceID string) {
	if h == nil || h.sessionRegistry == nil || workspaceID == "" {
		return
	}
	h.presenceMu.Lock()
	if h.presencePending == nil {
		h.presencePending = make(map[string]bool)
	}
	if h.presencePending[workspaceID] {
		h.presenceMu.Unlock()
		return
	}
	h.presencePending[workspaceID] = true
	h.presenceMu.Unlock()

	go func() {
		time.Sleep(presenceBroadcastDebounce)
		h.presenceMu.Lock()
		delete(h.presencePending, workspaceID)
		h.presenceMu.Unlock()
		h.broadcastPresence(workspaceID)
	}()
}

func (h *CallSessionWSHandler) broadcastPresence(workspaceID string) {
	if h == nil || h.sessionRegistry == nil || workspaceID == "" {
		return
	}

	presence := h.sessionRegistry.ListPresence(workspaceID)
	users := make([]CallSessionPresenceUser, 0, len(presence))
	ids := make([]string, 0, len(presence))
	seats := make([]telephony.HumanSeat, 0, len(presence))
	now := time.Now().UTC()
	for _, p := range presence {
		ids = append(ids, p.UserID)
		users = append(users, CallSessionPresenceUser{
			UserID:     p.UserID,
			Busy:       p.Busy,
			OnCall:     p.OnCall,
			Ringing:    p.Ringing,
			HasBrowser: p.HasBrowser,
		})
		state := telephony.SeatFree
		switch {
		case p.OnCall:
			state = telephony.SeatOnCall
		case p.Ringing:
			state = telephony.SeatRinging
		case p.Busy:
			state = telephony.SeatOnCall
		}
		seats = append(seats, telephony.HumanSeat{
			UserID:     p.UserID,
			State:      state,
			HasBrowser: p.HasBrowser,
			Since:      now,
		})
	}
	if h.userResolver != nil && len(ids) > 0 {
		names := h.userResolver.ResolveUsernames(ids)
		for i := range users {
			if name, ok := names[users[i].UserID]; ok {
				users[i].Username = name
			}
		}
		for i := range seats {
			if name, ok := names[seats[i].UserID]; ok {
				seats[i].Username = name
			}
		}
	}

	var boardSnap *telephony.BoardSnapshot
	if h.boardSync != nil {
		var used, max int64
		if h.capacityReader != nil {
			used, max, _ = h.capacityReader.Snapshot(workspaceID)
		}
		if snap, err := h.boardSync.SyncHumansFromPresence(workspaceID, seats, used, max); err == nil {
			boardSnap = snap
		}
	}

	recipients := h.sessionRegistry.ListBrowserSessions(workspaceID)
	if len(recipients) == 0 {
		return
	}

	fullMsg := callsession_domain.CallSessionControlMessage{
		Type:    string(WSEventCallSessionPresence),
		Payload: CallSessionPresencePayload{Users: users},
	}
	for _, s := range recipients {
		if s == nil {
			continue
		}
		canList := h.authorizer == nil || h.authorizer.HasWorkspacePermission(s.UserID(), workspaceID, "call_session", "list_members", false)
		msg := fullMsg
		if !canList {
			selfOnly := make([]CallSessionPresenceUser, 0, 1)
			for _, u := range users {
				if u.UserID == s.UserID() {
					selfOnly = append(selfOnly, u)
					break
				}
			}
			msg = callsession_domain.CallSessionControlMessage{
				Type:    string(WSEventCallSessionPresence),
				Payload: CallSessionPresencePayload{Users: selfOnly},
			}
		}
		if err := s.Notify(msg); err != nil {
			h.logger.Printf("[CallSessionWS] presence notify session=%s user=%s: %v", s.ID(), s.UserID(), err)
		}
		if canList && boardSnap != nil {
			_ = s.Notify(callsession_domain.CallSessionControlMessage{
				Type:    string(WSEventTelephonyBoard),
				Payload: boardSnap,
			})
		}
	}
}
