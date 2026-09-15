package conversation_usecase

import (
	"context"
	"fmt"
	"log"
	"time"

	"vozko/domain/ai"
	"vozko/domain/audience"
	"vozko/domain/balance"
	"vozko/domain/cache"
	"vozko/domain/conversation"
	"vozko/domain/lead"
	leadmemory "vozko/domain/lead_memory"
	"vozko/domain/shared"
	"vozko/domain/stage"
	toolsdomain "vozko/domain/tools"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
	lead_memory_usecase "vozko/usecases/lead_memory"
	tools_usecase "vozko/usecases/tools"
)

const analysisDebounceTimeout = 30 * time.Second

// AnalysisDebouncePolicy reports the workspaces that changed their debounce
// window from the product default.
//
// Only the ones that CHANGED it, which is what keeps this cheap: the map is
// empty for almost every deployment, and an empty map means the sweep behaves
// exactly as it did when the window was a constant, with no per-entry work
// added at all. See windowFor below for what a non-empty one costs.
type AnalysisDebouncePolicy interface {
	ConfiguredDebounceWindows(ctx context.Context) (map[string]time.Duration, error)
}

type analysisDebounceJob struct {
	sharedState    cache.SharedState
	messageRepo    conversation.MessageRepository
	wcEntryRepo    wce.Repository
	wcCampaignRepo wc.Repository
	leadRepo       lead.Repository
	aiService      ai.Service
	toolRegistry   toolsdomain.Service
	stageRepo      stage.Repository
	// leadMemories renders the lead's current memory block into the memory
	// pass, so the model updates existing facts instead of re-adding them.
	leadMemories         leadmemory.ListUseCase
	hub                  conversation.EventBroadcaster
	cachedBalanceChecker balance.CachedBalanceChecker
	// resolvers load an AnalysisSubject per channel. WhatsApp keeps its own
	// resolver below rather than being registered here, because it is the one
	// channel whose configuration lives behind a campaign indirection.
	resolvers map[shared.EntryType]AnalysisSubjectResolver
	// analysisQueue hands the conversation to the analysis engine instead of
	// classifying it here. See runAnalysisForEntry.
	analysisQueue ConversationAnalysisEnqueuer
	// debouncePolicy is the per-workspace quiet period. Optional: without one
	// every workspace waits the product default, which is what this job did
	// when the window was a constant.
	debouncePolicy AnalysisDebouncePolicy
}

// ConversationAnalysisEnqueuer queues one conversation for the analysis engine.
//
// Narrow on purpose: this job needs to hand a conversation over, nothing more.
// It does not need to know that the engine batches, budgets, retries or bills.
type ConversationAnalysisEnqueuer interface {
	EnqueueSubject(ctx context.Context, subject *AnalysisSubject) error
}

// SetAnalysisDebouncePolicy wires the per-workspace quiet period. Without one
// the job waits audience.DefaultDebounceMinutes for everybody, which is the
// behaviour it had when that number was a constant.
func (j *analysisDebounceJob) SetAnalysisDebouncePolicy(p AnalysisDebouncePolicy) {
	j.debouncePolicy = p
}

// SetAnalysisQueue wires the engine. Without one, conversation analysis simply
// does not run, and auto-staging and auto-memory are unaffected.
func (j *analysisDebounceJob) SetAnalysisQueue(q ConversationAnalysisEnqueuer) {
	if j != nil && q != nil {
		j.analysisQueue = q
	}
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
	stageRepo stage.Repository,
	leadMemories leadmemory.ListUseCase,
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
		stageRepo:            stageRepo,
		leadMemories:         leadMemories,
		hub:                  hub,
		cachedBalanceChecker: cachedBalanceChecker,
	}
}

