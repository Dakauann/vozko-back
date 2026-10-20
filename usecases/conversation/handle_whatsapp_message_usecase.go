package conversation_usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"strconv"
	"strings"
	"time"
	"vozko/domain/whatsapp/messagestatus"

	"github.com/google/uuid"

	agent "vozko/domain/agent"
	"vozko/domain/ai"
	"vozko/domain/balance"
	"vozko/domain/cache"
	"vozko/domain/config"
	"vozko/domain/conversation"
	"vozko/domain/lead"
	lmw "vozko/domain/lead_message_window"
	"vozko/domain/leadexit"
	"vozko/domain/media"
	"vozko/domain/messaging"
	"vozko/domain/rag"
	"vozko/domain/shared"
	"vozko/domain/stage"
	toolsdomain "vozko/domain/tools"
	businessphone "vozko/domain/whatsapp/business_phone"
	whatsapp_template "vozko/domain/whatsapp/template"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
	workflow_domain "vozko/domain/workflow"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
	"vozko/infra/whisper"
	"vozko/usecases/agentturn"
	balance_usecase "vozko/usecases/balance"
	ia_usecase "vozko/usecases/inbox_assignment"
	shared_usecase "vozko/usecases/shared"
	tools_usecase "vozko/usecases/tools"

	"vozko/usecases/conversation/loopguard"

	aa "vozko/domain/ai_attendance"
	stage_usecase "vozko/usecases/stage"
)

const (
	whatsAppTypingRefreshAfter   = 20 * time.Second
	whatsAppTypingMinVisibleTime = 1200 * time.Millisecond
)

type handleWhatsAppMessageUseCase struct {
	formReplies             FormReplyReceiver
	aiService               ai.Service
	leadRepo                lead.Repository
	agentRepo               agent.Repository
	toolRegistry            toolsdomain.Service
	whatsappClientFactory   conversation.WhatsAppClientFactory
	historyManager          conversation.MessageHistoryManager
	messageRepo             conversation.MessageRepository
	configRepo              config.SystemConfigRepository
	whisperPool             *whisper.Pool
	wcCampaignRepo          wc.Repository
	wcEntryRepo             wce.Repository
	businessPhoneRepo       businessphone.Repository
	messageWindowRepo       lmw.Repository
	mediaStore              conversation.MediaStore
	hub                     conversation.EventBroadcaster
	assignmentService       *ia_usecase.AssignmentService
	aiAttendance            AIAttendanceRecorder
	triggerEvaluator        workflow_domain.TriggerEvaluator
	inboundRouter           *InboundRouter
	turnLock                *AgentTurnLock
	turnAssembler           *agentturn.Assembler
	workflowNames           ActorNameResolver
	stageRepo               stage.Repository
	textExtractor           media.TextExtractor
	sharedState             cache.SharedState
	ragService              rag.RAGService
	cachedBalanceChecker    balance.CachedBalanceChecker
	spendGuard              balance_usecase.SpendGuard
	llmPriceFetcher         workspace_pricing.LLMPriceFetcher
	consumeWhatsappTemplate balance.ConsumeWhatsappTemplateUseCase
	serviceMessageBilling   conversation.ServiceMessageBilling
	templateSendAttempts    whatsapp_template.SendAttemptRepository
	statusEvents            messagestatus.Recorder
	balanceLedger           balance.Repository

	billingPub messaging.MessageQueuePub

	loopGuard   loopguard.Guard
	delegations conversation.DelegationRepository
	live        LiveMode
	analysis    *AnalysisScheduler
	receptive   wc.EnsureReceptiveContainerUseCase
	exitStep    leadexit.Step
}

const failedStatusRefundTTL = 30 * 24 * time.Hour
const failedStatusRefundLockTTL = 1 * time.Minute
const failedStatusRefundAttempts = 3

func (uc *handleWhatsAppMessageUseCase) SetAssignmentService(svc *ia_usecase.AssignmentService) {
	uc.assignmentService = svc
}

type AIAttendanceRecorder interface {
	RecordAIReply(in aa.StartInput, messageID string)
}

func (uc *handleWhatsAppMessageUseCase) SetAIAttendance(svc AIAttendanceRecorder) {
	uc.aiAttendance = svc
}

func (uc *handleWhatsAppMessageUseCase) SetTriggerEvaluator(eval workflow_domain.TriggerEvaluator) {
	uc.triggerEvaluator = eval
	uc.inboundRouter = InboundRouterOf(eval)
}

func (uc *handleWhatsAppMessageUseCase) SetAgentTurnLock(lock *AgentTurnLock) {
	uc.turnLock = lock
}

func (uc *handleWhatsAppMessageUseCase) SetBillingPub(pub messaging.MessageQueuePub) {
	uc.billingPub = pub
}

func (uc *handleWhatsAppMessageUseCase) SetDelegations(delegations conversation.DelegationRepository) {
	uc.delegations = delegations
}

func (uc *handleWhatsAppMessageUseCase) SetReceptiveContainers(receptive wc.EnsureReceptiveContainerUseCase) {
	uc.receptive = receptive
}

func (uc *handleWhatsAppMessageUseCase) SetLoopGuard(g loopguard.Guard) {
	uc.loopGuard = g
}

type LiveMode interface {
	Acts(ctx context.Context, workspaceID string) bool
}

func (uc *handleWhatsAppMessageUseCase) SetLiveGate(g LiveMode) {
	uc.live = g
}

func (uc *handleWhatsAppMessageUseCase) SetAnalysisScheduler(scheduler *AnalysisScheduler) {
	uc.analysis = scheduler
}

func (uc *handleWhatsAppMessageUseCase) liveActs(workspaceID string) bool {
	return uc.live != nil && uc.live.Acts(context.Background(), workspaceID)
}

func (uc *handleWhatsAppMessageUseCase) recordAIAttendance(agentCtx *agentContext, entryID string, entryType shared.EntryType, messageID string) {
	if uc == nil || uc.aiAttendance == nil || agentCtx == nil || agentCtx.agent == nil || entryID == "" {
		return
	}
	wsID := agentCtx.getWorkspaceID()
	if wsID == "" {
		return
	}
	campaignID, _ := agentCtx.getCampaignInfo()
	model := strings.TrimSpace(agentCtx.agent.MessagingModel)
	uc.aiAttendance.RecordAIReply(aa.StartInput{
		WorkspaceID: wsID,
		EntryID:     entryID,
		EntryType:   string(entryType),
		AgentID:     agentCtx.agent.ID,
		Channel:     "whatsapp",
		CampaignID:  campaignID,
		Model:       model,
	}, messageID)
}

func (uc *handleWhatsAppMessageUseCase) guardCheckInbound(ctx context.Context, workspaceID, conversationID, text string) loopguard.Decision {
	if uc == nil || uc.loopGuard == nil {
		return loopguard.Decision{}
	}
	return uc.loopGuard.CheckInbound(ctx, workspaceID, conversationID, text)
}

func (uc *handleWhatsAppMessageUseCase) guardRecordAIResponse(ctx context.Context, workspaceID, conversationID string) loopguard.Decision {
	if uc == nil || uc.loopGuard == nil {
		return loopguard.Decision{}
	}
	return uc.loopGuard.RecordAIResponse(ctx, workspaceID, conversationID)
}

func (uc *handleWhatsAppMessageUseCase) fireWorkflowTriggers(agentCtx *agentContext, event workflow_domain.TriggerEvent, history []*conversation.Message) {
	if uc.triggerEvaluator == nil || event.EntryID == "" {
		return
	}

	if agentCtx != nil && agentCtx.wcEntry != nil &&
		agentCtx.wcEntry.AutomationEnabled != nil && !*agentCtx.wcEntry.AutomationEnabled {
		log.Printf("[whatsapp-workflow] automation disabled for entry=%s, skipping workflow triggers", event.EntryID)
		return
	}

	if agentCtx != nil && agentCtx.wcCampaign != nil && !agentCtx.automation.RunsWorkflows() {
		return
	}

	if event.WorkspaceID == "" {
		return
	}

	messageText, _ := event.Data["message"].(string)
	if dec := uc.guardCheckInbound(context.Background(), event.WorkspaceID, event.EntryID, messageText); dec.Block {
		log.Printf("[whatsapp-workflow] loop suspected for entry=%s reason=%s count=%d, skipping workflow triggers", event.EntryID, dec.Reason, dec.Count)
		return
	}

	uc.triggerEvaluator.Evaluate(event)

	if isFirstInboundMessage(history) {
		first := event
		first.TriggerType = workflow_domain.TriggerFirstMessage
		first.Reply = nil
		uc.triggerEvaluator.Evaluate(first)
	}
}

func (uc *handleWhatsAppMessageUseCase) messageEvent(agentCtx *agentContext, entryID string, entryType shared.EntryType, senderNumber string, inboundWamid string, messageText string, msgType string, mediaType string, selected *workflow_domain.OptionSelection) workflow_domain.TriggerEvent {
	data := workflow_domain.InboundMessageData(messageText, msgType, mediaType, selected)
	contact := ""
	if agentCtx != nil && agentCtx.wcLeadRecord != nil {
		contact = agentCtx.wcLeadRecord.Number
	}
	if contact == "" {
		contact = lead.NormalizeWhatsAppNumber(senderNumber)
	}
	workflow_domain.ApplyContactNumber(data, contact)
	workflow_domain.ApplyTriggerMessage(data, uc.storedInboundID(inboundWamid))
	workspaceID := ""
	if agentCtx != nil {
		workspaceID = agentCtx.getWorkspaceID()
		if cID, _ := agentCtx.getCampaignInfo(); cID != "" {
			data["campaign_id"] = cID
		}

		if ref := agentCtx.automation.WorkflowRef(); agentCtx.wcCampaign != nil && ref != "" {
			data["campaign_workflow_id"] = ref
		}

		if agentCtx.wcEntry != nil {
			campvars := map[string]interface{}{}
			for i, v := range agentCtx.wcEntry.Variables {
				campvars[fmt.Sprintf("%d", i+1)] = v
			}
			for k, v := range agentCtx.wcEntry.Metadata {
				campvars[k] = v
			}
			if agentCtx.wcLeadRecord != nil {
				campvars["lead_name"] = agentCtx.wcLeadRecord.RealName()
				campvars["lead_number"] = agentCtx.wcLeadRecord.Number
			}
			if agentCtx.wcCampaign != nil {
				campvars["campaign_name"] = agentCtx.wcCampaign.Name
			}
			data["campvars"] = campvars
		}
	}

	return workflow_domain.TriggerEvent{
		WorkspaceID: workspaceID,
		EntryID:     entryID,
		EntryType:   string(entryType),
		TriggerType: workflow_domain.TriggerMessageReceived,
		Data:        data,
	}
}

func isFirstInboundMessage(history []*conversation.Message) bool {
	for _, m := range history {
		if m != nil && m.FromCustomer() {
			return false
		}
	}
	return true
}

func (uc *handleWhatsAppMessageUseCase) canAffordAI(workspaceID, model string) bool {
	if !uc.spendGuard.Allow(workspaceID) {
		return false
	}

	if uc.llmPriceFetcher == nil || model == "" {
		return true
	}
	inputMicros, outputMicros, fetchErr := uc.llmPriceFetcher.FetchLLMPriceMicros(model)
	if fetchErr != nil {
		log.Printf("[whatsapp-usecase] price fetch failed for model %s: %v, relying on balance floor only", model, fetchErr)
		return true
	}
	return uc.spendGuard.CanAfford(workspaceID, estimatedAICallMicros(inputMicros, outputMicros))
}

func (uc *handleWhatsAppMessageUseCase) canAffordAIReply(workspaceID, model string) bool {
	if !uc.canAffordAI(workspaceID, model) {
		return false
	}
	if uc.serviceMessageBilling == nil {
		return true
	}
	if err := uc.serviceMessageBilling.AllowSend(workspaceID); err != nil {
		log.Printf("[whatsapp-usecase] workspace %s cannot pay for a service message, skipping AI reply: %v", workspaceID, err)
		return false
	}
	return true
}

func estimatedAICallMicros(inputMicros, outputMicros int64) int64 {
	const (
		estimatedInputTokens  int64 = 4000
		estimatedOutputTokens int64 = 1000
		safetyMultiplier      int64 = 2
	)
	if inputMicros <= 0 && outputMicros <= 0 {
		return 0
	}
	estimated := (inputMicros*estimatedInputTokens + outputMicros*estimatedOutputTokens) * safetyMultiplier / 1_000_000
	if estimated < 1 {
		estimated = 1
	}
	return estimated
}

