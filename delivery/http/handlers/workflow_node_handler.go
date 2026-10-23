package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/workflow"
	"vozko/infra/http/middleware"
	workflow_usecase "vozko/usecases/workflow"

	"github.com/gorilla/mux"
)

type pinFromRunRequest struct {
	RunID    string `json:"runId"`
	StepID   string `json:"stepId,omitempty"`
	Revision int    `json:"revision" example:"4"`
}

type testNodeRequest struct {
	MockedState      map[string]interface{}                     `json:"mockedState"`
	TriggerVars      map[string]interface{}                     `json:"triggerVars"`
	SkipExecution    bool                                       `json:"skipExecution"`
	ConfirmRealCalls bool                                       `json:"confirmRealCalls"`
	ContinueWaits    bool                                       `json:"continueWaits"`
	WaitAnswers      map[string]workflow_usecase.TestWaitAnswer `json:"waitAnswers"`
	Trigger          *workflow_usecase.TestTrigger              `json:"trigger,omitempty"`
}

// @Summary Fixar a saída de um nó a partir de uma execução
// @Description Copia a saída e a conexão seguida por um nó numa execução de produção para o rascunho, como saída fixada (source run:<id da execução>). Nos testes o nó passa a devolver essa saída sem executar; a publicação remove as saídas fixadas, então produção nunca as lê. A saída copiada é a gravada, já com os valores sensíveis mascarados. Sem stepId usa a última etapa concluída do nó na execução. A execução precisa ser deste fluxo e de uma conversa que quem pede pode ver; caso contrário a resposta é 404. Fixar a partir de um teste, à mão ou remover a fixação é feito salvando o rascunho com o campo pinned do nó. revision é a revisão do rascunho que a pessoa está vendo; se outra sessão salvou antes, a resposta é 409 draft_conflict e nada é gravado.
// @Tags workflows
// @Accept json
// @Produce json
// @Param id path string true "ID do workflow"
// @Param nodeId path string true "ID do nó no rascunho"
// @Param body body pinFromRunRequest true "Execução e, opcionalmente, a etapa"
// @Success 200 {object} workflow.Workflow "Workflow com o rascunho salvo; version é a nova revisão"
// @Failure 400 {object} response.ErrorResponse "Corpo inválido ou revision ausente (draft_revision_required)"
// @Failure 404 {object} response.ErrorResponse "Workflow ou execução não encontrados"
// @Failure 409 {object} response.ErrorResponse{details=draftConflictDetails} "draft_conflict: o rascunho foi salvo por outra sessão"
// @Failure 422 {object} response.ErrorResponse "O nó não existe no rascunho, não pode ser fixado ou não tem etapa concluída na execução"
// @Router /workflows/{id}/nodes/{nodeId}/pin-from-run [post]
func (h *WorkflowHandler) PinFromRun(w http.ResponseWriter, r *http.Request) {
	viewer, ok := httpx.RequirePerson(w, r)
	if !ok {
		return
	}
	var req pinFromRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RunID == "" {
		response.WriteRefusal(w, refusalInvalidBody.Expecting(map[string]string{
			"runId":    "string (required)",
			"stepId":   "string (optional)",
			"revision": "int (required)",
		}))
		return
	}
	if err := workflow.RequireDraftRevision(req.Revision); err != nil {
		h.handleDomainError(w, err)
		return
	}
	if h.nodePins == nil {
		response.WriteRefusal(w, refusalPinUnavailable)
		return
	}
	vars := mux.Vars(r)
	wf, err := h.nodePins.PinFromRun(r.Context(), workflowScope(r), viewer, workflow_usecase.PinFromRunInput{
		WorkflowID: vars["id"], NodeID: vars["nodeId"], RunID: req.RunID, StepID: req.StepID, Revision: req.Revision,
	})
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, wf)
}

func (h *WorkflowHandler) NodeTypes(w http.ResponseWriter, r *http.Request) {
	response.WriteSuccess(w, http.StatusOK, h.catalogFn())
}

type resolveHandlesRequest struct {
	Nodes []workflow.Node `json:"nodes"`
}

type resolveHandlesResponse struct {
	Handles map[string][]workflow.HandleDefinition `json:"handles"`
}

