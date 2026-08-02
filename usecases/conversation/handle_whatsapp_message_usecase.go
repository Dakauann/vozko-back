package conversation_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	agent "vozko/domain/agent"
	"vozko/domain/ai"
	analysisdomain "vozko/domain/analysis"
	"vozko/domain/balance"
	"vozko/domain/business_metrics"
	"vozko/domain/cache"
	"vozko/domain/config"
	"vozko/domain/conversation"
	"vozko/domain/lead"
	lmw "vozko/domain/lead_message_window"
	"vozko/domain/media"
	"vozko/domain/messaging"
	"vozko/domain/rag"
	"vozko/domain/shared"
	"vozko/domain/stage"
	toolsdomain "vozko/domain/tools"
	businessphone "vozko/domain/whatsapp/business_phone"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
	workflow_domain "vozko/domain/workflow"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
	"vozko/infra/whisper"
	"vozko/usecases/agentctx"
	"vozko/usecases/agentturn"
	ia_usecase "vozko/usecases/inbox_assignment"
	shared_usecase "vozko/usecases/shared"
	tools_usecase "vozko/usecases/tools"

	"vozko/usecases/conversation/loopguard"

	aa "vozko/domain/ai_attendance"
)

const (
	whatsAppTypingRefreshAfter   = 20 * time.Second
	whatsAppTypingMinVisibleTime = 1200 * time.Millisecond
)

type handleWhatsAppMessageUseCase struct {
	aiService             ai.Service
	leadRepo              lead.Repository
	agentRepo             agent.Repository
	toolRegistry          toolsdomain.Service
	whatsappClientFactory conversation.WhatsAppClientFactory
	historyManager        conversation.MessageHistoryManager
	messageRepo           conversation.MessageRepository
	configRepo            config.SystemConfigRepository
	recordMetric          business_metrics.RecordMetricUseCase
	whisperPool           *whisper.Pool
	analysisRepo          analysisdomain.Repository
	wcCampaignRepo        wc.Repository
	wcEntryRepo           wce.Repository
	businessPhoneRepo     businessphone.Repository
	messageWindowRepo     lmw.Repository
	fileStorage           media.FileStorage
	conversationMediaRepo conversation.ConversationMediaRepository
	hub                   conversation.EventBroadcaster
	assignmentService     *ia_usecase.AssignmentService
	aiAttendance          AIAttendanceRecorder
	triggerEvaluator      workflow_domain.TriggerEvaluator
	// turnAssembler is the shared agent-turn recipe. Optional: unset falls back
	// to a locally constructed one so a partially-wired container still works.
	turnAssembler           *agentturn.Assembler
	stageRepo               stage.Repository
	textExtractor           media.TextExtractor
	sharedState             cache.SharedState
	ragService              rag.RAGService
	cachedBalanceChecker    balance.CachedBalanceChecker
	llmPriceFetcher         workspace_pricing.LLMPriceFetcher
	consumeWhatsappTemplate balance.ConsumeWhatsappTemplateUseCase

	billingPub messaging.MessageQueuePub

	loopGuard loopguard.Guard
}

const failedStatusRefundTTL = 30 * 24 * time.Hour
const failedStatusRefundLockTTL = 1 * time.Minute
const failedStatusRefundAttempts = 3

func (uc *handleWhatsAppMessageUseCase) SetAssignmentService(svc *ia_usecase.AssignmentService) {
	uc.assignmentService = svc
}

// AIAttendanceRecorder is the hot-path AI session surface (queue publish only, no DB).
type AIAttendanceRecorder interface {
	RecordAIReply(in aa.StartInput, messageID string)
}

func (uc *handleWhatsAppMessageUseCase) SetAIAttendance(svc AIAttendanceRecorder) {
	uc.aiAttendance = svc
}

func (uc *handleWhatsAppMessageUseCase) SetTriggerEvaluator(eval workflow_domain.TriggerEvaluator) {
	uc.triggerEvaluator = eval
}

func (uc *handleWhatsAppMessageUseCase) SetBillingPub(pub messaging.MessageQueuePub) {
	uc.billingPub = pub
}

func (uc *handleWhatsAppMessageUseCase) SetLoopGuard(g loopguard.Guard) {
	uc.loopGuard = g
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

// selectedInteractiveOption carries the STABLE identity of a tapped WhatsApp
// interactive reply (button or list row) into the workflow engine. The webhook
// flattening (extractInboundMessage) collapses the reply to its display text for
// back-compat; this preserves the id so the interactive prompt node can branch
// on the option chosen rather than on the localizable, length-capped title.
type selectedInteractiveOption struct {
	ID    string
	Title string
	Kind  string // "button" | "list"
}

func selectedOptionFromMessage(msg *conversation.WhatsAppMessage) *selectedInteractiveOption {
	if msg == nil || msg.Interactive == nil {
		return nil
	}
	if br := msg.Interactive.ButtonReply; br != nil && strings.TrimSpace(br.ID) != "" {
		return &selectedInteractiveOption{ID: br.ID, Title: br.Title, Kind: "button"}
	}
	if lr := msg.Interactive.ListReply; lr != nil && strings.TrimSpace(lr.ID) != "" {
		return &selectedInteractiveOption{ID: lr.ID, Title: lr.Title, Kind: "list"}
	}
	return nil
}

func (uc *handleWhatsAppMessageUseCase) fireWorkflowTriggers(agentCtx *agentContext, entryID string, entryType shared.EntryType, messageText string, msgType string, mediaType string, history []*conversation.Message, selected *selectedInteractiveOption) {
	if uc.triggerEvaluator == nil || entryID == "" {
		return
	}

	// A human explicitly turned automation off for this lead (per-entry
	// AutomationEnabled override == false). That must stop the campaign WORKFLOW
	// too, not only the direct AI agent — otherwise the workflow keeps replying and
	// the operator sees the "deactivated automation is still responding" bug. A nil
	// override (never toggled) is left untouched, so normal workflow campaigns still
	// run by default.
	if agentCtx != nil && agentCtx.wcEntry != nil &&
		agentCtx.wcEntry.AutomationEnabled != nil && !*agentCtx.wcEntry.AutomationEnabled {
		log.Printf("[whatsapp-workflow] automation disabled for entry=%s — skipping workflow triggers", entryID)
		return
	}

	if agentCtx != nil && agentCtx.wcCampaign != nil && !agentCtx.wcCampaign.EnableWorkflow {
		return
	}

	workspaceID := ""
	if agentCtx != nil {
		workspaceID = agentCtx.getWorkspaceID()
	}
	if workspaceID == "" {
		return
	}

	if dec := uc.guardCheckInbound(context.Background(), workspaceID, entryID, messageText); dec.Block {
		log.Printf("[whatsapp-workflow] loop suspected for entry=%s reason=%s count=%d — skipping workflow triggers", entryID, dec.Reason, dec.Count)
		return
	}

	data := map[string]interface{}{
		"message": messageText,
	}
	if msgType != "" {
		data["message_type"] = msgType
	}
	if mediaType != "" {
		data["media_type"] = mediaType
	}
	if selected != nil {
		// Stable identity of the tapped button / list row, keyed on by the
		// interactive prompt node to branch by option id. Written through the
		// shared helper so WhatsApp, Instagram and Telegram cannot drift on the
		// key names AdvanceOnReply reads.
		workflow_domain.ApplySelection(data, &workflow_domain.OptionSelection{
			ID:    selected.ID,
			Title: selected.Title,
			Kind:  selected.Kind,
		})
	}
	if agentCtx != nil {
		if cID, _ := agentCtx.getCampaignInfo(); cID != "" {
			data["campaign_id"] = cID
		}

		if agentCtx.wcCampaign != nil && agentCtx.wcCampaign.WorkflowID != "" {
			data["campaign_workflow_id"] = agentCtx.wcCampaign.WorkflowID
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
				campvars["lead_name"] = agentCtx.wcLeadRecord.Name
				campvars["lead_number"] = agentCtx.wcLeadRecord.Number
			}
			if agentCtx.wcCampaign != nil {
				campvars["campaign_name"] = agentCtx.wcCampaign.Name
			}
			data["campvars"] = campvars
		}
	}

	uc.triggerEvaluator.Evaluate(workflow_domain.TriggerEvent{
		WorkspaceID: workspaceID,
		EntryID:     entryID,
		EntryType:   string(entryType),
		TriggerType: workflow_domain.TriggerMessageReceived,
		Data:        data,
	})

	// trigger_first_message must fire on the customer's FIRST inbound message —
	// gate on the absence of a PRIOR inbound message, not on empty history. A
	// campaign that sends an outbound template records it in history before the
	// lead replies, so `len(history) == 0` was never true for template-first
	// campaigns and this trigger silently never fired (the reply started no
	// workflow). `history` excludes the message being processed, so "no prior
	// inbound" == "this is the first customer message".
	if isFirstInboundMessage(history) {
		uc.triggerEvaluator.Evaluate(workflow_domain.TriggerEvent{
			WorkspaceID: workspaceID,
			EntryID:     entryID,
			EntryType:   string(entryType),
			TriggerType: workflow_domain.TriggerFirstMessage,
			Data:        data,
		})
	}
}

