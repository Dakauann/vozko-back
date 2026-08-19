package whatsapp_campaign_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"vozko/domain/balance"
	"vozko/domain/business_metrics"
	"vozko/domain/cache"
	"vozko/domain/conversation"
	"vozko/domain/lead"
	lcs "vozko/domain/lead_campaign_send"
	lead_campaign_send "vozko/domain/lead_campaign_send"
	"vozko/domain/messaging"
	"vozko/domain/shared"
	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/whatsapp/template"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
	workflow_domain "vozko/domain/workflow"
	workspace_plan "vozko/domain/workspace/workspace_plan"
	wsc "vozko/domain/workspace_config"

	"github.com/google/uuid"
)

const messageSendDelay = 15 * time.Millisecond
const pauseRequeueDelay = 3 * time.Second
const balanceRequeueDelay = 5 * time.Second

const (
	internalWhatsAppCampaignErrNoBusinessPhoneConfigured = 900002
	internalWhatsAppCampaignErrResolveClient             = 900003
	internalWhatsAppCampaignErrMissingEntryVariables     = 900006
	internalWhatsAppCampaignErrTemplateUnavailable       = 900007
	internalWhatsAppCampaignErrTemplateNotReady          = 900008
)

var (
	waSharedStateRef cache.SharedState
)

type messageConsumerUseCase struct {
	MessageQueueSub         messaging.MessageQueueSub
	MessageQueuePub         messaging.MessageQueuePub
	CampaignRepo            wc.Repository
	EntryRepo               wce.Repository
	TemplateRepo            template.Repository
	BusinessPhoneRepo       businessphone.Repository
	WhatsAppClientFactory   conversation.WhatsAppClientFactory
	RecordMetric            business_metrics.RecordMetricUseCase
	ConsumeWhatsappTemplate balance.ConsumeWhatsappTemplateUseCase
	CheckBalance            balance.CheckBalanceUseCase
	MessageHistoryManager   conversation.MessageHistoryManager
	shared                  cache.SharedState
	WorkspaceConfigRepo     wsc.Repository
	LeadCampaignSendRepo    lead_campaign_send.Repository
	InflightReserver        balance.InflightReserver
	CachedBalanceChecker    balance.CachedBalanceChecker
	triggerEvaluator        workflow_domain.TriggerEvaluator

	subscribedMu    sync.RWMutex
	subscribedCamps map[string]bool
	pausedCamps     map[string]bool
}

func NewMessageConsumerUseCase(
	messageQueueSub messaging.MessageQueueSub,
	messageQueuePub messaging.MessageQueuePub,
	campaignRepo wc.Repository,
	entryRepo wce.Repository,
	templateRepo template.Repository,
	businessPhoneRepo businessphone.Repository,
	whatsAppClientFactory conversation.WhatsAppClientFactory,
	recordMetric business_metrics.RecordMetricUseCase,
	consumeWhatsappTemplate balance.ConsumeWhatsappTemplateUseCase,
	checkBalance balance.CheckBalanceUseCase,
	messageHistoryManager conversation.MessageHistoryManager,
	sharedState cache.SharedState,
	workspaceConfigRepo wsc.Repository,
	leadCampaignSendRepo lead_campaign_send.Repository,
	inflightReserver balance.InflightReserver,
	cachedBalanceChecker balance.CachedBalanceChecker,
) wc.MessageConsumerUseCase {
	waSharedStateRef = sharedState
	return &messageConsumerUseCase{
		MessageQueueSub:         messageQueueSub,
		MessageQueuePub:         messageQueuePub,
		CampaignRepo:            campaignRepo,
		EntryRepo:               entryRepo,
		TemplateRepo:            templateRepo,
		BusinessPhoneRepo:       businessPhoneRepo,
		WhatsAppClientFactory:   whatsAppClientFactory,
		RecordMetric:            recordMetric,
		ConsumeWhatsappTemplate: consumeWhatsappTemplate,
		CheckBalance:            checkBalance,
		MessageHistoryManager:   messageHistoryManager,
		shared:                  sharedState,
		WorkspaceConfigRepo:     workspaceConfigRepo,
		LeadCampaignSendRepo:    leadCampaignSendRepo,
		InflightReserver:        inflightReserver,
		CachedBalanceChecker:    cachedBalanceChecker,
		subscribedCamps:         make(map[string]bool),
		pausedCamps:             make(map[string]bool),
	}
}

func (c *messageConsumerUseCase) ensureCampaignStateMapsLocked() {
	if c.subscribedCamps == nil {
		c.subscribedCamps = make(map[string]bool)
	}
	if c.pausedCamps == nil {
		c.pausedCamps = make(map[string]bool)
	}
}