func decodeBoundedJSON(w http.ResponseWriter, r *http.Request, target interface{}) bool {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxImportSize))
	if err != nil {
		response.WriteRefusal(w, refusalBodyTooLarge)
		return false
	}
	if err := json.Unmarshal(body, target); err != nil {
		response.WriteRefusal(w, refusalInvalidBody)
		return false
	}
	return true
}

// @Summary Resolver as saídas dos nós
// @Description Devolve, para cada nó enviado, as saídas (handles) atuais segundo a regra do servidor: opções de botões e listas, ferramentas do agente, casos de texto e teclas, mais as saídas fixas do catálogo. O editor desenha as saídas e as opções dos nós só a partir desta resposta. Envie apenas os nós cuja configuração mudou. O corpo tem limite de 2 MB (body_too_large) e de 200 nós por pedido, o máximo de um workflow (graph_too_many_nodes).
// @Tags workflows
// @Accept json
// @Produce json
// @Param body body resolveHandlesRequest true "Nós com id, type e config"
// @Success 200 {object} resolveHandlesResponse "Saídas por id de nó"
// @Failure 400 {object} response.ErrorResponse "Corpo inválido (invalid_body) ou maior que 2 MB (body_too_large)"
// @Failure 422 {object} response.ErrorResponse "Mais nós do que um workflow pode ter (graph_too_many_nodes)"
// @Router /workflows/resolve-handles [post]
func (h *WorkflowHandler) ResolveHandles(w http.ResponseWriter, r *http.Request) {
	var req resolveHandlesRequest
	if !decodeBoundedJSON(w, r, &req) {
		return
	}
	if len(req.Nodes) > workflow.MaxNodesPerWorkflow {
		h.handleDomainError(w, fmt.Errorf("%w: %d nodes", workflow.ErrGraphTooManyNodes, len(req.Nodes)))
		return
	}
	response.WriteSuccess(w, http.StatusOK, resolveHandlesResponse{Handles: workflow_usecase.ResolveHandlesOf(h.catalogFn(), req.Nodes)})
}