// isFirstInboundMessage reports whether the message currently being handled is the
// first inbound (customer) message on the entry, i.e. prior history holds no
// inbound message. Outbound messages (campaign templates, agent/operator replies)
// do not count — they must not suppress the first-message trigger.
func isFirstInboundMessage(history []*conversation.Message) bool {
	for _, m := range history {
		if m != nil && m.MessageType.IsInbound() {
			return false
		}
	}
	return true
}

func (uc *handleWhatsAppMessageUseCase) canAffordAI(workspaceID, model string) bool {
	const (
		estimatedInputTokens  int64 = 4000
		estimatedOutputTokens int64 = 1000

		safetyMultiplier int64 = 2

		minBalanceFloor int64 = 10_000
	)

	if uc.cachedBalanceChecker == nil {
		log.Printf("CRITICAL: cachedBalanceChecker is nil — blocking AI response for workspace %s (fail-closed)", workspaceID)
		return false
	}
	bal, err := uc.cachedBalanceChecker.GetBalance(workspaceID)
	if err != nil {
		log.Printf("[whatsapp-usecase] balance check error for workspace %s: %v — blocking AI response (fail-closed)", workspaceID, err)
		return false
	}
	if bal <= 0 {
		log.Printf("[whatsapp-usecase] workspace %s has no balance (%d micros), blocking AI response", workspaceID, bal)
		return false
	}

	if bal < minBalanceFloor {
		log.Printf("[whatsapp-usecase] workspace %s balance (%d micros) below minimum floor (%d micros), blocking AI response", workspaceID, bal, minBalanceFloor)
		return false
	}

	if uc.llmPriceFetcher != nil && model != "" {
		inputMicros, outputMicros, fetchErr := uc.llmPriceFetcher.FetchLLMPriceMicros(model)
		if fetchErr != nil {
			log.Printf("[whatsapp-usecase] price fetch failed for model %s: %v — relying on balance floor only", model, fetchErr)
		} else if inputMicros > 0 || outputMicros > 0 {

			estimatedCost := (inputMicros*estimatedInputTokens + outputMicros*estimatedOutputTokens) * safetyMultiplier / 1_000_000
			if estimatedCost < 1 {
				estimatedCost = 1
			}
			if bal < estimatedCost {
				log.Printf("[whatsapp-usecase] workspace %s balance (%d micros) below estimated AI cost (%d micros, model=%s), blocking", workspaceID, bal, estimatedCost, model)
				return false
			}
		}
	}

	return true
}

type ResolvedTool struct {
	Definition toolsdomain.Definition
	Visibility []agent.ToolVisibility
	Config     map[string]interface{}
}

// TODO: refactor, this shouldnt be in the logic of
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

// toConversationContext maps the WhatsApp context onto the shared identity the
// assembler builds its preamble from. AvailableTools is deliberately not copied:
// the assembler fills it from the tools actually attached to the turn, so the
// preamble can never advertise a different set than the model receives.
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
		log.Printf("[whatsapp-usecase] WARNING: cannot resolve workspace_id — nil agent context")
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

