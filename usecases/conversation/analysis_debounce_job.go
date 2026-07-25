package conversation_usecase

import (
	"context"
	"log"
	"strings"
	"time"

	"vozko/domain/ai"
	analysisdomain "vozko/domain/analysis"
	"vozko/domain/balance"
	"vozko/domain/cache"
	"vozko/domain/conversation"
	"vozko/domain/lead"
	"vozko/domain/shared"
	"vozko/domain/stage"
	toolsdomain "vozko/domain/tools"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
	tools_usecase "vozko/usecases/tools"
)

const (
	analysisDebounceInactivity = 5 * time.Minute
	analysisDebounceTimeout    = 30 * time.Second
)

type analysisDebounceJob struct {
	sharedState          cache.SharedState
	messageRepo          conversation.MessageRepository
	wcEntryRepo          wce.Repository
	wcCampaignRepo       wc.Repository
	leadRepo             lead.Repository
	aiService            ai.Service
	toolRegistry         toolsdomain.Service
	analysisRepo         analysisdomain.Repository
	stageRepo            stage.Repository
	hub                  conversation.EventBroadcaster
	cachedBalanceChecker balance.CachedBalanceChecker
}

func NewAnalysisDebounceJob(
	sharedState cache.SharedState,
	messageRepo conversation.MessageRepository,
	wcEntryRepo wce.Repository,
	wcCampaignRepo wc.Repository,
	leadRepo lead.Repository,
	aiService ai.Service,
	toolRegistry toolsdomain.Service,
	analysisRepo analysisdomain.Repository,
	stageRepo stage.Repository,
	hub conversation.EventBroadcaster,
	cachedBalanceChecker balance.CachedBalanceChecker,
) conversation.AnalysisDebounceJob {
	return &analysisDebounceJob{
		sharedState:          sharedState,
		messageRepo:          messageRepo,
		wcEntryRepo:          wcEntryRepo,
		wcCampaignRepo:       wcCampaignRepo,
		leadRepo:             leadRepo,
		aiService:            aiService,
		toolRegistry:         toolRegistry,
		analysisRepo:         analysisRepo,
		stageRepo:            stageRepo,
		hub:                  hub,
		cachedBalanceChecker: cachedBalanceChecker,
	}
}

func (j *analysisDebounceJob) ProcessPendingAnalyses() error {
	if j.sharedState == nil {
		return nil
	}

	pending, err := j.sharedState.HGetAll(AnalysisDebounceRedisKey)
	if err != nil {
		return err
	}
	if len(pending) == 0 {
		return nil
	}

	now := time.Now().UTC()

	for entryID, tsStr := range pending {
		lastInteraction, err := time.Parse(time.RFC3339, tsStr)
		if err != nil {
			log.Printf("[analysis-debounce] invalid timestamp for entry %s: %q — removing", entryID, tsStr)
			_ = j.sharedState.HDel(AnalysisDebounceRedisKey, entryID)
			continue
		}

		if now.Sub(lastInteraction) < analysisDebounceInactivity {
			continue
		}

		lockKey := "lock:analysis_debounce:" + entryID
		acquired, err := j.sharedState.SetNX(lockKey, "1", 2*time.Minute)
		if err != nil || !acquired {
			continue
		}

		_ = j.sharedState.HDel(AnalysisDebounceRedisKey, entryID)

		log.Printf("[analysis-debounce] processing deferred analysis for entry %s (idle since %s)", entryID, tsStr)

		if err := j.runAnalysisForEntry(entryID); err != nil {
			log.Printf("[analysis-debounce] failed for entry %s: %v", entryID, err)
		}
	}

	return nil
}

