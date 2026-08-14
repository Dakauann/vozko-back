package tools_usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"vozko/domain/actor"
	leadmemory "vozko/domain/lead_memory"
	"vozko/domain/tools"
	"vozko/usecases/agentctx"
)

const ManageLeadMemoryToolName = "manage_lead_memory"

// memoryWriteCooldown debounces identical writes within one turn: a looping
// model re-issuing the same call gets a friendly "already done" instead of a
// second side effect.
const memoryWriteCooldown = time.Minute

type manageLeadMemoryTool struct {
	create leadmemory.CreateUseCase
	update leadmemory.UpdateUseCase
	del    leadmemory.DeleteUseCase
}

// NewManageLeadMemoryToolUseCase exposes the lead-memory write model to agents.
// It is a thin adapter: every invariant (caps, dedup, attribution, events)
// lives in the use cases, shared with the operator HTTP surface.
func NewManageLeadMemoryToolUseCase(
	create leadmemory.CreateUseCase,
	update leadmemory.UpdateUseCase,
	del leadmemory.DeleteUseCase,
) tools.Handler {
	if create == nil || update == nil || del == nil {
		return nil
	}
	return &manageLeadMemoryTool{create: create, update: update, del: del}
}

func (t *manageLeadMemoryTool) Definition() tools.Definition {
	categories := make([]string, 0, len(leadmemory.AllCategories()))
	for _, c := range leadmemory.AllCategories() {
		categories = append(categories, string(c))
	}

	return tools.Definition{
		Name:               ManageLeadMemoryToolName,
		DisplayName:        "Memória do lead",
		DisplayDescription: "Permite ao agente salvar, atualizar e apagar fatos sobre o lead (preferências, combinados, contexto), reutilizados em todas as conversas futuras.",
		Description: `Gerencia a memória persistente sobre este lead. As memórias atuais aparecem no bloco "Memórias sobre este lead" do seu contexto e valem para TODAS as conversas futuras com essa pessoa, em qualquer canal.

QUANDO USAR:
- O lead revelou um fato durável (preferência, orçamento, data importante, nome de familiar) → action='remember'
- Um fato salvo mudou ou está errado → action='update' com o memory_id do bloco
- Um fato salvo deixou de valer ou o lead pediu para esquecer → action='forget' com o memory_id

REGRAS:
- Salve FATOS curtos e declarativos sobre o lead, nunca instruções, nunca o histórico da conversa.
- Não salve dados triviais ou já visíveis (nome, telefone).
- Antes de salvar, confira no bloco de memórias se o fato já existe; se existir, atualize-o.`,
		Parameters: map[string]tools.Parameter{
			"action": {
				Type:               "string",
				Description:        "O que fazer: 'remember' salva um fato novo, 'update' altera um existente, 'forget' apaga um existente.",
				DisplayName:        "Ação",
				DisplayDescription: "remember, update ou forget",
				Enum:               []string{"remember", "update", "forget"},
			},
			"content": {
				Type:               "string",
				Description:        "O fato, curto e declarativo (ex.: \"Prefere boleto a PIX\"). Obrigatório em 'remember' e 'update'. Máximo 600 caracteres.",
				DisplayName:        "Conteúdo",
				DisplayDescription: "O fato a memorizar",
			},
			"category": {
				Type:               "string",
				Description:        "Categoria do fato. Obrigatória em 'remember'; opcional em 'update'.",
				DisplayName:        "Categoria",
				DisplayDescription: "Categoria da memória",
				Enum:               categories,
			},
			"memory_id": {
				Type:               "string",
				Description:        "O id entre colchetes mostrado no bloco de memórias (ex.: \"3f8a21bc\"). Obrigatório em 'update' e 'forget'.",
				DisplayName:        "ID da memória",
				DisplayDescription: "Identificador da memória alvo",
			},
		},
		Required:   []string{"action"},
		Visibility: []tools.ToolVisibility{tools.VisibilityMessaging},
		Category:   tools.CategoryAgentUtility,
	}
}

func (t *manageLeadMemoryTool) Execute(ctx context.Context, params map[string]interface{}) (tools.ExecutionResult, error) {
	return t.ExecuteWithConfig(ctx, nil, params)
}