type ResolvedTool struct {
	Definition toolsdomain.Definition
	Visibility []agent.ToolVisibility
	Config     map[string]interface{}
}

func (rt ResolvedTool) IsVisibleIn(v agent.ToolVisibility) bool {

	if len(rt.Visibility) > 0 {
		for _, vis := range rt.Visibility {
			if vis == v {
				return true
			}
		}
		return false
	}

	return rt.Definition.IsVisibleIn(toolsdomain.ToolVisibility(v))
}

type WhatsAppContext struct {
	UserPhoneNumber     string
	UserName            string
	ConversationID      string
	MessageReceivedTime time.Time
	CampaignName        string
	AgentName           string
	Metadata            map[string]interface{}
}

func (c WhatsAppContext) toConversationContext() shared_usecase.ConversationContext {
	return shared_usecase.ConversationContext{
		Channel:         shared_usecase.ChannelWhatsApp,
		UserPhoneNumber: c.UserPhoneNumber,
		UserName:        c.UserName,
		ConversationID:  c.ConversationID,
		StartTime:       c.MessageReceivedTime,
		CampaignName:    c.CampaignName,
		AgentName:       c.AgentName,
		Metadata:        c.Metadata,
	}
}

type agentContext struct {
	messagingPrompt string
	tools           []ResolvedTool
	wcCampaign      *wc.Campaign
	wcEntry         *wce.WhatsAppCampaignEntry
	wcLeadRecord    *lead.Lead
	agent           *agent.Agent
	skipResponse    bool
	automation      conversation.ChannelAutomation

	automationResolved bool
}

func (a *agentContext) firesWorkflow() bool {
	return a != nil && a.wcCampaign != nil && a.automation.RunsWorkflows() && a.automation.WorkflowRef() != ""
}

func (a *agentContext) sentBy() conversation.SentBy {
	if a == nil || a.agent == nil {
		return conversation.SentByAI("")
	}
	return conversation.SentByAI(a.agent.ID)
}

func (ctx *agentContext) getEntryInfo() (entryID string, entryType shared.EntryType) {
	if ctx == nil {
		return "", ""
	}
	if ctx.wcEntry != nil {
		return ctx.wcEntry.ID, shared.EntryTypeWhatsApp
	}
	return "", ""
}

func (ctx *agentContext) getLeadID() string {
	if ctx == nil || ctx.wcLeadRecord == nil {
		return ""
	}
	return ctx.wcLeadRecord.ID
}

func (ctx *agentContext) getCampaignInfo() (campaignID string, campaignType string) {
	if ctx == nil {
		return "", ""
	}
	if ctx.wcCampaign != nil && ctx.wcCampaign.ID != "" {
		return ctx.wcCampaign.ID, "whatsapp"
	}
	if ctx.wcEntry != nil && ctx.wcEntry.CampaignID != "" {
		return ctx.wcEntry.CampaignID, "whatsapp"
	}
	return "", ""
}

func (ctx *agentContext) getWorkspaceID() string {
	if ctx == nil {
		log.Printf("[whatsapp-usecase] WARNING: cannot resolve workspace_id, nil agent context")
		return ""
	}
	if ctx.agent != nil && ctx.agent.WorkspaceID != "" {
		return ctx.agent.WorkspaceID
	}
	if ctx.wcCampaign != nil && ctx.wcCampaign.WorkspaceID != "" {
		return ctx.wcCampaign.WorkspaceID
	}
	log.Printf("[whatsapp-usecase] WARNING: cannot resolve workspace_id from agent context (agent=%v, wcCampaign=%v)",
		ctx.agent != nil, ctx.wcCampaign != nil)
	return ""
}

func shouldSendAutomatedWhatsAppReply(ctx *agentContext) bool {
	return ctx != nil && !ctx.skipResponse && ctx.agent != nil
}

func (uc *handleWhatsAppMessageUseCase) sendWhatsAppFallbackTextIfEligible(
	ctx context.Context,
	client conversation.WhatsAppClient,
	agentCtx *agentContext,
	to string,
	body string,
	reason string,
) error {
	if !shouldSendAutomatedWhatsAppReply(agentCtx) {
		log.Printf("[whatsapp-audio] suppressing fallback reply (%s): no eligible automated-response context", reason)
		return nil
	}
	if client == nil {
		return errors.New("whatsapp client is required for fallback reply")
	}
	_, err := client.SendTextMessage(ctx, conversation.SendTextMessageInput{
		To:   to,
		Body: body,
	})
	return err
}

const (
	conversationHistoryLimit = 150

	AnalysisDebounceRedisKey = "analysis:debounce:pending"
)

func NewHandleWhatsAppMessageUseCase(aiService ai.Service, whatsappClientFactory conversation.WhatsAppClientFactory, leadRepo lead.Repository, agentRepo agent.Repository, toolRegistry toolsdomain.Service, historyManager conversation.MessageHistoryManager, messageRepo conversation.MessageRepository, configRepo config.SystemConfigRepository, whisperPool *whisper.Pool, wcCampaignRepo wc.Repository, wcEntryRepo wce.Repository, businessPhoneRepo businessphone.Repository, messageWindowRepo lmw.Repository, mediaStore conversation.MediaStore, hub conversation.EventBroadcaster, stageRepo stage.Repository, textExtractor media.TextExtractor, sharedState cache.SharedState, ragService rag.RAGService, cachedBalanceChecker balance.CachedBalanceChecker, llmPriceFetcher workspace_pricing.LLMPriceFetcher, consumeWhatsappTemplate balance.ConsumeWhatsappTemplateUseCase, serviceMessageBilling conversation.ServiceMessageBilling, statusEvents messagestatus.Recorder) (conversation.HandleWhatsAppMessageUseCase, error) {
	if consumeWhatsappTemplate == nil {
		return nil, fmt.Errorf("%w: whatsapp template billing", conversation.ErrRefundNotConfigured)
	}
	if wcCampaignRepo == nil {
		return nil, fmt.Errorf("%w: whatsapp campaign repository", conversation.ErrRefundNotConfigured)
	}
	if serviceMessageBilling == nil {
		return nil, fmt.Errorf("%w: whatsapp service message billing", conversation.ErrRefundNotConfigured)
	}
	if statusEvents == nil {
		return nil, errors.New("whatsapp message status recorder is required")
	}

	return &handleWhatsAppMessageUseCase{
		statusEvents:            statusEvents,
		aiService:               aiService,
		leadRepo:                leadRepo,
		agentRepo:               agentRepo,
		toolRegistry:            toolRegistry,
		whatsappClientFactory:   whatsappClientFactory,
		historyManager:          historyManager,
		messageRepo:             messageRepo,
		configRepo:              configRepo,
		whisperPool:             whisperPool,
		wcCampaignRepo:          wcCampaignRepo,
		wcEntryRepo:             wcEntryRepo,
		businessPhoneRepo:       businessPhoneRepo,
		messageWindowRepo:       messageWindowRepo,
		mediaStore:              mediaStore,
		hub:                     hub,
		stageRepo:               stageRepo,
		textExtractor:           textExtractor,
		sharedState:             sharedState,
		ragService:              ragService,
		cachedBalanceChecker:    cachedBalanceChecker,
		spendGuard:              balance_usecase.NewRequiredSpendGuard(cachedBalanceChecker, "whatsapp ai reply"),
		llmPriceFetcher:         llmPriceFetcher,
		consumeWhatsappTemplate: consumeWhatsappTemplate,
		serviceMessageBilling:   serviceMessageBilling,
	}, nil
}

func (uc *handleWhatsAppMessageUseCase) resolveWhatsAppClient(campaignBusinessPhoneID, receivedBusinessPhoneID string) (conversation.WhatsAppClient, error) {
	if uc.whatsappClientFactory == nil {
		return nil, fmt.Errorf("whatsapp client factory not configured")
	}

	if receivedBusinessPhoneID != "" {
		client, err := uc.whatsappClientFactory.ClientForPhone(receivedBusinessPhoneID)
		if err == nil {
			log.Printf("[whatsapp-usecase] resolved client for received phone: %s", receivedBusinessPhoneID)
			return client, nil
		}
		log.Printf("[whatsapp-usecase] failed to create client for received phone %s: %v", receivedBusinessPhoneID, err)
	}

	if campaignBusinessPhoneID != "" {
		client, err := uc.whatsappClientFactory.ClientForPhone(campaignBusinessPhoneID)
		if err == nil {
			log.Printf("[whatsapp-usecase] resolved client for campaign phone: %s", campaignBusinessPhoneID)
			return client, nil
		}
		log.Printf("[whatsapp-usecase] failed to create client for campaign phone %s: %v", campaignBusinessPhoneID, err)
	}

	return nil, fmt.Errorf("cannot resolve WhatsApp client: no valid business phone (campaign=%q, received=%q)", campaignBusinessPhoneID, receivedBusinessPhoneID)
}

