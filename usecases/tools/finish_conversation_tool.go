package tools_usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"vozko/domain/actor"
	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/tools"
)

const FinishConversationToolName = "finish_conversation"

// OutcomeCatalogue reads the outcomes a workspace collects when a conversation
// is finished.
type OutcomeCatalogue interface {
	OutcomeCaptureFor(ctx context.Context, workspaceID string) (*conversation.OutcomeCapture, error)
}

// finishConversationTool finishes through the status service, which records the
// finish and announces it to open screens, as it does for a person.
type finishConversationTool struct {
	status   conversation.ConversationStatusUpdater
	outcomes OutcomeCatalogue
}

func NewFinishConversationToolUseCase(status conversation.ConversationStatusUpdater, outcomes OutcomeCatalogue) tools.Handler {
	if status == nil {
		return nil
	}
	return &finishConversationTool{status: status, outcomes: outcomes}
}

func (t *finishConversationTool) Definition() tools.Definition {
	return tools.Definition{
		Name:               FinishConversationToolName,
		DisplayName:        "Finalizar conversa",
		DisplayDescription: "Encerra a conversa quando o atendimento foi concluído (cliente resolvido ou se despediu).",
		Description: `Finaliza a conversa atual (status finished) quando o atendimento está concluído.

QUANDO USAR:
- O cliente confirmou que o problema foi resolvido
- O cliente se despediu e não há mais pendências
- Você concluiu o fluxo e não há mais ações necessárias

QUANDO NÃO USAR:
- Ainda espera resposta do cliente
- Transferiu ou vai transferir para humano
- Pediu dados e ainda não recebeu

Após finalizar, se o cliente mandar mensagem de novo a conversa reabre automaticamente.`,
		Parameters: map[string]tools.Parameter{
			"reason": {
				Type:               "string",
				Description:        "Breve motivo em português (ex: cliente confirmou resolução).",
				DisplayName:        "Motivo",
				DisplayDescription: "Por que a conversa foi finalizada",
			},
		},
		Required:   []string{},
		Visibility: []tools.ToolVisibility{tools.VisibilityMessaging, tools.VisibilityPostConversation},
		Category:   tools.CategoryAgentUtility,
	}
}

// DefinitionWithContext shows the model the outcomes this workspace collects,
// and requires one when the workspace requires it of a person.
func (t *finishConversationTool) DefinitionWithContext(ctx tools.ToolContext) tools.Definition {
	def := t.Definition()
	capture := t.catalogue(ctx.WorkspaceID)
	if capture == nil || !capture.Enabled || len(capture.Outcomes) == 0 {
		return def
	}

	codes := make([]string, 0, len(capture.Outcomes))
	for _, o := range capture.Outcomes {
		codes = append(codes, o.Code)
	}
	description := "Desfecho do atendimento. Escolha o código que melhor descreve como a conversa terminou.\n\n" + outcomeGuide(capture)
	if capture.RequireOnFinish {
		description += "\n\nObrigatório: sem um desfecho a conversa não é finalizada."
	}

	params := make(map[string]tools.Parameter, len(def.Parameters)+1)
	for k, v := range def.Parameters {
		params[k] = v
	}
	params["outcome_code"] = tools.Parameter{
		Type:               "string",
		Description:        description,
		Enum:               codes,
		DisplayName:        "Desfecho",
		DisplayDescription: "Desfecho registrado no encerramento",
	}
	def.Parameters = params
	if capture.RequireOnFinish {
		def.Required = append(append([]string{}, def.Required...), "outcome_code")
	}
	return def
}

func (t *finishConversationTool) catalogue(workspaceID string) *conversation.OutcomeCapture {
	workspaceID = strings.TrimSpace(workspaceID)
	if t.outcomes == nil || workspaceID == "" {
		return nil
	}
	capture, err := t.outcomes.OutcomeCaptureFor(context.Background(), workspaceID)
	if err != nil {
		log.Printf("[FinishConversation] reading the outcome catalogue of workspace %s: %v", workspaceID, err)
		return nil
	}
	return capture
}

