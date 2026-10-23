package container

import (
	"log"
	"net/netip"
	"time"

	"vozko/brand"
	deliveryHttp "vozko/delivery/http"
	carrierhttp "vozko/delivery/http/carriermessaging"
	"vozko/domain/carriermessaging"
	conversation_domain "vozko/domain/conversation"
	"vozko/infra/cmcom"
	"vozko/infra/netguard"
	"vozko/infra/remotefile"
	carriercampaign_repository "vozko/infra/repositories/carriercampaign"
	carriermessaging_repository "vozko/infra/repositories/carriermessaging"
	cmuc "vozko/usecases/carriermessaging"
	conversation_usecase "vozko/usecases/conversation"
	"vozko/usecases/shared/automationrefs"
)

const (
	carrierMediaTimeout   = 20 * time.Second
	carrierExpiryBatch    = 500
	carrierSenderSyncSize = 200
)

type carrierBundle struct {
	Service   *cmuc.Service
	Consumers *cmuc.WebhookConsumers
	Handler   *carrierhttp.Handler
	Webhook   *carrierhttp.WebhookHandler
	Messages  *carriermessaging_repository.MessageRepository
	Spend     *carriermessaging_repository.SpendReader
	Overview  *carriercampaign_repository.OverviewReader

	RCSConversations *carriermessaging_repository.ConversationRepository
	SMSConversations *carriermessaging_repository.ConversationRepository
	Contacts         carriermessaging.ContactRepository
	Senders          carriermessaging.SenderRepository
	Routes           carriermessaging.RouteRepository
}

