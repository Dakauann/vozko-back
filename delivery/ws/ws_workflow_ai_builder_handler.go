package ws

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"

	"vozko/domain/metrics"
	"vozko/infra/http/middleware"
	workflow_usecase "vozko/usecases/workflow"
)

type BuilderRecorderFactory func(workspaceID, workflowID string) *workflow_usecase.BuilderSessionRecorder

type WSWorkflowAIBuilderHandler struct {
	useCase     workflow_usecase.AIBuilderUseCase
	wsMetrics   metrics.WSMetricsRecorder
	newRecorder BuilderRecorderFactory
}

func NewWSWorkflowAIBuilderHandler(useCase workflow_usecase.AIBuilderUseCase, wsMetrics metrics.WSMetricsRecorder, newRecorder BuilderRecorderFactory) *WSWorkflowAIBuilderHandler {
	if useCase == nil || wsMetrics == nil {
		return nil
	}
	return &WSWorkflowAIBuilderHandler{useCase: useCase, wsMetrics: wsMetrics, newRecorder: newRecorder}
}

// @Summary		WebSocket do construtor de fluxos com IA (novo fluxo)
// @Description	Conversa com o assistente que monta e corrige fluxos. Você descreve o que quer; o assistente adiciona nós, liga as saídas e valida o fluxo, enviando cada passo em tempo real.
// @Description
// @Description	## Conectar
// @Description
// @Description	```text
// @Description	wss://SUA_URL_BASE/ws/workflows/{id}/ai-builder?token=SEU_ACCESS_TOKEN&workspace_id=SEU_WORKSPACE_ID
// @Description	wss://SUA_URL_BASE/ws/workflows/ai-builder?token=SEU_ACCESS_TOKEN&workspace_id=SEU_WORKSPACE_ID
// @Description	```
// @Description
// @Description	- Com `id`: edita um fluxo existente. Permissão `workflows:update`.
// @Description	- Sem `id`: cria um fluxo novo, começando vazio e do tipo `messages`. Permissão `workflows:create`.
// @Description
// @Description	## Limites
// @Description
// @Description	| Limite | Valor | Ao atingir |
// @Description	|---|---|---|
// @Description	| Sessões simultâneas por workspace | 3 | `error` e a conexão é encerrada. |
// @Description	| Tempo por pedido | 10 minutos | `done` com o motivo em `summary`; a conexão continua aberta. |
// @Description	| Tokens por sessão | 1.000.000 | `done`; novos pedidos na mesma conexão recebem o mesmo `done`. |
// @Description	| Saldo | precisa ter saldo | `done` com `valid: false` e o motivo em `summary`. |
// @Description
// @Description	O uso do assistente é cobrado do saldo do workspace.
// @Description
// @Description	## Mensagens que você envia
// @Description
// @Description	Formato: `{"type": "...", "data": {...}}`.
// @Description
// @Description	| type | data | O que faz |
// @Description	|---|---|---|
// @Description	| `user_prompt` | `{"text": string, "graph"?: Graph}` | Pede uma alteração. Se `graph` vier, ele substitui o fluxo atual antes do pedido. Pedidos enviados durante outro pedido entram na fila. |
// @Description	| `set_workflow_type` | `{"type": "messages" \| "voice"}` | Troca o tipo do fluxo. Responde com `graph_snapshot`. |
// @Description	| `set_model` | `{"model": string}` | Escolhe o modelo de IA dos próximos pedidos. |
// @Description	| `hydrate_graph` | `{"graph": Graph}` | Substitui o fluxo (por exemplo, depois de edições manuais) e responde com `graph_snapshot`. |
// @Description	| `cancel` | sem `data` | Interrompe o pedido em andamento, que termina com `done` e `summary: "cancelado"`. |
// @Description
// @Description	## Mensagens que você recebe
// @Description
// @Description	Formato: `{"type": "...", "payload": {...}}`.
// @Description
// @Description	| type | payload | Quando |
// @Description	|---|---|---|
// @Description	| `builder_ready` | `{"workflowType": "messages" \| "voice", "nodeCount": number, "mode": "create" \| "edit", "model": string}` | Logo após conectar. |
// @Description	| `graph_snapshot` | `{"graph": Graph, "issues": LintIssue[], "valid": boolean}` | Após conectar, após trocar o tipo ou o fluxo, e a cada passo do assistente. |
// @Description	| `iteration` | `{"n": number, "max": number, "tokensUsed": number, "tokenBudget": number}` | Início de cada passo do assistente. |
// @Description	| `reasoning_delta` | `{"text": string}` | Trecho do raciocínio do assistente. |
// @Description	| `reasoning_done` | sem `payload` | Fim do raciocínio do passo. |
// @Description	| `assistant_delta` | `{"text": string}` | Trecho da resposta do assistente. |
// @Description	| `assistant_done` | `{"tools": number}` | Fim da resposta; `tools` é o número de ações do passo. |
// @Description	| `tool` | `{"name": string, "summary": string, "ok": boolean}` | Cada ação no fluxo. `ok: false` traz o motivo em `summary`. |
// @Description	| `resource_resolved` | `{"kind": string, "query": string, "matches": [{"id": string, "name": string}]}` | O assistente buscou um recurso do workspace (agentes, mídias, filas, troncos...). |
// @Description	| `meta` | `{"name": string, "description": string, "workflowType": string}` | O assistente definiu nome, descrição ou tipo do fluxo. |
// @Description	| `idle` | `{"valid": boolean}` | O assistente respondeu sem alterar o fluxo e espera o próximo pedido. |
// @Description	| `done` | `{"valid": boolean, "summary": string, "residualIssues"?: LintIssue[]}` | O pedido terminou: o fluxo foi validado, um limite foi atingido, foi cancelado ou houve erro do provedor de IA. |
// @Description	| `error` | `{"error": string}` | Não foi possível abrir a sessão (limite de sessões, fluxo inexistente ou de outro workspace). A conexão é encerrada. |
// @Description
// @Description	Valores de `tool.name`: `get_node_spec`, `find_resource`, `add_node`, `connect`, `update_node`, `remove_node`, `remove_edge`, `set_meta` e `finish`.
// @Description
// @Description	## Tipos usados nas mensagens
// @Description
// @Description	```json
// @Description	{
// @Description	  "Graph": {"nodes": ["Node"], "edges": ["Edge"]},
// @Description	  "Node": {"id": "string", "type": "string", "position": {"x": 0, "y": 0}, "config": {}},
// @Description	  "Edge": {"source": "string", "target": "string", "label": "string (opcional)"},
// @Description	  "LintIssue": {
// @Description	    "code": "string",
// @Description	    "severity": "blocking | advisory",
// @Description	    "nodeId": "string (opcional)",
// @Description	    "field": "string (opcional)",
// @Description	    "edgeRef": "string (opcional)",
// @Description	    "message": "string",
// @Description	    "hint": "string (opcional)"
// @Description	  }
// @Description	}
// @Description	```
// @Description
// @Description	Os tipos de nó e os campos de `config` são os de `GET /workflows/node-types`. Problemas `blocking` impedem ativar o fluxo; `advisory` são sugestões.
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
// @Success		101	{string}	string	"Switching Protocols: a conexão vira WebSocket"
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ws/workflows/ai-builder [get]
func (h *WSWorkflowAIBuilderHandler) HandleNewSession(w http.ResponseWriter, r *http.Request) {
	h.HandleSession(w, r)
}

