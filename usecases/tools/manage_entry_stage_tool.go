package tools_usecase

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/google/uuid"

	conversation "vozko/domain/conversation"
	"vozko/domain/stage"
	"vozko/domain/tools"
)

const ManageEntryStageToolName = "manage_entry_stage"

type manageEntryStageTool struct {
	stageRepo       stage.Repository
	conversationHub conversation.EventBroadcaster
}

func NewManageEntryStageToolUseCase(stageRepo stage.Repository, hub conversation.EventBroadcaster) tools.Handler {
	if stageRepo == nil {
		return nil
	}
	return &manageEntryStageTool{
		stageRepo:       stageRepo,
		conversationHub: hub,
	}
}

func (t *manageEntryStageTool) Definition() tools.Definition {
	return tools.Definition{
		Name:               ManageEntryStageToolName,
		DisplayName:        "Gerenciar Etapa atual do Lead",
		DisplayDescription: "Classifica e move o lead para uma etapa do kanban. Em mensagens: executa durante a conversa. Em voz: executa automaticamente após o encerramento da ligação.",
		Description: `Classifica e move o lead para uma etapa do kanban com base no resultado da conversa.

QUANDO USAR (mova ao identificar a intenção):
- Cliente demonstrou INTERESSE → mover para etapa adequada
- Cliente AGENDOU reunião/consulta → mover para etapa adequada
- Cliente COMPROU/CONTRATOU → mover para etapa adequada
- Cliente disse NÃO ou pediu para PARAR → mover para etapa adequada
- Cliente não atendeu / linha ocupada → mover para etapa adequada

Forneça SOMENTE um nome de etapa presente no enum do parâmetro "target_tag_name".
NUNCA invente ou adivinhe nomes de etapas.`,
		Parameters: map[string]tools.Parameter{
			"target_tag_name": {
				Type:               "string",
				Description:        "Nome exato da etapa destino. Use apenas nomes presentes no enum.",
				DisplayName:        "Etapa Destino",
				DisplayDescription: "Nome da etapa para classificar o lead",
			},
		},
		Required:   []string{"target_tag_name"},
		Visibility: []tools.ToolVisibility{tools.VisibilityMessaging, tools.VisibilityPostConversation},
		Category:   tools.CategoryAgentUtility,
	}
}

func (t *manageEntryStageTool) DefinitionWithContext(ctx tools.ToolContext) tools.Definition {
	base := t.Definition()
	if ctx.WorkspaceID == "" || ctx.CampaignID == "" || ctx.CampaignType == "" {
		return base
	}

	tags, err := t.stageRepo.ListByCampaign(ctx.WorkspaceID, ctx.CampaignID, ctx.CampaignType)
	if err != nil || len(tags) == 0 {
		log.Printf("[ManageEntryTag] DefinitionWithContext: could not load tags for campaign %s/%s: %v", ctx.CampaignID, ctx.CampaignType, err)
		return base
	}

	names := make([]string, 0, len(tags))
	var descParts []string
	for _, tg := range tags {
		names = append(names, tg.Name)
		desc := tg.Description
		if desc == "" {
			desc = "(sem descrição)"
		}
		descParts = append(descParts, fmt.Sprintf("  • \"%s\" → %s", tg.Name, desc))
	}

	tagGuide := fmt.Sprintf("Etapa destino do lead. Escolha UMA das %d etapas disponíveis listadas no enum.\n\nGuia de etapas:\n%s\n\nEscolha a etapa que MELHOR descreve o estado atual do lead baseado na conversa.", len(names), strings.Join(descParts, "\n"))

	result := base
	result.Parameters = map[string]tools.Parameter{
		"target_tag_name": {
			Type:        "string",
			Description: tagGuide,
			Enum:        names,
		},
	}
	return result
}

func (t *manageEntryStageTool) Execute(ctx context.Context, params map[string]interface{}) (tools.ExecutionResult, error) {
	return t.ExecuteWithConfig(ctx, nil, params)
}