func NewHandleWhatsAppMessageUseCase(aiService ai.Service, whatsappClientFactory conversation.WhatsAppClientFactory, leadRepo lead.Repository, agentRepo agent.Repository, toolRegistry toolsdomain.Service, historyManager conversation.MessageHistoryManager, messageRepo conversation.MessageRepository, configRepo config.SystemConfigRepository, recordMetric business_metrics.RecordMetricUseCase, whisperPool *whisper.Pool, analysisRepo analysisdomain.Repository, wcCampaignRepo wc.Repository, wcEntryRepo wce.Repository, businessPhoneRepo businessphone.Repository, messageWindowRepo lmw.Repository, fileStorage media.FileStorage, conversationMediaRepo conversation.ConversationMediaRepository, hub conversation.EventBroadcaster, stageRepo stage.Repository, textExtractor media.TextExtractor, sharedState cache.SharedState, ragService rag.RAGService, cachedBalanceChecker balance.CachedBalanceChecker, llmPriceFetcher workspace_pricing.LLMPriceFetcher, consumeWhatsappTemplate balance.ConsumeWhatsappTemplateUseCase) conversation.HandleWhatsAppMessageUseCase {
	return &handleWhatsAppMessageUseCase{
		aiService:               aiService,
		leadRepo:                leadRepo,
		agentRepo:               agentRepo,
		toolRegistry:            toolRegistry,
		whatsappClientFactory:   whatsappClientFactory,
		historyManager:          historyManager,
		messageRepo:             messageRepo,
		configRepo:              configRepo,
		recordMetric:            recordMetric,
		whisperPool:             whisperPool,
		analysisRepo:            analysisRepo,
		wcCampaignRepo:          wcCampaignRepo,
		wcEntryRepo:             wcEntryRepo,
		businessPhoneRepo:       businessPhoneRepo,
		messageWindowRepo:       messageWindowRepo,
		fileStorage:             fileStorage,
		conversationMediaRepo:   conversationMediaRepo,
		hub:                     hub,
		stageRepo:               stageRepo,
		textExtractor:           textExtractor,
		sharedState:             sharedState,
		ragService:              ragService,
		cachedBalanceChecker:    cachedBalanceChecker,
		llmPriceFetcher:         llmPriceFetcher,
		consumeWhatsappTemplate: consumeWhatsappTemplate,
	}
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

	// Drop messages from blocked contacts before any processing. Meta-side
	// blocking should already stop most of these, but this guard covers Meta lag,
	// the 24h block window, and contacts that reach a second business phone.
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
	log.Printf("[whatsapp-usecase] message received at %s\n%s", time.Now().UTC().Format(time.RFC3339), message.Text.Body)

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

	if agentCtx != nil && agentCtx.agent != nil {
		ctx = agentctx.WithAgent(ctx, agentCtx.agent)
	}

	whatsappCtx := WhatsAppContext{
		UserPhoneNumber:     message.From,
		ConversationID:      conversationID,
		MessageReceivedTime: parseWhatsAppTimestamp(message.Timestamp),
	}
	if agentCtx != nil {
		if agentCtx.wcCampaign != nil {
			whatsappCtx.CampaignName = agentCtx.wcCampaign.Name
		}
		if agentCtx.agent != nil {
			whatsappCtx.AgentName = agentCtx.agent.Name
		}
	}
	if leadRecord != nil {
		whatsappCtx.UserName = leadRecord.Name
	}
	if agentCtx != nil {
		if agentCtx.wcEntry != nil {
			whatsappCtx.Metadata = agentCtx.wcEntry.Metadata
		}
	}

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

	var outboundBusinessPhoneID string
	if agentCtx != nil && agentCtx.wcCampaign != nil && agentCtx.wcCampaign.BusinessPhoneID != "" {
		outboundBusinessPhoneID = agentCtx.wcCampaign.BusinessPhoneID
		log.Printf("[whatsapp-usecase] using campaign business phone for reply: db_id=%s", outboundBusinessPhoneID)
	}
	if outboundBusinessPhoneID == "" && receivedBusinessPhoneID != "" {
		outboundBusinessPhoneID = receivedBusinessPhoneID
		log.Printf("[whatsapp-usecase] using received phone for reply: db_id=%s", outboundBusinessPhoneID)
	}

	if uc.assignmentService != nil && entryID != "" {
		// TODO: makes no sense??????
		phoneForAssignment := receivedBusinessPhoneID
		if phoneForAssignment == "" {
			phoneForAssignment = outboundBusinessPhoneID
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

	if uc.historyManager != nil && entryID != "" {
		record := conversation.MessageHistoryRecord{
			EntryID:        entryID,
			EntryType:      entryType,
			Channel:        conversation.MessageChannelWhatsApp,
			MessageType:    conversation.MessageTypeUserMessage,
			ConversationID: conversationID,
			MessageID:      strings.TrimSpace(message.ID),
			From:           strings.TrimSpace(message.From),
			To:             businessNumber,
			Text:           strings.TrimSpace(message.Text.Body),
			Timestamp:      parseWhatsAppTimestamp(message.Timestamp),
		}
		// The lead is already loaded, so the live broadcast is labelled without
		// the hub re-resolving it. WhatsApp is the highest-volume channel here;
		// making it pay for a lookup per inbound message would be a real cost.
		if leadRecord != nil {
			record.SenderName = leadRecord.Name
			record.SenderAvatar = leadRecord.ProfilePictureURL
		}
		if err := uc.historyManager.Record(ctx, conversation.MessageDirectionInbound, record); err != nil {
			return err
		}
	}
	recipientPhone := strings.TrimSpace(message.From)

	conversationMessages := uc.composeConversationHistory(history, businessNumber, message.From)
	conversationMessages = append(conversationMessages, ai.Message{Role: ai.RoleUser, Content: message.Text.Body})

	if agentCtx != nil && agentCtx.skipResponse {
		log.Printf("[whatsapp-usecase] message recorded, but skipping AI response (agent responses disabled)")

		if agentCtx.wcCampaign != nil && (agentCtx.wcCampaign.EnableAnalysis || agentCtx.wcCampaign.EnableAutoStaging) {
			go uc.maybeRunWhatsAppCampaignTools(context.Background(), agentCtx, leadRecord, conversationID, history)
		}

		if agentCtx.wcCampaign != nil && agentCtx.wcCampaign.EnableWorkflow && strings.TrimSpace(agentCtx.wcCampaign.WorkflowID) != "" {
			log.Printf("[whatsapp-usecase] firing workflow triggers for campaign %s (workflow %s)", agentCtx.wcCampaign.ID, agentCtx.wcCampaign.WorkflowID)
			go uc.fireWorkflowTriggers(agentCtx, entryID, entryType, message.Text.Body, "text", "", history, selectedOptionFromMessage(message))
		}
		return nil
	}

	if IsFloodDetected(ctx) {
		log.Printf("[whatsapp-usecase] message recorded, but skipping AI response (flood detected for %s)", message.From)
		return nil
	}

	if dec := uc.guardCheckInbound(ctx, agentCtx.getWorkspaceID(), entryID, message.Text.Body); dec.Block {
		log.Printf("[whatsapp-usecase] loop suspected for entry=%s reason=%s count=%d — skipping AI response", entryID, dec.Reason, dec.Count)
		return nil
	}

	if dec := uc.guardRecordAIResponse(ctx, agentCtx.getWorkspaceID(), entryID); dec.Block {
		log.Printf("[whatsapp-usecase] AI reply rate limit hit for entry=%s count=%d — skipping AI response", entryID, dec.Count)
		return nil
	}

	var messagingModel string
	if agentCtx != nil && agentCtx.agent != nil && agentCtx.agent.MessagingModel != "" {
		messagingModel = agentCtx.agent.MessagingModel
	}

	if !uc.canAffordAI(agentCtx.getWorkspaceID(), messagingModel) {
		log.Printf("[whatsapp-usecase] message recorded, but skipping AI response (insufficient balance for workspace %s)", agentCtx.getWorkspaceID())
		return nil
	}

	var campaignPhoneID string
	if agentCtx != nil && agentCtx.wcCampaign != nil {
		campaignPhoneID = agentCtx.wcCampaign.BusinessPhoneID
	}
	outboundClient, err := uc.resolveWhatsAppClient(campaignPhoneID, receivedBusinessPhoneID)
	if err != nil {
		log.Printf("[whatsapp-usecase] cannot send reply: %v", err)
		return fmt.Errorf("cannot send WhatsApp reply: %w", err)
	}

	recipientNumber := strings.TrimSpace(message.From)
	incomingMessageID := strings.TrimSpace(message.ID)
	initialTypingSentAt := ensureWhatsAppTypingIndicatorFresh(ctx, outboundClient, incomingMessageID, "[whatsapp-usecase]", time.Time{})

	// Assembled here, AFTER the skip/flood/guard/balance gates: it runs a vector
	// search, and the previous inline version paid for one on every message the
	// system then declined to answer.
	turnEntryID, turnEntryType := "", shared.EntryType("")
	if agentCtx != nil {
		turnEntryID, turnEntryType = agentCtx.getEntryInfo()
	}
	generateInput := uc.assembleWhatsAppTurn(ctx, whatsAppTurn{
		agentCtx:        agentCtx,
		whatsappCtx:     whatsappCtx,
		RecipientPhone:  recipientPhone,
		BusinessPhoneID: outboundBusinessPhoneID,
		EntryID:         turnEntryID,
		EntryType:       turnEntryType,
		Vars:            agent.MetadataToVars(whatsappCtx.Metadata),
		Query:           message.Text.Body,
		Messages:        conversationMessages,
		Model:           messagingModel,
		Temperature:     0.2,
		Segmented:       true,
	})
	toolsForAgent := generateInput.Tools

	log.Printf("[whatsapp-usecase] Starting AI response generation (model: %s, tools: %d)", messagingModel, len(toolsForAgent))

	output, err := uc.aiService.Generate(ctx, generateInput)
	if err != nil {
		fmt.Println(err)
		return err
	}

	if uc.historyManager != nil && (conversationID != "" || entryID != "") && len(output.ToolCalls) > 0 {
		for _, tc := range output.ToolCalls {
			toolCallText := fmt.Sprintf("[Tool Call] %s: %s", tc.Name, tc.Arguments)
			toolCallRecord := conversation.MessageHistoryRecord{
				EntryID:        entryID,
				EntryType:      entryType,
				Channel:        conversation.MessageChannelWhatsApp,
				MessageType:    conversation.MessageTypeToolCall,
				ConversationID: conversationID,
				MessageID:      uuid.NewString(),
				From:           "system",
				To:             "system",
				Text:           toolCallText,
				Timestamp:      time.Now().UTC(),
			}
			if err := uc.historyManager.Record(ctx, conversation.MessageDirectionOutbound, toolCallRecord); err != nil {
				log.Printf("[whatsapp-usecase] failed to record tool call: %v", err)
			}

			if tc.Result != nil {
				var toolResultText string
				if tc.Result.IsError {
					if txt := strings.TrimSpace(tc.Result.ContextUpdateText); txt != "" {
						toolResultText = fmt.Sprintf("[Tool Error] %s", txt)
					} else if tc.Result.Result != nil {
						resultJSON, err := json.Marshal(tc.Result.Result)
						if err == nil {
							toolResultText = fmt.Sprintf("[Tool Error] %s", string(resultJSON))
						} else {
							toolResultText = fmt.Sprintf("[Tool Error] %v", tc.Result.Result)
						}
					} else {
						toolResultText = "[Tool Error] execution failed"
					}
				} else if tc.Result.ContextUpdateText != "" {
					toolResultText = fmt.Sprintf("[Tool Result] %s", tc.Result.ContextUpdateText)
				} else if tc.Result.Result != nil {
					resultJSON, err := json.Marshal(tc.Result.Result)
					if err == nil {
						toolResultText = fmt.Sprintf("[Tool Result] %s", string(resultJSON))
					} else {
						toolResultText = fmt.Sprintf("[Tool Result] %v", tc.Result.Result)
					}
				}

				if toolResultText != "" {
					toolResultRecord := conversation.MessageHistoryRecord{
						EntryID:        entryID,
						EntryType:      entryType,
						Channel:        conversation.MessageChannelWhatsApp,
						MessageType:    conversation.MessageTypeToolResult,
						ConversationID: conversationID,
						MessageID:      uuid.NewString(),
						From:           "system",
						To:             "system",
						Text:           toolResultText,
						Timestamp:      time.Now().UTC(),
					}
					if err := uc.historyManager.Record(ctx, conversation.MessageDirectionOutbound, toolResultRecord); err != nil {
						log.Printf("[whatsapp-usecase] failed to record tool result: %v", err)
					}
				}
			}
		}
	}

	messages := output.Messages
	if len(messages) == 0 {

		if content := strings.TrimSpace(output.Message.Content); content != "" {
			messages = []string{content}
		}
	}

	for i, m := range messages {
		messages[i] = sanitizeAIOutput(m)
	}
	messages = filterEmptyStrings(messages)

	{
		for i, msgText := range messages {
			if i == 0 {
				initialTypingSentAt = ensureWhatsAppTypingIndicatorFresh(ctx, outboundClient, incomingMessageID, "[whatsapp-usecase]", initialTypingSentAt)
				waitForWhatsAppTypingVisibility(initialTypingSentAt, whatsAppTypingMinVisibleTime)
			} else {
				sendWhatsAppTypingIndicatorWithDelay(ctx, outboundClient, incomingMessageID, "[whatsapp-usecase]", 1500*time.Millisecond, time.Second)
			}

			sendInput := conversation.SendTextMessageInput{
				To:   recipientNumber,
				Body: msgText,
			}
			sendOutput, err := outboundClient.SendTextMessage(ctx, sendInput)
			if err != nil {
				return err
			}

			if uc.historyManager != nil && (conversationID != "" || entryID != "") {
				record := conversation.MessageHistoryRecord{
					EntryID:        entryID,
					EntryType:      entryType,
					Channel:        conversation.MessageChannelWhatsApp,
					MessageType:    conversation.MessageTypeAIResponse,
					ConversationID: conversationID,
					MessageID:      "",
					From:           businessNumber,
					To:             sendInput.To,
					Text:           sendInput.Body,
					Timestamp:      time.Now().UTC(),
				}
				if sendOutput != nil {
					record.MessageID = strings.TrimSpace(sendOutput.MessageID)
					if strings.TrimSpace(record.To) == "" {
						record.To = strings.TrimSpace(sendOutput.ContactWaID)
					}
				}
				if err := uc.historyManager.Record(ctx, conversation.MessageDirectionOutbound, record); err != nil {
					return err
				}
				uc.recordAIAttendance(agentCtx, entryID, entryType, record.MessageID)
			}

			if uc.recordMetric != nil {
				entityID := ""
				if sendOutput != nil {
					entityID = strings.TrimSpace(sendOutput.MessageID)
				}
				if entityID == "" {
					entityID = uuid.NewString()
				}

				metadata := map[string]string{
					"to":                  strings.TrimSpace(sendInput.To),
					"conversation_id":     conversationID,
					"incoming_message_id": incomingMessageID,
				}

				if err := uc.recordMetric.Execute(business_metrics.RecordMetricInput{
					EventType:  business_metrics.EventWhatsAppMessageSent,
					EntityID:   entityID,
					EntityType: business_metrics.EntityTypeMessage,
					Metadata:   metadata,
				}); err != nil {
					log.Printf("[whatsapp-usecase] failed to record whatsapp_message_sent metric: %v", err)
				}
			}

			log.Printf("[whatsapp-usecase] ai response:\n%s", msgText)
		}
	}

	go uc.maybeRunWhatsAppCampaignTools(context.Background(), agentCtx, leadRecord, conversationID, history)

	return nil
}

func (uc *handleWhatsAppMessageUseCase) composeConversationHistory(history []*conversation.Message, businessNumber, userNumber string) []ai.Message {
	if len(history) == 0 {
		return nil
	}

	start := 0
	if len(history) > conversationHistoryLimit {
		start = len(history) - conversationHistoryLimit
	}

	businessNorm := lead.NormalizeNumber(businessNumber)
	userNorm := lead.NormalizeNumber(userNumber)
	messages := make([]ai.Message, 0, len(history[start:]))

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
		content := strings.TrimSpace(msg.Text)
		if content == "" {
			continue
		}

		if len(msg.Metadata) > 0 {
			var meta map[string]string
			if err := json.Unmarshal(msg.Metadata, &meta); err == nil {
				if et := strings.TrimSpace(meta["extracted_text"]); et != "" {
					content = content + "\n\n" + et
				}
			}
		}

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

		if uc.sharedState != nil {
			// The entry type travels with the timestamp: the hash is keyed by
			// entry id alone, and an entry id does not say which channel it is on.
			value := encodeAnalysisDebounceValue(shared.EntryTypeWhatsApp, time.Now().UTC())
			if err := uc.sharedState.HSet(AnalysisDebounceRedisKey, entryID, value); err != nil {
				log.Printf("[whatsapp-usecase] failed to stamp analysis debounce for entry %s: %v", entryID, err)
			}
		}
		return
	}

	isWhatsAppCampaign := agentCtx.wcCampaign != nil && agentCtx.wcEntry != nil

	if !isWhatsAppCampaign {
		log.Printf("[whatsapp-usecase] no campaign context available for campaign tools (missing campaign or entry)")
		return
	}

	campaignAnalysisEnabled := isWhatsAppCampaign && agentCtx.wcCampaign.EnableAnalysis
	autoTagEnabled := isWhatsAppCampaign && agentCtx.wcCampaign.EnableAutoStaging

	if !campaignAnalysisEnabled && !autoTagEnabled {
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
	var agentInstructions string
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

	if len(history) > 100 {
		history = history[len(history)-100:]
	}

	if agentCtx.messagingPrompt != "" {
		agentInstructions = agentCtx.messagingPrompt
	}
	if agentCtx.agent != nil {
		entryMetadata := func() map[string]interface{} {
			if agentCtx.wcEntry != nil {
				return agentCtx.wcEntry.Metadata
			}
			return nil
		}()
		interpolated := agent.InterpolateAgent(agentCtx.agent, agent.MetadataToVars(entryMetadata))
		if interpolated.MessagingPrompt != "" {
			agentInstructions = interpolated.MessagingPrompt
		}
	}

	var aiTools []toolsdomain.Definition
	toolConfigs := map[string]map[string]interface{}{}

	wantAnalysis := campaignAnalysisEnabled
	if wantAnalysis {
		allDefs := uc.toolRegistry.Definitions()
		for _, def := range allDefs {
			if strings.EqualFold(def.Name, tools_usecase.ConversationAnalysisToolName) {
				aiTools = append(aiTools, def)
				toolConfigs[tools_usecase.ConversationAnalysisToolName] = map[string]interface{}{
					"entry_id":      entryID,
					"entry_type":    entryType,
					"message_count": int(totalCount),
				}
				break
			}
		}
		if len(aiTools) == 0 {
			wantAnalysis = false
		}
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

	var systemPrompt string
	if wantAnalysis {
		systemPrompt = BuildAnalysisPrompt(AnalysisPromptInput{
			AnalysisType:      AnalysisTypeOngoing,
			CampaignName:      campaignName,
			UserPhoneNumber:   userPhoneNumber,
			MessageCount:      int(totalCount),
			AgentInstructions: agentInstructions,
			History:           history,
		})
	} else {

		transcript := BuildTranscript(history, userPhoneNumber)
		var currentTagName string
		var allTags []*stage.Stage
		if uc.stageRepo != nil {
			if et, err := uc.stageRepo.GetEntryStage(entryID, entryType, workspaceID); err == nil && et != nil {
				currentTagName = et.StageName
			}
			if tags, err := uc.stageRepo.ListByCampaign(workspaceID, campaignID, entryType); err == nil {
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

	response, err := uc.aiService.Generate(ctx, ai.GenerateInput{
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
		log.Printf("[whatsapp-usecase] campaign tools AI call failed: %v", err)
		return
	}

	for _, toolCall := range response.ToolCalls {
		if toolCall.Name == tools_usecase.ConversationAnalysisToolName && toolCall.Result != nil {
			log.Printf("[whatsapp-usecase] conversation_analysis result: %v", toolCall.Result.Result)
			if uc.hub != nil && uc.analysisRepo != nil {
				if latest, err := uc.analysisRepo.FindLatestByEntry(entryID, shared.EntryType(entryType)); err == nil && latest != nil {
					uc.hub.BroadcastAnalysisUpdate(entryID, entryType, latest)
				}
			}
		}
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

	for _, entry := range payload.Entry {
		for _, change := range entry.Changes {
			for _, status := range change.Value.Statuses {
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
					if err := uc.messageRepo.UpdateDeliveryStatus(status.ID, deliveryStatus); err != nil {
						log.Printf("[whatsapp-status] Failed to update delivery status for wamid %s: %v", status.ID, err)
						statusErrors = append(statusErrors, fmt.Errorf("update delivery status for wamid %s: %w", status.ID, err))
					}

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

	if len(statusErrors) > 0 {
		return fmt.Errorf("%w: %v", conversation.ErrWhatsAppWebhookRetryable, errors.Join(statusErrors...))
	}
	return nil
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

	campaign, err := uc.wcCampaignRepo.FindByID(entry.CampaignID)
	if err != nil || campaign == nil {
		return fmt.Errorf("resolve campaign %s for refund on message %s: %w", entry.CampaignID, messageID, err)
	}

	templateCategory, err := resolveWhatsAppRefundCategory(status, entry)
	if err != nil {
		return fmt.Errorf("determine refund category for message %s: %w", messageID, err)
	}

	if err := uc.retryFailedWhatsAppCampaignRefund(campaign.WorkspaceID, campaign.ID, templateCategory); err != nil {
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

func (uc *handleWhatsAppMessageUseCase) retryFailedWhatsAppCampaignRefund(workspaceID, campaignID, templateCategory string) error {
	var lastErr error
	for attempt := 1; attempt <= failedStatusRefundAttempts; attempt++ {
		lastErr = uc.consumeWhatsappTemplate.Refund(workspaceID, campaignID, templateCategory)
		if lastErr == nil {
			return nil
		}
	}
	return lastErr
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

// isLeadBlocked reports whether the contact behind an inbound message has been
// blocked in the receiving workspace. It is a cheap pre-check run on every
// inbound message, so it fails open (returns false) on any resolution error.
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

	var receivingBusinessPhoneID string
	var receivingWorkspaceID string
	if metadata != nil && metadata.PhoneNumberID != "" && uc.businessPhoneRepo != nil {
		if bp, err := uc.businessPhoneRepo.FindByMetaPhoneNumberID(metadata.PhoneNumberID); err == nil && bp != nil {
			receivingBusinessPhoneID = bp.ID
			receivingWorkspaceID = strings.TrimSpace(bp.OwnerWorkspaceID)
		}
	}

	type candidate struct {
		ctx              *agentContext
		lead             *lead.Lead
		createdAt        time.Time
		responsesEnabled bool
		source           string
	}

	var candidates []candidate

	if uc.wcEntryRepo != nil && uc.wcCampaignRepo != nil && uc.agentRepo != nil {
		if wcEntry, err := uc.wcEntryRepo.FindByNumberAndBusinessPhone(normalized, receivingBusinessPhoneID); err == nil && wcEntry != nil {
			if wcCampaign, err := uc.wcCampaignRepo.FindByID(wcEntry.CampaignID); err == nil && wcCampaign != nil {

				if receivingWorkspaceID == "" && wcCampaign.WorkspaceID != "" {
					receivingWorkspaceID = wcCampaign.WorkspaceID
					log.Printf("[whatsapp-usecase] derived workspace %s from whatsapp campaign %s for business phone %s (phone has no owner_workspace_id)",
						receivingWorkspaceID, wcCampaign.ID, receivingBusinessPhoneID)
				}

				responsesEnabled := wcCampaign.EnableAgentResponses
				if wcEntry.AutomationEnabled != nil {
					responsesEnabled = *wcEntry.AutomationEnabled
				}

				var wcLead *lead.Lead
				if receivingWorkspaceID != "" {
					wcLead, _ = uc.leadRepo.FindByNumber(receivingWorkspaceID, normalized)
				}

				wcCtx := &agentContext{
					wcCampaign:   wcCampaign,
					wcEntry:      wcEntry,
					wcLeadRecord: wcLead,
				}

				hasWorkflow := wcCampaign.EnableWorkflow && strings.TrimSpace(wcCampaign.WorkflowID) != ""
				hasAgent := responsesEnabled && strings.TrimSpace(wcCampaign.AgentID) != ""

				if hasWorkflow {

					wcCtx.skipResponse = true
					responsesEnabled = false
					log.Printf("[whatsapp-usecase] campaign %s has workflow %s — agent responses will be skipped", wcCampaign.ID, wcCampaign.WorkflowID)
				} else if hasAgent {
					agentRecord, prompt, tools := uc.resolveAgentPromptAndTools(wcCampaign.AgentID, wcCampaign.ID, "whatsapp")
					if agentRecord != nil {
						wcCtx.agent = agentRecord
						wcCtx.messagingPrompt = prompt
						wcCtx.tools = tools
					} else {
						wcCtx.skipResponse = true
						responsesEnabled = false
						log.Printf("[whatsapp-usecase] campaign %s could not resolve agent %s — skipping responses", wcCampaign.ID, wcCampaign.AgentID)
					}
				} else {

					wcCtx.skipResponse = true
					responsesEnabled = false
					log.Printf("[whatsapp-usecase] campaign %s has no workflow and no agent configured — skipping responses", wcCampaign.ID)
				}

				candidates = append(candidates, candidate{
					ctx:              wcCtx,
					lead:             wcLead,
					createdAt:        wcEntry.CreatedAt,
					responsesEnabled: responsesEnabled,
					source:           "whatsapp",
				})
			}
		}
	}

	if len(candidates) > 0 {
		sort.Slice(candidates, func(i, j int) bool {
			return candidates[i].createdAt.After(candidates[j].createdAt)
		})
		winner := candidates[0]

		if !winner.responsesEnabled {
			log.Printf("[whatsapp-usecase] most recent entry is %s campaign (created: %s) but agent responses are disabled - will record message but not respond",
				winner.source, winner.createdAt.Format(time.RFC3339))
			winner.ctx.skipResponse = true
		} else if len(candidates) > 1 {
			log.Printf("[whatsapp-usecase] using %s campaign (more recent entry: %s > %s)",
				winner.source, winner.createdAt.Format(time.RFC3339), candidates[1].createdAt.Format(time.RFC3339))
		} else {
			log.Printf("[whatsapp-usecase] using %s campaign (no other campaign found)", winner.source)
		}

		return winner.ctx, winner.lead
	}

	log.Printf("[whatsapp-usecase] no campaign found for number %s — checking organic campaigns", normalized)

	if metadata != nil && metadata.PhoneNumberID != "" && uc.businessPhoneRepo != nil && uc.wcCampaignRepo != nil && uc.wcEntryRepo != nil && uc.leadRepo != nil {
		businessPhone, err := uc.businessPhoneRepo.FindByMetaPhoneNumberID(metadata.PhoneNumberID)
		if err != nil || businessPhone == nil {
			log.Printf("[whatsapp-usecase] no campaign found for number %s (including organic)", normalized)
			return nil, nil
		}

		organicWorkspaceID := strings.TrimSpace(businessPhone.OwnerWorkspaceID)

		if organicWorkspaceID == "" {
			organicWorkspaceID = receivingWorkspaceID
		}

		organicCampaign, err := uc.wcCampaignRepo.FindLatestOrganicByBusinessPhone(organicWorkspaceID, businessPhone.ID)
		if err != nil || organicCampaign == nil {
			log.Printf("[whatsapp-usecase] no campaign found for number %s (including organic)", normalized)
			return nil, nil
		}

		if organicWorkspaceID == "" {
			organicWorkspaceID = organicCampaign.WorkspaceID
			log.Printf("[whatsapp-usecase] derived workspace %s from organic campaign %s for business phone %s (phone has no owner_workspace_id)", organicWorkspaceID, organicCampaign.ID, businessPhone.ID)
		}

		log.Printf("[whatsapp-usecase] found organic campaign %s for business phone %s", organicCampaign.ID, businessPhone.ID)

		leadRecord, _, err := uc.leadRepo.FindOrCreate(organicWorkspaceID, normalized, lead.LeadUpdate{})
		if err != nil {
			log.Printf("[whatsapp-usecase] failed to find/create lead for organic entry: %v", err)
			return nil, nil
		}

		newEntry := &wce.WhatsAppCampaignEntry{
			ID:         uuid.New().String(),
			CampaignID: organicCampaign.ID,
			LeadID:     leadRecord.ID,
			Status:     wce.SendStatusDelivered,
		}
		if err := uc.wcEntryRepo.Create(newEntry); err != nil {
			log.Printf("[whatsapp-usecase] failed to create organic entry: %v", err)
			return nil, nil
		}
		log.Printf("[whatsapp-usecase] created organic entry %s for lead %s in campaign %s", newEntry.ID, leadRecord.ID, organicCampaign.ID)

		organicCtx := &agentContext{
			wcCampaign:   organicCampaign,
			wcEntry:      newEntry,
			wcLeadRecord: leadRecord,
		}

		hasWorkflow := organicCampaign.EnableWorkflow && strings.TrimSpace(organicCampaign.WorkflowID) != ""
		hasAgent := organicCampaign.EnableAgentResponses && strings.TrimSpace(organicCampaign.AgentID) != ""

		if hasWorkflow {
			organicCtx.skipResponse = true
			log.Printf("[whatsapp-usecase] organic campaign %s has workflow %s — agent responses will be skipped", organicCampaign.ID, organicCampaign.WorkflowID)
		} else if hasAgent {
			agentRecord, prompt, tools := uc.resolveAgentPromptAndTools(organicCampaign.AgentID, organicCampaign.ID, "whatsapp")
			if agentRecord != nil {
				organicCtx.agent = agentRecord
				organicCtx.messagingPrompt = prompt
				organicCtx.tools = tools
			} else {
				organicCtx.skipResponse = true
				log.Printf("[whatsapp-usecase] organic campaign %s could not resolve agent %s — skipping responses", organicCampaign.ID, organicCampaign.AgentID)
			}
		} else {
			organicCtx.skipResponse = true
			log.Printf("[whatsapp-usecase] organic campaign %s has no workflow and no agent configured — skipping responses", organicCampaign.ID)
		}

		return organicCtx, leadRecord
	}

	log.Printf("[whatsapp-usecase] no campaign found for number %s (including organic)", normalized)
	return nil, nil
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

func brazilDoubleNineVariant(number string) string {
	trimmed := strings.TrimSpace(number)
	if trimmed == "" {
		return ""
	}

	withPlus := strings.HasPrefix(trimmed, "+")
	digits := trimmed
	if withPlus {
		digits = digits[1:]
	}

	if len(digits) <= 4 || !strings.HasPrefix(digits, "55") {
		return ""
	}

	suffix := digits[4:]

	if len(suffix) >= 1 && suffix[0] == '9' {

		variant := digits[:4] + suffix[1:]
		if withPlus {
			return "+" + variant
		}
		return variant
	}

	variant := digits[:4] + "9" + suffix
	if withPlus {
		return "+" + variant
	}
	return variant
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

func (uc *handleWhatsAppMessageUseCase) prependBaseSystemPrompt(ctx context.Context, prompt string) string {
	if uc.configRepo == nil {
		log.Println("[whatsapp-usecase] configRepo is nil, skipping base prompt")
		return prompt
	}

	cfg, err := uc.configRepo.Get(ctx)
	if err != nil {
		log.Printf("[whatsapp-usecase] error loading config: %v", err)
		return prompt
	}
	if cfg == nil {
		log.Println("[whatsapp-usecase] config is nil, skipping base prompt")
		return prompt
	}

	basePrompt := strings.TrimSpace(cfg.BaseSystemPrompt)
	if basePrompt == "" {
		log.Println("[whatsapp-usecase] BaseSystemPrompt is empty")
		return prompt
	}

	log.Printf("[whatsapp-usecase] Base system prompt loaded (%d chars)", len(basePrompt))

	if prompt == "" {
		return basePrompt
	}

	return basePrompt + "\n\n" + prompt
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

	if mediaClient != nil && uc.fileStorage != nil {
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

			cdnMediaID = uuid.NewString()
			extension := getExtensionFromMimeType(mimeType)
			var cdnKey string
			if entryID != "" {
				cdnKey = fmt.Sprintf("conversations/%s/%s/%s%s", entryType, entryID, cdnMediaID, extension)
			} else {
				cdnKey = fmt.Sprintf("conversations/unknown/%s/%s%s", message.From, cdnMediaID, extension)
			}

			if err := uc.fileStorage.UploadFile(cdnKey, mediaBytes); err != nil {
				log.Printf("[whatsapp-media] Failed to upload media to CDN: %v", err)
				cdnMediaID = ""
			} else {
				cdnURL = uc.fileStorage.GetFileURL(cdnKey)
				log.Printf("[whatsapp-media] Media uploaded to CDN: %s", cdnURL)

				if uc.conversationMediaRepo != nil && entryID != "" {
					var domainType conversation.MediaType
					switch mediaType {
					case "image":
						domainType = conversation.MediaTypeImage
					case "video":
						domainType = conversation.MediaTypeVideo
					case "document":
						domainType = conversation.MediaTypeDocument
					case "sticker":
						domainType = conversation.MediaTypeSticker
					default:
						domainType = conversation.MediaType(mediaType)
					}

					mediaRecord := &conversation.ConversationMedia{
						ID:               cdnMediaID,
						EntryID:          entryID,
						EntryType:        entryType,
						Type:             domainType,
						MimeType:         mimeType,
						URL:              cdnURL,
						OriginalFilename: captionOrFilename,
						SizeBytes:        int64(len(mediaBytes)),
						WhatsAppMediaID:  mediaID,
						CreatedAt:        time.Now().UTC(),
					}
					if err := uc.conversationMediaRepo.Create(mediaRecord); err != nil {
						log.Printf("[whatsapp-media] Failed to save media to DB: %v", err)
					} else {
						log.Printf("[whatsapp-media] Media saved to conversation_media table: %s", cdnMediaID)
					}
				}
			}
		}
	} else {
		if mediaClient == nil {
			log.Println("[whatsapp-media] WhatsApp client not resolved, skipping media download")
		}
		if uc.fileStorage == nil {
			log.Println("[whatsapp-media] File storage not configured, skipping media upload")
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
		var domainMediaType conversation.MediaType
		switch mediaType {
		case "image":
			domainMediaType = conversation.MediaTypeImage
		case "video":
			domainMediaType = conversation.MediaTypeVideo
		case "document":
			domainMediaType = conversation.MediaTypeDocument
		case "sticker":
			domainMediaType = conversation.MediaTypeSticker
		default:
			domainMediaType = conversation.MediaType(mediaType)
		}

		record := conversation.MessageHistoryRecord{
			EntryID:        entryID,
			EntryType:      entryType,
			Channel:        conversation.MessageChannelWhatsApp,
			MessageType:    conversation.MessageTypeMedia,
			ConversationID: conversationID,
			MessageID:      strings.TrimSpace(message.ID),
			From:           strings.TrimSpace(message.From),
			To:             businessNumber,
			Text:           displayText,
			Timestamp:      parseWhatsAppTimestamp(message.Timestamp),
			MediaID:        cdnMediaID,
			MediaType:      domainMediaType,
			MediaURL:       cdnURL,
		}

		if extractedText != "" {
			if meta, err := json.Marshal(map[string]string{"extracted_text": extractedText}); err == nil {
				record.Metadata = meta
			}
		}
		if err := uc.historyManager.Record(ctx, conversation.MessageDirectionInbound, record); err != nil {
			log.Printf("[whatsapp-media] Failed to record history: %v", err)
		} else {
			log.Printf("[whatsapp-media] Message recorded to history (extractedText=%d chars)", len(extractedText))
		}
	}

	if extractedText != "" {
		sourceLabel := strings.Title(mediaType)
		var userMessage string
		if captionOrFilename != "" {
			userMessage = fmt.Sprintf("[%s: %s]\n\nConteúdo extraído:\n%s", sourceLabel, captionOrFilename, extractedText)
		} else {
			userMessage = fmt.Sprintf("[%s]\n\nConteúdo extraído:\n%s", sourceLabel, extractedText)
		}

		if uc.aiService != nil && agentCtx != nil && !agentCtx.skipResponse && !IsFloodDetected(ctx) {
			var mediaModel string
			if agentCtx.agent != nil {
				mediaModel = agentCtx.agent.MessagingModel
			}
			if !uc.canAffordAI(agentCtx.getWorkspaceID(), mediaModel) {
				log.Printf("[whatsapp-media] media text extracted and recorded, but skipping AI response (insufficient balance for workspace %s)", agentCtx.getWorkspaceID())
			} else if dec := uc.guardCheckInbound(ctx, agentCtx.getWorkspaceID(), entryID, userMessage); dec.Block {
				log.Printf("[whatsapp-media] loop suspected for entry=%s reason=%s count=%d — skipping AI response", entryID, dec.Reason, dec.Count)
			} else if dec := uc.guardRecordAIResponse(ctx, agentCtx.getWorkspaceID(), entryID); dec.Block {
				log.Printf("[whatsapp-media] AI reply rate limit hit for entry=%s count=%d — skipping AI response", entryID, dec.Count)
			} else {
				uc.generateMediaAIResponse(ctx, agentCtx, leadRecord, message, metadata, conversationID, businessNumber, entryID, entryType, receivedPhoneID, history, userMessage)
			}
		} else if agentCtx != nil && agentCtx.skipResponse {
			log.Printf("[whatsapp-media] media text extracted and recorded, but skipping AI response (agent responses disabled)")
		} else if IsFloodDetected(ctx) {
			log.Printf("[whatsapp-media] media text extracted and recorded, but skipping AI response (flood detected)")
		}
	}

	go uc.maybeRunWhatsAppCampaignTools(context.Background(), agentCtx, leadRecord, conversationID, history)
	go uc.fireWorkflowTriggers(agentCtx, entryID, entryType, captionOrFilename, "media", mediaType, history, nil)

	log.Printf("[whatsapp-media] %s message processed", mediaType)
	return nil
}

func (uc *handleWhatsAppMessageUseCase) generateMediaAIResponse(
	ctx context.Context,
	agentCtx *agentContext,
	leadRecord *lead.Lead,
	message *conversation.WhatsAppMessage,
	metadata *conversation.WhatsAppMetadata,
	conversationID string,
	businessNumber string,
	entryID string,
	entryType shared.EntryType,
	receivedPhoneID string,
	history []*conversation.Message,
	userMessage string,
) {
	var toolsForAgent []toolsdomain.Definition

	var campaignPhoneID string
	if agentCtx.wcCampaign != nil {
		campaignPhoneID = agentCtx.wcCampaign.BusinessPhoneID
	}

	recipientPhone := strings.TrimSpace(message.From)

	mediaBusinessPhoneID := campaignPhoneID
	if mediaBusinessPhoneID == "" {
		mediaBusinessPhoneID = receivedPhoneID
	}
	var mediaEntryMetadata map[string]interface{}
	if agentCtx.wcEntry != nil {
		mediaEntryMetadata = agentCtx.wcEntry.Metadata
	}

	whatsappCtx := WhatsAppContext{
		UserPhoneNumber:     message.From,
		ConversationID:      conversationID,
		MessageReceivedTime: parseWhatsAppTimestamp(message.Timestamp),
	}
	if agentCtx.wcCampaign != nil {
		whatsappCtx.CampaignName = agentCtx.wcCampaign.Name
	}
	if agentCtx.agent != nil {
		whatsappCtx.AgentName = agentCtx.agent.Name
		ctx = agentctx.WithAgent(ctx, agentCtx.agent)
	}
	if agentCtx.wcLeadRecord != nil {
		whatsappCtx.UserName = agentCtx.wcLeadRecord.Name
	}
	if agentCtx.wcEntry != nil {
		whatsappCtx.Metadata = agentCtx.wcEntry.Metadata
	}
	if leadRecord != nil {
		whatsappCtx.UserName = leadRecord.Name
	}

	conversationMessages := uc.composeConversationHistory(history, businessNumber, message.From)
	conversationMessages = append(conversationMessages, ai.Message{Role: ai.RoleUser, Content: userMessage})

	var mediaMessagingModel string
	if agentCtx != nil && agentCtx.agent != nil && agentCtx.agent.MessagingModel != "" {
		mediaMessagingModel = agentCtx.agent.MessagingModel
	}
	outboundClient, err := uc.resolveWhatsAppClient(campaignPhoneID, receivedPhoneID)
	if err != nil {
		log.Printf("[whatsapp-media] Cannot resolve client for media AI reply: %v", err)
		return
	}
	incomingMessageID := strings.TrimSpace(message.ID)
	initialTypingSentAt := ensureWhatsAppTypingIndicatorFresh(ctx, outboundClient, incomingMessageID, "[whatsapp-media]", time.Time{})

	log.Printf("[whatsapp-media] Generating AI response for extracted media content (model: %s, tools: %d)...", mediaMessagingModel, len(toolsForAgent))
	generateInput := uc.assembleWhatsAppTurn(ctx, whatsAppTurn{
		agentCtx:        agentCtx,
		whatsappCtx:     whatsappCtx,
		RecipientPhone:  recipientPhone,
		BusinessPhoneID: mediaBusinessPhoneID,
		EntryID:         entryID,
		EntryType:       entryType,
		Vars:            agent.MetadataToVars(mediaEntryMetadata),
		Query:           userMessage,
		Messages:        conversationMessages,
		Model:           mediaMessagingModel,
		Temperature:     0.2,
	})
	toolsForAgent = generateInput.Tools

	output, err := uc.aiService.Generate(ctx, generateInput)
	if err != nil {
		log.Printf("[whatsapp-media] AI generation failed for media: %v", err)
		return
	}

	messages := output.Messages
	if len(messages) == 0 {
		if content := strings.TrimSpace(output.Message.Content); content != "" {
			messages = []string{content}
		}
	}

	for i, m := range messages {
		messages[i] = sanitizeAIOutput(m)
	}
	messages = filterEmptyStrings(messages)

	for i, msgText := range messages {
		if i == 0 {
			initialTypingSentAt = ensureWhatsAppTypingIndicatorFresh(ctx, outboundClient, incomingMessageID, "[whatsapp-media]", initialTypingSentAt)
			waitForWhatsAppTypingVisibility(initialTypingSentAt, whatsAppTypingMinVisibleTime)
		} else {
			sendWhatsAppTypingIndicatorWithDelay(ctx, outboundClient, incomingMessageID, "[whatsapp-media]", 1500*time.Millisecond, time.Second)
		}

		sendOutput, sendErr := outboundClient.SendTextMessage(ctx, conversation.SendTextMessageInput{
			To:   recipientPhone,
			Body: msgText,
		})
		if sendErr != nil {
			log.Printf("[whatsapp-media] Failed to send AI response: %v", sendErr)
			return
		}

		if uc.historyManager != nil && (conversationID != "" || entryID != "") {
			record := conversation.MessageHistoryRecord{
				EntryID:        entryID,
				EntryType:      entryType,
				Channel:        conversation.MessageChannelWhatsApp,
				MessageType:    conversation.MessageTypeAIResponse,
				ConversationID: conversationID,
				From:           businessNumber,
				To:             recipientPhone,
				Text:           msgText,
				Timestamp:      time.Now().UTC(),
			}
			if sendOutput != nil {
				record.MessageID = strings.TrimSpace(sendOutput.MessageID)
			}
			_ = uc.historyManager.Record(ctx, conversation.MessageDirectionOutbound, record)
		}

		log.Printf("[whatsapp-media] AI response sent for media content")
	}
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

	var inboundMediaID, inboundMediaURL string
	if uc.fileStorage != nil && audioEntryID != "" {
		inboundMediaID = uuid.NewString()
		ext := getExtensionFromMimeType(mimeType)
		key := fmt.Sprintf("conversations/%s/%s/%s%s", audioEntryType, audioEntryID, inboundMediaID, ext)
		if err := uc.fileStorage.UploadFile(key, audioBytes); err != nil {
			log.Printf("[whatsapp-audio] Failed to upload incoming audio to CDN: %v", err)
		} else {
			inboundMediaURL = uc.fileStorage.GetFileURL(key)
			log.Printf("[whatsapp-audio] Incoming audio saved to CDN: %s", inboundMediaURL)

			if uc.conversationMediaRepo != nil {
				mediaRecord := &conversation.ConversationMedia{
					ID:              inboundMediaID,
					EntryID:         audioEntryID,
					EntryType:       audioEntryType,
					Type:            conversation.MediaTypeAudio,
					MimeType:        mimeType,
					URL:             inboundMediaURL,
					SizeBytes:       int64(len(audioBytes)),
					WhatsAppMediaID: message.Audio.ID,
					CreatedAt:       time.Now().UTC(),
				}
				if err := uc.conversationMediaRepo.Create(mediaRecord); err != nil {
					log.Printf("[whatsapp-audio] Failed to save inbound audio to DB: %v", err)
				} else {
					log.Printf("[whatsapp-audio] Inbound audio saved to conversation_media table: %s", inboundMediaID)
				}
			}
		}
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
			From:              strings.TrimSpace(message.From),
			To:                businessNumber,
			Text:              "[Áudio]",
			MediaID:           mediaIDPtr,
			MediaType:         conversation.MediaTypeAudio,
			WhatsAppMessageID: &wamid,
			CreatedAt:         timestamp,
			UpdatedAt:         now,
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
		log.Println("[whatsapp-audio] Whisper (STT) not configured — audio saved without transcription")
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
		log.Printf("[whatsapp-audio] STT failed: %v — audio saved without transcription", err)
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
		log.Printf("[whatsapp-audio] Empty transcription (conf=%.2f, dur=%.2fs) — audio saved without transcription", transcription.Confidence, transcription.Duration)
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

	if savedMessageID != "" && uc.messageRepo != nil {
		updatedMsg := &conversation.Message{
			Text:      "[Áudio] " + transcribedText,
			UpdatedAt: time.Now().UTC(),
		}
		if err := uc.messageRepo.Update(savedMessageID, updatedMsg); err != nil {
			log.Printf("[whatsapp-audio] Failed to update message with transcription: %v", err)
		} else {
			log.Printf("[whatsapp-audio] Message %s updated with transcription", savedMessageID)
		}
	}

	if uc.aiService == nil {
		log.Println("[whatsapp-audio] AI service not configured")
		return errors.New("ai service not configured")
	}

	if agentCtx != nil && agentCtx.agent != nil {
		ctx = agentctx.WithAgent(ctx, agentCtx.agent)
	}

	whatsappCtx := WhatsAppContext{
		UserPhoneNumber:     message.From,
		ConversationID:      conversationID,
		MessageReceivedTime: parseWhatsAppTimestamp(message.Timestamp),
	}
	if agentCtx != nil {
		if agentCtx.wcCampaign != nil {
			whatsappCtx.CampaignName = agentCtx.wcCampaign.Name
		}
		if agentCtx.agent != nil {
			whatsappCtx.AgentName = agentCtx.agent.Name
		}
		if agentCtx.wcLeadRecord != nil {
			whatsappCtx.UserName = agentCtx.wcLeadRecord.Name
		}
		if agentCtx.wcEntry != nil {
			whatsappCtx.Metadata = agentCtx.wcEntry.Metadata
		}
	}
	if leadRecord != nil {
		whatsappCtx.UserName = leadRecord.Name
	}

	if agentCtx != nil && agentCtx.skipResponse {
		log.Printf("[whatsapp-audio] audio transcribed and recorded, but skipping AI response (agent responses disabled)")
		if agentCtx.wcCampaign != nil && (agentCtx.wcCampaign.EnableAnalysis || agentCtx.wcCampaign.EnableAutoStaging) {
			go uc.maybeRunWhatsAppCampaignTools(context.Background(), agentCtx, leadRecord, conversationID, history)
		}

		if agentCtx.wcCampaign != nil && agentCtx.wcCampaign.EnableWorkflow && strings.TrimSpace(agentCtx.wcCampaign.WorkflowID) != "" {
			log.Printf("[whatsapp-audio] firing workflow triggers for campaign %s (workflow %s)", agentCtx.wcCampaign.ID, agentCtx.wcCampaign.WorkflowID)
			go uc.fireWorkflowTriggers(agentCtx, audioEntryID, audioEntryType, transcribedText, "audio", "audio", history, nil)
		}
		return nil
	}

	if IsFloodDetected(ctx) {
		log.Printf("[whatsapp-audio] audio transcribed and recorded, but skipping AI response (flood detected)")
		return nil
	}

	if dec := uc.guardCheckInbound(ctx, agentCtx.getWorkspaceID(), audioEntryID, transcribedText); dec.Block {
		log.Printf("[whatsapp-audio] loop suspected for entry=%s reason=%s count=%d — skipping AI response", audioEntryID, dec.Reason, dec.Count)
		return nil
	}
	if dec := uc.guardRecordAIResponse(ctx, agentCtx.getWorkspaceID(), audioEntryID); dec.Block {
		log.Printf("[whatsapp-audio] AI reply rate limit hit for entry=%s count=%d — skipping AI response", audioEntryID, dec.Count)
		return nil
	}

	var audioMessagingModel string
	if agentCtx != nil && agentCtx.agent != nil && agentCtx.agent.MessagingModel != "" {
		audioMessagingModel = agentCtx.agent.MessagingModel
	}

	if !uc.canAffordAI(agentCtx.getWorkspaceID(), audioMessagingModel) {
		log.Printf("[whatsapp-audio] audio transcribed and recorded, but skipping AI response (insufficient balance for workspace %s)", agentCtx.getWorkspaceID())
		return nil
	}

	recipientPhone := strings.TrimSpace(message.From)

	audioBusinessPhoneID := audioCampaignPhoneID
	if audioBusinessPhoneID == "" {
		audioBusinessPhoneID = audioReceivedBusinessPhoneID
	}
	audioEntryIDSeed, audioEntryTypeSeed := "", shared.EntryType("")
	if agentCtx != nil {
		audioEntryIDSeed, audioEntryTypeSeed = agentCtx.getEntryInfo()
	}

	conversationMessages := uc.composeConversationHistory(history, businessNumber, message.From)
	conversationMessages = append(conversationMessages, ai.Message{Role: ai.RoleUser, Content: transcribedText})

	generateInput := uc.assembleWhatsAppTurn(ctx, whatsAppTurn{
		agentCtx:        agentCtx,
		whatsappCtx:     whatsappCtx,
		RecipientPhone:  recipientPhone,
		BusinessPhoneID: audioBusinessPhoneID,
		EntryID:         audioEntryIDSeed,
		EntryType:       audioEntryTypeSeed,
		Vars:            agent.MetadataToVars(whatsappCtx.Metadata),
		Query:           transcribedText,
		Messages:        conversationMessages,
		Model:           audioMessagingModel,
	})
	toolsForAgent := generateInput.Tools

	log.Printf("[whatsapp-audio] Generating AI response (model: %s, tools: %d)...", audioMessagingModel, len(toolsForAgent))
	aiStart := time.Now()

	output, err := uc.aiService.Generate(ctx, generateInput)
	aiLatency := time.Since(aiStart)

	if err != nil {
		log.Printf("[whatsapp-audio] AI generation failed: %v", err)
		return fmt.Errorf("AI generation failed: %w", err)
	}

	responseText := sanitizeAIOutput(strings.TrimSpace(output.Message.Content))
	if responseText == "" {
		log.Println("[whatsapp-audio] Empty AI response")
		return nil
	}

	log.Printf("[whatsapp-audio] AI response (latency=%dms): %q", aiLatency.Milliseconds(), responseText)

	log.Println("[whatsapp-audio] Sending text response...")
	sendStart := time.Now()

	sendResult, err := audioClient.SendTextMessage(ctx, conversation.SendTextMessageInput{
		To:   message.From,
		Body: responseText,
	})
	sendLatency := time.Since(sendStart)
	if err != nil {
		log.Printf("[whatsapp-audio] Failed to send text reply: %v", err)
		return err
	}

	log.Printf("[whatsapp-audio] Text reply sent! Message ID: %s (latency=%dms)", sendResult.MessageID, sendLatency.Milliseconds())

	if uc.historyManager != nil && (conversationID != "" || audioEntryID != "") {
		record := conversation.MessageHistoryRecord{
			EntryID:        audioEntryID,
			EntryType:      audioEntryType,
			Channel:        conversation.MessageChannelWhatsApp,
			MessageType:    conversation.MessageTypeAIResponse,
			ConversationID: conversationID,
			MessageID:      sendResult.MessageID,
			From:           businessNumber,
			To:             strings.TrimSpace(message.From),
			Text:           responseText,
			Timestamp:      time.Now().UTC(),
		}
		if err := uc.historyManager.Record(ctx, conversation.MessageDirectionOutbound, record); err != nil {
			log.Printf("[whatsapp-audio] Failed to record outbound history: %v", err)
		}
	}

	totalLatency := sttLatency + aiLatency + sendLatency
	log.Printf("[whatsapp-audio] Complete audio flow: STT=%dms, AI=%dms, Send=%dms, Total=%dms",
		sttLatency.Milliseconds(), aiLatency.Milliseconds(), sendLatency.Milliseconds(), totalLatency.Milliseconds())

	go uc.maybeRunWhatsAppCampaignTools(context.Background(), agentCtx, leadRecord, conversationID, history)
	go uc.fireWorkflowTriggers(agentCtx, audioEntryID, audioEntryType, transcribedText, "audio", "audio", history, nil)

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

func extractMetadataMap(metadata interface{}) map[string]interface{} {
	if metadata == nil {
		return nil
	}

	switch m := metadata.(type) {
	case map[string]interface{}:
		return m
	case map[string]string:
		result := make(map[string]interface{}, len(m))
		for k, v := range m {
			result[k] = v
		}
		return result
	default:
		return nil
	}
}

// whatsAppTurn is the per-call variation between WhatsApp's three agent turns
// (text, media and audio).
//
// Everything else about them was identical — interpolate the prompt, resolve
// tools and stamp the same seven seeds, build the identity preamble from the
// resolved tool names, then ground in the knowledge base — and had been
// copy-pasted three times. Only these fields actually differed.
type whatsAppTurn struct {
	agentCtx    *agentContext
	whatsappCtx WhatsAppContext

	// Seeds. The business phone differs per turn: the text path uses the
	// outbound phone, media and audio prefer the campaign's and fall back to
	// the one the message arrived on.
	RecipientPhone  string
	BusinessPhoneID string
	EntryID         string
	EntryType       shared.EntryType

	// Vars source also differs: text and audio interpolate from the WhatsApp
	// context metadata, media from the campaign entry's.
	Vars map[string]string

	// Query drives knowledge-base retrieval: the message body, the extracted
	// media content, or the transcription.
	Query string
	// Messages is the full history INCLUDING the turn being answered.
	Messages []ai.Message

	Model       string
	Temperature float32
	Segmented   bool
}

// assembleWhatsAppTurn builds the model request through the shared recipe.
//
// WhatsApp resolved its tools earlier (agentCtx.tools, which carries the
// campaign context a ContextualHandler needs), so this passes them through
// rather than asking the assembler to resolve again — the seeds and the
// identity/RAG assembly are what was duplicated, not the resolution.
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
	}
	campaignID, campaignType := "", ""
	if t.agentCtx != nil {
		if cID, cType := t.agentCtx.getCampaignInfo(); cID != "" {
			campaignID, campaignType = cID, cType
			seed["__campaign_id"] = cID
			seed["__campaign_type"] = cType
		}
	}

	// The tools were resolved upstream with the campaign context, so they are
	// handed over rather than resolved again. Seed stamping and the identity's
	// tool awareness are the assembler's job either way.
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
		History:            t.Messages,
		Model:              t.Model,
		Temperature:        t.Temperature,
		Segmented:          t.Segmented,
	})

	if t.agentCtx != nil {
		assembled.Input.WorkspaceID = t.agentCtx.getWorkspaceID()
	}
	return assembled.Input
}

// assembler returns the shared recipe, falling back to an empty one so a
// partially-wired container still assembles a prompt.
func (uc *handleWhatsAppMessageUseCase) assembler() *agentturn.Assembler {
	if uc.turnAssembler != nil {
		return uc.turnAssembler
	}
	return agentturn.New(uc.toolRegistry, uc.ragService)
}

// SetTurnAssembler wires the shared agent-turn recipe.
func (uc *handleWhatsAppMessageUseCase) SetTurnAssembler(a *agentturn.Assembler) {
	uc.turnAssembler = a
}