func (j *analysisDebounceJob) ProcessPendingAnalyses() error {
	if j.sharedState == nil {
		return nil
	}
	ack, ok := j.sharedState.(cache.HashFieldAcknowledger)
	if !ok {
		return fmt.Errorf("analysis debounce store does not support atomic acknowledgement")
	}

	pending, err := j.sharedState.HGetAll(AnalysisDebounceRedisKey)
	if err != nil {
		return err
	}
	if len(pending) == 0 {
		return nil
	}

	now := time.Now().UTC()
	windows := j.configuredWindows()
	shortest := shortestDebounceWindow(windows)

	for entryID, raw := range pending {
		pendingEntry, ok := decodeAnalysisDebounceValue(raw)
		if !ok {
			log.Printf("[analysis-debounce] invalid pending value for entry %s: %q, removing", entryID, raw)
			_ = ack.HDelIfValue(AnalysisDebounceRedisKey, entryID, raw)
			continue
		}

		// The cheap gate first: nothing can be due before the shortest window
		// any workspace has, and no workspace has to be identified to know it.
		age := now.Sub(pendingEntry.At)
		if age < shortest {
			continue
		}
		if age < j.windowFor(entryID, pendingEntry.EntryType, windows) {
			continue
		}

		lockKey := "lock:analysis_debounce:" + entryID
		acquired, err := j.sharedState.SetNX(lockKey, "1", 2*time.Minute)
		if err != nil || !acquired {
			continue
		}

		log.Printf("[analysis-debounce] processing deferred analysis for entry %s (%s, idle since %s)",
			entryID, pendingEntry.EntryType, pendingEntry.At.Format(time.RFC3339))

		if err := j.runAnalysisForEntry(entryID, pendingEntry.EntryType); err != nil {
			log.Printf("[analysis-debounce] failed for entry %s: %v", entryID, err)
			continue
		}
		if err := ack.HDelIfValue(AnalysisDebounceRedisKey, entryID, raw); err != nil {
			log.Printf("[analysis-debounce] acknowledging entry %s: %v", entryID, err)
		}
	}

	return nil
}

// configuredWindows lists the workspaces that changed their quiet period.
//
// Read once per tick, not once per entry. A failure degrades to "nobody changed
// it", which is the product default for everyone: a sweep that stopped because
// a settings read failed would silently hold up every analysis in the system.
func (j *analysisDebounceJob) configuredWindows() map[string]time.Duration {
	if j.debouncePolicy == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), analysisDebounceTimeout)
	defer cancel()
	windows, err := j.debouncePolicy.ConfiguredDebounceWindows(ctx)
	if err != nil {
		log.Printf("[analysis-debounce] debounce settings unavailable, using the default for every workspace: %v", err)
		return nil
	}
	return windows
}

// shortestDebounceWindow is the soonest anything can possibly be due.
//
// It is the floor the loop gates on before identifying whose entry it is. With
// no workspace configured it equals the default, so the gate alone decides every
// entry and the sweep costs exactly what it always did.
func shortestDebounceWindow(windows map[string]time.Duration) time.Duration {
	shortest := audience.DebounceWindow(0)
	for _, w := range windows {
		if w > 0 && w < shortest {
			shortest = w
		}
	}
	return shortest
}

// windowFor is the quiet period this entry has to clear.
//
// The default, unless the entry belongs to a workspace that changed it. Finding
// out which workspace costs a resolve, so it is skipped entirely when no
// workspace configured anything, which is the normal case. When one has, the
// cost is one resolve per entry per tick while that entry sits between the
// shortest configured window and its own. That is bounded by how many
// conversations are mid-debounce at once, and it is paid only by deployments
// that asked for a non-default window.
func (j *analysisDebounceJob) windowFor(entryID string, entryType shared.EntryType, windows map[string]time.Duration) time.Duration {
	fallback := audience.DebounceWindow(0)
	if len(windows) == 0 {
		return fallback
	}
	subject, err := j.resolveSubject(entryID, entryType)
	if err != nil || subject == nil || subject.WorkspaceID == "" {
		// Unattributable: the default is the safe answer, since the alternative
		// is either analysing too early or never analysing at all.
		return fallback
	}
	if w, ok := windows[subject.WorkspaceID]; ok && w > 0 {
		return w
	}
	return fallback
}