func outcomeGuide(capture *conversation.OutcomeCapture) string {
	lines := make([]string, 0, len(capture.Outcomes))
	for _, o := range capture.Outcomes {
		line := fmt.Sprintf("  • \"%s\" → %s", o.Code, o.Label)
		if o.IsDurable {
			line += " (resolve a demanda)"
		}
		lines = append(lines, line)
	}
	return "Desfechos disponíveis:\n" + strings.Join(lines, "\n")
}

func (t *finishConversationTool) Execute(ctx context.Context, params map[string]interface{}) (tools.ExecutionResult, error) {
	return t.ExecuteWithConfig(ctx, nil, params)
}

func (t *finishConversationTool) ExecuteWithConfig(ctx context.Context, config map[string]interface{}, params map[string]interface{}) (tools.ExecutionResult, error) {
	_ = ctx
	entryID, _ := config["__entry_id"].(string)
	entryType, _ := config["__entry_type"].(string)
	entryID = strings.TrimSpace(entryID)
	entryType = strings.TrimSpace(entryType)
	if entryID == "" || entryType == "" {
		return tools.ExecutionResult{
			Result:  "Não foi possível identificar a conversa atual. Esta ferramenta só funciona durante um atendimento.",
			IsError: true,
		}, nil
	}
	if !shared.EntryType(entryType).SupportsConversationClosing() {
		return tools.ExecutionResult{
			Result:  "Tipo de conversa não suportado para finalização.",
			IsError: true,
		}, nil
	}

	outcomeCode, _ := params["outcome_code"].(string)
	agentID, _ := config["__agent_id"].(string)
	closedBy := ""
	if agentID = strings.TrimSpace(agentID); agentID != "" {
		closedBy = actor.FormatAI(agentID)
	}
	if err := t.status.Finish(entryID, entryType, conversation.FinishOptions{
		Source:      conversation.CloseSourceAI,
		Reason:      conversation.CloseReasonAIResolved,
		OutcomeCode: strings.TrimSpace(outcomeCode),
		ActorID:     closedBy,
	}); err != nil {
		log.Printf("[FinishConversation] entry=%s type=%s err=%v", entryID, entryType, err)
		if errors.Is(err, conversation.ErrOutcomeRequired) || errors.Is(err, conversation.ErrOutcomeUnknown) {
			workspaceID, _ := config["__workspace_id"].(string)
			return tools.ExecutionResult{Result: t.outcomeRefusal(workspaceID, err), IsError: true}, nil
		}
		return tools.ExecutionResult{
			Result:  "Falha ao finalizar a conversa. Tente novamente.",
			IsError: true,
		}, nil
	}

	reason, _ := params["reason"].(string)
	reason = strings.TrimSpace(reason)
	msg := "Conversa finalizada com sucesso (status finished, origem IA)."
	if reason != "" {
		msg = "Conversa finalizada: " + reason
	}
	return tools.ExecutionResult{Result: msg}, nil
}

// outcomeRefusal tells the model why the finish was refused and which outcomes
// it may use, so its next call can succeed.
func (t *finishConversationTool) outcomeRefusal(workspaceID string, err error) string {
	msg := "A conversa não foi finalizada: este workspace exige um desfecho válido no encerramento."
	if errors.Is(err, conversation.ErrOutcomeUnknown) {
		msg = "A conversa não foi finalizada: o desfecho informado não existe neste workspace."
	}
	capture := t.catalogue(workspaceID)
	if capture == nil || len(capture.Outcomes) == 0 {
		return msg
	}
	return msg + " Chame finish_conversation de novo com outcome_code igual a um destes códigos.\n" + outcomeGuide(capture)
}

var _ tools.ContextualHandler = (*finishConversationTool)(nil)