func (c *messageConsumerUseCase) PauseCampaignConsumer(campaignID string) error {
	if err := c.shared.SetString("campaign:whatsapp:paused:"+campaignID, "1", 0); err != nil {
		return err
	}

	c.subscribedMu.RLock()
	isSubscribed := c.subscribedCamps[campaignID]
	c.subscribedMu.RUnlock()
	if !isSubscribed {
		return nil
	}

	stopper, ok := c.MessageQueueSub.(interface{ StopConsumer(string) error })
	if !ok {
		return nil
	}

	topic := fmt.Sprintf("%s.%s", wc.WhatsAppCampaignDispatchTopic, campaignID)
	if err := stopper.StopConsumer(topic); err != nil {
		return fmt.Errorf("failed to pause whatsapp campaign %s: %w", campaignID, err)
	}

	c.subscribedMu.Lock()
	c.ensureCampaignStateMapsLocked()
	delete(c.subscribedCamps, campaignID)
	c.pausedCamps[campaignID] = true
	c.subscribedMu.Unlock()

	return nil
}

func (c *messageConsumerUseCase) SetTriggerEvaluator(eval workflow_domain.TriggerEvaluator) {
	c.triggerEvaluator = eval
}

func (c *messageConsumerUseCase) StopCampaignConsumer(campaignID string) error {
	topic := fmt.Sprintf("%s.%s", wc.WhatsAppCampaignDispatchTopic, campaignID)

	_ = c.shared.SetString("campaign:whatsapp:stopped:"+campaignID, "1", 0)
	_ = c.shared.Del("campaign:whatsapp:paused:"+campaignID, "campaign:whatsapp:remaining:"+campaignID)

	if err := c.MessageQueueSub.DeleteQueue(topic); err != nil {
		return fmt.Errorf("failed to delete queue for whatsapp campaign %s: %w", campaignID, err)
	}

	c.subscribedMu.Lock()
	c.ensureCampaignStateMapsLocked()
	delete(c.subscribedCamps, campaignID)
	delete(c.pausedCamps, campaignID)
	c.subscribedMu.Unlock()

	fmt.Printf("whatsapp campaign consumer: stopped and deleted queue for campaign %s\n", campaignID)
	return nil
}

func (c *messageConsumerUseCase) ResumeCampaignConsumer(campaignID string) error {
	if err := c.shared.Del("campaign:whatsapp:paused:" + campaignID); err != nil {
		return err
	}

	c.subscribedMu.Lock()
	c.ensureCampaignStateMapsLocked()
	wasPaused := c.pausedCamps[campaignID]
	delete(c.pausedCamps, campaignID)
	c.subscribedMu.Unlock()
	if !wasPaused {
		return nil
	}

	return c.SubscribeToCampaign(campaignID)
}

func (c *messageConsumerUseCase) Start() error {
	if c.MessageQueueSub == nil {
		return fmt.Errorf("whatsapp campaign consumer: message queue subscriber is required")
	}

	if c.WhatsAppClientFactory == nil {
		fmt.Println("whatsapp campaign consumer: WhatsApp client factory not configured; skipping subscription")
		return nil
	}

	go c.shared.Subscribe(context.Background(), "signal:wa_sendings", func(_ []byte) {

	})

	activeCampaigns, err := c.CampaignRepo.ListByStatus(wc.CampaignStatusRunning)
	if err != nil {
		return fmt.Errorf("whatsapp campaign consumer: failed to list active campaigns: %w", err)
	}

	var wg sync.WaitGroup
	subErrors := make([]error, len(activeCampaigns))
	for i, camp := range activeCampaigns {
		wg.Add(1)
		go func(idx int, campaignID string) {
			defer wg.Done()
			topic := fmt.Sprintf("%s.%s", wc.WhatsAppCampaignDispatchTopic, campaignID)
			if c.isQueueEmpty(topic) {
				if updated, err := c.CampaignRepo.UpdateStatus(campaignID, wc.CampaignStatusCompleted, wc.CampaignStatusRunning); err == nil && updated {
					fmt.Printf("whatsapp campaign consumer: campaign %s completed on startup (queue empty)\n", campaignID)
				}
				return
			}
			c.seedCounterIfMissing(campaignID)
			if err := c.SubscribeToCampaign(campaignID); err != nil {
				subErrors[idx] = fmt.Errorf("whatsapp campaign consumer: failed to subscribe to campaign %s: %w", campaignID, err)
			}
		}(i, camp.ID)
	}
	wg.Wait()

	for _, subErr := range subErrors {
		if subErr != nil {
			return subErr
		}
	}
	return nil
}

