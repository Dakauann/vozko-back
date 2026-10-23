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
// @Description	| `subscribe` | `conversations:read` | `{"entry_id": string, "entry_type": string, "page_size"?: number}` | `conversation:subscribed` e `conversation:history`. A partir daí você recebe os eventos dessa conversa. `page_size` padrão 50, máximo 100. Abrir a conversa tira a sua marca pessoal de não lida (chega `conversation:personal_mark` se havia uma). Logo depois chega `conversation:viewers` com quem está com a conversa aberta, e todos os inscritos recebem a lista nova. |
// @Description	| `unsubscribe` | | `{"entry_id": string, "entry_type": string}` | `conversation:unsubscribed`. Os inscritos que ficam recebem `conversation:viewers` sem você (quando era a sua última conexão inscrita). |
// @Description	| `send` | `conversations:send` | `{"entry_id": string, "entry_type": string, "text"?: string, "media_id"?: string, "media_type"?: "image" \| "video" \| "audio" \| "document" \| "sticker", "signed"?: boolean, "request_id": string, "reply_to_message_id"?: string}` | `conversation:message_sent` para todos os inscritos e sempre para a conexão que enviou (mesmo sem inscrição, ou quando ela já recebeu a mensagem no histórico), ou `conversation:message_error`. Informe `text` ou `media_id`. `request_id` (até 128 caracteres, um UUID por envio) torna o envio idempotente: repetir o mesmo `request_id` na mesma conversa nunca envia de novo; se a mensagem já foi gravada, a resposta é o mesmo `conversation:message_sent` de antes (mesmo `request_id`); se o primeiro envio ainda está em andamento, a repetição fica sem resposta e vale a resposta do primeiro. Sem `request_id` (clientes antigos) não há essa proteção. |
// @Description	| `send_button` | `conversations:send` | `{"entry_id": string, "entry_type": string, "body_text": string, "buttons": [{"id": string, "title": string}], "header_type"?: string, "header_text"?: string, "footer_text"?: string, "request_id": string, "reply_to_message_id"?: string}` | Igual a `send`, inclusive a idempotência por `request_id`. De 1 a 3 botões. Os botões ficam guardados em `metadata.buttons` da mensagem, para que ela possa ser reenviada. |
// @Description	| `resend_message` | `conversations:send` | `{"entry_id": string, "entry_type": string, "message_id": string, "request_id": string}` | Reenvia uma mensagem que falhou, pelo mesmo caminho de `send` (saldo, janela, limite mensal, permissões e cobrança valem iguais), com o conteúdo original (texto, mídia, botões e resposta citada). Só vale para mensagem dessa conversa, enviada por um atendente, com `deliveryStatus: "failed"` e ainda não reenviada; senão `conversation:error` com `not_found`, `not_allowed` ou `invalid` (falta `request_id`). O reenvio cria uma mensagem nova: chega `conversation:message_updated` da mensagem que falhou, agora com `resent_as` apontando para a nova, e depois `conversation:message_sent` da nova com o seu `request_id`. Se o canal recusar, `conversation:message_error` com esse `request_id` e `code: "send_failed"`, e a mensagem que falhou fica como estava. Repetir o mesmo `request_id` responde com a mesma mensagem nova, sem enviar de novo. |
// @Description	| `react_message` | `conversations:send` | `{"entry_id": string, "entry_type": string, "message_id": string, "emoji": string, "request_id": string}` | Reage a uma mensagem no canal; `emoji: ""` tira a sua reação. Deu certo: `conversation:message_updated` com a reação em `reactions` (`direction: "outbound"`, `actor_id` é você). Falhou: `conversation:error`, e nada muda. |
// @Description	| `edit_message` | `conversations:send` | `{"entry_id": string, "entry_type": string, "message_id": string, "text": string, "request_id": string}` | Edita o texto de uma mensagem sua. Deu certo: `conversation:message_updated` com o novo `text` e `edited_at`. Falhou: `conversation:error`, e nada muda. |
// @Description	| `delete_message` | `conversations:send` | `{"entry_id": string, "entry_type": string, "message_id": string, "request_id": string}` | Apaga para todos uma mensagem sua. Deu certo: `conversation:message_updated` com `retracted_at` e sem o conteúdo. Falhou: `conversation:error`, e nada muda. |
// @Description	| `mark_read` | `conversations:read` | `{"entry_id": string, "entry_type": string, "message_ids": string[]}` | `conversation:read` para todos os inscritos. Também tira a sua marca pessoal de não lida dessa conversa (veja Marcas pessoais). |
// @Description	| `pin_conversation` | `conversations:read` | `{"entry_id": string, "entry_type": string, "pinned": boolean, "request_id": string}` | Fixa (`pinned: true`) ou desafixa a conversa só para você. Resposta `conversation:personal_mark` em todas as suas conexões deste workspace, ou `conversation:error`. |
// @Description	| `mark_unread` | `conversations:read` | `{"entry_id": string, "entry_type": string, "request_id": string}` | Marca a conversa como não lida só para você. Resposta `conversation:personal_mark` em todas as suas conexões deste workspace, ou `conversation:error`. |
// @Description	| `clear_unread_mark` | `conversations:read` | `{"entry_id": string, "entry_type": string, "request_id": string}` | Tira só a sua marca pessoal de não lida (o desfazer de `mark_unread`): não marca nenhuma mensagem como lida e não manda confirmação de leitura ao contato. Resposta `conversation:personal_mark` em todas as suas conexões deste workspace (mesmo quando não havia marca), ou `conversation:error`. |
// @Description	| `typing` | `conversations:read` | `{"entry_id": string, "entry_type": string, "is_typing"?: boolean}` | `conversation:typing` para os outros inscritos. Sem erros. |
// @Description	| `request_connected_users` | `conversations:read` | `{}` | `conversation:connected_users` |
// @Description	| `load_history` | `conversations:read` | `{"entry_id": string, "entry_type": string, "before": string, "page_size"?: number}` | `conversation:history` com mensagens anteriores a `before` (RFC 3339). `has_more` diz se há mensagens ainda mais antigas. |
// @Description	| `load_around` | `conversations:read` | `{"entry_id": string, "entry_type": string, "timestamp"?: string, "message_id"?: string, "page_size"?: number}` | `conversation:history` em torno de `timestamp` (RFC 3339) ou da mensagem `message_id` (informe exatamente um dos dois). Com `message_id` (por exemplo o `replyToMessageId` de uma citação que ainda não está carregada), o servidor confere o acesso à conversa, carrega a mensagem e usa o `createdAt` dela; se ela não existe ou não é dessa conversa, `conversation:error` com `code: "not_found"`, `entry_id` e `entry_type`. `has_after` diz se há mensagens mais novas depois da página. |
// @Description	| `load_after` | `conversations:read` | `{"entry_id": string, "entry_type": string, "after": string, "page_size"?: number}` | `conversation:history` com `direction: "after"` e as mensagens logo depois de `after` (RFC 3339 com nanossegundos, por exemplo o `createdAt` da mensagem mais nova que você já tem). `has_after: true` indica que há mais páginas depois; repita com o `createdAt` da última mensagem recebida. Serve para recuperar o que chegou durante uma queda de conexão e para voltar ao presente depois de um salto. Mesma checagem de acesso e mesmos limites de `page_size` de `load_history`. |
// @Description	| `request_inbox_page` | `conversations:read` | `{"page"?: number, "page_size"?: number}` | `conversation:inbox`. `page_size` padrão 20, máximo 50. |
// @Description	| `search_inbox` | `conversations:read` | Veja Busca na caixa de entrada | `conversation:search_results` |
// @Description	| `search_messages` | `conversations:read` | `{"entry_id": string, "entry_type": string, "query": string, "page"?: number, "page_size"?: number}` | `conversation:search_messages_results` |
// @Description	| `load_entry_matches` | `conversations:read` | Igual a `search_messages` (`page_size` padrão 10, máximo 50) | `conversation:entry_matches_results` |
// @Description	| `reopen_window` | `conversations:reopen` | `{"entry_id": string, "entry_type": string, "template_id": string, "parameters"?: string[], "header_parameters"?: string[], "request_id"?: string}` | Envia um template para reabrir a janela de atendimento. `parameters` são os valores das variáveis do corpo, na ordem em que aparecem; `header_parameters`, o da variável do texto do cabeçalho. | Resposta `conversation:window_reopened`. |
// @Description	| `assign_to` | `conversations:assign` | `{"entry_id": string, "entry_type": string, "user_id": string}` | Sem resposta direta: você recebe `conversation:entry_update`. |
// @Description	| `request_funnel_column` | `conversations:read` | `{"stage_id": string, "page"?: number, "page_size"?: number}` | `conversation:funnel_column` |
// @Description	| `request_funnel_summary` | `conversations:read` | `{"stage_ids": string[]}` | `conversation:funnel_summary` |
// @Description	| `switch_view` | | `{"campaign_id"?: string, "campaign_type"?: string, "whatsapp_campaign_type"?: "standard" \| "organic", "container_kind"?: "" \| "campaign", "conversation_status"?: "new" \| "ongoing" \| "finished"}` | `conversation:view_switched` e a primeira página em `conversation:inbox`. Cancela todas as inscrições em conversas. |
// @Description	| `set_conversation_status` | `conversations:send` | `{"entry_id": string, "entry_type": string, "status": "ongoing" \| "finished", "outcome_code"?: string}` | Sem resposta direta: você recebe `conversation:conversation_status_update`. |
// @Description
// @Description	`entry_type` é o canal da conversa: `whatsapp`, `unofficial_whatsapp`, `instagram`, `facebook` ou `telegram`.
// @Description
// @Description	Regras de `react_message`, `edit_message` e `delete_message`: o servidor confere o mesmo acesso de `send` e a regra da ação antes de chamar o canal, então um cliente desatualizado nunca age fora dela. Reagir exige um canal que reage e uma mensagem que o canal identifica e que não foi apagada. Editar exige um canal que edita, uma mensagem só de texto enviada por você e dentro da janela de edição do canal. Apagar exige um canal que apaga, uma mensagem enviada por você e dentro da janela de exclusão do canal. Hoje: WhatsApp por QR Code reage, edita por 15 minutos e apaga por 48 horas; Telegram reage, edita e apaga por 48 horas; Instagram, Facebook e WhatsApp oficial só reagem; o site não tem nenhuma ação. `conversation:subscribed` traz o que o canal da conversa permite em `message_capabilities`.
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
// @Description	  "is_group": true,
// @Description	  "channel": "string",
// @Description	  "date_from": "RFC 3339",
// @Description	  "date_to": "RFC 3339",
// @Description	  "conversation_status": "new | ongoing | finished",
// @Description	  "responsible_user_id": "string",
// @Description	  "responsible_unassigned": true,
// @Description	  "responsible_kind": "ai | workflow",
// @Description	  "sort": "recent | waiting",
// @Description	  "label_id": "string",
// @Description	  "personal": "pinned",
// @Description	  "page": 1,
// @Description	  "page_size": 20
// @Description	}
// @Description	```
// @Description
// @Description	- `sort`: `recent` (padrão, a ordem de sempre: a mensagem mais recente primeiro) ou `waiting`: primeiro as conversas que esperam resposta (as que têm `waiting_since`), a que espera desde mais cedo no topo, depois as demais pela mais recente. É a mesma regra de `waiting_since`: conversa finalizada não espera. Outro valor responde `conversation:error` com `invalid_sort`.
// @Description	- `label_id`: só conversas com essa etiqueta. Um id ilegível responde `invalid_label`.
// @Description	- `is_group`: `true`, só conversas de grupo (hoje, grupos do WhatsApp não oficial); `false`, só conversas com pessoas. Num canal sem grupos, `true` não traz nada.
// @Description	- `personal`: sem valor, a sua caixa de entrada (as suas fixadas primeiro, todas as outras em seguida); `pinned`, só as que você fixou. Outro valor responde `invalid_personal`.
// @Description	- Em qualquer `sort`, as conversas que você fixou vêm primeiro. `request_inbox_page`, a primeira página ao conectar e `switch_view` usam a sua caixa de entrada (sem `personal`) com `sort: "recent"`. As colunas do funil (`request_funnel_column`, `request_funnel_summary`) não usam marcas pessoais.
// @Description
// @Description	### Marcas pessoais
// @Description
// @Description	Fixar e marcar como não lida são só seus: ninguém da equipe vê as suas marcas, e elas nunca mostram uma conversa que você não pode ver (valem as mesmas regras de `subscribe`: departamento, responsável e `view_others`). Uma conversa que você deixou de ver some das suas marcas.
// @Description
// @Description	- Toda mudança chega em `conversation:personal_mark` a todas as suas conexões neste workspace (abas e janelas, em qualquer servidor), com `mark: null` quando não sobrou marca.
// @Description	- Abrir a conversa (`subscribe`) ou `mark_read` tira a sua marca de não lida. Para desfazer `mark_unread` use `clear_unread_mark`, que tira só a marca: `mark_read` marca as mensagens do contato como lidas e manda a confirmação de leitura a ele.
// @Description	- `conversation:inbox` e `conversation:search_results` trazem `personal_marks` com as suas marcas das conversas da página.
// @Description	- Os totais por status (`conversation_status_counts`) são da equipe: as suas marcas não os mudam.
// @Description
// @Description	Recusas de `pin_conversation`, `mark_unread` e `clear_unread_mark` chegam em `conversation:error` com `request_id`, `entry_id`, `entry_type` e o código: `not_allowed` (sem `conversations:read` ou sem acesso à conversa), `invalid` (falta `entry_id`, `entry_type` ou `pinned`), `unsupported` (servidor sem marcas pessoais) ou `failed` (não foi possível gravar).
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
// @Description	| `conversation:subscribed` | `{"entry_id", "entry_type", "lead_id"?, "lead_name": string, "lead_number": string, "lead_version": number, "blocked": boolean, "lead_picture": string, "lead_metadata"?: object, "entry_variables"?: string[], "unread_count": number, "automation_enabled": boolean, "window_open": boolean, "window_expires_at"?: string, "window_closed_reason"?: string, "window_closed_detail"?: string, "window_tier"?: "standard" | "human_agent", "ad_origin"?: AdOrigin, "message_capabilities": {"react": boolean, "edit": boolean, "delete": boolean, "edit_window_seconds": number, "delete_window_seconds": number}, "media_limits"?: [{"kind": "image" | "video" | "audio" | "document", "max_bytes": number, "mime_types": string[]}]}`. `message_capabilities` diz o que o canal da conversa permite fazer com as mensagens (veja as regras de `react_message`, `edit_message` e `delete_message`); as janelas contam a partir do `createdAt` da mensagem e valem 0 quando a ação não existe. `media_limits` diz quais anexos o canal aceita, um item por tipo, com o tamanho máximo em bytes; `mime_types` vazio aceita qualquer formato daquele tipo; um tipo fora da lista não é aceito; a lista vazia quer dizer que o canal não aceita anexos; sem o campo, os limites do canal não são conhecidos e o cliente não deve enviar anexos. `lead_name`, `lead_number`, `lead_version` e `lead_picture` vêm sempre (texto vazio ou 0 quando não há valor), para que um valor apagado substitua o anterior na tela. |
// @Description	| `conversation:unsubscribed` | `{"entry_id": string, "entry_type": string}` |
// @Description	| `conversation:history` | `{"entry_id", "entry_type", "direction": "latest" \| "before" \| "around" \| "after", "messages": Message[], "has_more": boolean, "has_after": boolean, "total": number, "page_size": number}`. `direction` diz qual pedido a página responde: `latest` (primeira página de `subscribe`), `before` (`load_history`), `around` (`load_around`) ou `after` (`load_after`). As mensagens vêm sempre da mais antiga para a mais nova. `has_more`: há mensagens mais antigas que a página (não se aplica a `after`). `has_after`: há mensagens mais novas que a página (só em `around` e `after`). `total` vem em `latest` e `around`. |
// @Description	| `conversation:inbox` | `{"entries": InboxEntry[], "personal_marks": PersonalMark[], "page", "page_size", "total_items", "total_pages", "stage_counts"?: object, "conversation_status_counts"?: object, "available_labels"?: Label[]}`. `personal_marks` traz as suas marcas das conversas da página (lista vazia quando não há). |
// @Description	| `conversation:search_results` | `{"query"?, "filters": object, "entries": InboxEntry[], "personal_marks": PersonalMark[], "page", "page_size", "total_items", "total_pages"}`. `filters` repete os filtros pedidos, inclusive `sort`, `label_id` e `personal`. |
// @Description	| `conversation:personal_mark` | `{"entry_id", "entry_type", "mark": {"pinned_at"?: string, "unread_marked_at"?: string} \| null}`. Só para as suas conexões neste workspace: resposta a `pin_conversation`, `mark_unread` e `clear_unread_mark`, e também quando a sua marca muda sozinha (abrir ou ler a conversa). `mark: null`: nenhuma marca. |
// @Description	| `conversation:search_messages_results` | `{"entry_id", "entry_type", "query", "messages": Message[], "page", "page_size", "total_items", "total_pages"}` |
// @Description	| `conversation:entry_matches_results` | `{"entry_id", "entry_type", "query", "matches": MatchedMessage[], "page", "page_size", "total_items", "total_pages"}` |
// @Description	| `conversation:window_reopened` | `{"entry_id", "entry_type", "request_id"?, "template_id", "message_id"}` |
// @Description	| `conversation:funnel_column` | `{"stage_id", "entries": InboxEntry[], "page", "page_size", "total_items", "total_pages"}` |
// @Description	| `conversation:funnel_summary` | `{"columns": [{"stage_id": string, "total_items": number}]}` |
// @Description	| `conversation:view_switched` | `{"view_mode": "global" \| "campaign", "campaign_id"?, "campaign_type"?, "whatsapp_campaign_type"?, "container_kind"?, "conversation_status"?}` |
// @Description	| `conversation:connected_users` | `{"users": [{"user_id", "workspace_id", "campaign_id", "campaign_type", "campaign_name"?, "view_mode", "username"?, "email"?, "connected_at"}]}`. Também chega sozinho quando alguém entra, sai ou troca de visão. Você só vê colegas dos seus departamentos, salvo se tiver acesso a todo o workspace. |
// @Description	| `conversation:message_error` | `{"request_id", "entry_id", "entry_type", "error": string, "code"?: string}`. Falha de `send`, `send_button` ou do envio de `resend_message`. |
// @Description	| `conversation:error` | Veja Erros. |
// @Description
// @Description	### Eventos das conversas em que você se inscreveu
// @Description
// @Description	| type | payload | Quando |
// @Description	|---|---|---|
// @Description	| `conversation:message` | `{"entry_id", "entry_type", "message": Message}` | Chegou ou foi enviada uma mensagem (contato, IA, fluxo, agendamento, chamadas). |
// @Description	| `conversation:message_sent` | `{"request_id", "entry_id", "entry_type", "message": Message}` | Um atendente enviou uma mensagem. Chega para todos os inscritos. |
// @Description	| `conversation:message_updated` | `{"entry_id", "entry_type", "message": Message}` | Uma mensagem já entregue mudou: foi editada ou apagada (pelo contato ou pela equipe), recebeu ou perdeu uma reação, ganhou a transcrição do áudio, ou falhou e foi reenviada (`resent_as`). Traz a mensagem inteira no mesmo formato de `conversation:message`; substitua a que você tem pelo `id`. Chega só para quem está inscrito na conversa. |
// @Description	| `conversation:read` | `{"entry_id", "entry_type", "message_ids": string[], "read_by": string, "read_at": string}` | Mensagens foram marcadas como lidas. |
// @Description	| `conversation:typing` | `{"entry_id", "entry_type", "user_id"?, "is_typing": boolean}` | Um colega está digitando. |
// @Description	| `conversation:message_status` | `{"entry_id", "entry_type", "message_id", "status": "sent" \| "delivered" \| "read" \| "failed"}` | O canal confirmou a entrega. |
// @Description	| `conversation:analysis_update` | `{"entry_id", "entry_type", "analysis": Analysis \| null, "pending": boolean}` | A análise da conversa mudou. |
// @Description	| `conversation:ad_origin` | `{"entry_id", "entry_type", "ad_origin": AdOrigin}` | O contato chegou por um anúncio (WhatsApp, Instagram ou Facebook). Só a primeira vez: o anúncio de origem não muda. |
// @Description	| `conversation:conversation_status_update` | `{"entry_id", "entry_type", "status", "close_source"?, "close_reason"?, "close_outcome"?, "closed_at"?}` | O status da conversa mudou. |
// @Description	| `conversation:viewers` | `{"entry_id", "entry_type", "viewers": [{"user_id": string, "name": string}]}` | Quem está com a conversa aberta agora mudou: alguém se inscreveu, cancelou a inscrição ou desconectou (em qualquer servidor). Também chega logo depois do seu `subscribe`, com a lista atual. Traz todos os inscritos, inclusive você (filtre o seu `user_id`); cada pessoa aparece uma vez, mesmo com várias abas ou janelas destacadas. `name` é o nome de usuário (texto vazio quando não foi encontrado). A lista é a fotografia completa: substitua a anterior. |
// @Description	| `conversation:note` | `{"entry_id", "entry_type", "note": Note}` | Alguém da equipe gravou uma nota interna na conversa (POST /conversations/{entryType}/{entryId}/notes). |
// @Description	| `conversation:note_deleted` | `{"entry_id", "entry_type", "note_id": string}` | O autor apagou uma nota interna; tire-a da tela. |
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
// @Description	| `notification:created` | `{"workspaceId", "item": {"id", "category", "severity", "title", "body", "imageUrl"?, "actionUrl"?, "actionLabel"?, "createdAt"}}` | Uma notificação nova chegou para você neste workspace (a mesma forma de GET /notifications). Só para os destinatários dela. Atualize o contador com GET /notifications/summary. |
// @Description	| `conversation:lead_update` | `{"leadId": string, "version": number, "fields": string[]}` | Um lead mudou (edição, nome, bloqueio, responsável, opt-out, família, anonimização). Traz só os nomes dos campos, nunca os valores; um campo personalizado aparece como `customFields.<chave>`, e `anonymized` diz que o lead foi anonimizado e não existe mais (a nova leitura responde 404): busque o lead de novo por uma rota que você pode usar (GET /leads/{id} com `leads:read`, ou a própria conversa). Chega para quem tem `leads:read` e para quem está inscrito em uma conversa desse lead. Use `version` no `If-Match` da próxima edição. |
// @Description	| `conversation:leads_bulk_update` | `{"runId": string}` | Um lote de uma ação em massa sobre leads (classificar, atribuir, bloquear) foi gravado. Chega uma vez por lote, só para quem tem `leads:read`; não traz ids de leads nem valores. Recarregue a lista e consulte o andamento em GET /leads/actions/{runId}. |
// @Description
// @Description	### Chat da equipe
// @Description
// @Description	Conversas internas entre colegas (veja a seção Chat da equipe da API). Os eventos chegam só para quem participa da conversa, em todas as suas conexões deste workspace, em qualquer servidor. Os comandos são pela API HTTP; o socket só traz os eventos e a digitação. Quem não tem `conversations:read` não abre este socket: atualize GET /team-chat/summary a cada minuto. Ao reconectar, recupere o que perdeu com GET /team-chat/threads e, em cada conversa aberta, GET /team-chat/threads/{id}/messages com `after_seq` igual ao maior `seq` que você já tem.
// @Description
// @Description	| type | Direção | payload | Quando |
// @Description	|---|---|---|---|
// @Description	| `team_chat_typing` | você envia | `{"thread_id": string}` | Você está digitando. Envie no máximo um a cada 3 segundos. Precisa participar da conversa; recusas não têm resposta. |
// @Description	| `team_chat:message` | você recebe | `{"thread_id", "message": TeamChatMessage}` | Mensagem nova, inclusive as suas (para as suas outras abas), as de sistema e as agendadas no momento do envio. |
// @Description	| `team_chat:message_updated` | você recebe | `{"thread_id", "message": TeamChatMessage}` | Uma mensagem foi editada (`edited_at`) ou apagada (`deleted_at`, sem texto). |
// @Description	| `team_chat:read` | você recebe | `{"thread_id", "user_id", "seq"}` | Alguém leu a conversa até `seq` (confirmação de leitura; quando é você, sincroniza as suas abas). |
// @Description	| `team_chat:thread` | você recebe | `{"thread_id"}` | A conversa mudou (criada com você, título, participantes, silenciada): busque GET /team-chat/threads/{id}. |
// @Description	| `team_chat:thread_removed` | você recebe | `{"thread_id"}` | Você saiu ou foi tirado do grupo: remova a conversa da tela. |
// @Description	| `team_chat:typing` | você recebe | `{"thread_id", "user_id"}` | Alguém está digitando. Some sozinho depois de 5 segundos sem um novo aviso. |
// @Description	| `team_chat:scheduled` | você recebe | `{"thread_id", "scheduled": TeamChatScheduled}` | Uma mensagem que você agendou mudou: criada, alterada, cancelada, enviada ou falhou (`failure_reason`). Só para o autor. |
// @Description
// @Description	**TeamChatMessage**: `id`, `thread_id`, `seq` (ordem na conversa, sem buracos), `author_id`, `author_name`, `kind` (`text | system`), `body`, `event`? (só em `system`: `created | renamed | members_added | member_removed | member_left`), `subject_ids` (quem a mensagem de sistema afeta), `reply_to_id`?, `mentions`, `client_id`?, `created_at`, `edited_at`?, `deleted_at`?. **TeamChatScheduled**: `id`, `thread_id`, `body`, `mentions`, `reply_to_id`?, `send_at`, `status` (`pending | sent | canceled | failed`), `failure_reason`?, `sent_message_id`?, `created_at`, `updated_at`.
// @Description
// @Description	## Tipos usados nas mensagens
// @Description
// @Description	**InboxEntry**: `entry_id`, `entry_type`, `campaign_id`?, `campaign_name`?, `lead_id`?, `lead_name`?, `lead_number`?, `lead_version`?, `blocked`, `lead_picture`?, `is_group`?, `lead_metadata`?, `entry_variables`?, `unread_count`, `last_message_preview`?, `last_message_at`, `last_message_type`?, `last_message_sender`?, `last_message_sender_avatar`?, `last_message_sender_kind`? (`contact | human | ai | workflow | campaign | system | external`: quem mandou a última mensagem, para o cliente nomear o remetente no idioma de quem lê; `external` é uma mensagem enviada pela conta fora da plataforma, no app do próprio canal ou no celular; ausente em mensagens antigas sem remetente gravado), `last_message_author`? (groups: name of the member who sent the last message), `last_message_from_us`? (groups: the last message was ours), `last_customer_message_at`? (quando o contato escreveu por último, em todos os canais), `last_agent_message_at`? (quando a equipe, uma IA, um fluxo ou uma campanha escreveu por último), `waiting_since`? (RFC 3339: desde quando o contato espera resposta, a primeira mensagem dele depois da última resposta da equipe, de uma IA ou de um fluxo, inclusive a digitada no celular fora da plataforma; campanha e aviso do sistema não são resposta; ausente quando ninguém espera ou a conversa está finalizada; finalizar a conversa encerra a espera e a próxima mensagem do contato começa outra), `window_open`, `window_expires_at`?, `window_closed_reason`?, `window_closed_detail`? (o mesmo estado da janela que `conversation:subscribed` informa, em todos os canais; sem como confirmar o estado, a linha vem fechada com `channel_unavailable`), `business_phone_id`?, `assigned_user_id`?, `assigned_username`?, `automation_enabled`, `stage`?: Stage, `labels`?: Label[], `available_stages`?: Stage[], `matched_messages`?: MatchedMessage[], `total_matches`?, `latest_analysis`?: Analysis, `analysis_phase`?: `"awaiting" | "queued"`, `live_read`?: LiveRead, `conversation_status`?: `new | ongoing | finished`, `close_source`?: `human | ai | system`, `close_reason`?: `manual | customer_idle | ai_resolved | max_age | workflow`, `closed_at`?, `close_outcome`?, `ai_handler`?: AIHandler.
// @Description
// @Description	**Message** (chaves em camelCase): `id`, `entryId`, `entryType`, `channel`, `messageType`, `direction`?: `INBOUND | OUTBOUND`, `from`, `to`, `text`, `mediaId`?, `mediaType`?, `read`, `readAt`?, `readBy`?, `whatsappMessageId`?, `externalMessageId`?, `replyToMessageId`?, `deliveryStatus`?: `sent | delivered | read | failed`, `senderName`?, `senderAvatar`?, `sentVia`?: `business_app`, `sentBy`: `{"kind": "contact" | "human" | "ai" | "workflow" | "campaign" | "external" | "system", "id": string}`, `metadata`?, `location`?: `{"latitude": number, "longitude": number, "name"?: string, "address"?: string, "candidate": boolean}`, `createdAt`, `updatedAt`. Em snake_case: `transcript`? (transcrição de um áudio recebido; hoje só no WhatsApp oficial; o `text` do áudio continua sendo o marcador `[Áudio]`), `edited_at`? (a mensagem foi editada, pelo contato ou por um atendente; `text` já é o novo texto), `retracted_at`? (a mensagem foi apagada, pelo contato ou por um atendente: `text` vem vazio e `transcript`, `mediaId`, `mediaType`, `media`, `metadata` e `location` não vêm), `reactions`?: Reaction[] (ausente quando não há reações), `sender_user_id`? (o usuário da equipe que enviou a mensagem; ausente quando não foi um atendente), `client_request_id`? (o `request_id` do envio de um atendente, para casar a mensagem com o balão pendente), `resent_as`? (esta mensagem falhou e foi reenviada como a mensagem com esse `id`; esconda esta e mostre a nova).
// @Description
// @Description	`location` aparece em toda mensagem de localização (WhatsApp oficial e coexistência, Telegram), recebida ou enviada, para desenhar o cartão do mapa. `candidate: true` marca a localização que o lead mandou, dentro do Brasil, que pode virar a posição dele por `POST /leads/{id}/location-candidates/{messageId}/accept` (exige `leads:update`; o servidor confere de novo a conversa e o lead).
// @Description
// @Description	Valores de `messageType`: `user_message`, `ai_response`, `tool_call`, `tool_result`, `audio`, `system`, `media`, `operator`, `template`, `call_permission_request`, `call_permission_granted`, `call_permission_rejected`, `call_received`, `call_answered`, `call_missed`, `call_ended`, `story_reply`, `story_mention`, `reaction`, `unsupported`, `post_share`, `sticker`, `link_share`.
// @Description
// @Description	**Stage**: `stage_id`, `name`, `color`?. **Label**: `label_id`, `name`, `color`?.
// @Description
// @Description	**PersonalMark**: `entry_id`, `entry_type`, `pinned_at`? (fixada por você), `unread_marked_at`? (marcada por você como não lida). Só as marcas de quem pediu.
// @Description
// @Description	**Note** (nota interna, só da equipe; nunca vai para o contato, a IA, a análise, os fluxos, as exportações ou o chat do site): `id`, `entry_id`, `entry_type`, `author_id`, `author_name`, `body`, `mentions`: string[] (IDs de usuários mencionados; lista vazia quando não há), `created_at`. A mesma forma de GET /conversations/{entryType}/{entryId}/notes.
// @Description
// @Description	**Reaction**: `emoji`, `direction`: `inbound | outbound` (`inbound` é do contato, `outbound` é nossa), `actor_id`? (quem reagiu: o identificador do contato no canal ou o usuário da equipe), `at`. Há no máximo uma reação de cada contato e uma só nossa (`outbound`), porque a conta da empresa reage uma vez por mensagem: uma reação nova substitui a anterior do mesmo lado e remover a reação a tira da lista. Na reação `outbound`, `actor_id` é o atendente que reagiu pelo sistema; reações feitas direto no celular chegam sem `actor_id`. Mensagens antigas do Instagram e do Facebook com a reação guardada no formato anterior chegam na mesma forma.
// @Description
// @Description	**MatchedMessage**: `message_id`, `text`, `from`, `message_type`, `channel`, `created_at`, `position`, `page`.
// @Description
// @Description	**AIHandler**: `kind`, `agent_id`?, `agent_name`?, `agent_avatar`?, `agent_active`, `workflow_id`?, `workflow_name`?, `workflow_run_id`?, `run_status`?, `current_node_id`?, `current_node_type`?.
// @Description
// @Description	**AdOrigin** (camelCase): o primeiro anúncio que trouxe o contato. `adId`?, `platform`?: `facebook | instagram`, `title`?, `sourceUrl`?, `image`?: `{"url", "mimeType"?, "layout": {"width"?, "height"?, "thumbhash"?}}` (cópia guardada pela plataforma), `arrivedAt`.
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
// @Description	`conversation:error` traz `{"code": string, "message": string}`; recusas de `reopen_window`, `react_message`, `edit_message`, `delete_message`, `resend_message`, `pin_conversation`, `mark_unread` e `clear_unread_mark` também trazem `request_id`, `entry_id` e `entry_type`; em mudanças de status, também `entry_id`, `entry_type`, `status`, `previous_status` e `outcomes` (`[{"code", "label", "isDurable", "position"}]`).
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
// @Description	| `invalid_sort`, `invalid_personal`, `invalid_label` | `search_inbox` com `sort`, `personal` ou `label_id` inválido. |
// @Description	| `invalid_campaign_type`, `invalid_container_kind`, `invalid_whatsapp_campaign_type` | Filtro de visão inválido. |
// @Description	| `not_found` | Campanha não encontrada, ou mensagem que não existe ou não é dessa conversa. |
// @Description	| `not_configured` | O recurso não está disponível neste servidor. |
// @Description	| `not_allowed` | Ações sobre mensagens: falta `conversations:send` ou acesso à conversa, a mensagem não é sua, já foi apagada ou não pode receber essa ação (por exemplo, editar uma mídia ou reenviar uma mensagem que não falhou). Marcas pessoais: falta `conversations:read` ou acesso à conversa. |
// @Description	| `invalid` | Ações sobre mensagens: falta `entry_id`, `entry_type`, `message_id`, o texto da edição ou o `request_id` do reenvio, ou o `request_id` passa de 128 caracteres. Marcas pessoais: falta `entry_id`, `entry_type` ou `pinned`. |
// @Description	| `unsupported` | O canal da conversa não tem essa ação. Em marcas pessoais: o servidor não oferece marcas pessoais. |
// @Description	| `window_expired` | A janela do canal para essa ação já passou, ou a janela de atendimento está fechada. |
// @Description	| `failed` | O canal recusou a ação ou ela não pôde ser gravada; nada mudou na conversa. Em marcas pessoais: a marca não pôde ser gravada. |
// @Description	| `outcome_required` | Escolha um desfecho (lista em `outcomes`) para finalizar. |
// @Description	| `outcome_unknown` | O desfecho não existe no workspace (lista em `outcomes`). |
// @Description	| `history_error`, `inbox_error`, `search_error`, `fetch_error`, `template_send_failed`, `assign_failed`, `internal_error` | Falha ao processar o pedido; tente de novo. |
// @Description	| `lead_opted_out`, `lead_blocked`, `within_spam_window`, `window_already_open`, `insufficient_balance`, `monthly_send_cap_reached`, `pricing_unavailable`, `quote_out_of_range`, `template_not_sendable`, `template_phone_mismatch`, `invalid_phone`, `phone_not_connected`, `send_in_progress`, `idempotency_key_required`, `billing_unavailable`, `conversation_not_found` | Recusa de `reopen_window`, com `request_id` (o mesmo do pedido), `entry_id` e `entry_type`: o mesmo código que POST /whatsapp/outreach/conversations responde para a mesma recusa (inclusive `forbidden` e `not_found` para número ou modelo). Uma falha sem motivo conhecido chega como `template_send_failed`. |
// @Description	| `send_outcome_unknown` | `reopen_window` sem resposta do WhatsApp: o modelo pode ter sido entregue e cobrado. Confira a conversa antes de enviar de novo; não repita automaticamente. Traz `request_id`, `entry_id` e `entry_type`. |
// @Description
// @Description	Códigos de `conversation:message_error`: `unauthorized`, `not_configured`, `send_failed`, `internal_error`, `media_not_sendable` (a mídia não é um envio desta conversa nem da biblioteca do workspace, ou é imagem de nota interna; a mesma resposta vale para uma mídia que não existe) e, no e-mail, `email_balance_insufficient` (o saldo da carteira não cobre o envio), `email_attachment_blocked`, `email_attachment_too_large`, `email_domain_checking` (o domínio do destinatário ainda está sendo verificado; nada foi cobrado, tente de novo em instantes), `email_domain_unknown`, `email_disposable_address` e `email_no_mail_domain` (a higiene de lista recusou o destinatário; nada foi cobrado).
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
