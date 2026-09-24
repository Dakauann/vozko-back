package tools_usecase

import (
	"context"
	"errors"
	"log"
	"strings"

	ia "vozko/domain/inbox_assignment"
	"vozko/domain/tools"
	ia_usecase "vozko/usecases/inbox_assignment"
)

const TransferToHumanToolName = "transfer_to_human"

// ConversationRouletteHandOff deals the conversation through the roulette
// inbound conversations use, or to the team queue when nobody is eligible
// (""), and takes the automation out of the conversation.
type ConversationRouletteHandOff interface {
	HandOffToRoulette(in ia.RouletteHandOff) (string, error)
}

type transferToHumanTool struct {
	handOff ConversationRouletteHandOff
}

func NewTransferToHumanToolUseCase(handOff ConversationRouletteHandOff) tools.Handler {
	if handOff == nil {
		return nil
	}
	return &transferToHumanTool{handOff: handOff}
}

func (t *transferToHumanTool) Definition() tools.Definition {
	return tools.Definition{
		Name:               TransferToHumanToolName,
		DisplayName:        "Transferir para humano",
		DisplayDescription: "Passa a conversa para um atendente humano pela roleta e pausa a IA nesta conversa.",
		Description: `Transfere a conversa atual para um atendente humano e encerra a sua participação nela.

QUANDO USAR:
- O cliente pediu para falar com uma pessoa
- O assunto exige decisão, negociação ou acesso que você não tem
- Você não conseguiu resolver após tentar e o cliente continua com dúvida

QUANDO NÃO USAR:
- Você consegue responder com as informações e ferramentas disponíveis
- O atendimento terminou (use finish_conversation)

Depois de transferir você não responde mais nesta conversa. Avise o cliente, em uma mensagem curta, que um atendente vai continuar o atendimento.`,
		Parameters: map[string]tools.Parameter{
			"reason": {
				Type:               "string",
				Description:        "Breve motivo da transferência em português (ex: cliente pediu um atendente).",
				DisplayName:        "Motivo",
				DisplayDescription: "Por que a conversa foi transferida",
			},
		},
		Required: []string{},
		ConfigSchema: map[string]tools.ConfigParameter{
			"department_id": {
				Type:               "string",
				Description:        "Departamento cuja roleta recebe a conversa. Vazio: o departamento da própria conversa.",
				DisplayName:        "Departamento",
				DisplayDescription: "Para qual departamento a IA transfere. Vazio usa o departamento da conversa.",
				OptionsSource:      "departments",
			},
		},
		Visibility: []tools.ToolVisibility{tools.VisibilityMessaging},
		Category:   tools.CategoryAgentUtility,
	}
}

func (t *transferToHumanTool) Execute(ctx context.Context, params map[string]interface{}) (tools.ExecutionResult, error) {
	return t.ExecuteWithConfig(ctx, nil, params)
}

func (t *transferToHumanTool) ExecuteWithConfig(_ context.Context, config map[string]interface{}, params map[string]interface{}) (tools.ExecutionResult, error) {
	workspaceID := configString(config, "__workspace_id")
	entryID := configString(config, "__entry_id")
	entryType := configString(config, "__entry_type")
	if workspaceID == "" || entryID == "" || entryType == "" {
		return tools.ExecutionResult{
			Result:  "Não foi possível identificar a conversa atual. Esta ferramenta só funciona durante um atendimento.",
			IsError: true,
		}, nil
	}

	reason, _ := params["reason"].(string)
	reason = strings.TrimSpace(reason)

	// The department comes from the agent's tool settings, never from the
	// model's arguments, so the AI cannot route to a department it invented.
	owner, err := t.handOff.HandOffToRoulette(ia.RouletteHandOff{
		WorkspaceID:  workspaceID,
		EntryID:      entryID,
		EntryType:    entryType,
		DepartmentID: configString(config, "department_id"),
	})
	if errors.Is(err, ia_usecase.ErrAutomationStillActive) {
		log.Printf("[TransferToHuman] entry=%s type=%s handed to %q but the AI could not be paused: %v", entryID, entryType, owner, err)
		return tools.ExecutionResult{
			Result:  "A conversa foi transferida, mas não foi possível pausar a IA. Avise o cliente que um atendente vai continuar.",
			IsError: true,
		}, nil
	}
	if err != nil {
		log.Printf("[TransferToHuman] entry=%s type=%s hand-off failed: %v", entryID, entryType, err)
		return tools.ExecutionResult{
			Result:  "Falha ao transferir a conversa. Continue o atendimento e tente novamente se necessário.",
			IsError: true,
		}, nil
	}

	log.Printf("[TransferToHuman] entry=%s type=%s handed to %q (reason: %s)", entryID, entryType, owner, reason)
	if owner == "" {
		return tools.ExecutionResult{
			Result: "Conversa colocada na fila da equipe; o próximo atendente disponível vai assumir. Avise o cliente que um atendente vai continuar.",
		}, nil
	}
	return tools.ExecutionResult{
		Result: "Conversa transferida para um atendente humano. Avise o cliente que um atendente vai continuar.",
	}, nil
}

func configString(config map[string]interface{}, key string) string {
	v, _ := config[key].(string)
	return strings.TrimSpace(v)
}

var _ tools.Handler = (*transferToHumanTool)(nil)
