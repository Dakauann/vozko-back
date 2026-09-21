package whatsapp_campaign_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"vozko/domain/balance"
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
	"vozko/usecases/campaignqueue"

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
	ConsumeWhatsappTemplate balance.ConsumeWhatsappTemplateUseCase
	CheckBalance            balance.CheckBalanceUseCase
	MessageHistoryManager   conversation.MessageHistoryManager
	shared                  cache.SharedState
	WorkspaceConfigRepo     wsc.Repository
	LeadCampaignSendRepo    lead_campaign_send.Repository
	InflightReserver        balance.InflightReserver
	CachedBalanceChecker    balance.CachedBalanceChecker
	triggerEvaluator        workflow_domain.TriggerEvaluator

	// runner owns the queue: subscription bookkeeping, pause/stop, requeue and
	// completion. Everything above is what this channel adds on top.
	runner *campaignqueue.Runner
}

func NewMessageConsumerUseCase(
	messageQueueSub messaging.MessageQueueSub,
	messageQueuePub messaging.MessageQueuePub,
	campaignRepo wc.Repository,
	entryRepo wce.Repository,
	templateRepo template.Repository,
	businessPhoneRepo businessphone.Repository,
	whatsAppClientFactory conversation.WhatsAppClientFactory,
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
	uc := &messageConsumerUseCase{
		MessageQueueSub:         messageQueueSub,
		MessageQueuePub:         messageQueuePub,
		CampaignRepo:            campaignRepo,
		EntryRepo:               entryRepo,
		TemplateRepo:            templateRepo,
		BusinessPhoneRepo:       businessPhoneRepo,
		WhatsAppClientFactory:   whatsAppClientFactory,
		ConsumeWhatsappTemplate: consumeWhatsappTemplate,
		CheckBalance:            checkBalance,
		MessageHistoryManager:   messageHistoryManager,
		shared:                  sharedState,
		WorkspaceConfigRepo:     workspaceConfigRepo,
		LeadCampaignSendRepo:    leadCampaignSendRepo,
		InflightReserver:        inflightReserver,
		CachedBalanceChecker:    cachedBalanceChecker,
	}

	uc.attachRunner(messageQueueSub, messageQueuePub, sharedState)
	return uc
}

// attachRunner builds the shared queue runner for this consumer.
//
// Separate from the constructor so the test harness, which assembles the struct
// field by field to inject doubles, gets the SAME runner configuration as
// production instead of a second copy that can drift from it.
func (c *messageConsumerUseCase) attachRunner(
	sub messaging.MessageQueueSub,
	pub messaging.MessageQueuePub,
	sharedState cache.SharedState,
) {
	c.runner = campaignqueue.New(
		sub, pub, sharedState,
		campaignStatusStore{repo: c.CampaignRepo},
		pendingEntryCounter{repo: c.EntryRepo},
		campaignqueue.Config{
			Namespace:         wc.QueueNamespace,
			PauseRequeueDelay: pauseRequeueDelay,
			Precheck:          c.precheck,
			// Meta throttles this channel, so the pace is a token delay rather
			// than a ban-avoidance control.
			Pace: func(string) time.Duration { return messageSendDelay },
			Logf: func(format string, args ...any) {
				fmt.Printf("whatsapp "+format+"\n", args...)
			},
		},
		c.handle,
	)
}

// campaignStatusStore and pendingEntryCounter adapt this channel's repositories
// onto the two narrow ports the shared runner needs, so the runner never sees a
// campaign repository and cannot start depending on one.
type campaignStatusStore struct{ repo wc.Repository }

func (s campaignStatusStore) ListRunningCampaignIDs() ([]string, error) {
	items, err := s.repo.ListByStatus(wc.CampaignStatusRunning)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(items))
	for _, c := range items {
		if c != nil && c.ID != "" {
			ids = append(ids, c.ID)
		}
	}
	return ids, nil
}

func (s campaignStatusStore) CompleteCampaign(campaignID string) (bool, error) {
	return s.repo.UpdateStatus(campaignID, wc.CampaignStatusCompleted, wc.CampaignStatusRunning)
}

type pendingEntryCounter struct{ repo wce.Repository }

func (p pendingEntryCounter) CountPendingEntries(campaignID string) (int64, error) {
	counts, err := p.repo.CountByStatus(campaignID)
	if err != nil || counts == nil {
		return 0, err
	}
	return counts.Pending, nil
}

func (c *messageConsumerUseCase) SetTriggerEvaluator(eval workflow_domain.TriggerEvaluator) {
	c.triggerEvaluator = eval
}

