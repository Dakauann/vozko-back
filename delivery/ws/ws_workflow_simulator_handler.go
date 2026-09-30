package ws

import (
	"log"
	"net/http"
	"strings"

	"github.com/gorilla/mux"

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
// @Description	## Mensagens que você envia
// @Description
// @Description	Formato: `{"type": "...", "data": {...}}`.
// @Description
// @Description	### Fluxos de mensagens
// @Description
// @Description	| type | data | O que faz |
// @Description	|---|---|---|
// @Description	| `reply` | `{"text": string}` (obrigatório) | Mensagem do contato. Dispara o fluxo depois de `waiting_trigger` ou responde um `waiting_reply`. Em mensagens interativas, o texto é usado como a opção escolhida. |
// @Description	| `set_variable` | `{"key": string, "value": any}` | Grava uma variável no estado da execução antes do próximo passo. |
// @Description	| `cancel` | sem `data` | Cancela a execução. O servidor responde `run_cancelled`. |
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
// @Description	| `waiting_trigger` | `{"triggerType": string}` | Só em fluxos disparados por mensagem: envie um `reply` para começar. |
// @Description	| `sim_started` | `{"runId": string, "workflowId": string}` | A execução começou. |
// @Description	| `state_update` | `{"vars": object}` | Antes e depois de cada passo, com todas as variáveis. |
// @Description	| `node_executed` | `{"nodeId": string, "nodeType": string, "output"?: object, "error"?: string}` | Um nó terminou. |
// @Description	| `message_sent` | `{"direction": "outbound", "text": string, "msgType": string, "nodeId"?: string, "messageId": string, "audioBase64"?: string, "audioMime"?: string, "audioUrl"?: string}` | O fluxo enviou uma mensagem ou, em voz, tocou um áudio (`msgType: "audio"` com `audioUrl`). |
// @Description	| `waiting_reply` | `{"nodeId": string, "timeoutSeconds": number}` | O fluxo espera uma resposta do contato. |
// @Description	| `wait_skipped` | `{"nodeId": string, "reason": "duration" \| "event"}` | Uma espera por tempo ou evento foi pulada para agilizar o teste. |
// @Description	| `waiting_key` | `{"timeoutSeconds": number}` | Voz: o fluxo espera uma tecla. |
// @Description	| `call_transferred` | `{"queueId": string, "queueName": string, "notes"?: string}` | Voz: a ligação foi para a fila. Na simulação ela é considerada atendida e o fluxo termina. |
// @Description	| `run_completed` | `{"vars": object}` | A execução terminou. |
// @Description	| `run_cancelled` | sem `payload` | A execução foi cancelada. |
// @Description	| `run_error` | `{"error": string, "status"?: "error" \| "cancelled"}` | A execução parou com erro. |
// @Description	| `error` | `{"error": string}` | Não foi possível simular (fluxo inexistente, de outro workspace ou sem gatilho). A conexão é encerrada. |
// @Description
// @Description	Valores de `msgType` em fluxos de mensagens: `text`, `button`, `list`, `media`, `audio`, `template` e `call_permission_request`.
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

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[workflow-sim] failed to upgrade connection: %v", err)
		return
	}
	defer conn.Close()

	h.wsMetrics.IncWSConnections(metrics.WSEndpointWorkflowSimulator)
	defer h.wsMetrics.DecWSConnections(metrics.WSEndpointWorkflowSimulator)

	log.Printf("[workflow-sim] starting session for workflow=%s workspace=%s", workflowID, workspaceID)

	if err := h.useCase.HandleSession(r.Context(), conn, workflowID, workspaceID); err != nil {
		log.Printf("[workflow-sim] session ended with error: %v", err)
	}
}
