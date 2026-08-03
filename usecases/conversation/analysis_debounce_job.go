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
	// resolvers load an AnalysisSubject per channel. WhatsApp keeps its own
	// resolver below rather than being registered here, because it is the one
	// channel whose configuration lives behind a campaign indirection.
	resolvers map[shared.EntryType]AnalysisSubjectResolver
}

// SetAnalysisSubjectResolver registers a channel's subject loader.
//
// Without one, a channel's conversations are simply never analysed, which is
// what happened to Instagram for months, silently, while its EnableAnalysis
// switch sat in the UI doing nothing.
func (j *analysisDebounceJob) SetAnalysisSubjectResolver(entryType shared.EntryType, resolver AnalysisSubjectResolver) {
	if j == nil || resolver == nil || entryType == "" {
		return
	}
	if j.resolvers == nil {
		j.resolvers = make(map[shared.EntryType]AnalysisSubjectResolver, 2)
	}
	j.resolvers[entryType] = resolver
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

	for entryID, raw := range pending {
		pendingEntry, ok := decodeAnalysisDebounceValue(raw)
		if !ok {
			log.Printf("[analysis-debounce] invalid pending value for entry %s: %q, removing", entryID, raw)
			_ = j.sharedState.HDel(AnalysisDebounceRedisKey, entryID)
			continue
		}

		if now.Sub(pendingEntry.At) < analysisDebounceInactivity {
			continue
		}

		lockKey := "lock:analysis_debounce:" + entryID
		acquired, err := j.sharedState.SetNX(lockKey, "1", 2*time.Minute)
		if err != nil || !acquired {
			continue
		}

		_ = j.sharedState.HDel(AnalysisDebounceRedisKey, entryID)

		log.Printf("[analysis-debounce] processing deferred analysis for entry %s (%s, idle since %s)",
			entryID, pendingEntry.EntryType, pendingEntry.At.Format(time.RFC3339))

		if err := j.runAnalysisForEntry(entryID, pendingEntry.EntryType); err != nil {
			log.Printf("[analysis-debounce] failed for entry %s: %v", entryID, err)
		}
	}

	return nil
}

// resolveSubject loads the channel-specific facts for one entry.
//
// WhatsApp is resolved inline because its configuration lives behind a campaign
// indirection no other channel has; every other channel registers a resolver.
func (j *analysisDebounceJob) resolveSubject(entryID string, entryType shared.EntryType) (*AnalysisSubject, error) {
	if entryType == shared.EntryTypeWhatsApp {
		return j.resolveWhatsAppSubject(entryID)
	}
	resolver, ok := j.resolvers[entryType]
	if !ok || resolver == nil {
		// No resolver means the channel is switched off in this deployment. Not
		// an error, but worth saying out loud: silence here is exactly how the
		// Instagram gap stayed invisible.
		log.Printf("[analysis-debounce] no analysis resolver registered for %q, skipping entry %s",
			entryType, entryID)
		return nil, nil
	}
	return resolver(context.Background(), entryID)
}

// resolveWhatsAppSubject walks entry → campaign → workspace.
func (j *analysisDebounceJob) resolveWhatsAppSubject(entryID string) (*AnalysisSubject, error) {
	wcEntry, err := j.wcEntryRepo.FindByID(entryID)
	if err != nil || wcEntry == nil {
		return nil, err
	}
	wcCampaign, err := j.wcCampaignRepo.FindByID(wcEntry.CampaignID)
	if err != nil || wcCampaign == nil {
		return nil, err
	}

	// The lead's phone number is WhatsApp's contact label. It is still required
	// HERE, a WhatsApp conversation without one is malformed, but it is no
	// longer a precondition for the job as a whole, which is what excluded every
	// channel whose contacts have no phone.
	var contactLabel string
	if wcEntry.LeadID != "" {
		if leadRecord, err := j.leadRepo.FindByID(wcCampaign.WorkspaceID, wcEntry.LeadID); err == nil && leadRecord != nil {
			contactLabel = leadRecord.Number
		}
	}
	if contactLabel == "" {
		return nil, nil
	}

	return &AnalysisSubject{
		EntryID:           entryID,
		EntryType:         shared.EntryTypeWhatsApp,
		WorkspaceID:       wcCampaign.WorkspaceID,
		ContainerID:       wcCampaign.ID,
		ContainerName:     wcCampaign.Name,
		ContactLabel:      contactLabel,
		EnableAnalysis:    wcCampaign.EnableAnalysis,
		EnableAutoStaging: wcCampaign.EnableAutoStaging,
		AIModel:           wcCampaign.AiModel,
	}, nil
}