// The queue lifecycle is the shared runner's. Everything this consumer still
// owns below is about Meta: templates, billing categories and balance.
// Start asserts this channel's own wiring, then hands the queue to the runner.
//
// The two preconditions are deliberately checked here rather than in the shared
// runner: a WhatsApp client factory is a Cloud API concept, and a runner that
// knew about one could not serve a channel that has none.
func (c *messageConsumerUseCase) Start() error {
	if c.MessageQueueSub == nil {
		return fmt.Errorf("whatsapp campaign consumer: message queue subscriber is required")
	}
	if c.WhatsAppClientFactory == nil {
		fmt.Println("whatsapp campaign consumer: WhatsApp client factory not configured; skipping subscription")
		return nil
	}

	// Paired with SignalSendingsAvailable, which publishes on this channel.
	go c.shared.Subscribe(context.Background(), "signal:wa_sendings", func(_ []byte) {})

	return c.runner.Start()
}
func (c *messageConsumerUseCase) SubscribeToCampaign(id string) error {
	return c.runner.SubscribeToCampaign(id)
}
func (c *messageConsumerUseCase) PauseCampaignConsumer(id string) error {
	return c.runner.PauseCampaignConsumer(id)
}
func (c *messageConsumerUseCase) ResumeCampaignConsumer(id string) error {
	return c.runner.ResumeCampaignConsumer(id)
}
func (c *messageConsumerUseCase) StopCampaignConsumer(id string) error {
	return c.runner.StopCampaignConsumer(id)
}
func (c *messageConsumerUseCase) IsSubscribed(id string) bool { return c.runner.IsSubscribed(id) }

// precheck refuses to subscribe a campaign whose template can no longer be sent.
//
// The campaign is STOPPED here rather than left running, because an unapproved
// template is not a transient condition: every entry would fail identically, and
// a campaign that fails 40.000 times is worse than one that refuses to start.
func (c *messageConsumerUseCase) precheck(campaignID string) error {
	initial, err := c.CampaignRepo.FindByID(campaignID)
	if err != nil || initial == nil {
		return fmt.Errorf("whatsapp campaign consumer: campaign %s not found: %w", campaignID, err)
	}
	cachedTemplate, err := c.TemplateRepo.FindByID(initial.TemplateID)
	if err != nil || cachedTemplate == nil {
		_, _ = c.CampaignRepo.UpdateStatus(campaignID, wc.CampaignStatusStopped, wc.CampaignStatusRunning)
		return fmt.Errorf("whatsapp campaign consumer: template %s not found for campaign %s", initial.TemplateID, campaignID)
	}
	if !cachedTemplate.IsReadyToSend() {
		_, _ = c.CampaignRepo.UpdateStatus(campaignID, wc.CampaignStatusStopped, wc.CampaignStatusRunning)
		return fmt.Errorf("whatsapp campaign consumer: template %s not ready for campaign %s", cachedTemplate.Name, campaignID)
	}
	return nil
}