func (uc *handleWhatsAppMessageUseCase) Execute(ctx context.Context, payload *conversation.WhatsAppWebhookPayload) error {
	if ctx == nil {
		ctx = context.Background()
	}

	if payload == nil {
		return errors.New("whatsapp payload is nil")
	}

	statusErr := uc.logStatusUpdates(payload)
	uc.foldInboundReactions(payload)

	message, metadata, _ := uc.extractInboundMessage(payload)
	if message == nil {
		if statusErr != nil {
			return statusErr
		}
		return conversation.ErrWhatsAppWebhookSkipped
	}
	if statusErr != nil {
		log.Printf("[whatsapp-status] continuing inbound message processing despite status update failure: %v", statusErr)
	}

	if uc.isLeadBlocked(message.From, metadata) {
		log.Printf("[whatsapp-usecase] ignoring inbound message from blocked lead %s", message.From)
		return conversation.ErrWhatsAppWebhookSkipped
	}

	if message.Image != nil && message.Image.ID != "" {
		return uc.handleMediaMessage(ctx, message, metadata, "image", message.Image.ID, message.Image.MimeType, message.Image.Caption)
	}
	if message.Video != nil && message.Video.ID != "" {
		return uc.handleMediaMessage(ctx, message, metadata, "video", message.Video.ID, message.Video.MimeType, message.Video.Caption)
	}
	if message.Document != nil && message.Document.ID != "" {
		return uc.handleMediaMessage(ctx, message, metadata, "document", message.Document.ID, message.Document.MimeType, message.Document.FileName)
	}
	if message.Sticker != nil && message.Sticker.ID != "" {
		return uc.handleMediaMessage(ctx, message, metadata, "sticker", message.Sticker.ID, message.Sticker.MimeType, "")
	}

	if message.Audio != nil && message.Audio.ID != "" {
		return uc.handleAudioMessage(ctx, message, metadata)
	}

	if message.Text == nil || strings.TrimSpace(message.Text.Body) == "" {

		return conversation.ErrWhatsAppWebhookSkipped
	}
	log.Printf("[whatsapp-usecase] message %s of type %s received at %s", message.ID, message.Type, time.Now().UTC().Format(time.RFC3339))

	if uc.aiService == nil {
		return errors.New("ai service not configured")
	}

	agentCtx, leadRecord := uc.resolveAgentContext(message.From, metadata)
	var conversationID string
	if agentCtx != nil && agentCtx.wcLeadRecord != nil {
		conversationID = agentCtx.wcLeadRecord.ID
	} else {
		conversationID = deriveConversationID(message.From, leadRecord)
	}
	businessNumber := metadataBusinessNumber(metadata)

	ctx = withAgentTurn(ctx, agentCtx)

	entryID, entryType := agentCtx.getEntryInfo()
	if entryID != "" {
		log.Printf("[whatsapp-usecase] resolved entry_id=%s, entry_type=%s for number %s", entryID, entryType, message.From)
	} else {
		log.Printf("[whatsapp-usecase] no entry found for number %s - message will not be recorded to history", message.From)
	}

	var receivedBusinessPhoneID string
	if metadata != nil && metadata.PhoneNumberID != "" && uc.businessPhoneRepo != nil {
		businessPhone, err := uc.businessPhoneRepo.FindByMetaPhoneNumberID(metadata.PhoneNumberID)

		if err == nil && businessPhone != nil {
			receivedBusinessPhoneID = businessPhone.ID
			log.Printf("[whatsapp-usecase] resolved business phone: meta_id=%s -> db_id=%s", metadata.PhoneNumberID, receivedBusinessPhoneID)

			if entryType == shared.EntryTypeWhatsApp && entryID != "" && uc.wcEntryRepo != nil {
				if err := uc.wcEntryRepo.UpdateReceivedBusinessPhone(entryID, receivedBusinessPhoneID); err != nil {
					log.Printf("[whatsapp-usecase] failed to update received business phone for entry %s: %v", entryID, err)
				}
			}

			if leadRecord != nil && uc.messageWindowRepo != nil {
				if _, err := uc.messageWindowRepo.RecordMessage(leadRecord.ID, receivedBusinessPhoneID); err != nil {
					log.Printf("[whatsapp-usecase] failed to record message window for lead %s, phone %s: %v", leadRecord.ID, receivedBusinessPhoneID, err)
				} else {
					log.Printf("[whatsapp-usecase] recorded message window for lead %s, business phone %s", leadRecord.ID, receivedBusinessPhoneID)
				}
			}

			if uc.messageWindowRepo != nil && agentCtx != nil && agentCtx.wcEntry != nil && agentCtx.wcEntry.LeadID != "" {
				entryLeadID := agentCtx.wcEntry.LeadID
				if leadRecord == nil || entryLeadID != leadRecord.ID {
					if _, err := uc.messageWindowRepo.RecordMessage(entryLeadID, receivedBusinessPhoneID); err != nil {
						log.Printf("[whatsapp-usecase] failed to record message window for entry lead %s: %v", entryLeadID, err)
					} else {
						log.Printf("[whatsapp-usecase] recorded message window for entry lead %s (entry %s), business phone %s", entryLeadID, agentCtx.wcEntry.ID, receivedBusinessPhoneID)
					}
				}
			}
		} else if err != nil {
			log.Printf("[whatsapp-usecase] failed to find business phone by meta ID %s: %v", metadata.PhoneNumberID, err)
		}
	}

	if uc.assignmentService != nil && entryID != "" {
		phoneForAssignment := receivedBusinessPhoneID
		if phoneForAssignment == "" {
			phoneForAssignment = replyPhoneID(agentCtx, receivedBusinessPhoneID)
		}
		if phoneForAssignment != "" {
			assignedUID := uc.assignmentService.EnsureAssignment(entryID, string(entryType), phoneForAssignment)
			if assignedUID != "" {
				log.Printf("[whatsapp-usecase] inbox assigned to user %s for entry %s", assignedUID, entryID)
			}
		}
	}

	var history []*conversation.Message
	if uc.messageRepo != nil && entryID != "" {
		var err error
		history, err = uc.messageRepo.ListByEntry(entryID, entryType)
		if err != nil {
			log.Printf("[whatsapp-usecase] failed to load conversation history: %v", err)
		} else {
			log.Printf("[whatsapp-usecase] loaded %d messages from entry history", len(history))
		}
	}

	formTarget := formReplyTarget{workspaceID: agentCtx.getWorkspaceID(), entryID: entryID, entryType: entryType, conversationID: conversationID, businessPhoneID: receivedBusinessPhoneID}
	if agentCtx != nil && agentCtx.wcEntry != nil {
		formTarget.leadID = agentCtx.wcEntry.LeadID
	}
	if formTarget.leadID == "" && leadRecord != nil {
		formTarget.leadID = leadRecord.ID
	}
	formReply, err := uc.receiveFormReply(ctx, message, formTarget)
	if err != nil {
		return err
	}

	if uc.historyManager != nil && entryID != "" {
		record, err := withFormCard(uc.inboundTextRecord(message, inboundTarget{entryID: entryID, entryType: entryType, conversationID: conversationID, businessNumber: businessNumber}, leadRecord), formReply)
		if err != nil {
			return err
		}
		if err := uc.historyManager.Record(ctx, record); err != nil {
			return err
		}
	}
	exit, err := uc.runLeadExit(ctx, message, whatsAppExitTarget{
		agentCtx: agentCtx, leadRecord: leadRecord, entryID: entryID, entryType: entryType, conversationID: conversationID,
		businessNumber: businessNumber, receivedPhoneID: receivedBusinessPhoneID, history: history,
	})
	if err != nil {
		return err
	}
	selected := selectedOptionFromMessage(message, exit.Selection)

	resumedByForm := formReply != nil && formReply.ResumedRunID != ""
	inboundEvent := uc.messageEvent(agentCtx, entryID, entryType, message.From, message.ID, message.Text.Body, "text", "", selected)
	route := uc.routeInbound(agentCtx, inboundEvent, resumedByForm)
	uc.deliverInbound(agentCtx, route, inboundEvent, history, agentCtx.firesWorkflow() && !resumedByForm)

	if agentCtx != nil && (agentCtx.skipResponse || exit.OptedOut || !route.Consumer.AgentReplies()) {
		log.Printf("[whatsapp-usecase] message recorded, but skipping AI response (agent responses disabled=%v, lead opted out=%v, consumer=%s)", agentCtx.skipResponse, exit.OptedOut, route.Consumer)

		if agentCtx.wcCampaign != nil && (agentCtx.wcCampaign.EnableAnalysis || agentCtx.wcCampaign.EnableAutoStaging) {
			go uc.maybeRunWhatsAppCampaignTools(context.Background(), agentCtx, leadRecord, conversationID, history)
		}
		return nil
	}

	if IsFloodDetected(ctx) {
		log.Printf("[whatsapp-usecase] message recorded, but skipping AI response (flood detected for %s)", message.From)
		return nil
	}

	if dec := uc.guardCheckInbound(ctx, agentCtx.getWorkspaceID(), entryID, message.Text.Body); dec.Block {
		log.Printf("[whatsapp-usecase] loop suspected for entry=%s reason=%s count=%d, skipping AI response", entryID, dec.Reason, dec.Count)
		return nil
	}

	release, held := uc.turnLock.HoldForInbound(ctx, entryID, entryType)
	if !held {
		release()
		return nil
	}
	turn := whatsAppInboundTurn{agentCtx: agentCtx, leadRecord: leadRecord, message: message, entryID: entryID, entryType: entryType,
		conversationID: conversationID, businessNumber: businessNumber, receivedPhoneID: receivedBusinessPhoneID}
	err = uc.sendAgentReply(ctx, turn.reply(message.Text.Body, conversation.PromptContent(message.Text.Body, "")))
	release()
	if err := failedReply(err); err != nil {
		return err
	}

	go uc.maybeRunWhatsAppCampaignTools(context.Background(), agentCtx, leadRecord, conversationID, history)

	return nil
}

func (uc *handleWhatsAppMessageUseCase) composeConversationHistory(history []*conversation.Message, businessNumber, userNumber string) []ai.Message {
	if len(history) == 0 {
		return nil
	}

	start := ai.HistoryWindowStart(len(history), conversationHistoryLimit)

	businessNorm := lead.NormalizeNumber(businessNumber)
	userNorm := lead.NormalizeNumber(userNumber)
	messages := make([]ai.Message, 0, len(history[start:]))
	labels := historySourceLabels(uc.workflowNames, history[start:])

	for _, msg := range history[start:] {
		if msg == nil {
			continue
		}

		switch msg.MessageType {
		case conversation.MessageTypeToolCall,
			conversation.MessageTypeToolResult,
			conversation.MessageTypeSystem:
			continue
		}
		if msg.MessageType.IsCallEvent() {
			continue
		}
		if strings.TrimSpace(msg.ReadableText()) == "" {
			continue
		}
		content := labels.Label(msg, msg.PromptContent())

		senderNorm := lead.NormalizeNumber(msg.From)
		role := ai.RoleUser
		switch {
		case businessNorm != "" && senderNorm == businessNorm:
			role = ai.RoleAssistant
		case userNorm != "" && senderNorm == userNorm:
			role = ai.RoleUser
		case businessNorm == "" && strings.EqualFold(strings.TrimSpace(msg.From), strings.TrimSpace(businessNumber)):
			role = ai.RoleAssistant
		case userNorm == "" && strings.EqualFold(strings.TrimSpace(msg.From), strings.TrimSpace(userNumber)):
			role = ai.RoleUser
		case msg.To != "" && businessNorm != "" && lead.NormalizeNumber(msg.To) == businessNorm:
			role = ai.RoleUser
		case msg.To != "" && userNorm != "" && lead.NormalizeNumber(msg.To) == userNorm:
			role = ai.RoleAssistant
		}

		messages = append(messages, ai.Message{Role: role, Content: content})
	}

	return messages
}

func sanitizeAIOutput(text string) string {
	lines := strings.Split(text, "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[Tool Call]") ||
			strings.HasPrefix(trimmed, "[Tool Result]") ||
			strings.HasPrefix(trimmed, "[Tool Error]") {
			continue
		}
		filtered = append(filtered, line)
	}
	result := strings.TrimSpace(strings.Join(filtered, "\n"))
	return result
}

func filterEmptyStrings(ss []string) []string {
	result := make([]string, 0, len(ss))
	for _, s := range ss {
		if s != "" {
			result = append(result, s)
		}
	}
	return result
}

func (uc *handleWhatsAppMessageUseCase) maybeRunWhatsAppCampaignTools(ctx context.Context, agentCtx *agentContext, leadRecord *lead.Lead, conversationID string, history []*conversation.Message) {
	if uc.aiService == nil || uc.toolRegistry == nil {
		return
	}
	if agentCtx == nil {
		return
	}

	if agentCtx.agent == nil {
		entryID, _ := agentCtx.getEntryInfo()
		if entryID == "" {
			return
		}

		uc.analysis.ScheduleAnalysis(entryID, shared.EntryTypeWhatsApp)
		return
	}

	isWhatsAppCampaign := agentCtx.wcCampaign != nil && agentCtx.wcEntry != nil

	if !isWhatsAppCampaign {
		log.Printf("[whatsapp-usecase] no campaign context available for campaign tools (missing campaign or entry)")
		return
	}

	campaignAnalysisEnabled := isWhatsAppCampaign && agentCtx.wcCampaign.EnableAnalysis
	autoTagEnabled := isWhatsAppCampaign && agentCtx.wcCampaign.EnableAutoStaging
	liveStages := autoTagEnabled && uc.liveActs(agentCtx.wcCampaign.WorkspaceID)
	if liveStages {
		autoTagEnabled = false
	}

	if !campaignAnalysisEnabled && !autoTagEnabled && !liveStages {
		return
	}

	var userPhoneNumber string
	if agentCtx.wcLeadRecord != nil {
		userPhoneNumber = agentCtx.wcLeadRecord.Number
	} else if leadRecord != nil {
		userPhoneNumber = leadRecord.Number
	}
	if userPhoneNumber == "" {
		return
	}

	var entryID string
	var entryType string
	var campaignID string
	var campaignName string
	var workspaceID string

	entryID = agentCtx.wcEntry.ID
	entryType = "whatsapp"
	campaignID = agentCtx.wcCampaign.ID
	campaignName = agentCtx.wcCampaign.Name
	workspaceID = agentCtx.wcCampaign.WorkspaceID

	totalCount, err := uc.messageRepo.CountByEntry(entryID, shared.EntryType(entryType))
	if err != nil {
		log.Printf("[whatsapp-usecase] failed to count messages for campaign tools: %v", err)
		return
	}

	log.Printf("[whatsapp-usecase] triggering campaign tools (total messages: %d, analysis: %v, auto_tag: %v)", totalCount, campaignAnalysisEnabled, autoTagEnabled)

	var aiTools []toolsdomain.Definition
	toolConfigs := map[string]map[string]interface{}{}

	if campaignAnalysisEnabled || liveStages {
		uc.analysis.ScheduleAnalysis(entryID, shared.EntryType(entryType))
	}

	if autoTagEnabled {
		if h, ok := uc.toolRegistry.Handler(tools_usecase.ManageEntryStageToolName); ok {
			var tagDef toolsdomain.Definition
			if ch, ok2 := h.(toolsdomain.ContextualHandler); ok2 && workspaceID != "" {
				tagDef = ch.DefinitionWithContext(toolsdomain.ToolContext{WorkspaceID: workspaceID, CampaignID: campaignID, CampaignType: entryType})
			} else {
				tagDef = h.Definition()
			}
			aiTools = append(aiTools, tagDef)
			toolConfigs[tools_usecase.ManageEntryStageToolName] = map[string]interface{}{
				"__entry_id":      entryID,
				"__entry_type":    entryType,
				"__workspace_id":  workspaceID,
				"__campaign_id":   campaignID,
				"__campaign_type": entryType,
			}
		}
	}

	if len(aiTools) == 0 {
		log.Printf("[whatsapp-usecase] no tools resolved for campaign tools call")
		return
	}

	var currentTagName string
	var allTags []*stage.Stage
	if uc.stageRepo != nil {
		current, err := stage_usecase.StagesForEntry(uc.stageRepo, workspaceID, stage_usecase.EntryRef{
			EntryID: entryID, EntryType: entryType, CampaignID: campaignID, CampaignType: entryType,
		})
		if err != nil {
			log.Printf("[whatsapp-message] reading the funnel of entry %s failed, offering no stage: %v", entryID, err)
		} else {
			allTags = current.Stages
			if current.Current != nil {
				currentTagName = current.Current.StageName
			}
		}
	}
	prompt := BuildAutoTagPrompt(AutoTagPromptInput{
		EntryID:        entryID,
		CampaignName:   campaignName,
		MessageCount:   int(totalCount),
		History:        history,
		CurrentTagName: currentTagName,
		Tags:           allTags,
	})

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	aiModel := "openai/gpt-4o-mini"
	if isWhatsAppCampaign && agentCtx.wcCampaign.AiModel != "" {
		aiModel = agentCtx.wcCampaign.AiModel
	}

	if !uc.canAffordAI(workspaceID, aiModel) {
		log.Printf("[whatsapp-usecase] skipping campaign tools AI call (insufficient balance for workspace %s)", workspaceID)
		return
	}

	response, err := uc.aiService.Generate(ctx, prompt.ApplyTo(ai.GenerateInput{
		WorkspaceID: workspaceID,
		Model:       aiModel,
		Temperature: 0.2,
		Tools:       aiTools,
		ToolConfigs: toolConfigs,
	}))
	if err != nil {
		log.Printf("[whatsapp-usecase] campaign tools AI call failed: %v", err)
		return
	}

	for _, toolCall := range response.ToolCalls {
		if toolCall.Name == tools_usecase.ManageEntryStageToolName && toolCall.Result != nil {
			log.Printf("[whatsapp-usecase] auto-stage result: %v", toolCall.Result.Result)
		}
	}
}