// runAnalysisForEntry analyses one conversation on any channel.
//
// Everything channel-specific is resolved into an AnalysisSubject first; the rest
// of the function reads the shared transcript and drives the same two tools it
// always did.
func (j *analysisDebounceJob) runAnalysisForEntry(entryID string, entryType shared.EntryType) error {
	subject, err := j.resolveSubject(entryID, entryType)
	if err != nil || subject == nil {
		return err
	}
	if !subject.WantsWork() {
		return nil
	}

	history, err := j.messageRepo.ListByEntry(entryID, entryType)
	if err != nil || len(history) == 0 {
		return err
	}

	totalCount, err := j.messageRepo.CountByEntry(entryID, entryType)
	if err != nil {
		return err
	}

	if len(history) > 100 {
		history = history[len(history)-100:]
	}

	campaignName := subject.ContainerName
	workspaceID := subject.WorkspaceID
	userPhoneNumber := subject.ContactLabel
	entryTypeStr := string(entryType)

	var aiTools []toolsdomain.Definition
	toolConfigs := map[string]map[string]interface{}{}

	wantAnalysis := subject.EnableAnalysis
	if wantAnalysis {
		allDefs := j.toolRegistry.Definitions()
		for _, def := range allDefs {
			if strings.EqualFold(def.Name, tools_usecase.ConversationAnalysisToolName) {
				aiTools = append(aiTools, def)
				toolConfigs[tools_usecase.ConversationAnalysisToolName] = map[string]interface{}{
					"__workspace_id": workspaceID,
					"entry_id":       entryID,
					"entry_type":     entryTypeStr,
					"message_count":  int(totalCount),
				}
				break
			}
		}
		if len(aiTools) == 0 {
			wantAnalysis = false
		}
	}

	autoTagEnabled := subject.EnableAutoStaging
	if autoTagEnabled {
		if h, ok := j.toolRegistry.Handler(tools_usecase.ManageEntryStageToolName); ok {
			var stageDef toolsdomain.Definition
			if ch, ok2 := h.(toolsdomain.ContextualHandler); ok2 && workspaceID != "" {
				stageDef = ch.DefinitionWithContext(toolsdomain.ToolContext{WorkspaceID: workspaceID, CampaignID: subject.ContainerID, CampaignType: entryTypeStr})
			} else {
				stageDef = h.Definition()
			}
			aiTools = append(aiTools, stageDef)
			toolConfigs[tools_usecase.ManageEntryStageToolName] = map[string]interface{}{
				"__entry_id":      entryID,
				"__entry_type":    entryTypeStr,
				"__workspace_id":  workspaceID,
				"__campaign_id":   subject.ContainerID,
				"__campaign_type": entryTypeStr,
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
			if et, err := j.stageRepo.GetEntryStage(entryID, entryTypeStr, workspaceID); err == nil && et != nil {
				currentTagName = et.StageName
			}
			if tags, err := j.stageRepo.ListByCampaign(workspaceID, subject.ContainerID, entryTypeStr); err == nil {
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
	if subject.AIModel != "" {
		aiModel = subject.AIModel
	}

	const minBalanceFloor int64 = 10_000
	if j.cachedBalanceChecker != nil {
		bal, err := j.cachedBalanceChecker.GetBalance(workspaceID)
		if err != nil {
			log.Printf("[analysis-debounce] balance check error for workspace %s: %v, skipping analysis (fail-closed)", workspaceID, err)
			return nil
		}
		if bal < minBalanceFloor {
			log.Printf("[analysis-debounce] workspace %s balance (%d micros) below minimum floor (%d micros), skipping analysis", workspaceID, bal, minBalanceFloor)
			return nil
		}
	}

	response, err := j.aiService.Generate(ctx, ai.GenerateInput{
		WorkspaceID: workspaceID,
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
				if latest, err := j.analysisRepo.FindLatestByEntry(entryID, entryType); err == nil && latest != nil {
					j.hub.BroadcastAnalysisUpdate(entryID, entryTypeStr, latest)
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