func (c *messageConsumerUseCase) SubscribeToCampaign(campaignID string) error {
	c.subscribedMu.RLock()
	if c.subscribedCamps[campaignID] {
		c.subscribedMu.RUnlock()
		fmt.Printf("whatsapp campaign consumer: already subscribed to campaign %s\n", campaignID)
		return nil
	}
	c.subscribedMu.RUnlock()

	_ = c.shared.Del("campaign:whatsapp:stopped:" + campaignID)
	_ = c.shared.Del("campaign:whatsapp:paused:" + campaignID)

	initialCampaign, err := c.CampaignRepo.FindByID(campaignID)
	if err != nil || initialCampaign == nil {
		return fmt.Errorf("whatsapp campaign consumer: campaign %s not found: %w", campaignID, err)
	}
	cachedTemplate, err := c.TemplateRepo.FindByID(initialCampaign.TemplateID)
	if err != nil || cachedTemplate == nil {
		fmt.Printf("whatsapp campaign consumer: template %s not found for campaign %s, stopping campaign\n", initialCampaign.TemplateID, campaignID)
		_, _ = c.CampaignRepo.UpdateStatus(campaignID, wc.CampaignStatusStopped, wc.CampaignStatusRunning)
		return fmt.Errorf("whatsapp campaign consumer: template %s not found for campaign %s", initialCampaign.TemplateID, campaignID)
	}
	if !cachedTemplate.IsReadyToSend() {
		fmt.Printf("whatsapp campaign consumer: template %s is not ready to send (status: %s) for campaign %s, stopping campaign\n", cachedTemplate.Name, cachedTemplate.Status, campaignID)
		_, _ = c.CampaignRepo.UpdateStatus(campaignID, wc.CampaignStatusStopped, wc.CampaignStatusRunning)
		return fmt.Errorf("whatsapp campaign consumer: template %s not ready for campaign %s", cachedTemplate.Name, campaignID)
	}

	topic := fmt.Sprintf("%s.%s", wc.WhatsAppCampaignDispatchTopic, campaignID)

	err = c.MessageQueueSub.Subscribe(topic, func(message []byte, ack messaging.MessageAck) {
		var payload wc.DispatchQueueMessage
		if err := json.Unmarshal(message, &payload); err != nil {
			fmt.Println("failed to decode whatsapp campaign dispatch payload:", err)
			_ = ack.Nack(false)
			return
		}

		paused, _ := c.shared.Exists("campaign:whatsapp:paused:" + payload.CampaignID)
		if paused {

			c.requeueWithDelay(topic, message, pauseRequeueDelay)
			_ = ack.Ack()
			return
		}

		campaign, err := c.CampaignRepo.FindByID(payload.CampaignID)
		if err != nil || campaign == nil {
			fmt.Printf("whatsapp campaign consumer: failed to find campaign %s for balance check: %v\n", payload.CampaignID, err)
			_ = ack.Nack(true)
			return
		}

		currentTemplate, err := c.TemplateRepo.FindByID(campaign.TemplateID)
		if err != nil || currentTemplate == nil {
			fmt.Printf("whatsapp campaign consumer: current template %s not found for campaign %s: %v\n", campaign.TemplateID, payload.CampaignID, err)
			c.updateEntryStatusWithError(payload.EntryID, wce.SendStatusFailed, "", internalWhatsAppCampaignErrTemplateUnavailable, "Template metadata unavailable")
			_ = ack.Ack()
			c.completeCampaignByCounter(payload.CampaignID, topic)
			return
		}

		if !currentTemplate.IsReadyToSend() {
			errorMessage := currentTemplate.GetUsabilityMessage()
			if errorMessage == "" {
				errorMessage = fmt.Sprintf("Template is not ready to send. Current status: %s", currentTemplate.Status)
			}
			fmt.Printf("whatsapp campaign consumer: template %s is not ready for campaign %s: %s\n", currentTemplate.Name, payload.CampaignID, errorMessage)
			c.updateEntryStatusWithError(payload.EntryID, wce.SendStatusFailed, "", internalWhatsAppCampaignErrTemplateNotReady, errorMessage)
			_ = ack.Ack()
			c.completeCampaignByCounter(payload.CampaignID, topic)
			return
		}

		templateCategory, err := currentTemplate.BillingCategory()
		if err != nil {
			fmt.Printf("whatsapp campaign consumer: invalid billing category for template %s in campaign %s: %v\n", currentTemplate.Name, payload.CampaignID, err)
			c.updateEntryStatusWithError(payload.EntryID, wce.SendStatusFailed, "", internalWhatsAppCampaignErrTemplateUnavailable, "Template billing category unavailable")
			_ = ack.Ack()
			c.completeCampaignByCounter(payload.CampaignID, topic)
			return
		}

		templateCostMicros, err := c.ConsumeWhatsappTemplate.GetTemplateCostMicros(campaign.WorkspaceID, templateCategory)
		if err != nil {
			if errors.Is(err, workspace_plan.ErrSubscriptionNotCurrent) || errors.Is(err, workspace_plan.ErrSubscriptionNotActive) {
				fmt.Printf("whatsapp campaign consumer: no active subscription for workspace %s (campaign %s), failing entry\n", campaign.WorkspaceID, payload.CampaignID)
				c.updateEntryStatusWithError(payload.EntryID, wce.SendStatusFailed, "", 0, "no active subscription")
				_ = ack.Ack()
				c.completeCampaignByCounter(payload.CampaignID, topic)
				return
			}
			if errors.Is(err, balance.ErrPriceUnavailable) {
				// No price configured. Requeuing spins forever and sending would be
				// free, so the entry fails and says why.
				fmt.Printf("whatsapp campaign consumer: no price configured for workspace %s (campaign %s), refusing to send unbilled\n", campaign.WorkspaceID, payload.CampaignID)
				c.updateEntryStatusWithError(payload.EntryID, wce.SendStatusFailed, "", 0, "no price configured for this template category")
				_ = ack.Ack()
				c.completeCampaignByCounter(payload.CampaignID, topic)
				return
			}
			fmt.Printf("whatsapp campaign consumer: failed to get template cost for workspace %s (campaign %s): %v\n", campaign.WorkspaceID, payload.CampaignID, err)
			_ = ack.Nack(true)
			return
		}

		// Belt as well as braces. The price guard lives in the billing use case so
		// every sender inherits it, but this path sends at bulk volume and a zero
		// slipping through here is a whole campaign delivered free.
		if templateCostMicros <= 0 {
			fmt.Printf("whatsapp campaign consumer: refusing to send unbilled for workspace %s (campaign %s): price is zero\n", campaign.WorkspaceID, payload.CampaignID)
			c.updateEntryStatusWithError(payload.EntryID, wce.SendStatusFailed, "", 0, "no price configured for this template category")
			_ = ack.Ack()
			c.completeCampaignByCounter(payload.CampaignID, topic)
			return
		}

		if c.InflightReserver == nil || c.CachedBalanceChecker == nil {
			fmt.Printf("CRITICAL: whatsapp campaign consumer, inflightReserver or cachedBalanceChecker is nil, blocking send for workspace %s (fail-closed)\n", campaign.WorkspaceID)
			_ = ack.Nack(true)
			return
		}

		budget, balErr := c.CachedBalanceChecker.GetBalance(campaign.WorkspaceID)
		if balErr != nil {
			fmt.Printf("whatsapp campaign consumer: balance read error for workspace %s: %v, blocking send (fail-closed)\n", campaign.WorkspaceID, balErr)
			_ = ack.Nack(true)
			return
		}

		reserved, reserveErr := c.InflightReserver.Reserve(campaign.WorkspaceID, templateCostMicros, budget)
		if reserveErr != nil {
			fmt.Printf("whatsapp campaign consumer: reserve error for workspace %s: %v, blocking send (fail-closed)\n", campaign.WorkspaceID, reserveErr)
			_ = ack.Nack(true)
			return
		}
		if !reserved {
			c.requeueWithDelay(topic, message, balanceRequeueDelay)
			_ = ack.Ack()
			return
		}

		defer func() {
			_ = c.InflightReserver.Release(campaign.WorkspaceID, templateCostMicros)
		}()

		_, consumeErr := c.ConsumeWhatsappTemplate.Execute(campaign.WorkspaceID, payload.CampaignID, templateCategory)
		if consumeErr != nil {
			if errors.Is(consumeErr, balance.ErrInsufficientBalance) || errors.Is(consumeErr, balance.ErrBalanceNotFound) {
				fmt.Printf("whatsapp campaign consumer: debit failed (insufficient balance) for workspace %s (campaign %s), requeuing with delay\n", campaign.WorkspaceID, payload.CampaignID)
				c.requeueWithDelay(topic, message, balanceRequeueDelay)
				_ = ack.Ack()
				return
			}
			if errors.Is(consumeErr, workspace_plan.ErrSubscriptionNotCurrent) || errors.Is(consumeErr, workspace_plan.ErrSubscriptionNotActive) {
				fmt.Printf("whatsapp campaign consumer: subscription expired during debit for workspace %s (campaign %s), failing entry\n", campaign.WorkspaceID, payload.CampaignID)
				c.updateEntryStatusWithError(payload.EntryID, wce.SendStatusFailed, "", 0, "no active subscription")
				_ = ack.Ack()
				c.completeCampaignByCounter(payload.CampaignID, topic)
				return
			}

			if errors.Is(consumeErr, balance.ErrPriceUnavailable) {
				fmt.Printf("whatsapp campaign consumer: no price configured for workspace %s (campaign %s), refusing to send unbilled\n", campaign.WorkspaceID, payload.CampaignID)
				c.updateEntryStatusWithError(payload.EntryID, wce.SendStatusFailed, "", 0, "no price configured for this template category")
				_ = ack.Ack()
				c.completeCampaignByCounter(payload.CampaignID, topic)
				return
			}
			fmt.Printf("whatsapp campaign consumer: debit failed for workspace %s: %v, requeuing message\n", campaign.WorkspaceID, consumeErr)
			_ = ack.Nack(true)
			return
		}

		fmt.Printf("whatsapp campaign consumer: debited balance for workspace %s (campaign %s, category %s), now sending\n", campaign.WorkspaceID, payload.CampaignID, templateCategory)

		sendResult := c.sendTemplateMessage(campaign, currentTemplate, payload.EntryID, payload.PhoneNumber)

		if sendResult == sendResultConfigError || sendResult == sendResultAPIError {
			if refundErr := c.ConsumeWhatsappTemplate.Refund(campaign.WorkspaceID, payload.CampaignID, templateCategory); refundErr != nil {
				fmt.Printf("whatsapp campaign consumer: WARNING: failed to refund balance for workspace %s after send failure: %v\n", campaign.WorkspaceID, refundErr)
			} else {
				fmt.Printf("whatsapp campaign consumer: refunded balance for workspace %s (campaign %s), send failed, message not delivered\n", campaign.WorkspaceID, payload.CampaignID)
			}
		}

		if err := ack.Ack(); err != nil {
			fmt.Printf("whatsapp campaign consumer: failed to ack message for %s/%s: %v\n", payload.CampaignID, payload.PhoneNumber, err)
		}

		time.Sleep(messageSendDelay)

		c.completeCampaignByCounter(payload.CampaignID, topic)
	})

	if err != nil {
		return fmt.Errorf("failed to subscribe to whatsapp campaign %s: %w", campaignID, err)
	}

	c.subscribedMu.Lock()
	c.ensureCampaignStateMapsLocked()
	c.subscribedCamps[campaignID] = true
	delete(c.pausedCamps, campaignID)
	c.subscribedMu.Unlock()

	fmt.Printf("whatsapp campaign consumer: subscribed to campaign %s messages\n", campaignID)
	return nil
}