// resolveSubject loads the channel-specific facts for one entry.
//
// WhatsApp is resolved inline because its configuration lives behind a campaign
// indirection no other channel has; every other channel registers a resolver.
func (j *analysisDebounceJob) resolveSubject(entryID string, entryType shared.EntryType) (*AnalysisSubject, error) {
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

// NewWhatsAppAnalysisResolver walks entry → campaign → workspace.
//
// WhatsApp used to be special-cased inside this job rather than registered like
// every other channel. That was invisible until a SECOND consumer of the
// resolvers appeared: the analysis engine saw every channel except the one the
// feature was originally built for. A constructor, registered through the same
// registry, means there is one way to answer "what is this conversation".
func NewWhatsAppAnalysisResolver(
	wcEntryRepo wce.Repository,
	wcCampaignRepo wc.Repository,
	leadRepo lead.Repository,
) AnalysisSubjectResolver {
	return func(_ context.Context, entryID string) (*AnalysisSubject, error) {
		return resolveWhatsAppSubject(wcEntryRepo, wcCampaignRepo, leadRepo, entryID)
	}
}

func resolveWhatsAppSubject(
	wcEntryRepo wce.Repository,
	wcCampaignRepo wc.Repository,
	leadRepo lead.Repository,
	entryID string,
) (*AnalysisSubject, error) {
	wcEntry, err := wcEntryRepo.FindByID(entryID)
	if err != nil || wcEntry == nil {
		return nil, err
	}
	wcCampaign, err := wcCampaignRepo.FindByID(wcEntry.CampaignID)
	if err != nil || wcCampaign == nil {
		return nil, err
	}

	// The lead's phone number is WhatsApp's contact label. It is still required
	// HERE, a WhatsApp conversation without one is malformed, but it is no
	// longer a precondition for the job as a whole, which is what excluded every
	// channel whose contacts have no phone.
	var contactLabel string
	if wcEntry.LeadID != "" {
		if leadRecord, err := leadRepo.FindByID(wcCampaign.WorkspaceID, wcEntry.LeadID); err == nil && leadRecord != nil {
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
		LeadID:            wcEntry.LeadID,
		AgentID:           wcCampaign.AgentID,
		EnableAnalysis:    wcCampaign.EnableAnalysis,
		EnableAutoStaging: wcCampaign.EnableAutoStaging,
		EnableAutoMemory:  wcCampaign.EnableAutoMemory,
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
	if subject.EnableAnalysis && j.analysisQueue != nil {
		queueCtx, cancelQueue := context.WithTimeout(context.Background(), analysisDebounceTimeout)
		queueErr := j.analysisQueue.EnqueueSubject(queueCtx, subject)
		cancelQueue()
		if queueErr != nil {
			return queueErr
		}
	}
	if !subject.EnableAutoStaging && !(subject.EnableAutoMemory && subject.LeadID != "") {
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

	// wantAnalysis stays false: the prompt below is now only ever about staging
	// or memory, and if neither is enabled no call is made at all.
	const wantAnalysis = false

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

	ctx, cancel := context.WithTimeout(context.Background(), analysisDebounceTimeout)
	defer cancel()

	// Auto-memorization is the third capability of this same pass: the seeded
	// manage_lead_memory tool joins the call, so caps, dedup, attribution and
	// timeline events all come from the one write model the operators and the
	// live agent already use. A lead is required: without one there is nowhere
	// to remember to.
	wantMemory := subject.EnableAutoMemory && subject.LeadID != ""
	var memoryBlock string
	if wantMemory {
		if h, ok := j.toolRegistry.Handler(tools_usecase.ManageLeadMemoryToolName); ok {
			aiTools = append(aiTools, h.Definition())
			toolConfigs[tools_usecase.ManageLeadMemoryToolName] = map[string]interface{}{
				"__workspace_id": workspaceID,
				"__lead_id":      subject.LeadID,
				"__agent_id":     subject.AgentID,
				"__entry_id":     entryID,
				"__entry_type":   entryTypeStr,
			}
			// Injecting the current memories is what makes repeated extraction
			// idempotent: the model sees what is already known and updates
			// instead of re-adding.
			memoryBlock = lead_memory_usecase.BuildContext(ctx, j.leadMemories, lead_memory_usecase.ContextInput{
				WorkspaceID:   workspaceID,
				LeadID:        subject.LeadID,
				HasMemoryTool: true,
			})
		} else {
			wantMemory = false
		}
	}

	if len(aiTools) == 0 {
		return nil
	}

	wantAutoTag := autoTagEnabled

	var systemPrompt string
	switch {
	case wantAnalysis:
		systemPrompt = BuildAnalysisPrompt(AnalysisPromptInput{
			AnalysisType:    AnalysisTypeOngoing,
			CampaignName:    campaignName,
			UserPhoneNumber: userPhoneNumber,
			MessageCount:    int(totalCount),
			History:         history,
		})
	case wantAutoTag:
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
	default:
		// Memory-only container: one memory-focused call instead of grafting the
		// task onto an analysis prompt that was never requested.
		systemPrompt = BuildAutoMemoryPrompt(AutoMemoryPromptInput{
			ContainerName:   campaignName,
			ContactLabel:    userPhoneNumber,
			MessageCount:    int(totalCount),
			CurrentMemories: memoryBlock,
			Transcript:      BuildTranscript(history, userPhoneNumber),
		})
	}

	// When analysis or auto-tag already runs for this entry, memory extraction
	// joins the same call rather than paying for a second one.
	if wantMemory && (wantAnalysis || wantAutoTag) {
		systemPrompt += BuildAutoMemorySection(memoryBlock)
	}

	var userMessage string
	switch {
	case wantAnalysis && wantAutoTag:
		userMessage = "Analise a conversa acima e: 1) chame a ferramenta conversation_analysis com sua avaliação; 2) chame a ferramenta manage_entry_stage para classificar o lead na etapa mais adequada baseado no estado atual da conversa."
	case wantAnalysis:
		userMessage = "Analise a conversa acima e chame a ferramenta conversation_analysis com sua avaliação."
	case wantAutoTag:
		userMessage = "Siga os passos do sistema: leia a transcrição INTEIRA, identifique o estado MAIS RECENTE da negociação (foque nas últimas mensagens), compare com as descrições das etapas, e chame manage_entry_stage com a etapa correta. Se a etapa atual já está correta, passe a mesma etapa."
	default:
		userMessage = "Siga as instruções do sistema: leia a transcrição INTEIRA e gerencie a memória do lead com a ferramenta manage_lead_memory. Se não houver fatos duráveis novos ou alterados, não chame nenhuma ferramenta."
	}
	if wantMemory && (wantAnalysis || wantAutoTag) {
		userMessage += " Além disso, registre na memória do lead, via manage_lead_memory, os fatos duráveis novos ou alterados desta conversa, se houver."
	}

	aiModel := "openai/gpt-4o-mini"
	if subject.AIModel != "" {
		aiModel = subject.AIModel
	}

	// The product-wide AI floor, fail-closed: a balance we cannot read counts
	// as too low. The number comes from the domain so every path that spends on
	// a model agrees on one floor.
	if j.cachedBalanceChecker != nil {
		bal, err := j.cachedBalanceChecker.GetBalance(workspaceID)
		if err != nil {
			log.Printf("[analysis-debounce] balance check error for workspace %s: %v, skipping analysis (fail-closed)", workspaceID, err)
			return nil
		}
		if bal < balance.MinAIFloorMicros {
			log.Printf("[analysis-debounce] workspace %s balance (%d micros) below minimum floor (%d micros), skipping analysis", workspaceID, bal, balance.MinAIFloorMicros)
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
		if toolCall.Name == tools_usecase.ManageEntryStageToolName && toolCall.Result != nil {
			log.Printf("[analysis-debounce] auto-stage result for entry %s: %v", entryID, toolCall.Result.Result)
		}
		if toolCall.Name == tools_usecase.ManageLeadMemoryToolName && toolCall.Result != nil {
			log.Printf("[analysis-debounce] auto-memory result for lead %s (entry %s): %v", subject.LeadID, entryID, toolCall.Result.Result)
		}
	}

	log.Printf("[analysis-debounce] completed analysis for entry %s", entryID)
	return nil
}