// handle is this channel's send step: resolve the template, charge the
// workspace, send, and refund if the send never reached the customer.
//
// Every return is one of the four shared outcomes. The distinction that matters
// most is Drop versus RetryLater: a Drop resolves the entry and counts it toward
// completion, while a RetryLater leaves it pending and must NOT count, or the
// campaign completes with work still queued.
func (c *messageConsumerUseCase) handle(msg campaignqueue.Message) campaignqueue.Result {
	campaignItem, err := c.CampaignRepo.FindByID(msg.CampaignID)
	if err != nil || campaignItem == nil {
		fmt.Printf("whatsapp campaign consumer: failed to find campaign %s: %v\n", msg.CampaignID, err)
		return campaignqueue.Requeue
	}

	currentTemplate, err := c.TemplateRepo.FindByID(campaignItem.TemplateID)
	if err != nil || currentTemplate == nil {
		fmt.Printf("whatsapp campaign consumer: current template %s not found for campaign %s: %v\n", campaignItem.TemplateID, msg.CampaignID, err)
		c.updateEntryStatusWithError(msg.EntryID, wce.SendStatusFailed, "", internalWhatsAppCampaignErrTemplateUnavailable, "Template metadata unavailable")
		return campaignqueue.Drop
	}

	if !currentTemplate.IsReadyToSend() {
		errorMessage := currentTemplate.GetUsabilityMessage()
		if errorMessage == "" {
			errorMessage = fmt.Sprintf("Template is not ready to send. Current status: %s", currentTemplate.Status)
		}
		fmt.Printf("whatsapp campaign consumer: template %s is not ready for campaign %s: %s\n", currentTemplate.Name, msg.CampaignID, errorMessage)
		c.updateEntryStatusWithError(msg.EntryID, wce.SendStatusFailed, "", internalWhatsAppCampaignErrTemplateNotReady, errorMessage)
		return campaignqueue.Drop
	}

	templateCategory, err := currentTemplate.BillingCategory()
	if err != nil {
		fmt.Printf("whatsapp campaign consumer: invalid billing category for template %s in campaign %s: %v\n", currentTemplate.Name, msg.CampaignID, err)
		c.updateEntryStatusWithError(msg.EntryID, wce.SendStatusFailed, "", internalWhatsAppCampaignErrTemplateUnavailable, "Template billing category unavailable")
		return campaignqueue.Drop
	}

	templateCostMicros, err := c.ConsumeWhatsappTemplate.GetTemplateCostMicros(campaignItem.WorkspaceID, templateCategory)
	if err != nil {
		if errors.Is(err, workspace_plan.ErrSubscriptionNotCurrent) || errors.Is(err, workspace_plan.ErrSubscriptionNotActive) {
			fmt.Printf("whatsapp campaign consumer: no active subscription for workspace %s (campaign %s), failing entry\n", campaignItem.WorkspaceID, msg.CampaignID)
			c.updateEntryStatusWithError(msg.EntryID, wce.SendStatusFailed, "", 0, "no active subscription")
			return campaignqueue.Drop
		}
		if errors.Is(err, balance.ErrPriceUnavailable) {
			// No price configured. Requeuing spins forever and sending would be
			// free, so the entry fails and says why.
			fmt.Printf("whatsapp campaign consumer: no price configured for workspace %s (campaign %s), refusing to send unbilled\n", campaignItem.WorkspaceID, msg.CampaignID)
			c.updateEntryStatusWithError(msg.EntryID, wce.SendStatusFailed, "", 0, "no price configured for this template category")
			return campaignqueue.Drop
		}
		fmt.Printf("whatsapp campaign consumer: failed to get template cost for workspace %s (campaign %s): %v\n", campaignItem.WorkspaceID, msg.CampaignID, err)
		return campaignqueue.Requeue
	}

	// Belt as well as braces. The price guard lives in the billing use case so
	// every sender inherits it, but this path sends at bulk volume and a zero
	// slipping through here is a whole campaign delivered free.
	if templateCostMicros <= 0 {
		fmt.Printf("whatsapp campaign consumer: refusing to send unbilled for workspace %s (campaign %s): price is zero\n", campaignItem.WorkspaceID, msg.CampaignID)
		c.updateEntryStatusWithError(msg.EntryID, wce.SendStatusFailed, "", 0, "no price configured for this template category")
		return campaignqueue.Drop
	}

	if c.InflightReserver == nil || c.CachedBalanceChecker == nil {
		fmt.Printf("CRITICAL: whatsapp campaign consumer, inflightReserver or cachedBalanceChecker is nil, blocking send for workspace %s (fail-closed)\n", campaignItem.WorkspaceID)
		return campaignqueue.Requeue
	}

	budget, balErr := c.CachedBalanceChecker.GetBalance(campaignItem.WorkspaceID)
	if balErr != nil {
		fmt.Printf("whatsapp campaign consumer: balance read error for workspace %s: %v, blocking send (fail-closed)\n", campaignItem.WorkspaceID, balErr)
		return campaignqueue.Requeue
	}

	reserved, reserveErr := c.InflightReserver.Reserve(campaignItem.WorkspaceID, templateCostMicros, budget)
	if reserveErr != nil {
		fmt.Printf("whatsapp campaign consumer: reserve error for workspace %s: %v, blocking send (fail-closed)\n", campaignItem.WorkspaceID, reserveErr)
		return campaignqueue.Requeue
	}
	if !reserved {
		return campaignqueue.RetryLater(balanceRequeueDelay)
	}

	defer func() {
		_ = c.InflightReserver.Release(campaignItem.WorkspaceID, templateCostMicros)
	}()

	// The debit is keyed on the ENTRY, not the campaign.
	//
	// Every recipient of a campaign previously shared one reference — the
	// campaign id — which is wrong in both directions. The ledger cannot tell two
	// charges for one recipient apart from two recipients charged once each, so a
	// redelivered queue message is indistinguishable from a legitimate second
	// send; and any dedup keyed on the reference would collapse a whole campaign
	// into a single charge. A per-entry reference is what makes a charge
	// attributable to the person who received it.
	_, consumeErr := c.ConsumeWhatsappTemplate.Execute(campaignItem.WorkspaceID, msg.EntryID, templateCategory)
	if consumeErr != nil {
		if errors.Is(consumeErr, balance.ErrInsufficientBalance) || errors.Is(consumeErr, balance.ErrBalanceNotFound) {
			fmt.Printf("whatsapp campaign consumer: debit failed (insufficient balance) for workspace %s (campaign %s), requeuing with delay\n", campaignItem.WorkspaceID, msg.CampaignID)
			return campaignqueue.RetryLater(balanceRequeueDelay)
		}
		if errors.Is(consumeErr, workspace_plan.ErrSubscriptionNotCurrent) || errors.Is(consumeErr, workspace_plan.ErrSubscriptionNotActive) {
			fmt.Printf("whatsapp campaign consumer: subscription expired during debit for workspace %s (campaign %s), failing entry\n", campaignItem.WorkspaceID, msg.CampaignID)
			c.updateEntryStatusWithError(msg.EntryID, wce.SendStatusFailed, "", 0, "no active subscription")
			return campaignqueue.Drop
		}
		if errors.Is(consumeErr, balance.ErrPriceUnavailable) {
			fmt.Printf("whatsapp campaign consumer: no price configured for workspace %s (campaign %s), refusing to send unbilled\n", campaignItem.WorkspaceID, msg.CampaignID)
			c.updateEntryStatusWithError(msg.EntryID, wce.SendStatusFailed, "", 0, "no price configured for this template category")
			return campaignqueue.Drop
		}
		fmt.Printf("whatsapp campaign consumer: debit failed for workspace %s: %v, requeuing message\n", campaignItem.WorkspaceID, consumeErr)
		return campaignqueue.Requeue
	}

	fmt.Printf("whatsapp campaign consumer: debited balance for workspace %s (campaign %s, category %s), now sending\n", campaignItem.WorkspaceID, msg.CampaignID, templateCategory)

	sendResult := c.sendTemplateMessage(campaignItem, currentTemplate, msg.EntryID, msg.PhoneNumber)

	if sendResult == sendResultConfigError || sendResult == sendResultAPIError {
		// Refunded under the SAME reference the debit used, or the credit cannot
		// be paired with the charge it reverses.
		if refundErr := c.ConsumeWhatsappTemplate.Refund(campaignItem.WorkspaceID, msg.EntryID, templateCategory); refundErr != nil {
			fmt.Printf("whatsapp campaign consumer: WARNING: failed to refund balance for workspace %s after send failure: %v\n", campaignItem.WorkspaceID, refundErr)
		} else {
			fmt.Printf("whatsapp campaign consumer: refunded balance for workspace %s (campaign %s), send failed, message not delivered\n", campaignItem.WorkspaceID, msg.CampaignID)
		}
	}

	// Done regardless of the send's own result: the entry has been resolved
	// either way, and the pacing delay applies because a provider call was made.
	return campaignqueue.Done
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

	fmt.Printf("whatsapp campaign consumer: template '%s' isNamedFormat=%v, paramNames=%v\n",
		tmpl.Name, tmpl.IsNamedParameterFormat(), tmpl.GetParameterNames())

	// One assembly for every send path, in the domain. It is what puts the
	// one-time code on an authentication template's button, which this consumer
	// has no reason to know about: the code arrives as the entry's first
	// variable, like any other.
	sendInput, err := tmpl.BuildSendInput(template.SendInputParams{
		To:         normalizedPhone,
		BodyParams: variables,
	})
	if err != nil {
		fmt.Printf("whatsapp campaign consumer: cannot build the send for %s: %v\n", phoneNumber, err)
		c.updateEntryStatusWithError(entryID, wce.SendStatusFailed, "",
			internalWhatsAppCampaignErrMissingEntryVariables, err.Error())
		return sendResultConfigError
	}
	if sendInput.HeaderMediaID != "" {
		fmt.Printf("whatsapp campaign consumer: using stored header media ID: %s (type: %s)\n",
			sendInput.HeaderMediaID, sendInput.HeaderType)
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
				// The canonical spelling every channel seeds on message_received.
				// Kept alongside the legacy phone_number so one workflow can use
				// {{contact_number}} on both triggers. Normalized, so it equals
				// what a later inbound reply will seed for the same lead.
				workflow_domain.DataKeyContactNumber: normalizedPhone,
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

func SignalSendingsAvailable() {
	if waSharedStateRef != nil {
		_ = waSharedStateRef.Publish("signal:wa_sendings", []byte("1"))
	}
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