func (c *messageConsumerUseCase) IsSubscribed(campaignID string) bool {
	c.subscribedMu.RLock()
	defer c.subscribedMu.RUnlock()
	return c.subscribedCamps[campaignID]
}

type sendTemplateMessageResult int

const (
	sendResultSuccess sendTemplateMessageResult = iota
	sendResultConfigError
	sendResultAPIError
	// sendResultUnknown is Meta ACCEPTING the send and our failing to read the
	// answer. It is deliberately NOT an error result: refunding here would credit
	// a message the customer has already received, so the charge stands and the
	// delivery-status webhook settles the entry.
	sendResultUnknown
)

func (c *messageConsumerUseCase) sendTemplateMessage(campaign *wc.Campaign, tmpl *template.Template, entryID, phoneNumber string) sendTemplateMessageResult {
	campaignID := campaign.ID

	if campaign.BusinessPhoneID == "" {
		fmt.Printf("whatsapp campaign consumer: campaign %s has no business phone configured\n", campaignID)
		c.updateEntryStatusWithError(entryID, wce.SendStatusFailed, "", internalWhatsAppCampaignErrNoBusinessPhoneConfigured, "No business phone configured")
		return sendResultConfigError
	}

	// Fetch the campaign entry once and reuse it for the spam check, variables,
	// template-info and send record below. Previously each of those steps
	// re-queried the same fat row (jsonb metadata + variables array), up to 4×
	// per message on the hottest path. The row is read at the start and the only
	// later read (recordCampaignSend → LeadID) is of an immutable field, so a
	// single fetch is safe.
	entry, entryErr := c.EntryRepo.FindByID(entryID)
	if entryErr != nil {
		fmt.Printf("whatsapp campaign consumer: failed to find entry %s: %v\n", entryID, entryErr)
		entry = nil
	}

	if c.isEntrySpam(entry, campaign.BusinessPhoneID, campaign.WorkspaceID) {
		fmt.Printf("whatsapp campaign consumer: entry %s marked as possible spam, skipping\n", entryID)
		c.updateEntryStatus(entryID, wce.SendStatusNotEligiblePossibleSpam, "")
		return sendResultConfigError
	}

	whatsappClient, err := c.WhatsAppClientFactory.ClientForPhone(campaign.BusinessPhoneID)
	if err != nil {
		fmt.Printf("whatsapp campaign consumer: failed to resolve WhatsApp client for business phone %s: %v\n", campaign.BusinessPhoneID, err)
		c.updateEntryStatusWithError(entryID, wce.SendStatusFailed, "", internalWhatsAppCampaignErrResolveClient, "Could not resolve WhatsApp client")
		return sendResultConfigError
	}

	requiredParams := tmpl.ParameterCount()

	var variables []string
	if requiredParams > 0 {
		if entry == nil {
			c.updateEntryStatusWithError(entryID, wce.SendStatusFailed, "", internalWhatsAppCampaignErrMissingEntryVariables, "Could not find entry for variables")
			return sendResultConfigError
		}
		if len(entry.Variables) >= requiredParams {
			variables = entry.Variables[:requiredParams]
		}
	}

	ctx := context.Background()

	normalizedPhone := lead.NormalizeWhatsAppNumber(phoneNumber)
	if normalizedPhone != phoneNumber {
		fmt.Printf("whatsapp campaign consumer: normalized phone %s -> %s\n", phoneNumber, normalizedPhone)
	}

	paramNames := tmpl.GetParameterNames()
	isNamedFormat := tmpl.IsNamedParameterFormat()
	fmt.Printf("whatsapp campaign consumer: template '%s' isNamedFormat=%v, paramNames=%v\n", tmpl.Name, isNamedFormat, paramNames)

	sendInput := conversation.SendTemplateMessageInput{
		To:                     normalizedPhone,
		TemplateName:           tmpl.Name,
		Language:               tmpl.Language,
		Parameters:             variables,
		ParameterNames:         paramNames,
		IsNamedParameterFormat: isNamedFormat,
	}

	if tmpl.HasMediaHeader() {
		headerMediaID := tmpl.GetHeaderMediaID()
		sendInput.HeaderType = strings.ToLower(tmpl.GetHeaderFormat())
		sendInput.HeaderMediaID = headerMediaID
		fmt.Printf("whatsapp campaign consumer: using stored header media ID: %s (type: %s)\n", headerMediaID, sendInput.HeaderType)
	}

	result, err := whatsappClient.SendTemplateMessage(ctx, sendInput)

	if errors.Is(err, conversation.ErrSendOutcomeUnknown) {
		// Delivered as far as Meta is concerned. Anything that looks like a
		// failure from here — refund, retry, a failed entry status — acts on a
		// message the recipient already has.
		fmt.Printf("whatsapp campaign consumer: send outcome unknown for %s (Meta accepted, response unreadable), keeping the charge: %v\n", phoneNumber, err)
		c.updateEntryStatus(entryID, wce.SendStatusSent, "")
		return sendResultUnknown
	}

	if err != nil {
		fmt.Printf("whatsapp campaign consumer: failed to send template to %s: %v\n", phoneNumber, err)
		errorCode, errorMessage := parseMetaAPIError(result)
		if errorMessage == "" {
			errorMessage = err.Error()
			if len(errorMessage) > 500 {
				errorMessage = errorMessage[:500]
			}
		}
		c.updateEntryStatusWithError(entryID, wce.SendStatusFailed, "", errorCode, errorMessage)
		return sendResultAPIError
	}

	messageID := ""
	if result != nil {
		messageID = result.MessageID
	}

	c.storeTemplateInfoOnEntry(entry, tmpl, variables)

	if c.RecordMetric != nil {
		entityID := strings.TrimSpace(messageID)
		if entityID == "" {
			entityID = uuid.NewString()
		}

		metadata := map[string]string{
			"to":            phoneNumber,
			"template_name": tmpl.Name,
			"template_id":   campaign.TemplateID,
			"language":      tmpl.Language,
			"campaign_id":   campaignID,
			"entry_id":      entryID,
			"source":        "whatsapp_campaign",
		}

		if err := c.RecordMetric.Execute(business_metrics.RecordMetricInput{
			EventType:  business_metrics.EventWhatsAppTemplateMessageSent,
			EntityID:   entityID,
			EntityType: business_metrics.EntityTypeMessage,
			Metadata:   metadata,
		}); err != nil {
			fmt.Printf("whatsapp campaign consumer: failed to record metric: %v\n", err)
		}
	}

	c.updateEntryStatus(entryID, wce.SendStatusSent, messageID)
	c.recordCampaignSend(entry, campaign.BusinessPhoneID, campaignID)

	// When enabled on the campaign, persist + broadcast the sent template as a
	// real conversation message so it shows up in the CRM even if the recipient
	// never replies.
	if campaign.ShowTemplateInCrm {
		c.recordTemplateMessage(entry, tmpl, variables, normalizedPhone, messageID)
	}

	fmt.Printf("whatsapp campaign consumer: successfully sent template to %s, messageId: %s\n", phoneNumber, messageID)

	if c.triggerEvaluator != nil && campaign.EnableWorkflow {
		log.Printf("[workflow] campaign trigger: dispatching trigger_campaign_sent for campaign=%s entry=%s workspace=%s",
			campaignID, entryID, campaign.WorkspaceID)
		go c.triggerEvaluator.Evaluate(workflow_domain.TriggerEvent{
			WorkspaceID: campaign.WorkspaceID,
			EntryID:     entryID,
			EntryType:   "whatsapp_campaign_entry",
			TriggerType: workflow_domain.TriggerCampaignSent,
			Data: map[string]interface{}{
				"campaign_id":  campaignID,
				"phone_number": phoneNumber,
				"template_id":  campaign.TemplateID,
				"message_id":   messageID,
				"entry_id":     entryID,
			},
		})
	}

	return sendResultSuccess
}

