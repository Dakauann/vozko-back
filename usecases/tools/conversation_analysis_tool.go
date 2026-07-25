package tools_usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/analysis"
	"vozko/domain/shared"
	"vozko/domain/tools"
	wc_entry "vozko/domain/whatsapp_campaign_entry"
)

const ConversationAnalysisToolName = "conversation_analysis"

// AnalysisTelemetrySink records analysis_created without coupling tools to Rabbit.
type AnalysisTelemetrySink interface {
	AnalysisCreated(workspaceID, entryID, entryType, analysisID, disposition string, quality int)
}

type conversationAnalysisTool struct {
	analysisRepo analysis.Repository
	wcEntryRepo  wc_entry.Repository
	telemetry    AnalysisTelemetrySink
}

func NewConversationAnalysisToolUseCase(
	analysisRepo analysis.Repository,
	wcEntryRepo wc_entry.Repository,
) *conversationAnalysisTool {
	return &conversationAnalysisTool{
		analysisRepo: analysisRepo,
		wcEntryRepo:  wcEntryRepo,
	}
}

// SetAnalysisTelemetry enables async analysis_created timeline events (optional).
func (t *conversationAnalysisTool) SetAnalysisTelemetry(tel AnalysisTelemetrySink) {
	if t != nil {
		t.telemetry = tel
	}
}

func (t *conversationAnalysisTool) Definition() tools.Definition {
	params := map[string]tools.Parameter{
		"product_interest": {
			Type:               "string",
			Description:        "O produto ou serviço específico em que o usuário demonstrou interesse (opcional, pode ser nulo)",
			DisplayName:        "Produto de Interesse",
			DisplayDescription: "Produto ou serviço de interesse do usuário",
		},
		"summary": {
			Type:               "string",
			Description:        "Um breve resumo profissional de 1-2 frases do estado atual da conversa, em relação ao objetivo (EM PORTUGUÊS)",
			DisplayName:        "Resumo",
			DisplayDescription: "Resumo da conversa",
		},
		"message_count": {
			Type:               "integer",
			Description:        "O número de mensagens na conversa (para rastreamento do WhatsApp)",
			DisplayName:        "Qtd. Mensagens",
			DisplayDescription: "Número de mensagens na conversa",
		},
	}
	required := []string{"summary"}

	// Classification fields — values, criteria and required-ness all come from
	// the domain rubric (single source of truth, objective-relative wording), so
	// the schema and the analysis prompts can never diverge.
	for _, f := range analysis.ClassificationFields() {
		params[f.Key] = tools.Parameter{
			Type:               "string",
			Description:        f.Description(),
			DisplayName:        f.Title,
			DisplayDescription: f.Title,
			Enum:               f.Values(),
		}
		required = append(required, f.Key)
	}

	// Attendance quality is decomposed into ordinal dimensions (also single
	// source of truth). The model rates each; the 0-100 score is computed
	// deterministically in Execute — LLMs are far more consistent rating
	// "none/low/medium/high" than emitting an arbitrary number.
	for _, d := range analysis.QualityDimensions() {
		params[d.Key] = tools.Parameter{
			Type:               "string",
			Description:        fmt.Sprintf(`%s Níveis: "none" (ausente), "low" (fraco), "medium" (razoável), "high" (forte). Peso de %.0f%% na nota final, que é calculada automaticamente.`, d.Description, d.Weight*100),
			DisplayName:        d.Label,
			DisplayDescription: d.Description,
			Enum:               analysis.QualityLevelValues(),
		}
		required = append(required, d.Key)
	}

	return tools.Definition{
		Name:               ConversationAnalysisToolName,
		DisplayName:        "Análise de Conversa",
		DisplayDescription: "Registra uma análise estruturada da conversa com insights sobre interesse, sentimento e qualificação do lead.",
		Description: `Registra uma análise estruturada da conversa. Use esta ferramenta para salvar insights sobre o nível de interesse do usuário, disposição, sentimento, qualificação do lead e ações recomendadas.
A análise é vinculada automaticamente ao entry (entrada da campanha) atual para referência futura e relatórios.

Chame esta ferramenta quando tiver coletado informações suficientes para avaliar o resultado da conversa.
NÃO é necessário fornecer entry_id ou entry_type - estes são determinados automaticamente pelo sistema.`,
		Parameters: params,
		Required:   required,
		Visibility: []tools.ToolVisibility{tools.VisibilityPostConversation},
		Category:   tools.CategoryAgentUtility,
	}
}

func (t *conversationAnalysisTool) Execute(ctx context.Context, params map[string]interface{}) (tools.ExecutionResult, error) {
	return t.ExecuteWithConfig(ctx, nil, params)
}