// @Summary		WebSocket do construtor de fluxos com IA (editar fluxo)
// @Description	Conversa com o assistente que monta e corrige fluxos. Você descreve o que quer; o assistente adiciona nós, liga as saídas e valida o fluxo, enviando cada passo em tempo real.
// @Description
// @Description	## Conectar
// @Description
// @Description	```text
// @Description	wss://SUA_URL_BASE/ws/workflows/{id}/ai-builder?token=SEU_ACCESS_TOKEN&workspace_id=SEU_WORKSPACE_ID
// @Description	wss://SUA_URL_BASE/ws/workflows/ai-builder?token=SEU_ACCESS_TOKEN&workspace_id=SEU_WORKSPACE_ID
// @Description	```
// @Description
// @Description	- Com `id`: edita um fluxo existente. Permissão `workflows:update`.
// @Description	- Sem `id`: cria um fluxo novo, começando vazio e do tipo `messages`. Permissão `workflows:create`.
// @Description
// @Description	## Limites
// @Description
// @Description	| Limite | Valor | Ao atingir |
// @Description	|---|---|---|
// @Description	| Sessões simultâneas por workspace | 3 | `error` e a conexão é encerrada. |
// @Description	| Tempo por pedido | 10 minutos | `done` com o motivo em `summary`; a conexão continua aberta. |
// @Description	| Tokens por sessão | 1.000.000 | `done`; novos pedidos na mesma conexão recebem o mesmo `done`. |
// @Description	| Saldo | precisa ter saldo | `done` com `valid: false` e o motivo em `summary`. |
// @Description
// @Description	O uso do assistente é cobrado do saldo do workspace.
// @Description
// @Description	## Mensagens que você envia
// @Description
// @Description	Formato: `{"type": "...", "data": {...}}`.
// @Description
// @Description	| type | data | O que faz |
// @Description	|---|---|---|
// @Description	| `user_prompt` | `{"text": string, "graph"?: Graph}` | Pede uma alteração. Se `graph` vier, ele substitui o fluxo atual antes do pedido. Pedidos enviados durante outro pedido entram na fila. |
// @Description	| `set_workflow_type` | `{"type": "messages" \| "voice"}` | Troca o tipo do fluxo. Responde com `graph_snapshot`. |
// @Description	| `set_model` | `{"model": string}` | Escolhe o modelo de IA dos próximos pedidos. |
// @Description	| `hydrate_graph` | `{"graph": Graph}` | Substitui o fluxo (por exemplo, depois de edições manuais) e responde com `graph_snapshot`. |
// @Description	| `cancel` | sem `data` | Interrompe o pedido em andamento, que termina com `done` e `summary: "cancelado"`. |
// @Description
// @Description	## Mensagens que você recebe
// @Description
// @Description	Formato: `{"type": "...", "payload": {...}}`.
// @Description
// @Description	| type | payload | Quando |
// @Description	|---|---|---|
// @Description	| `builder_ready` | `{"workflowType": "messages" \| "voice", "nodeCount": number, "mode": "create" \| "edit", "model": string}` | Logo após conectar. |
// @Description	| `graph_snapshot` | `{"graph": Graph, "issues": LintIssue[], "valid": boolean}` | Após conectar, após trocar o tipo ou o fluxo, e a cada passo do assistente. |
// @Description	| `iteration` | `{"n": number, "max": number, "tokensUsed": number, "tokenBudget": number}` | Início de cada passo do assistente. |
// @Description	| `reasoning_delta` | `{"text": string}` | Trecho do raciocínio do assistente. |
// @Description	| `reasoning_done` | sem `payload` | Fim do raciocínio do passo. |
// @Description	| `assistant_delta` | `{"text": string}` | Trecho da resposta do assistente. |
// @Description	| `assistant_done` | `{"tools": number}` | Fim da resposta; `tools` é o número de ações do passo. |
// @Description	| `tool` | `{"name": string, "summary": string, "ok": boolean}` | Cada ação no fluxo. `ok: false` traz o motivo em `summary`. |
// @Description	| `resource_resolved` | `{"kind": string, "query": string, "matches": [{"id": string, "name": string}]}` | O assistente buscou um recurso do workspace (agentes, mídias, filas, troncos...). |
// @Description	| `meta` | `{"name": string, "description": string, "workflowType": string}` | O assistente definiu nome, descrição ou tipo do fluxo. |
// @Description	| `idle` | `{"valid": boolean}` | O assistente respondeu sem alterar o fluxo e espera o próximo pedido. |
// @Description	| `done` | `{"valid": boolean, "summary": string, "residualIssues"?: LintIssue[]}` | O pedido terminou: o fluxo foi validado, um limite foi atingido, foi cancelado ou houve erro do provedor de IA. |
// @Description	| `error` | `{"error": string}` | Não foi possível abrir a sessão (limite de sessões, fluxo inexistente ou de outro workspace). A conexão é encerrada. |
// @Description
// @Description	Valores de `tool.name`: `get_node_spec`, `find_resource`, `add_node`, `connect`, `update_node`, `remove_node`, `remove_edge`, `set_meta` e `finish`.
// @Description
// @Description	## Tipos usados nas mensagens
// @Description
// @Description	```json
// @Description	{
// @Description	  "Graph": {"nodes": ["Node"], "edges": ["Edge"]},
// @Description	  "Node": {"id": "string", "type": "string", "position": {"x": 0, "y": 0}, "config": {}},
// @Description	  "Edge": {"source": "string", "target": "string", "label": "string (opcional)"},
// @Description	  "LintIssue": {
// @Description	    "code": "string",
// @Description	    "severity": "blocking | advisory",
// @Description	    "nodeId": "string (opcional)",
// @Description	    "field": "string (opcional)",
// @Description	    "edgeRef": "string (opcional)",
// @Description	    "message": "string",
// @Description	    "hint": "string (opcional)"
// @Description	  }
// @Description	}
// @Description	```
// @Description
// @Description	Os tipos de nó e os campos de `config` são os de `GET /workflows/node-types`. Problemas `blocking` impedem ativar o fluxo; `advisory` são sugestões.
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
// @Param			id		path	string	true	"ID do fluxo a editar"
// @Param			token		query	string	false	"Access token (alternativa ao cabeçalho Authorization e ao cookie accessToken)"
// @Param			workspace_id	query	string	false	"Workspace usado na checagem de permissão da conexão"
// @Success		101	{string}	string	"Switching Protocols: a conexão vira WebSocket"
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ws/workflows/{id}/ai-builder [get]
func (h *WSWorkflowAIBuilderHandler) HandleSession(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.useCase == nil {
		http.Error(w, "workflow ai builder not configured", http.StatusNotImplemented)
		return
	}

	workflowID := strings.TrimSpace(mux.Vars(r)["id"])

	workspaceID := middleware.GetWorkspaceID(r)
	if workspaceID == "" {
		http.Error(w, "workspace id is required", http.StatusUnauthorized)
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[workflow-ai-builder] failed to upgrade connection: %v", err)
		return
	}
	defer conn.Close()

	h.wsMetrics.IncWSConnections(metrics.WSEndpointWorkflowAIBuilder)
	defer h.wsMetrics.DecWSConnections(metrics.WSEndpointWorkflowAIBuilder)

	log.Printf("[workflow-ai-builder] starting session for workflow=%q workspace=%s", workflowID, workspaceID)

	var sessionConn workflow_usecase.BuilderConn = conn
	if h.newRecorder != nil {
		if rec := h.newRecorder(workspaceID, workflowID); rec != nil {
			defer rec.Close()
			sessionConn = &recordingConn{Conn: conn, rec: rec}
		}
	}

	if err := h.useCase.HandleSession(r.Context(), sessionConn, workflowID, workspaceID); err != nil {
		log.Printf("[workflow-ai-builder] session ended with error: %v", err)
	}
}

type recordingConn struct {
	*websocket.Conn
	rec *workflow_usecase.BuilderSessionRecorder
}

func (c *recordingConn) ReadMessage() (int, []byte, error) {
	mt, raw, err := c.Conn.ReadMessage()
	if err == nil {
		c.rec.Inbound(raw)
	}
	return mt, raw, err
}

func (c *recordingConn) WriteJSON(v interface{}) error {
	if data, mErr := json.Marshal(v); mErr == nil {
		c.rec.Outbound(data)
	}
	return c.Conn.WriteJSON(v)
}