// storeTemplateInfoOnEntry renders the sent template and stores it on the entry
// metadata (template_info), the source the CRM entry panel reads.
func (c *messageConsumerUseCase) storeTemplateInfoOnEntry(entry *wce.WhatsAppCampaignEntry, tmpl *template.Template, params []string) {
	if entry == nil || tmpl == nil {
		return
	}
	templateInfo := tmpl.RenderInfo(params)

	meta := entry.Metadata
	if meta == nil {
		meta = make(map[string]interface{})
	}
	meta["template_info"] = templateInfo

	if err := c.EntryRepo.UpdateMetadata(entry.ID, meta); err != nil {
		fmt.Printf("whatsapp campaign consumer: failed to store template info on entry %s: %v\n", entry.ID, err)
	}
}

// recordTemplateMessage persists the outbound campaign template as a real
// conversation message (type=template) and broadcasts it, so the send appears
// in the CRM (inbox + thread) even when the recipient never replies. Gated per
// campaign by ShowTemplateInCrm. The metadata mirrors the entry's template_info
// so the CRM TemplateBubble renders any template shape (header/media, body,
// footer, buttons). Dedup by WhatsApp message id keeps retries idempotent.
func (c *messageConsumerUseCase) recordTemplateMessage(entry *wce.WhatsAppCampaignEntry, tmpl *template.Template, params []string, toPhone, messageID string) {
	if c.MessageHistoryManager == nil || entry == nil || tmpl == nil {
		return
	}
	templateInfo := tmpl.RenderInfo(params)
	metaBytes, err := json.Marshal(templateInfo)
	if err != nil {
		fmt.Printf("whatsapp campaign consumer: failed to marshal template metadata for entry %s: %v\n", entry.ID, err)
		return
	}
	bodyText, _ := templateInfo["body_text"].(string)

	record := conversation.MessageHistoryRecord{
		EntryID:     entry.ID,
		EntryType:   shared.EntryTypeWhatsApp,
		Channel:     conversation.MessageChannelWhatsApp,
		MessageType: conversation.MessageTypeTemplate,
		MessageID:   messageID,
		To:          toPhone,
		Text:        bodyText,
		Metadata:    json.RawMessage(metaBytes),
	}
	if err := c.MessageHistoryManager.Record(context.Background(), conversation.MessageDirectionOutbound, record); err != nil {
		fmt.Printf("whatsapp campaign consumer: failed to record template message for entry %s: %v\n", entry.ID, err)
	}
}