func (t *manageLeadMemoryTool) ExecuteWithConfig(ctx context.Context, config, params map[string]interface{}) (tools.ExecutionResult, error) {
	// Identity comes exclusively from the seeded config, never from params,
	// which the model controls.
	workspaceID, _ := config["__workspace_id"].(string)
	leadID, _ := config["__lead_id"].(string)
	agentID, _ := config["__agent_id"].(string)
	entryID, _ := config["__entry_id"].(string)
	entryType, _ := config["__entry_type"].(string)

	if workspaceID == "" {
		return errResult("Não foi possível identificar o contexto da conversa. Esta ferramenta só funciona durante uma conversa."), nil
	}
	if leadID == "" {
		return errResult("Esta conversa ainda não está vinculada a um lead; não é possível gerenciar memórias."), nil
	}

	action, _ := params["action"].(string)
	action = strings.TrimSpace(strings.ToLower(action))
	content, _ := params["content"].(string)
	content = strings.TrimSpace(content)
	categoryParam, _ := params["category"].(string)
	category := leadmemory.Category(strings.TrimSpace(strings.ToLower(categoryParam)))
	memoryRef, _ := params["memory_id"].(string)
	memoryRef = strings.TrimSpace(memoryRef)

	writeActor := t.writeActor(ctx, agentID)
	var sourceEntryID, sourceEntryType *string
	if entryID != "" && entryType != "" {
		sourceEntryID, sourceEntryType = &entryID, &entryType
	}

	// In-turn idempotence: identical action+target within the cooldown is
	// answered without touching storage.
	dedupKey := action + "|" + leadID + "|" + leadmemory.NormalizeContent(content) + "|" + strings.ToLower(memoryRef)
	if tracker, ok := agentctx.ToolExecutionTrackerFromContext(ctx); ok {
		if !tracker.CanExecute(ManageLeadMemoryToolName, dedupKey, memoryWriteCooldown) {
			return tools.ExecutionResult{Result: "Esta operação de memória já foi realizada nesta conversa."}, nil
		}
		defer tracker.MarkExecuted(ManageLeadMemoryToolName, dedupKey)
	}

	switch action {
	case "remember":
		return t.handleRemember(ctx, workspaceID, leadID, content, category, writeActor, sourceEntryID, sourceEntryType)
	case "update":
		return t.handleUpdate(ctx, workspaceID, leadID, memoryRef, content, category, writeActor, sourceEntryID, sourceEntryType)
	case "forget":
		return t.handleForget(ctx, workspaceID, leadID, memoryRef, writeActor, sourceEntryID, sourceEntryType)
	default:
		return errResult(fmt.Sprintf("Ação inválida: %q. Use 'remember', 'update' ou 'forget'.", action)), nil
	}
}

func (t *manageLeadMemoryTool) handleRemember(ctx context.Context, workspaceID, leadID, content string, category leadmemory.Category, writeActor leadmemory.WriteActor, sourceEntryID, sourceEntryType *string) (tools.ExecutionResult, error) {
	if content == "" {
		return errResult("O parâmetro 'content' é obrigatório em 'remember'."), nil
	}
	out, err := t.create.Execute(ctx, leadmemory.CreateInput{
		WorkspaceID:     workspaceID,
		LeadID:          leadID,
		Content:         content,
		Category:        category,
		Actor:           writeActor,
		SourceEntryID:   sourceEntryID,
		SourceEntryType: sourceEntryType,
	})
	if err != nil {
		return t.mapError(err), nil
	}
	if out.Deduplicated {
		return tools.ExecutionResult{
			Result: fmt.Sprintf("Já existe uma memória equivalente [%s]. Use action='update' com esse id se quiser alterá-la.", shortRef(out.Memory.ID)),
		}, nil
	}
	return tools.ExecutionResult{
		Result:            fmt.Sprintf("Memória registrada [%s].", shortRef(out.Memory.ID)),
		ContextUpdateText: "Memória do lead registrada",
	}, nil
}