func (t *manageEntryStageTool) ExecuteWithConfig(ctx context.Context, config map[string]interface{}, params map[string]interface{}) (tools.ExecutionResult, error) {
	entryID, _ := config["__entry_id"].(string)
	entryType, _ := config["__entry_type"].(string)
	workspaceID, _ := config["__workspace_id"].(string)
	campaignID, _ := config["__campaign_id"].(string)
	campaignType, _ := config["__campaign_type"].(string)

	if entryID == "" || entryType == "" || workspaceID == "" {
		log.Printf("[ManageEntryTag] Missing required config: entry_id=%q, entry_type=%q, workspace_id=%q", entryID, entryType, workspaceID)
		return tools.ExecutionResult{
			Result:  "Não foi possível identificar o lead atual. Esta ferramenta só funciona durante uma conversa vinculada a uma campanha.",
			IsError: true,
		}, nil
	}

	if campaignID == "" {
		campaignID = entryID
	}
	if campaignType == "" {
		campaignType = entryType
	}

	if entryType != "voice" && entryType != "whatsapp" && entryType != "support" {
		return tools.ExecutionResult{
			Result:  fmt.Sprintf("Tipo de entrada inválido: %s", entryType),
			IsError: true,
		}, nil
	}

	action, _ := params["action"].(string)
	action = strings.TrimSpace(strings.ToLower(action))
	if action == "" {

		action = "move"
	}

	targetTagName, _ := params["target_tag_name"].(string)
	targetTagName = strings.TrimSpace(strings.ToLower(targetTagName))

	if err := t.stageRepo.EnsureDefaultStagesForCampaign(workspaceID, campaignID, campaignType); err != nil {
		log.Printf("[ManageEntryTag] Error ensuring default tags for campaign %s: %v", campaignID, err)
	}

	switch action {
	case "view":
		return t.handleView(workspaceID, campaignID, campaignType, entryID, entryType)
	case "move":
		if targetTagName == "" {
			return tools.ExecutionResult{
				Result:  "O parâmetro 'target_tag_name' é obrigatório. Forneça o nome exato de uma tag do enum.",
				IsError: true,
			}, nil
		}
		return t.handleMove(workspaceID, campaignID, campaignType, entryID, entryType, targetTagName)
	default:
		return tools.ExecutionResult{
			Result:  fmt.Sprintf("Ação inválida: %q. Forneça apenas 'target_tag_name' com um nome de tag válido.", action),
			IsError: true,
		}, nil
	}
}

func (t *manageEntryStageTool) handleView(workspaceID, campaignID, campaignType, entryID, entryType string) (tools.ExecutionResult, error) {
	allTags, err := t.stageRepo.ListByCampaign(workspaceID, campaignID, campaignType)
	if err != nil {
		log.Printf("[ManageEntryTag] Error listing tags for workspace %s: %v", workspaceID, err)
		return tools.ExecutionResult{
			Result:  "Erro ao listar tags disponíveis.",
			IsError: true,
		}, nil
	}

	currentTag, err := t.stageRepo.GetEntryStage(entryID, entryType, workspaceID)
	if err != nil && err != stage.ErrEntryTagNotFound {
		log.Printf("[ManageEntryTag] Error getting entry tag: %v", err)
	}

	var sb strings.Builder
	sb.WriteString("=== Estado da Tag do Lead ===\n\n")

	if currentTag != nil {
		sb.WriteString(fmt.Sprintf("Tag atual: \"%s\"", currentTag.StageName))
		if currentTag.StageColor != "" {
			sb.WriteString(fmt.Sprintf(" (cor: %s)", currentTag.StageColor))
		}
		sb.WriteString("\n\n")
	} else {
		sb.WriteString("Tag atual: nenhuma tag atribuída\n\n")
	}

	sb.WriteString("Tags disponíveis:\n")
	for i, t := range allTags {
		marker := "  "
		if currentTag != nil && t.ID == currentTag.StageID {
			marker = "→ "
		}
		sb.WriteString(fmt.Sprintf("%s%d. \"%s\"", marker, i+1, t.Name))
		if t.Color != "" {
			sb.WriteString(fmt.Sprintf(" (cor: %s)", t.Color))
		}
		if t.IsDefault {
			sb.WriteString(" [padrão]")
		}
		sb.WriteString("\n")
	}

	sb.WriteString("\nPara mover o lead, use action='move' com target_tag_name='<nome da tag>'.")

	return tools.ExecutionResult{
		Result: sb.String(),
	}, nil
}