func (c *messageConsumerUseCase) updateEntryStatus(entryID string, status wce.SendStatus, messageID string) {
	if err := c.EntryRepo.UpdateStatus(entryID, status, messageID, 0, ""); err != nil {
		fmt.Printf("whatsapp campaign consumer: failed to update entry status for %s: %v\n", entryID, err)
	}
}

func (c *messageConsumerUseCase) updateEntryStatusWithError(entryID string, status wce.SendStatus, messageID string, errorCode int, errorMessage string) {
	if err := c.EntryRepo.UpdateStatus(entryID, status, messageID, errorCode, errorMessage); err != nil {
		fmt.Printf("whatsapp campaign consumer: failed to update entry status for %s: %v\n", entryID, err)
	}
}

// parseMetaAPIError delegates to the domain so the campaign pipeline and cold
// outbound report a provider failure the same way.
func parseMetaAPIError(result *conversation.SendTextMessageOutput) (int, string) {
	return template.ParseProviderError(result)
}

func (c *messageConsumerUseCase) requeueWithDelay(topic string, message []byte, delay time.Duration) {
	if err := c.MessageQueuePub.PublishWithDelay(topic, message, delay); err != nil {
		fmt.Printf("whatsapp campaign consumer: failed to requeue message with delay on topic %s: %v\n", topic, err)
	}
}