func (t *conversationAnalysisTool) ExecuteWithConfig(ctx context.Context, config map[string]interface{}, params map[string]interface{}) (tools.ExecutionResult, error) {
	entryID, _ := config["entry_id"].(string)
	entryTypeStr, _ := config["entry_type"].(string)

	interestStr, _ := params["interest"].(string)
	dispositionStr, _ := params["disposition"].(string)
	sentimentStr, _ := params["sentiment"].(string)
	qualificationStr, _ := params["qualification"].(string)
	nextActionStr, _ := params["next_action"].(string)
	summary, _ := params["summary"].(string)

	productInterest, _ := params["product_interest"].(string)

	messageCount := 0
	if mc, ok := config["message_count"].(int); ok {
		messageCount = mc
	} else if mc, ok := config["message_count"].(float64); ok {
		messageCount = int(mc)
	} else if mc, ok := params["message_count"].(float64); ok {
		messageCount = int(mc)
	} else if mc, ok := params["message_count"].(int); ok {
		messageCount = mc
	}

	// Attendance quality is derived from the model's ordinal rating per
	// dimension; the 0-100 number is computed from the domain rubric weights
	// (always within [0,100]), not emitted by the model.
	// Lenient on the (new, required) quality dimensions: a missing or unknown
	// value defaults to "none" rather than failing the whole save, so we never
	// lose the classification just because one ordinal was malformed.
	levels := make(map[string]analysis.QualityLevel, len(analysis.QualityDimensions()))
	for _, d := range analysis.QualityDimensions() {
		raw, _ := params[d.Key].(string)
		lvl := analysis.QualityLevel(strings.TrimSpace(raw))
		if !lvl.Valid() {
			lvl = analysis.QualityLevelNone
		}
		levels[d.Key] = lvl
	}
	attendanceQuality := analysis.NewQualityAssessment(levels).Score()

	if strings.TrimSpace(summary) == "" {
		return tools.ExecutionResult{}, errors.New("summary is required")
	}

	entryID = strings.TrimSpace(entryID)
	if entryID == "" {
		return tools.ExecutionResult{}, errors.New("entry_id is required")
	}

	entryType := shared.EntryType(entryTypeStr)
	if !entryType.Valid() {
		return tools.ExecutionResult{}, fmt.Errorf("invalid entry_type: must be 'whatsapp' or 'support', got '%s'", entryTypeStr)
	}

	if entryType == shared.EntryTypeWhatsApp {
		if t.wcEntryRepo != nil {
			entry, err := t.wcEntryRepo.FindByID(entryID)
			if err != nil {
				return tools.ExecutionResult{}, fmt.Errorf("failed to find whatsapp campaign entry: %w", err)
			}
			if entry == nil {
				return tools.ExecutionResult{}, errors.New("whatsapp campaign entry not found")
			}
		}
	}

	// Allowed values are enforced by the domain enums (and advertised in the
	// tool schema); the messages stay generic so there is no third copy of the
	// value lists to drift.
	interest := analysis.Interest(interestStr)
	if !interest.Valid() {
		return tools.ExecutionResult{}, fmt.Errorf("invalid interest value: %q", interestStr)
	}

	disposition := analysis.Disposition(dispositionStr)
	if !disposition.Valid() {
		return tools.ExecutionResult{}, fmt.Errorf("invalid disposition value: %q", dispositionStr)
	}

	sentiment := analysis.Sentiment(sentimentStr)
	if !sentiment.Valid() {
		return tools.ExecutionResult{}, fmt.Errorf("invalid sentiment value: %q", sentimentStr)
	}

	qualification := analysis.Qualification(qualificationStr)
	if !qualification.Valid() {
		return tools.ExecutionResult{}, fmt.Errorf("invalid qualification value: %q", qualificationStr)
	}

	nextAction := analysis.NextAction(nextActionStr)
	if !nextAction.Valid() {
		return tools.ExecutionResult{}, fmt.Errorf("invalid next_action value: %q", nextActionStr)
	}

	var productInterestPtr *string
	if strings.TrimSpace(productInterest) != "" {
		productInterestPtr = &productInterest
	}

	analysisRecord := &analysis.Analysis{
		ID:                uuid.New().String(),
		EntryID:           entryID,
		EntryType:         entryType,
		Interest:          interest,
		ProductInterest:   productInterestPtr,
		Disposition:       disposition,
		Sentiment:         sentiment,
		Qualification:     qualification,
		NextAction:        nextAction,
		Summary:           summary,
		AttendanceQuality: attendanceQuality,
		MessageCount:      messageCount,
		CreatedAt:         time.Now(),
	}

	if err := t.analysisRepo.Create(analysisRecord); err != nil {
		return tools.ExecutionResult{}, fmt.Errorf("failed to save analysis: %w", err)
	}

	if t.telemetry != nil {
		wsID, _ := config["__workspace_id"].(string)
		if wsID == "" {
			wsID, _ = config["workspace_id"].(string)
		}
		t.telemetry.AnalysisCreated(wsID, entryID, string(entryType), analysisRecord.ID, string(disposition), attendanceQuality)
	}

	return tools.ExecutionResult{
		Result: fmt.Sprintf("Analysis saved successfully. ID: %s, Interest: %s, Disposition: %s, Qualification: %s, Next Action: %s, Attendance Quality: %d/100",
			analysisRecord.ID, interest, disposition, qualification, nextAction, attendanceQuality),
		ContextUpdateText: fmt.Sprintf("Conversation analysis recorded: %s", summary),
	}, nil
}

var _ tools.Handler = (*conversationAnalysisTool)(nil)