func (uc *handleWhatsAppMessageUseCase) logStatusUpdates(payload *conversation.WhatsAppWebhookPayload) error {
	if payload == nil {
		return nil
	}

	var statusErrors []error
	var statusEvents []messagestatus.Event

	for _, entry := range payload.Entry {
		for _, change := range entry.Changes {
			for _, status := range change.Value.Statuses {
				if event, ok := statusEventOf(change.Value.Metadata.PhoneNumberID, status, time.Now()); ok {
					statusEvents = append(statusEvents, event)
				}
				logMessage := fmt.Sprintf("[whatsapp-status] Message %s: status=%s, recipient=%s, timestamp=%s",
					status.ID, status.Status, status.RecipientID, status.Timestamp)

				if len(status.Errors) > 0 {
					for _, err := range status.Errors {
						logMessage += fmt.Sprintf(" | ERROR code=%d title=%s message=%s", err.Code, err.Title, err.Message)
					}
					log.Printf("%s", logMessage)
				} else {
					log.Printf("%s", logMessage)
				}

				entryStatus, ok := mapWhatsAppWebhookEntryStatus(status.Status)
				if !ok {
					continue
				}
				deliveryStatus, hasDeliveryStatus := mapWhatsAppWebhookDeliveryStatus(status.Status)

				uc.settleTemplateSendAttempt(status, deliveryStatus, hasDeliveryStatus)

				if uc.wcEntryRepo != nil && status.ID != "" {
					if entryStatus == wce.SendStatusFailed {
						errorCode, errorMessage := formatWhatsAppStatusError(status)
						entry, err := uc.wcEntryRepo.FindByMessageID(status.ID)
						if err != nil {
							if err := uc.wcEntryRepo.UpdateStatusByMessageID(status.ID, entryStatus); err != nil {
								log.Printf("[whatsapp-status] Failed to update failed entry status for message %s: %v", status.ID, err)
								statusErrors = append(statusErrors, fmt.Errorf("update failed campaign entry status by message id %s: %w", status.ID, err))
							}
						} else {
							if err := uc.wcEntryRepo.UpdateStatus(entry.ID, entryStatus, status.ID, errorCode, errorMessage); err != nil {
								log.Printf("[whatsapp-status] Failed to update failed entry status for message %s: %v", status.ID, err)
								statusErrors = append(statusErrors, fmt.Errorf("update failed campaign entry %s: %w", entry.ID, err))
							} else {
								if err := uc.refundFailedWhatsAppCampaignEntry(status, entry); err != nil {
									log.Printf("[whatsapp-status] Failed to refund failed campaign entry for message %s: %v", status.ID, err)
									statusErrors = append(statusErrors, fmt.Errorf("refund failed campaign entry for message %s: %w", status.ID, err))
								}
							}
						}
					} else {
						if err := uc.wcEntryRepo.UpdateStatusByMessageID(status.ID, entryStatus); err != nil {
							log.Printf("[whatsapp-status] Failed to update entry status for message %s: %v", status.ID, err)
							statusErrors = append(statusErrors, fmt.Errorf("update campaign entry status by message id %s: %w", status.ID, err))
						}
					}
				}

				if uc.messageRepo != nil && status.ID != "" && hasDeliveryStatus {
					failureCode, failureMessage := formatWhatsAppStatusError(status)
					receipt := buildDeliveryReceipt(status, deliveryStatus, failureCode, failureMessage)
					if err := uc.messageRepo.UpdateDeliveryReceipt(status.ID, receipt); err != nil {
						log.Printf("[whatsapp-status] Failed to update delivery status for wamid %s: %v", status.ID, err)
						statusErrors = append(statusErrors, fmt.Errorf("update delivery status for wamid %s: %w", status.ID, err))
					}

					uc.chargeServiceMessage(status, receipt, change.Value.Metadata.PhoneNumberID)

					if uc.hub != nil {
						msg, err := uc.messageRepo.GetByWhatsAppMessageID(status.ID)
						if err == nil && msg != nil {
							uc.hub.BroadcastMessageStatus(msg.EntryID, string(msg.EntryType), msg.ID, deliveryStatus)
						}
					}
				}

				if status.Conversation != nil {
					log.Printf("[whatsapp-status] Conversation: id=%s, origin_type=%s",
						status.Conversation.ID, status.Conversation.Origin.Type)
				}

			}
		}
	}

	if err := uc.statusEvents.Record(statusEvents...); err != nil {
		statusErrors = append(statusErrors, err)
	}

	if len(statusErrors) > 0 {
		return fmt.Errorf("%w: %v", conversation.ErrWhatsAppWebhookRetryable, errors.Join(statusErrors...))
	}
	return nil
}

func statusEventOf(phoneNumberID string, status conversation.WhatsAppStatus, now time.Time) (messagestatus.Event, bool) {
	parsed, ok := messagestatus.ParseStatus(status.Status)
	if !ok {
		return messagestatus.Event{}, false
	}
	occurredAt, ok := messagestatus.ParseOccurredAt(status.Timestamp)
	if !ok {
		occurredAt = now.UTC()
	}
	event := messagestatus.Event{MessageID: status.ID, PhoneNumberID: phoneNumberID, Status: parsed, OccurredAt: occurredAt}
	if len(status.Errors) > 0 {
		event.ErrorCode = status.Errors[0].Code
	}
	if status.Pricing != nil {
		event.Pricing = status.Pricing.Category
	}
	return event, event.Validate() == nil
}

func mapWhatsAppWebhookEntryStatus(status string) (wce.SendStatus, bool) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "sent":
		return wce.SendStatusSent, true
	case "delivered":
		return wce.SendStatusDelivered, true
	case "read":
		return wce.SendStatusRead, true
	case "failed":
		return wce.SendStatusFailed, true
	default:
		return "", false
	}
}

func mapWhatsAppWebhookDeliveryStatus(status string) (conversation.DeliveryStatus, bool) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "sent":
		return conversation.DeliveryStatusSent, true
	case "delivered":
		return conversation.DeliveryStatusDelivered, true
	case "read":
		return conversation.DeliveryStatusRead, true
	case "failed":
		return conversation.DeliveryStatusFailed, true
	default:
		return "", false
	}
}

func formatWhatsAppStatusError(status conversation.WhatsAppStatus) (int, string) {
	if len(status.Errors) == 0 {
		return 0, "WhatsApp delivery failed"
	}

	errorCode := 0
	parts := make([]string, 0, len(status.Errors))
	for _, err := range status.Errors {
		if errorCode == 0 && err.Code != 0 {
			errorCode = err.Code
		}

		base := strings.TrimSpace(err.Message)
		if base == "" {
			base = strings.TrimSpace(err.Title)
		}

		details := ""
		if err.ErrorData != nil {
			if raw, ok := err.ErrorData["details"]; ok {
				if text, ok := raw.(string); ok {
					details = strings.TrimSpace(text)
				}
			}
		}

		switch {
		case base != "" && details != "" && !strings.Contains(base, details):
			parts = append(parts, base+": "+details)
		case base != "":
			parts = append(parts, base)
		case details != "":
			parts = append(parts, details)
		}
	}

	message := strings.Join(parts, " | ")
	if message == "" {
		message = "WhatsApp delivery failed"
	}
	if len(message) > 500 {
		message = message[:500]
	}
	return errorCode, message
}

func AttachTemplateSendAttempts(
	uc conversation.HandleWhatsAppMessageUseCase,
	attempts whatsapp_template.SendAttemptRepository,
	ledger balance.Repository,
) conversation.HandleWhatsAppMessageUseCase {
	if concrete, ok := uc.(*handleWhatsAppMessageUseCase); ok {
		return concrete.WithTemplateSendAttempts(attempts, ledger)
	}
	return uc
}

func (uc *handleWhatsAppMessageUseCase) WithTemplateSendAttempts(
	attempts whatsapp_template.SendAttemptRepository,
	ledger balance.Repository,
) *handleWhatsAppMessageUseCase {
	uc.templateSendAttempts = attempts
	uc.balanceLedger = ledger
	return uc
}

func (uc *handleWhatsAppMessageUseCase) resolveSendAttempt(
	ctx context.Context,
	status conversation.WhatsAppStatus,
) *whatsapp_template.SendAttempt {
	if uc.templateSendAttempts == nil {
		return nil
	}
	if ref := strings.TrimSpace(status.BizOpaqueCallbackData); ref != "" {
		if attempt, _ := uc.templateSendAttempts.FindByID(ctx, ref); attempt != nil {
			return attempt
		}
	}
	if wamid := strings.TrimSpace(status.ID); wamid != "" {
		if attempt, _ := uc.templateSendAttempts.FindByProviderMessageID(ctx, "", wamid); attempt != nil {
			return attempt
		}
	}
	return nil
}

func (uc *handleWhatsAppMessageUseCase) chargeServiceMessage(
	status conversation.WhatsAppStatus,
	receipt conversation.DeliveryReceipt,
	phoneNumberID string,
) {
	if uc.serviceMessageBilling == nil {
		return
	}
	if !uc.serviceMessageBilling.ShouldCharge(receipt) {
		return
	}

	messageID := strings.TrimSpace(status.ID)
	workspaceID := uc.resolveWorkspaceForProviderMessage(messageID)
	if workspaceID == "" {
		log.Printf("[whatsapp-status] service message %s is billable but its workspace could not be resolved; not charged, recorded for the Meta cost report", messageID)
		if err := uc.serviceMessageBilling.RecordUnattributed(messageID, phoneNumberID, receipt); err != nil {
			log.Printf("[whatsapp-status] could not record unattributed service message %s: %v", messageID, err)
		}
		return
	}

	if err := uc.serviceMessageBilling.ChargeDelivered(workspaceID, receipt, messageID); err != nil {
		log.Printf("[whatsapp-status] could not charge service message %s for workspace %s: %v", messageID, workspaceID, err)
	}
}

func (uc *handleWhatsAppMessageUseCase) resolveWorkspaceForProviderMessage(messageID string) string {
	if messageID == "" || uc.messageRepo == nil || uc.wcEntryRepo == nil || uc.wcCampaignRepo == nil {
		return ""
	}
	message, err := uc.messageRepo.GetByWhatsAppMessageID(messageID)
	if err != nil || message == nil || message.EntryID == "" {
		return ""
	}
	entry, err := uc.wcEntryRepo.FindByID(message.EntryID)
	if err != nil || entry == nil || entry.CampaignID == "" {
		return ""
	}
	campaign, err := uc.wcCampaignRepo.FindByID(entry.CampaignID)
	if err != nil || campaign == nil {
		return ""
	}
	return campaign.WorkspaceID
}