func (t *manageEntryStageTool) handleMove(workspaceID, campaignID, campaignType, entryID, entryType, targetTagName string) (tools.ExecutionResult, error) {
	allTags, err := t.stageRepo.ListByCampaign(workspaceID, campaignID, campaignType)
	if err != nil {
		log.Printf("[ManageEntryTag] Error listing tags for workspace %s: %v", workspaceID, err)
		return tools.ExecutionResult{
			Result:  "Erro ao listar tags disponíveis.",
			IsError: true,
		}, nil
	}

	var targetTag *stage.Stage
	var availableNames []string
	for _, tg := range allTags {
		availableNames = append(availableNames, fmt.Sprintf("\"%s\"", tg.Name))
		if strings.EqualFold(tg.Name, targetTagName) {
			targetTag = tg
		}
	}

	if targetTag == nil {
		return tools.ExecutionResult{
			Result: fmt.Sprintf(
				"Tag \"%s\" não encontrada. Tags disponíveis: %s",
				targetTagName,
				strings.Join(availableNames, ", "),
			),
			IsError: true,
		}, nil
	}

	currentTag, _ := t.stageRepo.GetEntryStage(entryID, entryType, workspaceID)
	if currentTag != nil && currentTag.StageID == targetTag.ID {
		return tools.ExecutionResult{
			Result: fmt.Sprintf("O lead já está na tag \"%s\". Nenhuma alteração necessária.", targetTag.Name),
		}, nil
	}

	if err := t.stageRepo.RemoveEntryStage(entryID, entryType, workspaceID); err != nil {
		if err != stage.ErrEntryTagNotFound {
			log.Printf("[ManageEntryTag] Error removing existing tag: %v", err)
		}
	}

	EntryStage := &stage.EntryStage{
		ID:          uuid.New().String(),
		StageID:     targetTag.ID,
		EntryID:     entryID,
		EntryType:   entryType,
		WorkspaceID: workspaceID,
	}
	if err := t.stageRepo.AssignStage(EntryStage); err != nil {
		log.Printf("[ManageEntryTag] Error assigning tag: %v", err)
		return tools.ExecutionResult{
			Result:  fmt.Sprintf("Erro ao mover lead para tag \"%s\": %v", targetTag.Name, err),
			IsError: true,
		}, nil
	}

	log.Printf("[ManageEntryTag] Moved entry %s (%s) to tag \"%s\" (id=%s) for workspace %s",
		entryID, entryType, targetTag.Name, targetTag.ID, workspaceID)

	if t.conversationHub != nil {
		go t.conversationHub.BroadcastStageUpdate(workspaceID, entryID, entryType)
	}

	previousTagName := "nenhuma"
	if currentTag != nil {
		previousTagName = fmt.Sprintf("\"%s\"", currentTag.StageName)
	}

	return tools.ExecutionResult{
		Result: fmt.Sprintf(
			"Lead movido com sucesso de %s para \"%s\".",
			previousTagName, targetTag.Name,
		),
		ContextUpdateText: fmt.Sprintf("Tag do lead alterada para \"%s\"", targetTag.Name),
	}, nil
}

var _ tools.ContextualHandler = (*manageEntryStageTool)(nil)