func (c *messageConsumerUseCase) isQueueEmpty(topic string) bool {
	mainLen, err := c.MessageQueueSub.GetQueueLength(topic)
	if err != nil || mainLen > 0 {
		return false
	}
	delayLen, err := c.MessageQueueSub.GetQueueLength(topic + messaging.DelayQueueSuffix)
	return err == nil && delayLen == 0
}

func (c *messageConsumerUseCase) completeCampaignIfEmpty(campaignID, topic string) {
	mainLen, err := c.MessageQueueSub.GetQueueLength(topic)
	if err != nil || mainLen > 0 {
		return
	}
	delayLen, err := c.MessageQueueSub.GetQueueLength(topic + messaging.DelayQueueSuffix)
	if err != nil || delayLen > 0 {
		return
	}

	updated, err := c.CampaignRepo.UpdateStatus(campaignID, wc.CampaignStatusCompleted, wc.CampaignStatusRunning)
	if err != nil || !updated {
		return
	}

	fmt.Printf("whatsapp campaign consumer: campaign %s completed (all entries processed)\n", campaignID)

	_ = c.shared.Del("campaign:whatsapp:paused:" + campaignID)
	_ = c.MessageQueueSub.DeleteQueue(topic)

	c.subscribedMu.Lock()
	delete(c.subscribedCamps, campaignID)
	c.subscribedMu.Unlock()
}