func (uc *handleWhatsAppMessageUseCase) settleTemplateSendAttempt(
	status conversation.WhatsAppStatus,
	deliveryStatus conversation.DeliveryStatus,
	hasDeliveryStatus bool,
) {
	if uc.templateSendAttempts == nil || !hasDeliveryStatus {
		return
	}
	ctx := context.Background()

	attempt := uc.resolveSendAttempt(ctx, status)
	if attempt == nil {
		return
	}

	switch deliveryStatus {
	case conversation.DeliveryStatusSent,
		conversation.DeliveryStatusDelivered,
		conversation.DeliveryStatusRead:
		if attempt.Status.InFlight() || attempt.Status == whatsapp_template.SendAttemptUnknown {
			if err := uc.templateSendAttempts.MarkSent(
				ctx, attempt.ID, strings.TrimSpace(status.ID), 200, time.Now().UTC(),
			); err != nil && !errors.Is(err, whatsapp_template.ErrSendAttemptConflict) {
				log.Printf("[whatsapp-status] could not promote attempt %s to sent: %v", attempt.ID, err)
			}
		}

	case conversation.DeliveryStatusFailed:
		if _, err := uc.refundSingleTargetSend(status, nil); err != nil {
			log.Printf("[whatsapp-status] could not settle failed attempt %s: %v", attempt.ID, err)
		}
	}
}

func (uc *handleWhatsAppMessageUseCase) refundSingleTargetSend(status conversation.WhatsAppStatus, _ *wce.WhatsAppCampaignEntry) (bool, error) {
	if uc.templateSendAttempts == nil || uc.consumeWhatsappTemplate == nil {
		return false, nil
	}
	ctx := context.Background()

	attempt := uc.resolveSendAttempt(ctx, status)
	if attempt == nil {
		return false, nil
	}
	if attempt.Status == whatsapp_template.SendAttemptRefunded {
		return true, nil
	}

	refundRef := whatsapp_template.RefundReferenceID(attempt.ID)
	if uc.balanceLedger != nil {
		already, err := uc.balanceLedger.ExistsTransactionByReferenceID(refundRef)
		if err != nil {
			return true, fmt.Errorf("check refund for send attempt %s: %w", attempt.ID, err)
		}
		if already {
			_ = uc.templateSendAttempts.MarkRefunded(ctx, attempt.ID, time.Now().UTC())
			return true, nil
		}
	}

	if err := uc.consumeWhatsappTemplate.Refund(attempt.WorkspaceID, whatsapp_template.ChargeReferenceID(attempt.ID), attempt.Category); err != nil {
		return true, fmt.Errorf("refund send attempt %s: %w", attempt.ID, err)
	}
	if err := uc.templateSendAttempts.MarkRefunded(ctx, attempt.ID, time.Now().UTC()); err != nil {
		log.Printf("[whatsapp-status] refunded attempt %s but could not mark it: %v", attempt.ID, err)
	}
	log.Printf("[whatsapp-status] refunded workspace %s for failed single-target send %s", attempt.WorkspaceID, attempt.ID)
	return true, nil
}

func (uc *handleWhatsAppMessageUseCase) refundFailedWhatsAppCampaignEntry(status conversation.WhatsAppStatus, entry *wce.WhatsAppCampaignEntry) (returnErr error) {
	if uc.consumeWhatsappTemplate == nil || uc.wcCampaignRepo == nil || entry == nil {
		return nil
	}

	messageID := strings.TrimSpace(status.ID)
	if messageID == "" {
		return nil
	}

	if uc.sharedState == nil {
		return fmt.Errorf("shared state unavailable for whatsapp refund guard")
	}

	completedKey := "whatsapp:status:refund:done:" + messageID
	processingKey := "whatsapp:status:refund:processing:" + messageID
	processingAcquired := false

	completed, err := uc.sharedState.Exists(completedKey)
	if err != nil {
		return fmt.Errorf("check whatsapp refund completion key for message %s: %w", messageID, err)
	}
	if completed {
		return nil
	}

	created, err := uc.sharedState.SetNX(processingKey, "1", failedStatusRefundLockTTL)
	if err != nil {
		return fmt.Errorf("acquire whatsapp refund processing key for message %s: %w", messageID, err)
	}
	if !created {
		return nil
	}
	processingAcquired = true
	defer func() {
		if !processingAcquired || returnErr == nil || uc.sharedState == nil {
			return
		}
		if err := uc.sharedState.Del(processingKey); err != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("release whatsapp refund processing key for message %s: %w", messageID, err))
		}
	}()

	if handled, err := uc.refundSingleTargetSend(status, entry); handled {
		return err
	}

	campaign, err := uc.wcCampaignRepo.FindByID(entry.CampaignID)
	if err != nil || campaign == nil {
		return fmt.Errorf("resolve campaign %s for refund on message %s: %w", entry.CampaignID, messageID, err)
	}

	if campaign.IsOrganic() {
		return nil
	}

	templateCategory, err := resolveWhatsAppRefundCategory(status, entry)
	if err != nil {
		return fmt.Errorf("determine refund category for message %s: %w", messageID, err)
	}

	if err := uc.retryFailedWhatsAppCampaignRefund(campaign.WorkspaceID, entry.ChargeReference(), templateCategory); err != nil {
		return fmt.Errorf("refund workspace %s for failed message %s: %w", campaign.WorkspaceID, messageID, err)
	}

	processingAcquired = false

	if err := uc.sharedState.SetString(completedKey, "1", failedStatusRefundTTL); err != nil {

		log.Printf("[whatsapp-status] WARNING: failed to store refund completion key for message %s after successful refund: %v (processing key retained as guard)", messageID, err)
	} else {

		if err := uc.sharedState.Del(processingKey); err != nil {
			log.Printf("[whatsapp-status] failed to clear refund processing key for message %s after success: %v", messageID, err)
		}
	}

	log.Printf("[whatsapp-status] refunded workspace %s for failed campaign message %s (campaign=%s category=%s)", campaign.WorkspaceID, messageID, campaign.ID, templateCategory)
	return nil
}

func (uc *handleWhatsAppMessageUseCase) retryFailedWhatsAppCampaignRefund(workspaceID, chargeReference, templateCategory string) error {
	var lastErr error
	for attempt := 1; attempt <= failedStatusRefundAttempts; attempt++ {
		lastErr = uc.consumeWhatsappTemplate.Refund(workspaceID, chargeReference, templateCategory)
		if lastErr == nil {
			return nil
		}
	}
	return lastErr
}

func buildDeliveryReceipt(
	status conversation.WhatsAppStatus,
	deliveryStatus conversation.DeliveryStatus,
	failureCode int,
	failureMessage string,
) conversation.DeliveryReceipt {
	receipt := conversation.DeliveryReceipt{
		Status:       deliveryStatus,
		ErrorCode:    failureCode,
		ErrorMessage: failureMessage,
	}

	if status.Pricing != nil {
		receipt.Pricing = status.Pricing.MetaPricing()
	}

	if status.Conversation != nil {
		receipt.ConversationOrigin = status.Conversation.Origin.Type
	}

	return receipt
}

func resolveWhatsAppRefundCategory(status conversation.WhatsAppStatus, entry *wce.WhatsAppCampaignEntry) (string, error) {
	if status.Pricing != nil {
		category := strings.ToUpper(strings.TrimSpace(status.Pricing.Category))
		if category != "" {
			return category, nil
		}
	}

	if category := templateCategoryFromCampaignEntry(entry); category != "" {
		return category, nil
	}

	return "", fmt.Errorf("whatsapp template category unavailable for refund")
}

func templateCategoryFromCampaignEntry(entry *wce.WhatsAppCampaignEntry) string {
	if entry == nil || entry.Metadata == nil {
		return ""
	}

	rawTemplateInfo, ok := entry.Metadata["template_info"]
	if !ok {
		return ""
	}

	templateInfo, ok := rawTemplateInfo.(map[string]interface{})
	if !ok {
		return ""
	}

	rawCategory, ok := templateInfo["category"]
	if !ok {
		return ""
	}

	category, ok := rawCategory.(string)
	if !ok {
		return ""
	}

	return strings.ToUpper(strings.TrimSpace(category))
}

func (uc *handleWhatsAppMessageUseCase) extractInboundMessage(payload *conversation.WhatsAppWebhookPayload) (*conversation.WhatsAppMessage, *conversation.WhatsAppMetadata, string) {
	if payload == nil || len(payload.Entry) == 0 {
		return nil, nil, ""
	}

	for i := range payload.Entry {
		entry := payload.Entry[i]
		if len(entry.Changes) == 0 {
			continue
		}
		for j := range entry.Changes {
			change := entry.Changes[j]
			if len(change.Value.Messages) == 0 {
				continue
			}
			metadata := change.Value.Metadata

			var contactName string
			for _, contact := range change.Value.Contacts {
				if strings.TrimSpace(contact.Profile.Name) != "" {
					contactName = strings.TrimSpace(contact.Profile.Name)
					break
				}
			}

			for k := range change.Value.Messages {
				msg := change.Value.Messages[k]
				if uc.isOutboundMessage(msg.From, change.Value.Metadata.PhoneNumberID) {
					continue
				}

				msgType := strings.ToLower(msg.Type)
				log.Printf("[whatsapp-usecase] extractInboundMessage: msgType=%s, from=%s, id=%s", msgType, msg.From, msg.ID)

				if _, isForm := msg.FormReply(); msgType == "interactive" && isForm {
					msg.Type = "text"
					msg.Text = &conversation.WhatsAppTextMessage{Body: conversation.FormReplyPlaceholder}
					change.Value.Messages[k] = msg
					return &change.Value.Messages[k], &metadata, contactName
				}

				if msgType == "interactive" && msg.Interactive != nil {
					log.Printf("[whatsapp-usecase] Interactive message received: type=%s, buttonReply=%v, listReply=%v",
						msg.Interactive.Type, msg.Interactive.ButtonReply != nil, msg.Interactive.ListReply != nil)
					var buttonText string
					if msg.Interactive.ButtonReply != nil {
						buttonText = msg.Interactive.ButtonReply.Title
						log.Printf("[whatsapp-usecase] Button reply: id=%s, title=%s", msg.Interactive.ButtonReply.ID, buttonText)
					} else if msg.Interactive.ListReply != nil {
						buttonText = msg.Interactive.ListReply.Title
						log.Printf("[whatsapp-usecase] List reply: id=%s, title=%s", msg.Interactive.ListReply.ID, buttonText)
					}
					if strings.TrimSpace(buttonText) != "" {
						msg.Type = "text"
						msg.Text = &conversation.WhatsAppTextMessage{Body: buttonText}
						change.Value.Messages[k] = msg
						log.Printf("[whatsapp-usecase] Converted interactive to text message: body=%s", buttonText)
						return &change.Value.Messages[k], &metadata, contactName
					}
					log.Printf("[whatsapp-usecase] Interactive message had empty button text, skipping")
					continue
				}

				if msgType == "button" && msg.Button != nil {
					buttonText := msg.Button.Text
					log.Printf("[whatsapp-usecase] Button message received: text=%s, payload=%s", buttonText, msg.Button.Payload)
					if strings.TrimSpace(buttonText) != "" {
						msg.Type = "text"
						msg.Text = &conversation.WhatsAppTextMessage{Body: buttonText}
						change.Value.Messages[k] = msg
						log.Printf("[whatsapp-usecase] Converted button to text message: body=%s", buttonText)
						return &change.Value.Messages[k], &metadata, contactName
					}
					log.Printf("[whatsapp-usecase] Button message had empty text, skipping")
					continue
				}

				if msgType == "location" {
					if msg.Location == nil {
						continue
					}
					msg.Text = &conversation.WhatsAppTextMessage{Body: msg.Location.Text()}
					change.Value.Messages[k] = msg
					return &change.Value.Messages[k], &metadata, contactName
				}

				if msgType != "" && msgType != "text" && msgType != "audio" && msgType != "image" && msgType != "video" && msgType != "document" && msgType != "sticker" {
					continue
				}

				if msgType == "text" && (msg.Text == nil || strings.TrimSpace(msg.Text.Body) == "") {
					continue
				}

				if msgType == "audio" && (msg.Audio == nil || msg.Audio.ID == "") {
					continue
				}

				if msgType == "image" && (msg.Image == nil || msg.Image.ID == "") {
					continue
				}

				if msgType == "video" && (msg.Video == nil || msg.Video.ID == "") {
					continue
				}

				if msgType == "document" && (msg.Document == nil || msg.Document.ID == "") {
					continue
				}

				if msgType == "sticker" && (msg.Sticker == nil || msg.Sticker.ID == "") {
					continue
				}
				return &change.Value.Messages[k], &metadata, contactName
			}
		}
	}

	return nil, nil, ""
}

