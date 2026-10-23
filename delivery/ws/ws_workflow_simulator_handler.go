package ws

import (
	"log"
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/domain/metrics"
	"vozko/infra/http/middleware"
	workflow_usecase "vozko/usecases/workflow"
)

type WSWorkflowSimulatorHandler struct {
	useCase   workflow_usecase.WSWorkflowSimulationUseCase
	wsMetrics metrics.WSMetricsRecorder
}

func NewWSWorkflowSimulatorHandler(useCase workflow_usecase.WSWorkflowSimulationUseCase, wsMetrics metrics.WSMetricsRecorder) *WSWorkflowSimulatorHandler {
	if useCase == nil || wsMetrics == nil {
		return nil
	}
	return &WSWorkflowSimulatorHandler{useCase: useCase, wsMetrics: wsMetrics}
}

// @Summary		WebSocket do simulador de fluxos
// @Description	Executa um fluxo em modo de teste, sem enviar mensagens nem fazer ligações de verdade. Você faz o papel do contato: responde mensagens, aperta teclas e acompanha cada nó executado.
// @Description
// @Description	## Conectar
// @Description
// @Description	```text
// @Description	wss://SUA_URL_BASE/ws/workflows/{id}/simulate?token=SEU_ACCESS_TOKEN&workspace_id=SEU_WORKSPACE_ID
// @Description	```
// @Description
// @Description	- `id`: ID do fluxo.
// @Description	- Permissão: `workflows:update` no workspace.
// @Description
// @Description	O modo depende do tipo do fluxo: fluxos `voice` simulam uma ligação; os demais simulam uma conversa de mensagens.
// @Description
// @Description	- `await_start=1` (opcional): o servidor envia `sim_ready` e só começa depois de um `start` ou de um `reproduce_run`. Use para ligar pontos de parada e confirmar chamadas reais antes do primeiro passo. Sem ele, a execução começa logo pelo gatilho principal, como um `start` sem `data` (ou depois do `reply` inicial em fluxos disparados por mensagem). Se esse gatilho exige campos de entrada, nada roda: chega `command_refused` com `code: "trigger_input_invalid"` e `issues`, e a sessão espera um `start` (ou `reproduce_run`) como com `await_start=1`, sem `sim_ready`.
// @Description
// @Description	## Depuração
// @Description
// @Description	- O simulador usa o rascunho. Um nó com `pinned` (saída fixada) devolve essa saída e segue a conexão fixada, sem executar; a etapa vem com `pinned: true`.
// @Description	- Chamadas reais: nós de requisição HTTP sem saída fixada, nós de código que usam `fetch`, e-mail e agenda (agendar, reagendar, consultar disponibilidade) chamam serviços externos de verdade. Eles aparecem em `realCallNodes` e só executam depois de `confirm_real_calls` (vale para a sessão inteira). Sem a confirmação o nó falha com `errorKind: "real_call_not_confirmed"`. Envios de mensagem já são simulados.
// @Description	- Pontos de parada ficam só nesta sessão (não são gravados no fluxo). A pausa acontece antes de executar o nó. Uma pausa sem comando por 10 minutos encerra com `run_cancelled` e `reason: "paused_timeout"`.
// @Description	- Saída fixada de produção (`source: "run:<id>"`) só vale para quem pode ver a conversa daquela execução (mesma regra da caixa de entrada). Para os demais, o nó roda como se não tivesse saída fixada (inclusive a regra de chamadas reais) e a etapa vem com `notice: "saída fixada indisponível para você"`.
// @Description	- Nós que mexem no CRM (etiqueta, etapa, atribuir membro, transferir departamento, oportunidade e campos do lead) não gravam nada: conferem o alvo no workspace (etiqueta, etapa, membro, departamento, funil ou campo) e devolvem o que fariam. A etapa vem com `simulated: true` e `output.would_do`: lista de `{"kind": "label" \| "stage" \| "member" \| "department" \| "deal" \| "lead_field", "action": string, "target_id"?: string, "target_name"?: string, "field"?: string, "value"?: any, "description": string}`. Um alvo que não existe (ou é de outro workspace) faz o nó falhar pela saída de erro, sem `would_do`.
// @Description	- Cada etapa que enviou mensagens traz, em `output`, a lista `sent_messages` com as mesmas mensagens de `message_sent`, no formato `message` descrito abaixo.
// @Description	- Reproduzir uma execução de produção (`reproduce_run`) exige `workflows:read` e poder ver a conversa da execução (mesma regra da caixa de entrada). O teste começa com os dados do gatilho daquela execução, fixa a saída e a conexão gravadas de cada nó HTTP, de código ou com efeito externo que rodou nela (só nesta sessão, sem gravar no rascunho) e pausa antes do primeiro passo. Nós que não rodaram na execução seguem a regra de chamadas reais. Valores mascarados na execução continuam mascarados. Só para fluxos de mensagens.
// @Description
// @Description	## Entrada do gatilho
// @Description
// @Description	Cada item de `sim_ready.triggers` (TriggerInputSpec) é `{"nodeId": string, "triggerType": string, "label": string, "shape": "fields" | "json" | "message" | "none", "fields": [TriggerInputField], "sample": object | null, "readKeys": [string], "issues": [{"code": string, "key": string}], "window": "open" | "closed" | null, "windowEditable": boolean, "acceptedEvents": [string]}`. `label` é o nome dado ao nó (vazio sem nome). `acceptedEvents` lista os eventos que o webhook aceita, normalizados como a produção compara (minúsculas, sem espaços nas pontas); vazia quando o webhook aceita qualquer evento e em todo gatilho que não é webhook. `window` é a janela de atendimento de 24 h com que o gatilho costuma começar em produção: `open` para mensagens recebidas, chamado por agente e formulário respondido; `closed` para webhook, aniversário e entrada em etapa; `null` quando o gatilho não tem janela (ligação). `windowEditable` diz se o teste pode escolher a janela: `false` para mensagem recebida e primeira mensagem (a mensagem do próprio cliente abre a janela, como em produção) e para ligação; `true` para webhook, aniversário, entrada em etapa, formulário respondido e chamado por agente. Durante o teste, toda mensagem do cliente (a mensagem inicial, respostas, seleções, mídia, localização e formulário respondido) abre a janela pela mesma regra de produção (24 h desde a última mensagem do cliente). TriggerInputField é `{"key": string, "type": "text" | "number" | "boolean" | "object" | "list", "required": boolean, "description": string, "example": string, "options"?: [{"value": string, "label": string}], "readBy": [string]}`; `readBy` lista os nós que leem o campo e `readKeys` as chaves lidas pelo fluxo.
// @Description
// @Description	- `fields`: `input` é um objeto plano por `key`. Chamado por agente IA: os campos declarados mais `agent.name` (vazio vira "Agente de teste"); formulário: `form.<chave>` como a produção grava as respostas, mais `form_id` quando o gatilho aceita mais de um formulário; etapa: `stage_id` (uma das `options`); aniversário: `birthday` (AAAA-MM-DD, vazio usa hoje).
// @Description	- `json` (webhook): `input` é o corpo; `sample` é um corpo com os caminhos que o fluxo lê e o primeiro evento aceito em `event`.
// @Description	- `message`: `input` é ignorado; chega `waiting_trigger` e o primeiro `reply` começa a execução.
// @Description	- `none`: `input` é ignorado e a execução começa direto.
// @Description
// @Description	A entrada é validada como em produção e a execução é semeada pelo mesmo construtor de dados do gatilho. Códigos em `issues`: `required` (campo obrigatório vazio; o agente também não consegue chamar sem ele), `wrong_type`, `unknown_field`, `event_not_accepted` (o webhook filtra eventos), `unknown_option`, `too_large` (mais de 64 KB). Em `triggers[].issues`, `read_not_declared` aponta uma chave lida pelo fluxo que o gatilho não declara (sempre vazia em produção); ela nunca bloqueia o início. `trigger_input_unavailable` aparece quando os formulários ou as etapas do gatilho não puderam ser lidos.
// @Description
// @Description	## Mensagens que você envia
// @Description
// @Description	Formato: `{"type": "...", "data": {...}}`.
// @Description
// @Description	### Fluxos de mensagens
// @Description
// @Description	| type | data | O que faz |
// @Description	|---|---|---|
// @Description	| `reply` | `{"text": string}` (obrigatório) | Mensagem de texto do contato. Dispara o fluxo depois de `waiting_trigger` ou responde um `waiting_reply`. Em mensagens interativas, o texto ainda é usado como o id da opção escolhida (compatibilidade); prefira `reply_selection`. |
// @Description	| `reply_selection` | `{"kind": "button" \| "list", "id": string, "title": string}` (todos obrigatórios) | O contato tocou um botão ou uma linha de lista, como o webhook entrega: `message` = `title`, `message_type: "text"`, `selected_option_id`, `selected_option_title` e `selected_option_type` (`button` ou `list`). O ramo segue a saída com o id da opção, senão `no_match`. |
// @Description	| `reply_location` | `{"latitude": number, "longitude": number, "name"?: string, "address"?: string}` | O contato enviou uma localização: `message` = "📍 nome, endereço (lat, lng)" e `message_type: "text"`, como no webhook. |
// @Description	| `reply_media` | `{"kind": "image" \| "video" \| "audio" \| "document" \| "sticker", "url": string, "mime"?: string, "filename"?: string, "caption"?: string}` | O contato enviou uma mídia: `message_type: "media"` e `media_type` (áudio: `message_type: "audio"`). `message` é a legenda (imagem, vídeo), o nome do arquivo (documento), vazio (figurinha) ou a legenda como transcrição (áudio). |
// @Description	| `set_variable` | `{"key": string, "value": any}` | Grava uma variável no estado da execução antes do próximo passo. |
// @Description	| `form_reply` | `{"answers": {chave_do_campo: valor}}` ou `{"outcome": "no_reply"}` | Responde um `waiting_form`: as respostas ficam nas variáveis `form.<chave>`; `no_reply` segue a saída Sem resposta. Um `reply` de texto não conclui o formulário. |
// @Description	| `cancel` | sem `data` | Cancela a execução. O servidor responde `run_cancelled`. |
// @Description
// @Description	### Depuração (mensagens e voz)
// @Description
// @Description	| type | data | O que faz |
// @Description	|---|---|---|
// @Description	| `start` | sem `data` ou `{"triggerNodeId"?: string, "input"?: object, "windowOpen"?: boolean}` | Com `await_start=1` (ou depois da recusa do início automático sem ele): começa o teste depois do `sim_ready`, pelo gatilho `triggerNodeId` (sem ele, o `defaultTriggerNodeId`) com a entrada `input`, validada pelas regras de produção do gatilho (veja Entrada do gatilho). `windowOpen` escolhe a janela de atendimento com que o teste começa quando o gatilho tem `windowEditable: true`; sem ele vale o `window` do gatilho, e com `windowEditable: false` ele é ignorado (a mensagem do cliente decide). Com a janela fechada, todo envio livre (texto, mídia, botões, formulário, resposta do agente de IA) falha como em produção, com `errorKind: "window_closed"` na etapa, sem novas tentativas (recusa permanente, como contato bloqueado, descadastrado ou número inválido); só templates saem. Se for recusado, chega `command_refused` com `code` e a sessão continua esperando. |
// @Description	| `reproduce_run` | `{"runId": string}` | Com `await_start=1` (ou durante `waiting_trigger`): começa o teste reproduzindo a execução. Se for recusado, chega `command_refused` e a sessão continua esperando. |
// @Description	| `set_breakpoints` | `{"nodeIds": [string]}` | Troca os pontos de parada da sessão. A execução pausa antes de cada um desses nós. |
// @Description	| `run_until` | `{"nodeId": string}` | Para uma vez antes desse nó. Se estiver pausado, continua até ele. |
// @Description	| `step` | sem `data` | Pausado: executa um nó e pausa antes do próximo. |
// @Description	| `continue` | sem `data` | Pausado: segue até o próximo ponto de parada. |
// @Description	| `confirm_real_calls` | sem `data` | Libera as chamadas reais pelo resto da sessão. Pode ser enviado a qualquer momento, inclusive antes do `start`. |
// @Description
// @Description	### Fluxos de voz
// @Description
// @Description	| type | data | O que faz |
// @Description	|---|---|---|
// @Description	| `key` | `{"key": string}` | Tecla do telefone: um caractere entre `0-9`, `*` e `#`. |
// @Description	| `cancel` | sem `data` | Desliga a ligação simulada. O servidor responde `run_cancelled`. |
// @Description
// @Description	Fechar a conexão tem o mesmo efeito de `cancel`.
// @Description
// @Description	## Mensagens que você recebe
// @Description
// @Description	Formato: `{"type": "...", "payload": {...}}`.
// @Description
// @Description	| type | payload | Quando |
// @Description	|---|---|---|
// @Description	| `sim_ready` | `{"workflowId": string, "triggerType": string, "realCallNodes": [string], "triggers": [TriggerInputSpec], "defaultTriggerNodeId": string}` | Só com `await_start=1`: a sessão espera `start` ou `reproduce_run`. `triggers` descreve cada gatilho do rascunho (o principal primeiro) e o que ele recebe. |
// @Description	| `waiting_trigger` | `{"triggerType": string, "nodeId": string}` | O gatilho escolhido é de mensagem (`shape: "message"`): envie um `reply` para começar. |
// @Description	| `sim_started` | `{"runId": string, "workflowId": string, "realCallNodes": [string], "trigger": {"nodeId": string, "triggerType": string}, "reproducing"?: {"runId": string, "workflowVersion"?: number, "pinnedNodes": [string]}}` | A execução começou pelo gatilho `trigger` (ao reproduzir, o gatilho da execução quando ele ainda existe no rascunho). `realCallNodes` lista os nós que fariam chamadas reais; `reproducing` aparece ao reproduzir uma execução, com os nós fixados nesta sessão. |
// @Description	| `state_update` | `{"vars": object}` | Antes e depois de cada passo, com todas as variáveis. |
// @Description	| `node_executed` | Registro da etapa, o mesmo de `GET /workflow-runs/{runId}` (`id`, `runId`, `nodeId`, `nodeType`, `status`, `input`?, `output`?, `error`?, `executedAt`, `attempt`, `startedAt`?, `finishedAt`?, `durationMs`, `branch`?, `errorKind`?, `errorChain`?, `stack`?, `scriptLogs`?, `http`?), com valores sensíveis mascarados, mais `stepIndex` (número da etapa na sessão, a partir de 0), `pinned`? (`true` quando veio de uma saída fixada), `simulated`? (`true` quando um nó de CRM só descreveu o que faria, em `output.would_do`), `notice`? (por exemplo a saída fixada indisponível) e, quando o nó enviou mensagens, `output.sent_messages` | Um nó terminou. |
// @Description	| `paused` | `{"nodeId": string, "nodeType": string, "stepIndex": number, "reason": "breakpoint" \| "run_until" \| "step", "state": object}` | A execução parou antes desse nó. `stepIndex` é o número que a próxima etapa terá; `state` são as variáveis naquele momento, mascaradas. Responda `step`, `continue`, `run_until` ou `cancel`. |
// @Description	| `command_refused` | `{"command": string, "error": string, "code"?: "trigger_input_invalid" | "trigger_not_found" | "trigger_input_unavailable", "issues"?: [{"code": string, "key": string}]}` | Um comando foi recusado (por exemplo `reproduce_run` sem permissão, execução invisível, de outro fluxo ou sem dados do gatilho, ou um `start` com entrada inválida). `issues` vem com `trigger_input_invalid`. A sessão continua. |
// @Description	| `message_sent` | `{"direction": "outbound", "text": string, "msgType": string, "nodeId"?: string, "nodeName"?: string, "messageId": string, "audioBase64"?: string, "audioMime"?: string, "audioUrl"?: string, "message"?: object}` | O fluxo enviou uma mensagem ou, em voz, tocou um áudio (`msgType: "audio"` com `audioUrl`). Em fluxos de mensagens, `nodeId` e `nodeName` (o nome dado ao nó, senão o nome do tipo no catálogo) são do nó que enviou, e a mensagem chega antes do `node_executed` desse nó. Em fluxos de mensagens, `message` é a mensagem como a conversa do CRM a guardaria (formato abaixo); `text` continua com o resumo antigo. |
// @Description	| `message_received` | `{"direction": "inbound", "message": object}` | Uma resposta sua (`reply`, `reply_selection`, `reply_location`, `reply_media`) como a conversa do CRM a guardaria. |
// @Description	| `reply_ignored` | `{"nodeId": string, "reason": "interactive_reply_unhandled"}` | A mensagem interativa não tem saída para essa resposta (nem `no_match`, nem saída padrão). Como em produção, o fluxo continua esperando. |
// @Description	| `waiting_reply` | `{"nodeId": string, "timeoutSeconds": number}` | O fluxo espera uma resposta do contato. |
// @Description	| `waiting_form` | `{"nodeId": string, "formId": string, "timeoutSeconds": number}` | O fluxo enviou um formulário do WhatsApp e espera as respostas: envie um `form_reply`. |
// @Description	| `wait_skipped` | `{"nodeId": string, "reason": "duration" \| "event"}` | Uma espera por tempo ou evento foi pulada para agilizar o teste. |
// @Description	| `waiting_key` | `{"timeoutSeconds": number}` | Voz: o fluxo espera uma tecla. |
// @Description	| `call_transferred` | `{"queueId": string, "queueName": string, "notes"?: string}` | Voz: a ligação foi para a fila. Na simulação ela é considerada atendida e o fluxo termina. |
// @Description	| `run_completed` | `{"vars": object}` | A execução terminou. |
// @Description	| `run_cancelled` | `{"reason": "cancelled" \| "paused_timeout"}` | A execução foi cancelada: pelo `cancel` (ou pela conexão fechada) ou por uma pausa abandonada por 10 minutos. |
// @Description	| `run_error` | `{"error": string, "status"?: "error" \| "cancelled"}` | A execução parou com erro. |
// @Description	| `error` | `{"error": string}` | Não foi possível simular (fluxo inexistente, de outro workspace ou sem gatilho). A conexão é encerrada. |
// @Description
// @Description	Valores de `msgType` em fluxos de mensagens: `text`, `button`, `list`, `media`, `audio`, `template`, `form`, `flow` e `call_permission_request`.
// @Description
// @Description	### Formato de `message`
// @Description
// @Description	O mesmo das mensagens de `GET /conversations/...` (o CRM renderiza com os mesmos componentes): `id` (igual a `messageId`), `entryId`, `entryType`, `channel`, `messageType` (`ai_response`, `media`, `template`, `user_message`, `audio`), `direction` (`OUTBOUND` ou `INBOUND`), `sentBy`, `from`, `to`, `text`, `deliveryStatus` (`sent`), `createdAt` e, quando houver:
// @Description
// @Description	- `mediaType` (`image`, `video`, `audio`, `document`, `sticker`) e `media`: `{"url": string, "mimeType"?: string, "filename"?: string, "size"?: number, "layout": object}`; a legenda fica em `text`.
// @Description	- `metadata.buttons`: `{"header_text"?: string, "body_text": string, "footer_text"?: string, "buttons": [{"id": string, "title": string}]}` (botões e também as linhas de uma lista, achatadas).
// @Description	- `metadata.list`: `{"header_text"?: string, "body_text": string, "footer_text"?: string, "button_text": string, "sections": [{"title"?: string, "rows": [{"id": string, "title": string, "description"?: string}]}]}`.
// @Description	- Template (`messageType: "template"`): `metadata` com `template_name`, `language`, `category`, `body_text` (corpo com os parâmetros), `components` (com os botões) e `header_media_url`?; `text` é o corpo renderizado.
// @Description	- `metadata.form`: `{"kind": "message", "formId": string, "formName": string, "sendId": string, "cta": string}` (formulário do WhatsApp; `formName` vem vazio no teste).
// @Description	- `location`: `{"latitude": number, "longitude": number, "name"?: string, "address"?: string, "candidate": boolean}`.
// @Description
// @Description	As respostas segmentadas do nó de IA chegam como um `message_sent` por segmento.
// @Description
// @Description	A sessão termina depois de `run_completed`, `run_cancelled`, `run_error` ou `error`.
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
// @Param			id		path	string	true	"ID do fluxo"
// @Param			token		query	string	false	"Access token (alternativa ao cabeçalho Authorization e ao cookie accessToken)"
// @Param			workspace_id	query	string	false	"Workspace usado na checagem de permissão da conexão"
// @Param			await_start	query	string	false	"1 para esperar start ou reproduce_run antes de começar"
// @Success		101	{string}	string	"Switching Protocols: a conexão vira WebSocket"
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ws/workflows/{id}/simulate [get]
func (h *WSWorkflowSimulatorHandler) HandleSimulate(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.useCase == nil {
		http.Error(w, "workflow simulator not configured", http.StatusNotImplemented)
		return
	}

	vars := mux.Vars(r)
	workflowID := strings.TrimSpace(vars["id"])
	if workflowID == "" {
		http.Error(w, "workflow id is required", http.StatusBadRequest)
		return
	}

	workspaceID := middleware.GetWorkspaceID(r)
	if workspaceID == "" {
		http.Error(w, "workspace id is required", http.StatusUnauthorized)
		return
	}
	viewer, ok := httpx.RequirePerson(w, r)
	if !ok {
		return
	}
	session := workflow_usecase.SimulationSession{
		WorkflowID:  workflowID,
		WorkspaceID: workspaceID,
		Viewer:      viewer,
		AwaitStart:  r.URL.Query().Get("await_start") == "1",
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[workflow-sim] failed to upgrade connection: %v", err)
		return
	}
	defer conn.Close()

	h.wsMetrics.IncWSConnections(metrics.WSEndpointWorkflowSimulator)
	defer h.wsMetrics.DecWSConnections(metrics.WSEndpointWorkflowSimulator)

	log.Printf("[workflow-sim] starting session for workflow=%s workspace=%s", workflowID, workspaceID)

	if err := h.useCase.HandleSession(r.Context(), conn, session); err != nil {
		log.Printf("[workflow-sim] session ended with error: %v", err)
	}
}
