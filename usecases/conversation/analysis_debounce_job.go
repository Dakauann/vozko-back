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
	"vozko/domain/dealautomation"
	"vozko/domain/lead"
	leadmemory "vozko/domain/lead_memory"
	"vozko/domain/livedecision"
	"vozko/domain/shared"
	"vozko/domain/stage"
	toolsdomain "vozko/domain/tools"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
	lead_memory_usecase "vozko/usecases/lead_memory"
	livedecisions_usecase "vozko/usecases/livedecisions"
	stage_usecase "vozko/usecases/stage"
	tools_usecase "vozko/usecases/tools"
)

const analysisDebounceTimeout = 30 * time.Second

type AnalysisDebouncePolicy interface {
	ConfiguredDebounceWindows(ctx context.Context) (map[string]time.Duration, error)
}

type analysisDebounceJob struct {
	sharedState          cache.SharedState
	messageRepo          conversation.MessageRepository
	wcEntryRepo          wce.Repository
	wcCampaignRepo       wc.Repository
	leadRepo             lead.Repository
	aiService            ai.Service
	toolRegistry         toolsdomain.Service
	stageRepo            stage.Repository
	leadMemories         leadmemory.ListUseCase
	hub                  conversation.EventBroadcaster
	cachedBalanceChecker balance.CachedBalanceChecker
	resolvers            map[shared.EntryType]AnalysisSubjectResolver
	analysisQueue        ConversationAnalysisEnqueuer
	debouncePolicy       AnalysisDebouncePolicy
	dealSettings         DealAutomationSettings
	dealDesk             tools_usecase.OpportunityManager
	cascade              QuietCascade
}

type QuietCascade interface {
	StageNeedsReview(ctx context.Context, target livedecisions_usecase.Trigger) bool
	QuietGate(ctx context.Context, target livedecisions_usecase.Trigger, wantMemory, wantDeals bool, memory, deals string) livedecision.QuietGate
}

func (j *analysisDebounceJob) SetQuietCascade(cascade QuietCascade) {
	j.cascade = cascade
}

func (j *analysisDebounceJob) stageNeedsReview(ctx context.Context, target livedecisions_usecase.Trigger) bool {
	return j.cascade == nil || j.cascade.StageNeedsReview(ctx, target)
}

func (j *analysisDebounceJob) quietGate(ctx context.Context, target livedecisions_usecase.Trigger, wantMemory, wantDeals bool, memory, deals string) livedecision.QuietGate {
	if j.cascade == nil {
		return livedecision.QuietGate{NeedsMemory: wantMemory, NeedsDeals: wantDeals}
	}
	return j.cascade.QuietGate(ctx, target, wantMemory, wantDeals, memory, deals)
}

type DealAutomationSettings interface {
	PipelineFor(workspaceID string, channel dealautomation.Channel, containerID string) (string, error)
}

func (j *analysisDebounceJob) SetDealAutomation(settings DealAutomationSettings, desk tools_usecase.OpportunityManager) {
	j.dealSettings, j.dealDesk = settings, desk
}

func (j *analysisDebounceJob) autoDealsPipeline(subject *AnalysisSubject) (string, error) {
	if j.dealSettings == nil || j.dealDesk == nil {
		return "", nil
	}
	return j.dealSettings.PipelineFor(subject.WorkspaceID, dealautomation.Channel{EntryType: subject.EntryType, Kind: subject.ContainerKind}, subject.ContainerID)
}

func toolDefinition(h toolsdomain.Handler, ctx toolsdomain.ToolContext) toolsdomain.Definition {
	if contextual, ok := h.(toolsdomain.ContextualHandler); ok && ctx.WorkspaceID != "" {
		return contextual.DefinitionWithContext(ctx)
	}
	return h.Definition()
}

type ConversationAnalysisEnqueuer interface {
	EnqueueSubject(ctx context.Context, subject *AnalysisSubject) error
}

func (j *analysisDebounceJob) SetAnalysisDebouncePolicy(p AnalysisDebouncePolicy) {
	j.debouncePolicy = p
}

func (j *analysisDebounceJob) SetAnalysisQueue(q ConversationAnalysisEnqueuer) {
	if j != nil && q != nil {
		j.analysisQueue = q
	}
}

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