func (uc *handleWhatsAppMessageUseCase) isOutboundMessage(from string, phoneID string) bool {
	fromNorm := lead.NormalizeNumber(from)
	if fromNorm == "" {
		return true
	}
	phoneNorm := lead.NormalizeNumber(phoneID)
	if phoneNorm != "" && phoneNorm == fromNorm {
		return true
	}
	return false
}

func (uc *handleWhatsAppMessageUseCase) resolveAgentPromptAndTools(agentID, campaignID, campaignType string) (*agent.Agent, string, []ResolvedTool) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" || uc.agentRepo == nil {
		return nil, "", nil
	}
	agentRecord, err := uc.agentRepo.FindByID(agentID)
	if err != nil || agentRecord == nil {
		return nil, "", nil
	}
	prompt := strings.TrimSpace(agentRecord.MessagingPrompt)
	tools := uc.resolveAgentTools(agentRecord, campaignID, campaignType)
	return agentRecord, prompt, tools
}

func (uc *handleWhatsAppMessageUseCase) isLeadBlocked(from string, metadata *conversation.WhatsAppMetadata) bool {
	if uc.leadRepo == nil || uc.businessPhoneRepo == nil || metadata == nil || strings.TrimSpace(metadata.PhoneNumberID) == "" {
		return false
	}
	normalized := lead.NormalizeNumber(from)
	if normalized == "" {
		return false
	}
	bp, err := uc.businessPhoneRepo.FindByMetaPhoneNumberID(metadata.PhoneNumberID)
	if err != nil || bp == nil {
		return false
	}
	workspaceID := strings.TrimSpace(bp.OwnerWorkspaceID)
	if workspaceID == "" {
		return false
	}
	leadRecord, err := uc.leadRepo.FindByNumber(workspaceID, normalized)
	if err != nil || leadRecord == nil {
		return false
	}
	return leadRecord.Blocked
}

func (uc *handleWhatsAppMessageUseCase) resolveAgentContext(from string, metadata *conversation.WhatsAppMetadata) (*agentContext, *lead.Lead) {
	normalized := lead.NormalizeNumber(from)
	if normalized == "" {
		return nil, nil
	}
	phone := uc.receivingPhone(metadata)
	if actx, leadRecord, found := uc.existingConversation(normalized, phone); found {
		return actx, leadRecord
	}
	return uc.openReceptiveConversation(normalized, phone)
}

func (uc *handleWhatsAppMessageUseCase) receivingPhone(metadata *conversation.WhatsAppMetadata) *businessphone.WhatsAppBusinessPhoneNumber {
	if metadata == nil || metadata.PhoneNumberID == "" || uc.businessPhoneRepo == nil {
		return nil
	}
	phone, err := uc.businessPhoneRepo.FindByMetaPhoneNumberID(metadata.PhoneNumberID)
	if err != nil {
		return nil
	}
	return phone
}

func (uc *handleWhatsAppMessageUseCase) existingConversation(normalized string, phone *businessphone.WhatsAppBusinessPhoneNumber) (*agentContext, *lead.Lead, bool) {
	if uc.wcEntryRepo == nil || uc.wcCampaignRepo == nil || uc.agentRepo == nil {
		return nil, nil, false
	}
	var phoneID, ownerID string
	if phone != nil {
		phoneID, ownerID = phone.ID, strings.TrimSpace(phone.OwnerWorkspaceID)
	}
	entry, err := uc.wcEntryRepo.FindInboundRouteByNumberAndBusinessPhone(normalized, phoneID)
	if err != nil || entry == nil {
		return nil, nil, false
	}
	campaign, err := uc.wcCampaignRepo.FindByID(entry.CampaignID)
	if err != nil || campaign == nil {
		return nil, nil, false
	}

	var leadRecord *lead.Lead
	if uc.leadRepo != nil && campaign.WorkspaceID != "" {
		leadRecord, _ = uc.leadRepo.FindByNumber(campaign.WorkspaceID, normalized)
	}
	actx := &agentContext{wcCampaign: campaign, wcEntry: entry, wcLeadRecord: leadRecord}

	if !campaign.RunsAutomationOn(ownerID) {
		actx.skipResponse = true
		log.Printf("[whatsapp-usecase] entry %s is receptive in workspace %s, which does not own business phone %s: recording without automation",
			entry.ID, campaign.WorkspaceID, phoneID)
		return actx, leadRecord, true
	}

	responsesEnabled := campaign.EnableAgentResponses
	if entry.AutomationEnabled != nil {
		responsesEnabled = *entry.AutomationEnabled
	}
	uc.prepareAutomation(actx, responsesEnabled)
	return actx, leadRecord, true
}

func (uc *handleWhatsAppMessageUseCase) openReceptiveConversation(normalized string, phone *businessphone.WhatsAppBusinessPhoneNumber) (*agentContext, *lead.Lead) {
	if phone == nil || uc.receptive == nil || uc.leadRepo == nil || uc.wcEntryRepo == nil {
		log.Printf("[whatsapp-usecase] no conversation for number %s and no receptive to open one", normalized)
		return nil, nil
	}
	ownerID := strings.TrimSpace(phone.OwnerWorkspaceID)
	if ownerID == "" {
		log.Printf("[whatsapp-usecase] business phone %s has no owner workspace, new contact %s not routed", phone.ID, normalized)
		return nil, nil
	}
	container, _, err := uc.receptive.Execute(ownerID, phone.ID, phone.DisplayPhoneNumber)
	if err != nil || container == nil {
		log.Printf("[whatsapp-usecase] receptive of business phone %s unavailable, message from %s not routed: %v", phone.ID, normalized, err)
		return nil, nil
	}

	leadRecord, _, err := uc.leadRepo.FindOrCreate(ownerID, normalized, lead.LeadUpdate{Source: lead.SourceChannel})
	if err != nil {
		log.Printf("[whatsapp-usecase] failed to find/create lead for receptive entry: %v", err)
		return nil, nil
	}
	entry := &wce.WhatsAppCampaignEntry{
		ID:         uuid.New().String(),
		CampaignID: container.ID,
		LeadID:     leadRecord.ID,
		Status:     wce.SendStatusDelivered,
	}
	if err := uc.wcEntryRepo.Create(entry); err != nil {
		log.Printf("[whatsapp-usecase] failed to create receptive entry: %v", err)
		return nil, nil
	}
	log.Printf("[whatsapp-usecase] created receptive entry %s for lead %s in %s", entry.ID, leadRecord.ID, container.ID)

	actx := &agentContext{wcCampaign: container, wcEntry: entry, wcLeadRecord: leadRecord}
	uc.prepareAutomation(actx, container.EnableAgentResponses)
	return actx, leadRecord
}

func (uc *handleWhatsAppMessageUseCase) resolveAgentTools(agentRecord *agent.Agent, campaignID, campaignType string) []ResolvedTool {
	if uc.toolRegistry == nil || agentRecord == nil || len(agentRecord.InternalTools) == 0 {
		return nil
	}
	resolved := tools_usecase.ResolveTools(uc.toolRegistry, agentRecord.InternalTools, agent.ToolVisibilityMessaging, tools_usecase.ToolResolverOptions{
		Agent:        agentRecord,
		CampaignID:   campaignID,
		CampaignType: campaignType,
	})
	result := make([]ResolvedTool, 0, len(resolved.Definitions))
	for _, def := range resolved.Definitions {
		cfg := resolved.Configs[strings.ToLower(def.Name)]
		result = append(result, ResolvedTool{
			Definition: def,
			Config:     cfg,
		})
	}
	return result
}

func deriveConversationID(from string, leadRecord *lead.Lead) string {
	if leadRecord != nil {
		if id := strings.TrimSpace(leadRecord.ID); id != "" {
			return id
		}
	}

	candidate := lead.NormalizeNumber(from)
	if candidate == "" {
		candidate = strings.TrimSpace(from)
	}
	if candidate == "" {
		return ""
	}

	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(candidate)).String()
}

func metadataBusinessNumber(meta *conversation.WhatsAppMetadata) string {
	if meta == nil {
		return ""
	}
	if display := strings.TrimSpace(meta.DisplayPhoneNumber); display != "" {
		return display
	}
	return strings.TrimSpace(meta.PhoneNumberID)
}

func parseWhatsAppTimestamp(raw string) time.Time {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return time.Now().UTC()
	}
	seconds, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil {
		return time.Now().UTC()
	}
	return time.Unix(seconds, 0).UTC()
}

