package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	"vozko/domain/agent"
	"vozko/infra/http/middleware"
)

type SimulateAgentHistoryMessage struct {
	// Role: "user" (o operador, no papel do lead) | "assistant" (o agente).
	Role    string `json:"role" example:"user"`
	Content string `json:"content" example:"quanto custa o plano?"`
}

type SimulateAgentSessionMemory struct {
	ID       string `json:"id,omitempty"`
	Content  string `json:"content"`
	Category string `json:"category,omitempty"`
}

type SimulateAgentRequest struct {
	Message string `json:"message" example:"quero saber os preços"`
	// LeadID opcional: simula a conversa como se fosse com este lead: as
	// memórias reais dele são injetadas (somente leitura).
	LeadID  string                        `json:"lead_id,omitempty" example:"5f1c…"`
	History []SimulateAgentHistoryMessage `json:"history,omitempty"`
	// SessionMemories: fatos que o agente "memorizou" nesta simulação (as
	// chamadas de manage_lead_memory são interceptadas e nada persiste; o
	// cliente as reapresenta aqui para que o agente as veja no próximo turno).
	SessionMemories []SimulateAgentSessionMemory `json:"session_memories,omitempty"`
}

type SimulatedToolCallResponse struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
	Result    string                 `json:"result,omitempty"`
	IsError   bool                   `json:"isError"`
}

type SimulationDebugResponse struct {
	Model            string   `json:"model"`
	SystemPrompt     string   `json:"systemPrompt"`
	ToolNames        []string `json:"toolNames"`
	MemoryInjected   bool     `json:"memoryInjected"`
	RAGInjected      bool     `json:"ragInjected"`
	PromptTokens     int      `json:"promptTokens"`
	CompletionTokens int      `json:"completionTokens"`
	FinishReason     string   `json:"finishReason,omitempty"`
}

type SimulateAgentResponse struct {
	Replies   []string                    `json:"replies"`
	ToolCalls []SimulatedToolCallResponse `json:"toolCalls"`
	Debug     SimulationDebugResponse     `json:"debug"`
}

// @Summary		Simular uma conversa com o agente
// @Description	Executa um turno de conversa contra o agente em modo simulação: o prompt, as ferramentas, a base de conhecimento e as memórias do lead são montados exatamente como em produção, mas TODA execução de ferramenta é interceptada: nenhuma ação real acontece. Retorna as respostas do agente, as chamadas de ferramenta (com argumentos) e o raio-X do turno para depuração. Consome créditos de IA normalmente.
// @Tags			Agentes
// @Accept			json
// @Produce		json
// @Param			id		path		string					true	"ID do agente"
// @Param			request	body		SimulateAgentRequest	true	"Mensagem, histórico e lead opcional"
// @Success		200	{object}	SimulateAgentResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		502	{object}	response.ErrorResponse	"O provedor de IA recusou ou falhou o turno"
// @Security		BearerAuth
// @Router			/agents/{id}/simulate [post]
func (h *AgentHandler) Simulate(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	if h.simulateUseCase == nil {
		response.WriteError(w, http.StatusServiceUnavailable, "Simulação indisponível", nil)
		return
	}

	// Same access model as Get: workspace boundary lives in the use case; the
	// department gate is a delivery concern and mirrors the read path.
	agentItem, err := h.getUseCase.Execute(mux.Vars(r)["id"])
	if err != nil || agentItem == nil {
		response.WriteErrorWithCode(w, http.StatusNotFound, "not_found", "agente não encontrado", nil)
		return
	}
	if agentItem.WorkspaceID != middleware.GetWorkspaceID(r) || !canAccessDepartment(r, agentItem.DepartmentID) {
		response.WriteError(w, http.StatusForbidden, "You don't have access to this agent", nil)
		return
	}

	var req SimulateAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"message": "string (obrigatório): a fala do lead simulado",
			"history": "[{role: user|assistant, content}] (opcional)",
			"lead_id": "string (opcional): injeta as memórias reais deste lead",
		})
		return
	}

	history := make([]agent.SimulationMessage, 0, len(req.History))
	for _, m := range req.History {
		role := agent.SimulationRoleUser
		if m.Role == string(agent.SimulationRoleAssistant) {
			role = agent.SimulationRoleAssistant
		}
		history = append(history, agent.SimulationMessage{Role: role, Content: m.Content})
	}

	sessionMemories := make([]agent.SessionMemory, 0, len(req.SessionMemories))
	for _, sm := range req.SessionMemories {
		sessionMemories = append(sessionMemories, agent.SessionMemory{
			ID:       sm.ID,
			Content:  sm.Content,
			Category: sm.Category,
		})
	}

	out, err := h.simulateUseCase.Execute(r.Context(), agent.SimulateTurnInput{
		WorkspaceID:     middleware.GetWorkspaceID(r),
		AgentID:         agentItem.ID,
		Message:         req.Message,
		LeadID:          req.LeadID,
		History:         history,
		SessionMemories: sessionMemories,
	})
	if err != nil {
		switch {
		case errors.Is(err, agent.ErrSimulationMessageRequired),
			errors.Is(err, agent.ErrSimulationHistoryTooLong),
			errors.Is(err, agent.ErrSimulationMemoriesTooLong):
			response.WriteErrorWithCode(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		case errors.Is(err, agent.ErrAgentNotFound):
			response.WriteErrorWithCode(w, http.StatusNotFound, "not_found", err.Error(), nil)
		default:
			// The provider's own message goes back to the operator on purpose:
			// "model deprecated" or "context too long" IS the debugging signal
			// this page exists to surface.
			response.WriteErrorWithCode(w, http.StatusBadGateway, "ai_error", err.Error(), nil)
		}
		return
	}

	toolCalls := make([]SimulatedToolCallResponse, 0, len(out.ToolCalls))
	for _, call := range out.ToolCalls {
		toolCalls = append(toolCalls, SimulatedToolCallResponse{
			Name:      call.Name,
			Arguments: call.Arguments,
			Result:    call.Result,
			IsError:   call.IsError,
		})
	}

	response.WriteSuccess(w, http.StatusOK, SimulateAgentResponse{
		Replies:   out.Replies,
		ToolCalls: toolCalls,
		Debug: SimulationDebugResponse{
			Model:            out.Debug.Model,
			SystemPrompt:     out.Debug.SystemPrompt,
			ToolNames:        out.Debug.ToolNames,
			MemoryInjected:   out.Debug.MemoryInjected,
			RAGInjected:      out.Debug.RAGInjected,
			PromptTokens:     out.Debug.PromptTokens,
			CompletionTokens: out.Debug.CompletionTokens,
			FinishReason:     out.Debug.FinishReason,
		},
	})
}