func SignalSendingsAvailable() {
	if waSharedStateRef != nil {
		_ = waSharedStateRef.Publish("signal:wa_sendings", []byte("1"))
	}
}

func (c *messageConsumerUseCase) seedCounterIfMissing(campaignID string) {
	key := "campaign:whatsapp:remaining:" + campaignID
	exists, err := c.shared.Exists(key)
	if err != nil || exists {
		return
	}
	counts, err := c.EntryRepo.CountByStatus(campaignID)
	if err != nil || counts == nil || counts.Pending == 0 {
		return
	}
	_ = c.shared.SetString(key, strconv.FormatInt(counts.Pending, 10), 72*time.Hour)
}

func (c *messageConsumerUseCase) completeCampaignByCounter(campaignID, topic string) {
	key := "campaign:whatsapp:remaining:" + campaignID
	remaining, err := c.shared.Decr(key)

	if err != nil || remaining != 0 {
		return
	}

	updated, err := c.CampaignRepo.UpdateStatus(campaignID, wc.CampaignStatusCompleted, wc.CampaignStatusRunning)
	if err != nil || !updated {
		return
	}

	fmt.Printf("whatsapp campaign consumer: campaign %s completed (all entries processed)\n", campaignID)

	_ = c.shared.Del("campaign:whatsapp:paused:"+campaignID, key)
	_ = c.MessageQueueSub.DeleteQueue(topic)

	c.subscribedMu.Lock()
	delete(c.subscribedCamps, campaignID)
	c.subscribedMu.Unlock()
}

func (c *messageConsumerUseCase) isEntrySpam(entry *wce.WhatsAppCampaignEntry, businessPhoneID, workspaceID string) bool {
	if c.WorkspaceConfigRepo == nil || c.LeadCampaignSendRepo == nil {
		return false
	}

	cfg, err := c.WorkspaceConfigRepo.GetByWorkspaceID(context.Background(), workspaceID)
	if err != nil || cfg.CampaignSpamProtectionDays <= 0 {
		return false
	}

	if entry == nil {
		return false
	}

	lastSent, err := c.LeadCampaignSendRepo.GetLastSendTime(entry.LeadID, businessPhoneID)
	if err != nil {
		return false
	}

	return lcs.WithinSpamWindow(lastSent, cfg.CampaignSpamProtectionDays, time.Now())
}

func (c *messageConsumerUseCase) recordCampaignSend(entry *wce.WhatsAppCampaignEntry, businessPhoneID, campaignID string) {
	if c.LeadCampaignSendRepo == nil || entry == nil {
		return
	}

	if err := c.LeadCampaignSendRepo.Record(entry.LeadID, businessPhoneID, campaignID); err != nil {
		fmt.Printf("whatsapp campaign consumer: failed to record campaign send for lead %s: %v\n", entry.LeadID, err)
	}
}