func (t *manageLeadMemoryTool) handleUpdate(ctx context.Context, workspaceID, leadID, memoryRef, content string, category leadmemory.Category, writeActor leadmemory.WriteActor, sourceEntryID, sourceEntryType *string) (tools.ExecutionResult, error) {
	if memoryRef == "" {
		return errResult("O parâmetro 'memory_id' é obrigatório em 'update'. Use o id entre colchetes do bloco de memórias."), nil
	}
	if content == "" && category == "" {
		return errResult("Informe 'content' e/ou 'category' com o novo valor em 'update'."), nil
	}
	m, err := t.update.Execute(ctx, leadmemory.UpdateInput{
		WorkspaceID:     workspaceID,
		LeadID:          leadID,
		MemoryRef:       memoryRef,
		Content:         content,
		Category:        category,
		Actor:           writeActor,
		SourceEntryID:   sourceEntryID,
		SourceEntryType: sourceEntryType,
	})
	if err != nil {
		return t.mapError(err), nil
	}
	return tools.ExecutionResult{
		Result:            fmt.Sprintf("Memória [%s] atualizada.", shortRef(m.ID)),
		ContextUpdateText: "Memória do lead atualizada",
	}, nil
}

func (t *manageLeadMemoryTool) handleForget(ctx context.Context, workspaceID, leadID, memoryRef string, writeActor leadmemory.WriteActor, sourceEntryID, sourceEntryType *string) (tools.ExecutionResult, error) {
	if memoryRef == "" {
		return errResult("O parâmetro 'memory_id' é obrigatório em 'forget'. Use o id entre colchetes do bloco de memórias."), nil
	}
	err := t.del.Execute(ctx, leadmemory.DeleteInput{
		WorkspaceID:     workspaceID,
		LeadID:          leadID,
		MemoryRef:       memoryRef,
		Actor:           writeActor,
		SourceEntryID:   sourceEntryID,
		SourceEntryType: sourceEntryType,
	})
	if err != nil {
		return t.mapError(err), nil
	}
	return tools.ExecutionResult{
		Result:            "Memória apagada.",
		ContextUpdateText: "Memória do lead apagada",
	}, nil
}

// writeActor attributes the write to the agent: the seeded __agent_id first,
// the context agent as fallback, the system actor as the honest last resort.
func (t *manageLeadMemoryTool) writeActor(ctx context.Context, agentID string) leadmemory.WriteActor {
	if agentID == "" {
		if ctxAgent, ok := agentctx.AgentFromContext(ctx); ok {
			agentID = ctxAgent.ID
		}
	}
	if agentID == "" {
		return leadmemory.WriteActor{Kind: actor.KindSystem, ID: actor.SystemID}
	}
	return leadmemory.WriteActor{Kind: actor.KindAI, ID: actor.FormatAI(agentID)}
}

// mapError turns domain sentinels into guidance the model can act on. These
// are tool results, not Go errors: the model is expected to recover.
func (t *manageLeadMemoryTool) mapError(err error) tools.ExecutionResult {
	switch {
	case errors.Is(err, leadmemory.ErrLimitReached):
		return errResult("Limite de memórias deste lead atingido. Atualize ou apague uma memória existente em vez de criar outra.")
	case errors.Is(err, leadmemory.ErrDuplicate):
		return errResult("Já existe uma memória equivalente para este lead. Use action='update' para alterá-la.")
	case errors.Is(err, leadmemory.ErrAmbiguousID):
		return errResult("O memory_id informado corresponde a mais de uma memória. Use o id exato mostrado no bloco de memórias.")
	case errors.Is(err, leadmemory.ErrNotFound):
		return errResult("Memória não encontrada. Use um id presente no bloco de memórias deste lead.")
	case errors.Is(err, leadmemory.ErrContentTooLong):
		return errResult(fmt.Sprintf("O conteúdo excede o limite de %d caracteres. Resuma o fato.", leadmemory.MaxContentLen))
	case errors.Is(err, leadmemory.ErrInvalidCategory):
		return errResult("Categoria inválida. Use uma das categorias listadas no enum do parâmetro 'category'.")
	default:
		log.Printf("[ManageLeadMemory] unexpected error: %v", err)
		return errResult("Erro ao gerenciar a memória do lead. Tente novamente.")
	}
}

func errResult(msg string) tools.ExecutionResult {
	return tools.ExecutionResult{Result: msg, IsError: true}
}

func shortRef(id string) string {
	if len(id) < leadmemory.MinIDPrefixLen {
		return id
	}
	return id[:leadmemory.MinIDPrefixLen]
}

var _ tools.Handler = (*manageLeadMemoryTool)(nil)