func (h *WorkflowHandler) ValidateGraph(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Type  workflow.WorkflowType `json:"type"`
		Graph workflow.Graph        `json:"graph"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteRefusal(w, refusalInvalidBody)
		return
	}
	wf := &workflow.Workflow{Type: req.Type, Graph: req.Graph}
	report := workflow_usecase.LintWorkflowGraph(h.catalogFn(), wf)
	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"valid":  report.IsGreen(),
		"issues": report.Issues,
	})
}

// @Summary Analisar um nó antes do teste
// @Description Diz como o teste do nó vai rodar: modo (direct, mock ou execute_until), as dependências que precisam de valor simulado (mock_fields) e, em trigger, a entrada do gatilho de onde o caminho até o nó começa (o mesmo formato de sim_ready.triggers do Testar). O caminho começa no gatilho principal quando ele leva até o nó, senão no primeiro gatilho do rascunho que leva. trigger é omitido quando o caminho não começa num gatilho com entrada (gatilhos de mensagem, ligação recebida ou nenhum gatilho no caminho). Envie a entrada em POST /workflows/{id}/nodes/{nodeId}/test no campo trigger.
// @Tags workflows
// @Produce json
// @Param id path string true "ID do workflow"
// @Param nodeId path string true "ID do nó"
// @Success 200 {object} workflow_usecase.UIAnalysis
// @Failure 404 {object} response.ErrorResponse "Workflow ou nó não encontrado"
// @Failure 501 {object} response.ErrorResponse "Teste de nó não configurado"
// @Router /workflows/{id}/nodes/{nodeId}/analyze [get]
func (h *WorkflowHandler) AnalyzeNode(w http.ResponseWriter, r *http.Request) {
	wfID := mux.Vars(r)["id"]
	nodeID := mux.Vars(r)["nodeId"]

	if h.testNodeUC == nil {
		response.WriteRefusal(w, refusalNodeTestUnavailable)
		return
	}
	if _, err := h.workflows.Get(workflowScope(r), wfID); err != nil {
		h.handleDomainError(w, err)
		return
	}

	result, err := h.testNodeUC.Analyze(r.Context(), workflow_usecase.AnalyzeNodeInput{
		WorkflowID:  wfID,
		WorkspaceID: middleware.GetWorkspaceID(r),
		NodeID:      nodeID,
	})
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, result.ToUIAnalysis())
}

// @Summary Testar um nó do workflow
// @Description Executa um único nó no ambiente de teste (nunca no registro de produção) com um estado simulado (mockedState e triggerVars). Devolve o mesmo registro de etapa da produção e da simulação em step (parâmetros usados, saída, tipo e cadeia do erro, pilha, HTTP, logs do script, tempos em ms, saída seguida e se veio de uma saída fixada), e em execute_until também upstream_steps, cada etapa anterior que rodou, em ordem. Mensagens são sempre simuladas; HTTP, código com fetch, e-mail e agenda só fazem chamadas reais com confirmRealCalls, caso contrário falham com error_kind real_call_not_confirmed. Nós de espera descrevem a espera em wait sem esperar. Quando o caminho não chega ao nó, path_stop explica onde parou, onde divergiu e quais correções aplicar; com continueWaits e waitAnswers o teste segue por esperas no caminho (timer pulado, resposta ou formulário informados). O teste inteiro tem limite de 60 s (error_kind timeout); saídas grandes são cortadas e valores sensíveis mascarados. Com skipExecution, só resolve a configuração sem executar efeitos. Com trigger {nodeId, input} o teste começa pelo gatilho escolhido, com a mesma entrada e as mesmas regras do Testar (sim_ready.triggers): input segue o formato do gatilho (campos planos por chave, o corpo JSON do webhook, ignorado em gatilhos de mensagem ou sem entrada) e é validado como em produção; dados inválidos respondem 422 trigger_input_invalid com details.issues ({code, key}). O caminho do execute_until começa nesse gatilho; se ele não leva até o nó, path_stop vem com reason no_path.
// @Tags workflows
// @Accept json
// @Produce json
// @Param id path string true "ID do workflow"
// @Param nodeId path string true "ID do nó"
// @Param body body testNodeRequest true "Estado simulado para o teste"
// @Success 200 {object} workflow_usecase.TestNodeOutput
// @Failure 400 {object} response.ErrorResponse "Corpo inválido"
// @Failure 403 {object} response.ErrorResponse "Workflow de outro departamento"
// @Failure 404 {object} response.ErrorResponse "Workflow ou nó não encontrado; trigger_not_found quando trigger.nodeId não é um gatilho do rascunho"
// @Failure 422 {object} response.ErrorResponse{details=triggerInputIssuesDetails} "trigger_input_invalid: a entrada do gatilho não passa nas regras dele"
// @Failure 501 {object} response.ErrorResponse "Teste de nó não configurado"
// @Router /workflows/{id}/nodes/{nodeId}/test [post]
// @Failure 503 {object} response.ErrorResponse "trigger_input_unavailable: não foi possível ler os formulários ou as etapas do gatilho"
func (h *WorkflowHandler) TestNode(w http.ResponseWriter, r *http.Request) {
	wfID := mux.Vars(r)["id"]
	nodeID := mux.Vars(r)["nodeId"]

	if h.testNodeUC == nil {
		response.WriteRefusal(w, refusalNodeTestUnavailable)
		return
	}
	if _, err := h.workflows.Get(workflowScope(r), wfID); err != nil {
		h.handleDomainError(w, err)
		return
	}

	var req testNodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteRefusal(w, refusalInvalidBody.Expecting(map[string]string{
			"mockedState":      "object (optional)",
			"triggerVars":      "object (optional)",
			"skipExecution":    "boolean (optional)",
			"confirmRealCalls": "boolean (optional)",
			"continueWaits":    "boolean (optional)",
			"waitAnswers":      "object by node id (optional)",
			"trigger":          "{nodeId: string, input: object} (optional)",
		}))
		return
	}

	result, err := h.testNodeUC.Execute(r.Context(), workflow_usecase.TestNodeInput{
		WorkflowID:       wfID,
		WorkspaceID:      middleware.GetWorkspaceID(r),
		NodeID:           nodeID,
		MockedState:      req.MockedState,
		TriggerVars:      req.TriggerVars,
		SkipExecution:    req.SkipExecution,
		ConfirmRealCalls: req.ConfirmRealCalls,
		ContinueWaits:    req.ContinueWaits,
		WaitAnswers:      req.WaitAnswers,
		Trigger:          req.Trigger,
	})
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, result)
}

// @Summary Última entrada real de um gatilho
// @Description Devolve a entrada da execução mais recente deste fluxo iniciada pelo tipo do gatilho nodeId e que guardou os dados do gatilho, no mesmo formato de input do Testar (sim_ready.triggers): campos planos por chave para gatilhos com campos, o corpo JSON para webhook, {"message"} para gatilhos de mensagem e {} sem entrada. Só valem execuções que quem pede pode ver (mesma regra da caixa de entrada; ligações e execuções de lead são visíveis); uma execução invisível conta como nenhuma. A busca olha só as 200 execuções mais recentes deste gatilho que guardaram dados; se nenhuma delas é visível, responde no_recent_input. Os valores vêm mascarados como na execução gravada (CPF, tokens, senhas); maskedKeys lista as chaves (no webhook, os caminhos com ponto) que precisam ser preenchidas de novo. Campos que o gatilho não declara mais ficam de fora.
// @Tags workflows
// @Produce json
// @Param id path string true "ID do workflow"
// @Param nodeId path string true "ID do nó de gatilho no rascunho"
// @Success 200 {object} workflow_usecase.LastTriggerInput "Entrada, chaves mascaradas e a execução de origem"
// @Failure 404 {object} response.ErrorResponse "Workflow não encontrado, trigger_not_found (o nó não é um gatilho do rascunho) ou no_recent_input (nenhuma execução visível com os dados do gatilho entre as 200 mais recentes)"
// @Failure 503 {object} response.ErrorResponse "trigger_input_unavailable: não foi possível ler os formulários ou as etapas do gatilho"
// @Router /workflows/{id}/triggers/{nodeId}/last-input [get]
func (h *WorkflowHandler) LastTriggerInput(w http.ResponseWriter, r *http.Request) {
	viewer, ok := httpx.RequirePerson(w, r)
	if !ok {
		return
	}
	if h.lastTriggerInput == nil {
		h.handleDomainError(w, workflow.ErrNoRecentTriggerInput)
		return
	}
	vars := mux.Vars(r)
	wf, err := h.workflows.Get(workflowScope(r), vars["id"])
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	result, err := h.lastTriggerInput.Execute(viewer, wf, vars["nodeId"])
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, result)
}

// @Summary Variáveis disponíveis num nó
// @Description Lista as variáveis que o nó nodeId do rascunho pode usar em {{...}}, calculadas pelo servidor a partir do grafo: os dados de cada gatilho que leva ao nó (campos do agente, corpo do webhook, respostas de formulário, etapa, aniversário), as saídas e variáveis dos passos anteriores (envios, definir variável, captura do HTTP, chaves de state.set legíveis no código, resposta da IA, respostas de formulário e de espera), os dados do contato e da conversa, os valores do sistema e os nomes dos segredos do workspace (nunca os valores). Cada item traz ref (o que vai entre {{ e }}), label em português, group (trigger, nodes, contact, system ou secrets, nesta ordem; os passos do mais próximo ao mais distante), kind (default para os valores padrão do sistema, trigger, node, system para contato e conversa, secret), source com o nó que fornece o dado (null para sistema e segredos), type (text, number, boolean, object, list, date ou unknown), example (ou null) e availability: always quando todo caminho até o nó fornece o dado, some_paths quando só alguns fornecem. Cada ref aparece uma vez; last.<campo> aparece uma vez, com o passo mais próximo que o fornece. O mesmo resultado vem em variables na análise do nó (GET /workflows/{id}/nodes/{nodeId}/analyze).
// @Tags workflows
// @Produce json
// @Param id path string true "ID do workflow"
// @Param nodeId path string true "ID do nó no rascunho"
// @Success 200 {object} workflow.VariableCatalog
// @Failure 403 {object} response.ErrorResponse "Workflow de outro departamento"
// @Failure 404 {object} response.ErrorResponse "Workflow não encontrado ou node_not_found (o nó não existe no rascunho)"
// @Failure 503 {object} response.ErrorResponse "variables_unavailable: não foi possível ler os segredos do workspace agora"
// @Router /workflows/{id}/nodes/{nodeId}/variables [get]
func (h *WorkflowHandler) NodeVariables(w http.ResponseWriter, r *http.Request) {
	if h.variables == nil {
		h.handleDomainError(w, workflow.ErrVariablesUnavailable)
		return
	}
	vars := mux.Vars(r)
	wf, err := h.workflows.Get(workflowScope(r), vars["id"])
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	catalog, err := h.variables.At(r.Context(), wf, vars["nodeId"])
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, catalog)
}