func (uc *handleWhatsAppMessageUseCase) handleMediaMessage(
	ctx context.Context,
	message *conversation.WhatsAppMessage,
	metadata *conversation.WhatsAppMetadata,
	mediaType string,
	mediaID string,
	mimeType string,
	captionOrFilename string,
) error {
	log.Printf("[whatsapp-media] %s message received at %s from %s", mediaType, time.Now().UTC().Format(time.RFC3339), message.From)
	log.Printf("[whatsapp-media] Media ID: %s, MIME: %s, Caption/Filename: %s", mediaID, mimeType, captionOrFilename)

	agentCtx, leadRecord := uc.resolveAgentContext(message.From, metadata)

	var conversationID string
	if agentCtx != nil && agentCtx.wcLeadRecord != nil {
		conversationID = agentCtx.wcLeadRecord.ID
	} else {
		conversationID = deriveConversationID(message.From, leadRecord)
	}
	businessNumber := metadataBusinessNumber(metadata)

	entryID, entryType := agentCtx.getEntryInfo()

	displayText := fmt.Sprintf("[%s]", strings.Title(mediaType))
	if captionOrFilename != "" {
		displayText = fmt.Sprintf("[%s] %s", strings.Title(mediaType), captionOrFilename)
	}

	var cdnMediaID, cdnURL string
	var mediaBytes []byte
	var receivedPhoneID string
	if metadata != nil && metadata.PhoneNumberID != "" && uc.businessPhoneRepo != nil {
		if bp, err := uc.businessPhoneRepo.FindByMetaPhoneNumberID(metadata.PhoneNumberID); err == nil && bp != nil {
			receivedPhoneID = bp.ID
		}
	}

	if receivedPhoneID != "" {
		if leadRecord != nil && uc.messageWindowRepo != nil {
			if _, err := uc.messageWindowRepo.RecordMessage(leadRecord.ID, receivedPhoneID); err != nil {
				log.Printf("[whatsapp-media] failed to record message window for lead %s, phone %s: %v", leadRecord.ID, receivedPhoneID, err)
			} else {
				log.Printf("[whatsapp-media] recorded message window for lead %s, business phone %s", leadRecord.ID, receivedPhoneID)
			}
		}
		if uc.messageWindowRepo != nil && agentCtx != nil && agentCtx.wcEntry != nil && agentCtx.wcEntry.LeadID != "" {
			entryLeadID := agentCtx.wcEntry.LeadID
			if leadRecord == nil || entryLeadID != leadRecord.ID {
				if _, err := uc.messageWindowRepo.RecordMessage(entryLeadID, receivedPhoneID); err != nil {
					log.Printf("[whatsapp-media] failed to record message window for entry lead %s: %v", entryLeadID, err)
				} else {
					log.Printf("[whatsapp-media] recorded message window for entry lead %s (entry %s), business phone %s", entryLeadID, agentCtx.wcEntry.ID, receivedPhoneID)
				}
			}
		}
	}
	mediaClient, mediaClientErr := uc.resolveWhatsAppClient("", receivedPhoneID)
	if mediaClientErr != nil {
		log.Printf("[whatsapp-media] Cannot resolve WhatsApp client for media download: %v", mediaClientErr)
	}

	if uc.assignmentService != nil && entryID != "" && receivedPhoneID != "" {
		assignedUID := uc.assignmentService.EnsureAssignment(entryID, string(entryType), receivedPhoneID)
		if assignedUID != "" {
			log.Printf("[whatsapp-media] inbox assigned to user %s for entry %s", assignedUID, entryID)
		}
	}

	if mediaClient != nil && uc.mediaStore != nil {
		var downloadedMimeType string
		var err error
		mediaBytes, downloadedMimeType, err = mediaClient.DownloadMedia(ctx, mediaID)
		if err != nil {
			log.Printf("[whatsapp-media] Failed to download media: %v", err)
		} else {
			log.Printf("[whatsapp-media] Media downloaded: %d bytes, MIME: %s", len(mediaBytes), downloadedMimeType)

			if mimeType == "" && downloadedMimeType != "" {
				mimeType = downloadedMimeType
			}

			if stored := uc.storeInboundMedia(entryID, entryType, conversation.MediaType(mediaType), mimeType, mediaBytes, captionOrFilename, mediaID); stored != nil {
				cdnMediaID, cdnURL = stored.ID, stored.URL
			}
		}
	} else {
		if mediaClient == nil {
			log.Println("[whatsapp-media] WhatsApp client not resolved, skipping media download")
		}
		if uc.mediaStore == nil {
			log.Println("[whatsapp-media] Media storage not configured, skipping media upload")
		}
	}

	var extractedText string
	if len(mediaBytes) > 0 && uc.textExtractor != nil && uc.textExtractor.CanExtract(mimeType) {
		extraction, exErr := uc.textExtractor.ExtractText(mediaBytes, mimeType)
		if exErr != nil {
			log.Printf("[whatsapp-media] Text extraction failed for %s: %v", mimeType, exErr)
		} else if extraction != nil && extraction.Text != "" {
			log.Printf("[whatsapp-media] Extracted %d chars from %s (source=%s)", len(extraction.Text), mediaType, extraction.Source)
			extractedText = extraction.Text
		} else {
			log.Printf("[whatsapp-media] No text extracted from %s", mediaType)
		}
	}

	var history []*conversation.Message
	if uc.messageRepo != nil && entryID != "" {
		history, _ = uc.messageRepo.ListByEntry(entryID, entryType)
	}

	if uc.historyManager != nil && (conversationID != "" || entryID != "") {
		domainMediaType := conversation.MediaType(mediaType)

		record := conversation.MessageHistoryRecord{
			SentBy:             conversation.SentByContact(message.From),
			EntryID:            entryID,
			EntryType:          entryType,
			Channel:            conversation.MessageChannelWhatsApp,
			MessageType:        conversation.MessageTypeMedia,
			ConversationID:     conversationID,
			MessageID:          strings.TrimSpace(message.ID),
			From:               strings.TrimSpace(message.From),
			To:                 businessNumber,
			Text:               displayText,
			Timestamp:          parseWhatsAppTimestamp(message.Timestamp),
			MediaID:            cdnMediaID,
			MediaType:          domainMediaType,
			MediaURL:           cdnURL,
			ReplyToWAMessageID: quotedProviderMessageID(message),
			AdReferral:         message.Referral.AdReferral(),
		}

		if extractedText != "" {
			record.Metadata = conversation.ExtractedTextMetadata(extractedText)
		}
		if err := uc.historyManager.Record(ctx, record); err != nil {
			log.Printf("[whatsapp-media] Failed to record history: %v", err)
		} else {
			log.Printf("[whatsapp-media] Message recorded to history (extractedText=%d chars)", len(extractedText))
		}
	}

	inboundEvent := uc.messageEvent(agentCtx, entryID, entryType, message.From, message.ID, captionOrFilename, "media", mediaType, nil)
	route := uc.routeInbound(agentCtx, inboundEvent, false)

	if extractedText != "" && !route.Consumer.AgentReplies() {
		log.Printf("[whatsapp-media] media text extracted and recorded, but skipping AI response (consumer=%s)", route.Consumer)
	} else if extractedText != "" {
		userMessage := conversation.PromptContent(displayText, extractedText)

		if uc.aiService != nil && agentCtx != nil && !agentCtx.skipResponse && !IsFloodDetected(ctx) {
			if dec := uc.guardCheckInbound(ctx, agentCtx.getWorkspaceID(), entryID, userMessage); dec.Block {
				log.Printf("[whatsapp-media] loop suspected for entry=%s reason=%s count=%d, skipping AI response", entryID, dec.Reason, dec.Count)
			} else if release, held := uc.turnLock.HoldForInbound(ctx, entryID, entryType); held {
				turn := whatsAppInboundTurn{agentCtx: agentCtx, leadRecord: leadRecord, message: message, entryID: entryID, entryType: entryType,
					conversationID: conversationID, businessNumber: businessNumber, receivedPhoneID: receivedPhoneID}
				if err := failedReply(uc.sendAgentReply(withAgentTurn(ctx, agentCtx), turn.reply(userMessage, userMessage))); err != nil {
					log.Printf("[whatsapp-media] AI reply to the media content failed: %v", err)
				}
				release()
			}
		} else if agentCtx != nil && agentCtx.skipResponse {
			log.Printf("[whatsapp-media] media text extracted and recorded, but skipping AI response (agent responses disabled)")
		} else if IsFloodDetected(ctx) {
			log.Printf("[whatsapp-media] media text extracted and recorded, but skipping AI response (flood detected)")
		}
	}

	go uc.maybeRunWhatsAppCampaignTools(context.Background(), agentCtx, leadRecord, conversationID, history)
	uc.deliverInbound(agentCtx, route, inboundEvent, history, true)

	log.Printf("[whatsapp-media] %s message processed", mediaType)
	return nil
}

func getExtensionFromMimeType(mimeType string) string {
	switch mimeType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "video/mp4":
		return ".mp4"
	case "video/3gpp":
		return ".3gp"
	case "video/quicktime":
		return ".mov"
	case "audio/aac":
		return ".aac"
	case "audio/mpeg":
		return ".mp3"
	case "audio/ogg":
		return ".ogg"
	case "audio/amr":
		return ".amr"
	case "audio/opus":
		return ".opus"
	case "application/pdf":
		return ".pdf"
	case "application/msword":
		return ".doc"
	case "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		return ".docx"
	case "application/vnd.ms-excel":
		return ".xls"
	case "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
		return ".xlsx"
	case "application/vnd.ms-powerpoint":
		return ".ppt"
	case "application/vnd.openxmlformats-officedocument.presentationml.presentation":
		return ".pptx"
	case "text/plain":
		return ".txt"
	default:
		parts := strings.Split(mimeType, "/")
		if len(parts) == 2 {
			return "." + parts[1]
		}
		return ""
	}
}

func (uc *handleWhatsAppMessageUseCase) handleAudioMessage(ctx context.Context, message *conversation.WhatsAppMessage, metadata *conversation.WhatsAppMetadata) error {
	log.Printf("[whatsapp-audio] Audio message received at %s from %s", time.Now().UTC().Format(time.RFC3339), message.From)
	log.Printf("[whatsapp-audio] Audio ID: %s, MIME: %s, Voice: %v", message.Audio.ID, message.Audio.MimeType, message.Audio.Voice)

	var audioReceivedBusinessPhoneID string
	if metadata != nil && metadata.PhoneNumberID != "" && uc.businessPhoneRepo != nil {
		if bp, err := uc.businessPhoneRepo.FindByMetaPhoneNumberID(metadata.PhoneNumberID); err == nil && bp != nil {
			audioReceivedBusinessPhoneID = bp.ID
		}
	}

	agentCtx, leadRecord := uc.resolveAgentContext(message.From, metadata)

	if audioReceivedBusinessPhoneID != "" {
		if leadRecord != nil && uc.messageWindowRepo != nil {
			if _, err := uc.messageWindowRepo.RecordMessage(leadRecord.ID, audioReceivedBusinessPhoneID); err != nil {
				log.Printf("[whatsapp-audio] failed to record message window for lead %s, phone %s: %v", leadRecord.ID, audioReceivedBusinessPhoneID, err)
			} else {
				log.Printf("[whatsapp-audio] recorded message window for lead %s, business phone %s", leadRecord.ID, audioReceivedBusinessPhoneID)
			}
		}
		if uc.messageWindowRepo != nil && agentCtx != nil && agentCtx.wcEntry != nil && agentCtx.wcEntry.LeadID != "" {
			entryLeadID := agentCtx.wcEntry.LeadID
			if leadRecord == nil || entryLeadID != leadRecord.ID {
				if _, err := uc.messageWindowRepo.RecordMessage(entryLeadID, audioReceivedBusinessPhoneID); err != nil {
					log.Printf("[whatsapp-audio] failed to record message window for entry lead %s: %v", entryLeadID, err)
				} else {
					log.Printf("[whatsapp-audio] recorded message window for entry lead %s (entry %s), business phone %s", entryLeadID, agentCtx.wcEntry.ID, audioReceivedBusinessPhoneID)
				}
			}
		}
	}

	var audioCampaignPhoneID string
	if agentCtx != nil && agentCtx.wcCampaign != nil {
		audioCampaignPhoneID = agentCtx.wcCampaign.BusinessPhoneID
	}

	audioClient, err := uc.resolveWhatsAppClient(audioCampaignPhoneID, audioReceivedBusinessPhoneID)
	if err != nil {
		log.Printf("[whatsapp-audio] Cannot resolve WhatsApp client: %v", err)
		return fmt.Errorf("cannot process audio: %w", err)
	}

	audioEntryID, audioEntryType := agentCtx.getEntryInfo()
	if uc.assignmentService != nil && audioEntryID != "" && audioReceivedBusinessPhoneID != "" {
		assignedUID := uc.assignmentService.EnsureAssignment(audioEntryID, string(audioEntryType), audioReceivedBusinessPhoneID)
		if assignedUID != "" {
			log.Printf("[whatsapp-audio] inbox assigned to user %s for entry %s", assignedUID, audioEntryID)
		}
	}

	audioBytes, mimeType, err := audioClient.DownloadMedia(ctx, message.Audio.ID)
	if err != nil {
		log.Printf("[whatsapp-audio] Failed to download audio: %v", err)
		return fmt.Errorf("failed to download audio: %w", err)
	}
	log.Printf("[whatsapp-audio] Audio downloaded: %d bytes, MIME: %s", len(audioBytes), mimeType)

	var conversationID string
	if agentCtx != nil && agentCtx.wcLeadRecord != nil {
		conversationID = agentCtx.wcLeadRecord.ID
	} else {
		conversationID = deriveConversationID(message.From, leadRecord)
	}
	businessNumber := metadataBusinessNumber(metadata)
	audioEntryID, audioEntryType = agentCtx.getEntryInfo()

	var inboundMediaID string
	if stored := uc.storeInboundMedia(audioEntryID, audioEntryType, conversation.MediaTypeAudio, mimeType, audioBytes, "", message.Audio.ID); stored != nil {
		inboundMediaID = stored.ID
	}

	var savedMessageID string
	if uc.messageRepo != nil && audioEntryID != "" {
		now := time.Now().UTC()
		timestamp := parseWhatsAppTimestamp(message.Timestamp)
		if timestamp.IsZero() {
			timestamp = now
		}

		var mediaIDPtr *string
		if inboundMediaID != "" {
			mediaIDPtr = &inboundMediaID
		}
		wamid := strings.TrimSpace(message.ID)

		savedMessageID = uuid.NewString()
		inboundMsg := &conversation.Message{
			ID:                savedMessageID,
			EntryID:           audioEntryID,
			EntryType:         audioEntryType,
			Channel:           conversation.MessageChannelWhatsApp,
			MessageType:       conversation.MessageTypeAudio,
			SentBy:            conversation.SentByContact(message.From),
			From:              strings.TrimSpace(message.From),
			To:                businessNumber,
			Text:              conversation.AudioPlaceholder,
			MediaID:           mediaIDPtr,
			MediaType:         conversation.MediaTypeAudio,
			WhatsAppMessageID: &wamid,
			CreatedAt:         timestamp,
			UpdatedAt:         now,
		}
		if quoted := quotedMessageID(uc.messageRepo, audioEntryType, audioEntryID, quotedProviderMessageID(message)); quoted != "" {
			inboundMsg.ReplyToMessageID = &quoted
		}
		inboundMsg.Normalize()

		if err := uc.messageRepo.Create(inboundMsg); err != nil {
			log.Printf("[whatsapp-audio] Failed to persist inbound audio message: %v", err)
			savedMessageID = ""
		} else {
			log.Printf("[whatsapp-audio] Inbound audio message persisted: %s", savedMessageID)

			if uc.hub != nil {
				uc.hub.BroadcastNewMessage(audioEntryID, string(audioEntryType), inboundMsg)
			}
		}
	}

	var history []*conversation.Message
	if uc.messageRepo != nil && audioEntryID != "" {
		history, _ = uc.messageRepo.ListByEntry(audioEntryID, audioEntryType)
	}

	if uc.whisperPool == nil {
		log.Println("[whatsapp-audio] Whisper (STT) not configured, audio saved without transcription")
		_ = uc.sendWhatsAppFallbackTextIfEligible(
			ctx,
			audioClient,
			agentCtx,
			message.From,
			"Desculpe, não consigo processar mensagens de áudio no momento. Por favor, envie uma mensagem de texto.",
			"stt-unavailable",
		)
		return nil
	}

	sttStart := time.Now()
	transcription, err := uc.whisperPool.Transcribe(ctx, audioBytes, "pt")
	sttLatency := time.Since(sttStart)

	if err != nil {
		log.Printf("[whatsapp-audio] STT failed: %v, audio saved without transcription", err)
		_ = uc.sendWhatsAppFallbackTextIfEligible(
			ctx,
			audioClient,
			agentCtx,
			message.From,
			"Desculpe, não consegui entender seu áudio. Pode repetir ou enviar uma mensagem de texto?",
			"stt-failed",
		)
		return nil
	}

	transcribedText := strings.TrimSpace(transcription.Text)
	if transcribedText == "" {
		log.Printf("[whatsapp-audio] Empty transcription (conf=%.2f, dur=%.2fs), audio saved without transcription", transcription.Confidence, transcription.Duration)
		_ = uc.sendWhatsAppFallbackTextIfEligible(
			ctx,
			audioClient,
			agentCtx,
			message.From,
			"Desculpe, não consegui entender seu áudio. Pode repetir?",
			"stt-empty",
		)
		return nil
	}

	log.Printf("[whatsapp-audio] STT: %q (latency=%dms, conf=%.2f, dur=%.2fs)",
		transcribedText, sttLatency.Milliseconds(), transcription.Confidence, transcription.Duration)
	audioText := conversation.SpokenText(transcribedText)
	uc.storeTranscript(savedMessageID, transcribedText)

	if uc.aiService == nil {
		log.Println("[whatsapp-audio] AI service not configured")
		return errors.New("ai service not configured")
	}

	ctx = withAgentTurn(ctx, agentCtx)

	inboundEvent := uc.messageEvent(agentCtx, audioEntryID, audioEntryType, message.From, message.ID, transcribedText, "audio", "audio", nil)
	route := uc.routeInbound(agentCtx, inboundEvent, false)

	if agentCtx != nil && (agentCtx.skipResponse || !route.Consumer.AgentReplies()) {
		log.Printf("[whatsapp-audio] audio transcribed and recorded, but skipping AI response (agent responses disabled=%v, consumer=%s)", agentCtx.skipResponse, route.Consumer)
		if agentCtx.wcCampaign != nil && (agentCtx.wcCampaign.EnableAnalysis || agentCtx.wcCampaign.EnableAutoStaging) {
			go uc.maybeRunWhatsAppCampaignTools(context.Background(), agentCtx, leadRecord, conversationID, history)
		}
		uc.deliverInbound(agentCtx, route, inboundEvent, history, agentCtx.firesWorkflow())
		return nil
	}

	if IsFloodDetected(ctx) {
		log.Printf("[whatsapp-audio] audio transcribed and recorded, but skipping AI response (flood detected)")
		return nil
	}

	if dec := uc.guardCheckInbound(ctx, agentCtx.getWorkspaceID(), audioEntryID, transcribedText); dec.Block {
		log.Printf("[whatsapp-audio] loop suspected for entry=%s reason=%s count=%d, skipping AI response", audioEntryID, dec.Reason, dec.Count)
		return nil
	}
	release, held := uc.turnLock.HoldForInbound(ctx, audioEntryID, audioEntryType)
	defer release()
	if !held {
		return nil
	}

	turn := whatsAppInboundTurn{agentCtx: agentCtx, leadRecord: leadRecord, message: message, entryID: audioEntryID, entryType: audioEntryType,
		conversationID: conversationID, businessNumber: businessNumber, receivedPhoneID: audioReceivedBusinessPhoneID}
	replyStart := time.Now()
	err = uc.sendAgentReply(ctx, turn.reply(transcribedText, conversation.PromptContent(audioText, "")))
	if _, skipped := replySkipReason(err); skipped {
		return nil
	}
	if err != nil {
		log.Printf("[whatsapp-audio] AI reply failed: %v", err)
		return err
	}
	log.Printf("[whatsapp-audio] Complete audio flow: STT=%dms, reply=%dms",
		sttLatency.Milliseconds(), time.Since(replyStart).Milliseconds())

	go uc.maybeRunWhatsAppCampaignTools(context.Background(), agentCtx, leadRecord, conversationID, history)
	uc.deliverInbound(agentCtx, route, inboundEvent, history, true)

	return nil
}