func (c *Container) initCarrier(history conversation_domain.MessageHistoryManager) {
	cfg := c.cfg.Carrier
	if history == nil || c.useCases == nil || c.useCases.publishWebhook == nil || c.useCases.startOfficialConversation == nil || c.useCases.conversationTemplate == nil {
		log.Fatalf("[carrier] the history manager, the webhook publisher and the WhatsApp outreach must exist first")
	}
	if c.services.assignmentService == nil || c.services.conversationHub == nil || c.services.webhookQueueSub == nil || c.services.webhookQueuePub == nil || c.services.cachedBalanceChecker == nil || c.services.workspacePricer == nil {
		log.Fatalf("[carrier] assignments, the conversation hub, the webhook queue, the balance checker and the pricer must exist first")
	}
	client, err := cmcom.NewClient(cmcom.Config{
		ProductToken: cfg.CMComProductToken, AccountID: cfg.CMComAccountID,
		GatewayURL: cfg.CMComGatewayURL, APIURL: cfg.CMComAPIURL,
	})
	if err != nil {
		log.Fatalf("[carrier] provider client: %v", err)
	}
	links, err := cmuc.NewLinkTokens(cfg.LinkSecret, cfg.PublicBaseURL)
	if err != nil {
		log.Fatalf("[carrier] tracked links: %v", err)
	}
	messages := carriermessaging_repository.NewMessageRepository(c.db)
	b := &carrierBundle{
		Messages: messages,
		Spend:    carriermessaging_repository.NewSpendReader(c.db),
		Overview: carriercampaign_repository.NewOverviewReader(c.db),

		RCSConversations: carriermessaging_repository.NewRCSConversationRepository(c.db),
		SMSConversations: carriermessaging_repository.NewSMSConversationRepository(c.db),
		Contacts:         carriermessaging_repository.NewContactRepository(c.db),
		Senders:          carriermessaging_repository.NewSenderRepository(c.db),
		Routes:           carriermessaging_repository.NewRouteRepository(c.db),
	}
	automation := conversation_usecase.NewInboundAutomation(
		c.useCases.triggerEvaluator,
		c.mustChannelAIReply(),
		c.services.analysisScheduler,
		c.repositories.conversation,
	).WithDelegations(c.services.delegations)

	b.Service, err = cmuc.NewService(cmuc.Deps{
		Repos: cmuc.Repositories{
			Senders:          b.Senders,
			Messages:         messages,
			Events:           carriermessaging_repository.NewEventRepository(c.db),
			Suppressions:     carriermessaging_repository.NewSuppressionRepository(c.db),
			Settings:         carriermessaging_repository.NewSettingsRepository(c.db),
			Contacts:         b.Contacts,
			RCSConversations: b.RCSConversations,
			SMSConversations: b.SMSConversations,
			Routes:           b.Routes,
		},
		Provider:         client,
		SMS:              client,
		Charges:          cmuc.NewCharges(c.services.workspacePricer, c.repositories.balance),
		Balance:          c.services.cachedBalanceChecker,
		Spend:            b.Spend,
		Links:            links,
		Leads:            cmuc.NewLeadResolver(c.repositories.lead),
		Transcript:       history,
		TranscriptStatus: c.repositories.conversation,
		Announcer:        c.services.conversationHub,
		Timezones:        cmuc.NewWorkspaceTimezones(c.repositories.workspaceConfig),
		WhatsApp:         cmuc.NewWhatsAppStarter(c.useCases.startOfficialConversation, c.useCases.conversationTemplate),
		Notices:          cmuc.NewNotices(c.notifications().Publisher),
		Inbox:            cmuc.NewInbox(c.services.assignmentService, automation),
		Documents:        c.services.documentValidator,
		Library:          c.repositories.media,
		RemoteMedia:      remotefile.NewFetcher(netguard.NewHTTPClient(carrierMediaTimeout), 0),
		MediaURLs:        c.mediaURLResolver(),
		AutomationRefs: automationrefs.Finders{
			Agents: c.repositories.agent, Workflows: c.repositories.workflow,
			Pipelines: c.repositories.pipeline, Departments: c.repositories.workspaceDepartment,
		},
		PlatformName:        brand.Active().Name,
		PlatformEmailDomain: brand.Active().EmailDomain,
		SMSSharedNumber:     cfg.SMSSharedNumber,
	})
	if err != nil {
		log.Fatalf("[carrier] service: %v", err)
	}
	b.Consumers = cmuc.NewWebhookConsumers(cmuc.QueueDeps{
		Sub: c.services.webhookQueueSub, Pub: c.services.webhookQueuePub,
		SharedState: c.redisProvider.SharedState(), Durable: c.processedWebhookEvents(),
	}, b.Service)
	b.Webhook, err = carrierhttp.NewWebhookHandler(cfg.WebhookToken, map[string]carrierhttp.ProviderWebhooks{
		client.Name(): cmcomWebhooks{},
	}, c.useCases.publishWebhook)
	if err != nil {
		log.Fatalf("[carrier] webhook handler: %v", err)
	}
	b.Handler = carrierhttp.NewHandler(b.Service)
	c.carrier = b
	bindWorkflowCarrier(c.carrierSenderRelay, b)
	if err := b.Consumers.Start(); err != nil {
		log.Fatalf("[carrier] failed to start the webhook consumers: %v", err)
	}
	log.Printf("[carrier] RCS and SMS enabled; provider webhooks must point at %s/webhooks/carrier/%s/<CARRIER_WEBHOOK_TOKEN>/status|inbound|events", cfg.PublicBaseURL, client.Name())
}

func carrierRoutes(c *Container) deliveryHttp.CarrierRoutes {
	if c.carrier == nil || c.carrier.Handler == nil || c.carrier.Webhook == nil {
		log.Fatalf("[carrier] routes requested before RCS and SMS were built")
	}
	if c.carrierCampaigns == nil || c.carrierCampaigns.Handler == nil {
		log.Fatalf("[carrier] campaign routes requested before campaigns were built")
	}
	return deliveryHttp.CarrierRoutes{Messaging: c.carrier.Handler, Webhook: c.carrier.Webhook, Campaigns: c.carrierCampaigns.Handler}
}

type cmcomWebhooks struct{}

func (cmcomWebhooks) DecodeStatusReports(contentType string, body []byte) ([]carriermessaging.StatusReport, error) {
	return cmcom.DecodeStatusReports(contentType, body)
}

func (cmcomWebhooks) DecodeInbound(body []byte) (carriermessaging.Inbound, error) {
	return cmcom.DecodeInbound(body)
}

func (cmcomWebhooks) DecodeEvent(body []byte) (carriermessaging.SuggestionClick, bool, error) {
	return cmcom.DecodeEvent(body)
}

func (cmcomWebhooks) FromAllowedSource(ip netip.Addr) bool {
	return cmcom.FromAllowedSource(ip)
}