func (j *analysisDebounceJob) runAnalysisForEntry(entryID string) error {

	wcEntry, err := j.wcEntryRepo.FindByID(entryID)
	if err != nil || wcEntry == nil {
		return err
	}

	wcCampaign, err := j.wcCampaignRepo.FindByID(wcEntry.CampaignID)
	if err != nil || wcCampaign == nil {
		return err
	}

	if !wcCampaign.EnableAnalysis && !wcCampaign.EnableAutoStaging {
		return nil
	}

	var userPhoneNumber string
	if wcEntry.LeadID != "" {
		leadRecord, err := j.leadRepo.FindByID(wcCampaign.WorkspaceID, wcEntry.LeadID)
		if err == nil && leadRecord != nil {
			userPhoneNumber = leadRecord.Number
		}
	}
	if userPhoneNumber == "" {
		return nil
	}

	history, err := j.messageRepo.ListByEntry(entryID, shared.EntryTypeWhatsApp)
	if err != nil || len(history) == 0 {
		return err
	}

	totalCount, err := j.messageRepo.CountByEntry(entryID, shared.EntryTypeWhatsApp)
	if err != nil {
		return err
	}

	if len(history) > 100 {
		history = history[len(history)-100:]
	}

	entryType := "whatsapp"
	campaignName := wcCampaign.Name
	workspaceID := wcCampaign.WorkspaceID

	var aiTools []toolsdomain.Definition
	toolConfigs := map[string]map[string]interface{}{}

	wantAnalysis := wcCampaign.EnableAnalysis
	if wantAnalysis {
		allDefs := j.toolRegistry.Definitions()
		for _, def := range allDefs {
			if strings.EqualFold(def.Name, tools_usecase.ConversationAnalysisToolName) {
				aiTools = append(aiTools, def)
				toolConfigs[tools_usecase.ConversationAnalysisToolName] = map[string]interface{}{
					"__workspace_id": workspaceID,
					"entry_id":       entryID,
					"entry_type":     entryType,
					"message_count":  int(totalCount),
				}
				break
			}
		}
		if len(aiTools) == 0 {
			wantAnalysis = false
		}
	}

	autoTagEnabled := wcCampaign.EnableAutoStaging
	if autoTagEnabled {
		if h, ok := j.toolRegistry.Handler(tools_usecase.ManageEntryStageToolName); ok {
			var stageDef toolsdomain.Definition
			if ch, ok2 := h.(toolsdomain.ContextualHandler); ok2 && workspaceID != "" {
				stageDef = ch.DefinitionWithContext(toolsdomain.ToolContext{WorkspaceID: workspaceID, CampaignID: wcCampaign.ID, CampaignType: entryType})
			} else {
				stageDef = h.Definition()
			}
			aiTools = append(aiTools, stageDef)
			toolConfigs[tools_usecase.ManageEntryStageToolName] = map[string]interface{}{
				"__entry_id":      entryID,
				"__entry_type":    entryType,
				"__workspace_id":  workspaceID,
				"__campaign_id":   wcCampaign.ID,
				"__campaign_type": entryType,
			}
		}
	}

	if len(aiTools) == 0 {
		return nil
	}

	var systemPrompt string
	if wantAnalysis {
		systemPrompt = BuildAnalysisPrompt(AnalysisPromptInput{
			AnalysisType:    AnalysisTypeOngoing,
			CampaignName:    campaignName,
			UserPhoneNumber: userPhoneNumber,
			MessageCount:    int(totalCount),
			History:         history,
		})
	} else {
		transcript := BuildTranscript(history, userPhoneNumber)
		var currentTagName string
		var allTags []*stage.Stage
		if j.stageRepo != nil {
			if et, err := j.stageRepo.GetEntryStage(entryID, entryType, workspaceID); err == nil && et != nil {
				currentTagName = et.StageName
			}
			if tags, err := j.stageRepo.ListByCampaign(workspaceID, wcCampaign.ID, entryType); err == nil {
				allTags = tags
			}
		}
		systemPrompt = BuildAutoTagPrompt(AutoTagPromptInput{
			CampaignName:   campaignName,
			MessageCount:   int(totalCount),
			Transcript:     transcript,
			CurrentTagName: currentTagName,
			Tags:           allTags,
		})
	}

	wantAutoTag := autoTagEnabled

	var userMessage string
	switch {
	case wantAnalysis && wantAutoTag:
		userMessage = "Analise a conversa acima e: 1) chame a ferramenta conversation_analysis com sua avaliação; 2) chame a ferramenta manage_entry_stage para classificar o lead na etapa mais adequada baseado no estado atual da conversa."
	case wantAnalysis:
		userMessage = "Analise a conversa acima e chame a ferramenta conversation_analysis com sua avaliação."
	case wantAutoTag:
		userMessage = "Siga os passos do sistema: leia a transcrição INTEIRA, identifique o estado MAIS RECENTE da negociação (foque nas últimas mensagens), compare com as descrições das etapas, e chame manage_entry_stage com a etapa correta. Se a etapa atual já está correta, passe a mesma etapa."
	}

	ctx, cancel := context.WithTimeout(context.Background(), analysisDebounceTimeout)
	defer cancel()

	aiModel := "openai/gpt-4o-mini"
	if wcCampaign.AiModel != "" {
		aiModel = wcCampaign.AiModel
	}

	const minBalanceFloor int64 = 10_000
	if j.cachedBalanceChecker != nil {
		bal, err := j.cachedBalanceChecker.GetBalance(workspaceID)
		if err != nil {
			log.Printf("[analysis-debounce] balance check error for workspace %s: %v — skipping analysis (fail-closed)", workspaceID, err)
			return nil
		}
		if bal < minBalanceFloor {
			log.Printf("[analysis-debounce] workspace %s balance (%d micros) below minimum floor (%d micros), skipping analysis", workspaceID, bal, minBalanceFloor)
			return nil
		}
	}

	response, err := j.aiService.Generate(ctx, ai.GenerateInput{
		WorkspaceID: wcCampaign.WorkspaceID,
		Model:       aiModel,
		// Low temperature: analysis/auto-tag is classification, not generation.
		Temperature:  0.2,
		SystemPrompt: systemPrompt,
		Messages: []ai.Message{
			{
				Role:    "user",
				Content: userMessage,
			},
		},
		Tools:       aiTools,
		ToolConfigs: toolConfigs,
	})
	if err != nil {
		return err
	}

	for _, toolCall := range response.ToolCalls {
		if toolCall.Name == tools_usecase.ConversationAnalysisToolName && toolCall.Result != nil {
			log.Printf("[analysis-debounce] conversation_analysis result for entry %s: %v", entryID, toolCall.Result.Result)
			if j.hub != nil && j.analysisRepo != nil {
				if latest, err := j.analysisRepo.FindLatestByEntry(entryID, shared.EntryType(entryType)); err == nil && latest != nil {
					j.hub.BroadcastAnalysisUpdate(entryID, entryType, latest)
				}
			}
		}
		if toolCall.Name == tools_usecase.ManageEntryStageToolName && toolCall.Result != nil {
			log.Printf("[analysis-debounce] auto-stage result for entry %s: %v", entryID, toolCall.Result.Result)
		}
	}

	log.Printf("[analysis-debounce] completed analysis for entry %s", entryID)
	return nil
}