func ensureWhatsAppTypingIndicatorFresh(
	ctx context.Context,
	client conversation.WhatsAppClient,
	incomingMessageID string,
	logPrefix string,
	lastSentAt time.Time,
) time.Time {
	if !lastSentAt.IsZero() && time.Since(lastSentAt) < whatsAppTypingRefreshAfter {
		return lastSentAt
	}

	return sendWhatsAppTypingIndicator(ctx, client, incomingMessageID, logPrefix)
}

func sendWhatsAppTypingIndicator(
	ctx context.Context,
	client conversation.WhatsAppClient,
	incomingMessageID string,
	logPrefix string,
) time.Time {
	if client == nil {
		return time.Time{}
	}

	incomingMessageID = strings.TrimSpace(incomingMessageID)
	if incomingMessageID == "" {
		return time.Time{}
	}

	if err := client.SendTypingIndicator(ctx, incomingMessageID); err != nil {
		log.Printf("%s typing indicator failed: %v", logPrefix, err)
		return time.Time{}
	}

	sentAt := time.Now().UTC()
	log.Printf("%s typing indicator sent for %s", logPrefix, incomingMessageID)
	return sentAt
}

func waitForWhatsAppTypingVisibility(sentAt time.Time, minVisible time.Duration) {
	if sentAt.IsZero() || minVisible <= 0 {
		return
	}

	elapsed := time.Since(sentAt)
	if elapsed >= minVisible {
		return
	}

	time.Sleep(minVisible - elapsed)
}

func sendWhatsAppTypingIndicatorWithDelay(
	ctx context.Context,
	client conversation.WhatsAppClient,
	incomingMessageID string,
	logPrefix string,
	baseDelay time.Duration,
	jitter time.Duration,
) {
	if client == nil {
		return
	}

	incomingMessageID = strings.TrimSpace(incomingMessageID)
	if incomingMessageID == "" {
		return
	}

	sentAt := sendWhatsAppTypingIndicator(ctx, client, incomingMessageID, logPrefix)
	if sentAt.IsZero() {
		return
	}

	delay := baseDelay
	if jitter > 0 {
		delay += time.Duration(rand.Int63n(int64(jitter)))
	}
	if delay > 0 {
		time.Sleep(delay)
	}
}

type whatsAppTurn struct {
	agentCtx    *agentContext
	whatsappCtx WhatsAppContext

	RecipientPhone  string
	BusinessPhoneID string
	EntryID         string
	EntryType       shared.EntryType

	Vars map[string]string

	Query       string
	Messages    []ai.Message
	TriggerNote string

	Model       string
	Temperature float32
	Segmented   bool
}

func (uc *handleWhatsAppMessageUseCase) assembleWhatsAppTurn(ctx context.Context, t whatsAppTurn) ai.GenerateInput {
	var agentRecord *agent.Agent
	if t.agentCtx != nil {
		agentRecord = t.agentCtx.agent
	}

	seed := map[string]interface{}{
		"__recipient_phone":   t.RecipientPhone,
		"__business_phone_id": t.BusinessPhoneID,
	}
	if t.EntryID != "" {
		seed["__entry_id"] = t.EntryID
		seed["__entry_type"] = string(t.EntryType)
	}
	if agentRecord != nil {
		seed["__workspace_id"] = agentRecord.WorkspaceID
		seed["__agent_id"] = agentRecord.ID
	}
	leadID := t.agentCtx.getLeadID()
	if leadID != "" {
		seed["__lead_id"] = leadID
	}
	campaignID, campaignType := "", ""
	if t.agentCtx != nil {
		if cID, cType := t.agentCtx.getCampaignInfo(); cID != "" {
			campaignID, campaignType = cID, cType
			seed["__campaign_id"] = cID
			seed["__campaign_type"] = cType
		}
	}

	var defs []toolsdomain.Definition
	configs := make(map[string]map[string]interface{})
	if t.agentCtx != nil {
		for _, rt := range t.agentCtx.tools {
			if !rt.IsVisibleIn(agent.ToolVisibilityMessaging) {
				continue
			}
			defs = append(defs, rt.Definition)
			configs[strings.ToLower(rt.Definition.Name)] = rt.Config
		}
	}

	identity := t.whatsappCtx.toConversationContext()
	assembled := uc.assembler().Assemble(ctx, agentturn.Request{
		Agent:              agentRecord,
		Vars:               t.Vars,
		Identity:           &identity,
		CampaignID:         campaignID,
		CampaignType:       campaignType,
		PreResolved:        defs,
		PreResolvedConfigs: configs,
		ToolSeed:           seed,
		RAGQuery:           t.Query,
		LeadID:             leadID,
		Session:            agentturn.ReplySession(t.EntryID),
		History:            t.Messages,
		TriggerNote:        t.TriggerNote,
		Model:              t.Model,
		Temperature:        t.Temperature,
		Segmented:          t.Segmented,
	})

	if t.agentCtx != nil {
		assembled.Input.WorkspaceID = t.agentCtx.getWorkspaceID()
	}
	return assembled.Input
}

func (uc *handleWhatsAppMessageUseCase) assembler() *agentturn.Assembler {
	if uc.turnAssembler != nil {
		return uc.turnAssembler
	}
	return agentturn.New(uc.toolRegistry, uc.ragService, nil)
}

func (uc *handleWhatsAppMessageUseCase) SetTurnAssembler(a *agentturn.Assembler) {
	uc.turnAssembler = a
}

func (uc *handleWhatsAppMessageUseCase) SetWorkflowNames(names ActorNameResolver) {
	uc.workflowNames = names
}

func (uc *handleWhatsAppMessageUseCase) storeInboundMedia(entryID string, entryType shared.EntryType, kind conversation.MediaType, mimeType string, data []byte, filename, whatsappMediaID string) *conversation.ConversationMedia {
	if uc.mediaStore == nil || entryID == "" {
		return nil
	}
	id := uuid.NewString()
	stored, err := uc.mediaStore.Store(conversation.StoreMediaInput{
		ID:               id,
		Key:              fmt.Sprintf("conversations/%s/%s/%s%s", entryType, entryID, id, getExtensionFromMimeType(mimeType)),
		EntryID:          entryID,
		EntryType:        entryType,
		Type:             kind,
		MimeType:         mimeType,
		Data:             data,
		OriginalFilename: filename,
		WhatsAppMediaID:  whatsappMediaID,
	})
	if err != nil {
		log.Printf("[whatsapp-media] %s not stored for %s:%s: %v", kind, entryType, entryID, err)
		return nil
	}
	return stored
}

func (uc *handleWhatsAppMessageUseCase) prepareAutomation(actx *agentContext, responsesEnabled bool) bool {
	campaign := actx.wcCampaign
	agentID, workflowID := campaign.AgentID, campaign.WorkflowID
	automation, err := EffectiveAutomation(context.Background(), uc.delegations, actx.wcEntry.ID, shared.EntryTypeWhatsApp,
		conversation.ChannelAutomation{
			AgentID:              &agentID,
			EnableAgentResponses: responsesEnabled,
			WorkflowID:           &workflowID,
			EnableWorkflow:       campaign.EnableWorkflow,
		})
	if err != nil {
		log.Printf("[whatsapp-usecase] delegation of entry %s unreadable, no automation runs: %v", actx.wcEntry.ID, err)
		actx.skipResponse = true
		return false
	}
	actx.automation = automation
	actx.automationResolved = true

	switch {
	case actx.firesWorkflow():
		actx.skipResponse = true
		log.Printf("[whatsapp-usecase] campaign %s runs workflow %s, agent responses will be skipped", campaign.ID, automation.WorkflowRef())
		return false
	case automation.EnableAgentResponses && automation.HasAgent():
		agentRecord, prompt, tools := uc.resolveAgentPromptAndTools(*automation.AgentID, campaign.ID, "whatsapp")
		if agentRecord == nil {
			actx.skipResponse = true
			log.Printf("[whatsapp-usecase] campaign %s could not resolve agent %s, skipping responses", campaign.ID, *automation.AgentID)
			return false
		}
		actx.agent, actx.messagingPrompt, actx.tools = agentRecord, prompt, tools
		return true
	default:
		actx.skipResponse = true
		log.Printf("[whatsapp-usecase] campaign %s has no workflow and no agent configured, skipping responses", campaign.ID)
		return false
	}
}