func shortestDebounceWindow(windows map[string]time.Duration) time.Duration {
	shortest := audience.DebounceWindow(0)
	for _, w := range windows {
		if w > 0 && w < shortest {
			shortest = w
		}
	}
	return shortest
}

func (j *analysisDebounceJob) windowFor(entryID string, entryType shared.EntryType, windows map[string]time.Duration) time.Duration {
	fallback := audience.DebounceWindow(0)
	if len(windows) == 0 {
		return fallback
	}
	subject, err := j.resolveSubject(entryID, entryType)
	if err != nil || subject == nil || subject.WorkspaceID == "" {
		return fallback
	}
	if w, ok := windows[subject.WorkspaceID]; ok && w > 0 {
		return w
	}
	return fallback
}

func (j *analysisDebounceJob) resolveSubject(entryID string, entryType shared.EntryType) (*AnalysisSubject, error) {
	resolver, ok := j.resolvers[entryType]
	if !ok || resolver == nil {
		log.Printf("[analysis-debounce] no analysis resolver registered for %q, skipping entry %s",
			entryType, entryID)
		return nil, nil
	}
	return resolver(context.Background(), entryID)
}

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
		ContainerKind:     conversation.ContainerKindCampaign,
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

func (j *analysisDebounceJob) runAnalysisForEntry(entryID string, entryType shared.EntryType) error {
	subject, err := j.resolveSubject(entryID, entryType)
	if err != nil || subject == nil {
		return err
	}
	dealsPipeline, err := j.autoDealsPipeline(subject)
	if err != nil {
		return err
	}
	if !subject.WantsWork() && dealsPipeline == "" {
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
	if !subject.EnableAutoStaging && !(subject.EnableAutoMemory && subject.LeadID != "") && dealsPipeline == "" {
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

	workspaceID := subject.WorkspaceID
	entryTypeStr := string(entryType)

	ctx, cancel := context.WithTimeout(context.Background(), analysisDebounceTimeout)
	defer cancel()
	target := livedecisions_usecase.Trigger{WorkspaceID: workspaceID, EntryID: entryID, EntryType: entryType}
	quiet := quietConversation{subject: subject, entryID: entryID, entryType: entryTypeStr, history: history, messageCount: int(totalCount)}

	var tasks []quietTask
	if subject.EnableAutoStaging && j.stageNeedsReview(ctx, target) {
		if task, ok := j.stageTask(quiet); ok {
			tasks = append(tasks, task)
		}
	}

	memoryHandler, memoryAvailable := j.toolRegistry.Handler(tools_usecase.ManageLeadMemoryToolName)
	wantMemory := subject.EnableAutoMemory && subject.LeadID != "" && memoryAvailable
	var memoryBlock string
	if wantMemory {
		memoryBlock = lead_memory_usecase.BuildContext(ctx, j.leadMemories, lead_memory_usecase.ContextInput{
			WorkspaceID:   workspaceID,
			LeadID:        subject.LeadID,
			HasMemoryTool: true,
		})
	}

	dealHandler, dealAvailable := j.toolRegistry.Handler(tools_usecase.AutoManageOpportunityToolName)
	wantDeals := dealsPipeline != "" && dealAvailable
	var dealsBlock string
	if wantDeals {
		described, err := tools_usecase.DescribeEntryDeals(j.dealDesk, workspaceID, dealsPipeline, entryID, entryTypeStr)
		if err != nil {
			return err
		}
		dealsBlock = described
	}

	gate := j.quietGate(ctx, target, wantMemory, wantDeals, memoryBlock, dealsBlock)
	if gate.NeedsMemory {
		tasks = append(tasks, memoryTask(quiet, memoryHandler, memoryBlock))
	}
	if gate.NeedsDeals {
		tasks = append(tasks, dealTask(quiet, dealHandler, dealsBlock, dealsPipeline))
	}

	if len(tasks) == 0 {
		return nil
	}

	aiModel := "openai/gpt-4o-mini"
	if subject.AIModel != "" {
		aiModel = subject.AIModel
	}

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

	for _, task := range tasks {
		response, err := j.aiService.Generate(ctx, ai.GenerateInput{
			WorkspaceID:  workspaceID,
			Model:        aiModel,
			Temperature:  0.2,
			SystemPrompt: task.system,
			Messages:     []ai.Message{{Role: "user", Content: task.instruction}},
			Tools:        []toolsdomain.Definition{task.tool},
			ToolConfigs:  map[string]map[string]interface{}{task.tool.Name: task.config},
		})
		if err != nil {
			return err
		}
		for _, toolCall := range response.ToolCalls {
			if toolCall.Result != nil {
				log.Printf("[analysis-debounce] %s result for entry %s: %v", toolCall.Name, entryID, toolCall.Result.Result)
			}
		}
	}

	log.Printf("[analysis-debounce] completed analysis for entry %s", entryID)
	return nil
}

type quietConversation struct {
	subject      *AnalysisSubject
	entryID      string
	entryType    string
	history      []*conversation.Message
	messageCount int
}

type quietTask struct {
	system      string
	instruction string
	tool        toolsdomain.Definition
	config      map[string]interface{}
}

func (j *analysisDebounceJob) stageTask(c quietConversation) (quietTask, bool) {
	handler, ok := j.toolRegistry.Handler(tools_usecase.ManageEntryStageToolName)
	if !ok {
		return quietTask{}, false
	}
	workspaceID := c.subject.WorkspaceID
	var currentTagName string
	var allTags []*stage.Stage
	if j.stageRepo != nil {
		current, err := stage_usecase.StagesForEntry(j.stageRepo, workspaceID, stage_usecase.EntryRef{
			EntryID: c.entryID, EntryType: c.entryType, CampaignID: c.subject.ContainerID, CampaignType: c.entryType,
		})
		if err != nil {
			log.Printf("[analysis-debounce] reading the funnel of entry %s failed, offering no stage: %v", c.entryID, err)
		} else {
			allTags = current.Stages
			if current.Current != nil {
				currentTagName = current.Current.StageName
			}
		}
	}
	return quietTask{
		system: BuildAutoTagPrompt(AutoTagPromptInput{
			CampaignName:   c.subject.ContainerName,
			MessageCount:   c.messageCount,
			History:        c.history,
			CurrentTagName: currentTagName,
			Tags:           allTags,
		}),
		instruction: autoTagInstruction,
		tool: toolDefinition(handler, toolsdomain.ToolContext{
			WorkspaceID:  workspaceID,
			CampaignID:   c.subject.ContainerID,
			CampaignType: c.entryType,
			EntryID:      c.entryID,
		}),
		config: map[string]interface{}{
			"__entry_id":      c.entryID,
			"__entry_type":    c.entryType,
			"__workspace_id":  workspaceID,
			"__campaign_id":   c.subject.ContainerID,
			"__campaign_type": c.entryType,
		},
	}, true
}

func memoryTask(c quietConversation, handler toolsdomain.Handler, memoryBlock string) quietTask {
	return quietTask{
		system: BuildAutoMemoryPrompt(AutoMemoryPromptInput{
			ContainerName:   c.subject.ContainerName,
			ContactLabel:    c.subject.ContactLabel,
			MessageCount:    c.messageCount,
			CurrentMemories: memoryBlock,
			History:         c.history,
		}),
		instruction: autoMemoryInstruction,
		tool:        handler.Definition(),
		config: map[string]interface{}{
			"__workspace_id": c.subject.WorkspaceID,
			"__lead_id":      c.subject.LeadID,
			"__agent_id":     c.subject.AgentID,
			"__entry_id":     c.entryID,
			"__entry_type":   c.entryType,
		},
	}
}

func dealTask(c quietConversation, handler toolsdomain.Handler, dealsBlock, pipelineID string) quietTask {
	config := map[string]interface{}{
		"__workspace_id": c.subject.WorkspaceID,
		"__entry_id":     c.entryID,
		"__entry_type":   c.entryType,
		"__lead_id":      c.subject.LeadID,
		"__agent_id":     c.subject.AgentID,
		"pipeline_id":    pipelineID,
	}
	return quietTask{
		system: BuildAutoDealPrompt(AutoDealPromptInput{
			ContainerName: c.subject.ContainerName,
			ContactLabel:  c.subject.ContactLabel,
			MessageCount:  c.messageCount,
			CurrentDeals:  dealsBlock,
			History:       c.history,
		}),
		instruction: autoDealInstruction,
		tool:        toolDefinition(handler, toolsdomain.ToolContext{WorkspaceID: c.subject.WorkspaceID, Config: config}),
		config:      config,
	}
}
