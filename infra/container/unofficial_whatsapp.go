package container

import (
	"context"
	"log"
	"time"

	uwhttp "vozko/delivery/http/unofficial_whatsapp"
	conversation_domain "vozko/domain/conversation"
	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
	uwrepo "vozko/infra/repositories/unofficial_whatsapp"
	"vozko/infra/uazapi"
	conversation_usecase "vozko/usecases/conversation"
	uwuc "vozko/usecases/unofficial_whatsapp"
	uwcuc "vozko/usecases/unofficial_whatsapp_campaign"
)

type unofficialWhatsAppBundle struct {
	Enabled bool

	Servers       uw.ServerRepository
	Instances     uw.InstanceRepository
	Contacts      uw.ContactRepository
	Conversations uw.ConversationRepository
	Groups        uw.GroupRepository

	Provider  uw.ProviderAPI
	Messaging uw.MessagingAPI
	GroupAPI  uw.GroupAPI
	Assets    uw.RemoteAssetFetcher

	StartConv *uwuc.StartConversationUseCase

	Entitlements *uwuc.InstanceEntitlementReader

	ProcessedEvts uw.ProcessedEventRepository

	Handler        *uwhttp.Handler
	WebhookHandler *uwhttp.WebhookHandler
	GroupHandler   *uwhttp.GroupHandler

	Consume            *uwuc.ConsumeWebhookUseCase
	CheckHealth        *uwuc.CheckInstanceHealthUseCase
	ReconcileCapacity  *uwuc.ReconcileServerCapacityUseCase
	ProvisionInstances *uwuc.ProvisionInstanceUseCase
	PurgeEvents        *uwuc.PurgeProcessedEventsUseCase

	SeedInboxPublisher *uwuc.SeedInboxPublisher
	ConsumeSeedInbox   *uwuc.ConsumeSeedInboxUseCase
}

func (c *Container) initUnofficialWhatsApp() {
	bundle := &unofficialWhatsAppBundle{}
	c.unofficialWhatsApp = bundle

	if err := uw.ValidateWebhookBaseURL(c.cfg.UnofficialWhatsAppWebhookBaseURL); err != nil {
		log.Fatalf("[unofficial-whatsapp] %v", err)
	}

	provider := uazapi.NewClient(uazapi.Config{})
	bundle.Provider = provider
	bundle.Messaging = provider
	bundle.GroupAPI = provider
	bundle.Assets = provider
	bundle.Servers = uwrepo.NewServerRepository(c.db)
	bundle.Instances = uwrepo.NewInstanceRepository(c.db)
	bundle.Contacts = uwrepo.NewContactRepository(c.db)
	bundle.Conversations = uwrepo.NewConversationRepository(c.db)
	bundle.Groups = uwrepo.NewGroupRepository(c.db)
	bundle.ProcessedEvts = uwrepo.NewProcessedEventRepository(c.db)

	bundle.StartConv = uwuc.NewStartConversationUseCase(
		bundle.Instances, bundle.Servers, bundle.Contacts, bundle.Conversations,
		bundle.Messaging, uwrepo.NewLeadLinker(c.repositories.lead))

	bundle.Entitlements = uwuc.NewInstanceEntitlementReader(bundle.Instances)

	c.seedUnofficialWhatsAppServer(bundle)

	provision := uwuc.NewProvisionInstanceUseCase(
		bundle.Servers, bundle.Instances, provider, c.cfg.UnofficialWhatsAppWebhookBaseURL)
	provision.SetEntitlements(bundle.Entitlements)
	bundle.ProvisionInstances = provision

	bundle.Handler = uwhttp.NewHandler(uwhttp.HandlerDeps{
		Provision:   provision,
		Connect:     uwuc.NewConnectInstanceUseCase(bundle.Instances, bundle.Servers, provider),
		List:        uwuc.NewListInstancesUseCase(bundle.Instances),
		Get:         uwuc.NewGetInstanceUseCase(bundle.Instances),
		UpdateCfg:   uwuc.NewUpdateInstanceConfigUseCase(bundle.Instances),
		RotateToken: uwuc.NewRotateDeliveryTokenUseCase(bundle.Instances, bundle.Servers, provision),
		Remove:      uwuc.NewDeleteInstanceUseCase(bundle.Instances, bundle.Servers, provider),
		StartConv:   bundle.StartConv,
		Allowance:   uwuc.NewGetAllowanceUseCase(bundle.Entitlements),
		Departments: c.services.conversationAuthImpl,
	})

	bundle.GroupHandler = uwhttp.NewGroupHandler(uwuc.NewGroupUseCases(uwuc.GroupUseCaseDeps{
		Instances:     bundle.Instances,
		Servers:       bundle.Servers,
		Contacts:      bundle.Contacts,
		Conversations: bundle.Conversations,
		Groups:        bundle.Groups,
		GroupAPI:      bundle.GroupAPI,
		Messaging:     bundle.Messaging,
		Assets:        bundle.Assets,
		FileStorage:   c.services.fileStorage,
	}))

	bundle.WebhookHandler = uwhttp.NewWebhookHandler(bundle.Instances, nil)

	bundle.CheckHealth = uwuc.NewCheckInstanceHealthUseCase(
		bundle.Instances, bundle.Servers, provider, c.cfg.UnofficialWhatsAppWebhookBaseURL)
	bundle.ReconcileCapacity = uwuc.NewReconcileServerCapacityUseCase(
		bundle.Servers, bundle.Instances, provider)
	bundle.PurgeEvents = uwuc.NewPurgeProcessedEventsUseCase(bundle.ProcessedEvts, 30*24*time.Hour)

	bundle.Enabled = true
	log.Printf("[unofficial-whatsapp] channel enabled (webhook base=%s)",
		c.cfg.UnofficialWhatsAppWebhookBaseURL)
	log.Printf("[unofficial-whatsapp] webhook URL pattern: %s%s/{deliveryToken}",
		c.cfg.UnofficialWhatsAppWebhookBaseURL, uw.WebhookPathPrefix)
}

func (c *Container) seedUnofficialWhatsAppServer(bundle *unofficialWhatsAppBundle) {
	server, err := uwuc.NewEnsurePlatformServerUseCase(bundle.Servers).
		Execute(context.Background(), uwuc.PlatformServerInput{
			Name:       c.cfg.UnofficialWhatsAppServerName,
			BaseURL:    c.cfg.UnofficialWhatsAppServerURL,
			AdminToken: c.cfg.UnofficialWhatsAppAdminToken,
			Capacity:   c.cfg.UnofficialWhatsAppMaxSessions,
		})
	if err != nil {
		log.Fatalf("[unofficial-whatsapp] could not register the platform host: %v", err)
	}
	if server.Capacity <= 0 {
		log.Printf("[unofficial-whatsapp] host %s has no capacity configured "+
			"(UNOFFICIAL_WHATSAPP_MAX_SESSIONS); no number can be connected to it", server.BaseURL)
	}
}

func (c *Container) initUnofficialWhatsAppRuntime(history conversation_domain.MessageHistoryManager) {
	bundle := c.unofficialWhatsApp
	if bundle == nil || !bundle.Enabled {
		return
	}
	if c.useCases == nil {
		log.Fatalf("[unofficial-whatsapp] runtime wiring ran before useCases were built")
	}

	bundle.Entitlements.SetSource(c.useCases.getWorkspaceEntitlements)

	bundle.WebhookHandler = uwhttp.NewWebhookHandler(bundle.Instances, c.useCases.publishWebhook)

	handler := uwuc.NewHandleWebhookUseCase(uwuc.HandleWebhookDeps{
		Instances:     bundle.Instances,
		Servers:       bundle.Servers,
		Contacts:      bundle.Contacts,
		Conversations: bundle.Conversations,
		Groups:        bundle.Groups,
		Messaging:     bundle.Messaging,
		GroupAPI:      bundle.GroupAPI,
		Assets:        bundle.Assets,
		History:       history,
		Messages:      c.repositories.conversation,
		ConvMedia:     c.repositories.conversationMedia,
		FileStorage:   c.services.fileStorage,
		Broadcaster:   c.services.conversationHub,
		Assignments:   c.services.assignmentService,
		AIReply:       c.mustChannelAIReply(),
		Workflows:     c.useCases.triggerEvaluator,
		Leads:         uwrepo.NewLeadLinker(c.repositories.lead),
		Analysis:      conversation_usecase.NewAnalysisScheduler(c.redisProvider.SharedState()),
	})

	if c.unofficialWhatsAppCampaigns != nil && c.unofficialWhatsAppCampaigns.Enabled {
		handler.SetCampaignDeliverySink(
			uwcuc.NewDeliverySink(c.unofficialWhatsAppCampaigns.Entries))

		handler.SetCampaignAutomationSource(
			uwcuc.NewAutomationSource(
				bundle.Conversations,
				c.unofficialWhatsAppCampaigns.Campaigns))
	}

	bundle.SeedInboxPublisher = uwuc.NewSeedInboxPublisher(c.services.uwSeedQueuePub)
	bundle.ConsumeSeedInbox = uwuc.NewConsumeSeedInboxUseCase(
		c.services.uwSeedQueueSub,
		uwuc.NewSeedInboxUseCase(
			bundle.Instances,
			bundle.Contacts,
			bundle.Conversations,
			uwrepo.NewLeadLinker(c.repositories.lead),
			c.repositories.conversation,
			uwuc.NewConversationScripter(c.services.ai, c.cfg.OpenRouterDefaultModel),
			c.services.cachedBalanceChecker,
		).WithAttachments(c.repositories.media, c.repositories.conversationMedia),
	)
	if err := bundle.ConsumeSeedInbox.Start(); err != nil {
		log.Printf("[unofficial-whatsapp] inbox seed consumer failed to start: %v", err)
	}

	logChannelCapabilities("unofficial-whatsapp", map[string]bool{
		"ai-reply":             c.services.channelAIReply != nil,
		"workflows":            c.useCases.triggerEvaluator != nil,
		"assignment":           c.services.assignmentService != nil,
		"broadcaster":          c.services.conversationHub != nil,
		"campaigns":            c.unofficialWhatsAppCampaigns != nil && c.unofficialWhatsAppCampaigns.Enabled,
		"media":                c.services.fileStorage != nil,
		"avatars":              c.services.fileStorage != nil && bundle.Assets != nil,
		"groups":               bundle.Groups != nil && bundle.GroupAPI != nil,
		"inbox-seeding":        c.services.uwSeedQueuePub != nil && c.services.uwSeedQueueSub != nil,
		"inbox-seed-scripting": c.services.ai != nil,
		"entitlement-gate":     bundle.Entitlements.HasSource(),
		"department-scope":     c.services.conversationAuthImpl != nil,
	})

	bundle.Consume = uwuc.NewConsumeWebhookUseCase(
		c.services.webhookQueueSub,
		c.services.webhookQueuePub,
		c.redisProvider.SharedState(),
		bundle.ProcessedEvts,
		handler,
	)
}

func (c *Container) wireUnofficialWhatsAppConversationStack() {
	bundle := c.unofficialWhatsApp
	if bundle == nil || !bundle.Enabled {
		return
	}

	adapter := uwuc.NewChannelAdapter(
		bundle.Instances,
		bundle.Servers,
		bundle.Contacts,
		bundle.Conversations,
		bundle.Messaging,
	)
	if setter, ok := adapter.(interface{ SetVoiceTranscoder(uw.VoiceTranscoder) }); ok {
		setter.SetVoiceTranscoder(uazapi.NewVoiceTranscoder())
	}

	c.registerChannelAdapter(adapter)

	if setter, ok := c.services.conversationHistory.(interface {
		SetAutomationReader(shared.EntryType, func(context.Context, string) (*bool, error))
	}); ok {
		conversations := bundle.Conversations
		setter.SetAutomationReader(shared.EntryTypeUnofficialWhatsApp,
			func(ctx context.Context, entryID string) (*bool, error) {
				conv, err := conversations.FindByID(ctx, entryID)
				if err != nil {
					return nil, err
				}
				return conv.AutomationEnabled, nil
			})
	} else {
		log.Printf("[unofficial-whatsapp] history provider exposes no SetAutomationReader; " +
			"the automation toggle will read as always-on")
	}

	if c.services.conversationAutomation != nil {
		conversations := bundle.Conversations
		c.services.conversationAutomation.Register(
			shared.EntryTypeUnofficialWhatsApp,
			func(ctx context.Context, entryID string, enabled *bool) error {
				return conversations.SetAutomationEnabled(ctx, entryID, enabled)
			},
		)
	}

	if c.services.conversationAuthImpl != nil {
		c.services.conversationAuthImpl.SetEntryAccessRepo(
			shared.EntryTypeUnofficialWhatsApp, bundle.Conversations)
	}

	if c.services.conversationStatusService != nil {
		c.services.conversationStatusService.SetConversationStatusStore(
			shared.EntryTypeUnofficialWhatsApp,
			conversationStatusFuncs{
				status: bundle.Conversations.StatusForEntry,
				set:    bundle.Conversations.SetStatus,
			},
		)
		c.services.conversationStatusService.SetConversationCounter(
			shared.EntryTypeUnofficialWhatsApp, bundle.Conversations.CountByStatus)
	}

	if setter, ok := c.services.campaignWorkspaceResolver.(interface {
		SetEntryOwnerResolver(shared.EntryType, conversation_usecase.EntryOwnerResolver)
	}); ok {
		var _ conversation_usecase.EntryCampaignResolver = bundle.Conversations
		setter.SetEntryOwnerResolver(shared.EntryTypeUnofficialWhatsApp, bundle.Conversations)
	}

	if setter, ok := c.services.conversationHistory.(interface {
		SetContactIdentityLookup(shared.EntryType, conversation_usecase.ContactIdentityLookup)
	}); ok {
		setter.SetContactIdentityLookup(
			shared.EntryTypeUnofficialWhatsApp, unofficialWhatsAppContactIdentity(bundle))
	}
}

func unofficialWhatsAppContactIdentity(bundle *unofficialWhatsAppBundle) conversation_usecase.ContactIdentityLookup {
	contacts, conversations := bundle.Contacts, bundle.Conversations

	display := func(c *uw.Contact) conversation_usecase.ContactDisplay {
		leadID := ""
		if c.LeadID != nil {
			leadID = *c.LeadID
		}
		return conversation_usecase.ContactDisplay{
			ContactID:  c.ID,
			LeadID:     leadID,
			Ref:        c.JID,
			Handle:     c.Handle(),
			Name:       c.DisplayName(),
			PictureURL: c.PictureURL,
			IsGroup:    c.IsGroup,
		}
	}

	return contactIdentityFuncs{
		byIDs: func(ctx context.Context, ids []string) (map[string]conversation_usecase.ContactDisplay, error) {
			found, err := contacts.FindByIDs(ctx, ids)
			if err != nil {
				return nil, err
			}
			out := make(map[string]conversation_usecase.ContactDisplay, len(found))
			for _, contact := range found {
				if contact == nil {
					continue
				}
				d := display(contact)
				out[contact.ID] = d
				if d.LeadID != "" {
					out[d.LeadID] = d
				}
			}
			return out, nil
		},
		forConversation: func(ctx context.Context, conversationID string) (conversation_usecase.ContactDisplay, string, error) {
			conv, err := conversations.FindByID(ctx, conversationID)
			if err != nil {
				return conversation_usecase.ContactDisplay{}, "", err
			}
			contact, err := contacts.FindByID(ctx, conv.ContactID)
			if err != nil {
				return conversation_usecase.ContactDisplay{}, conv.WorkspaceID, err
			}
			return display(contact), conv.WorkspaceID, nil
		},
		authorsByHandle: func(ctx context.Context, entryID string, handles []string) (map[string]conversation_usecase.ContactDisplay, error) {
			conv, err := conversations.FindByID(ctx, entryID)
			if err != nil {
				return nil, err
			}
			if !conv.IsGroup {
				return nil, nil
			}
			found, err := contacts.FindByHandles(ctx, conv.InstanceID, handles)
			if err != nil {
				return nil, err
			}
			out := make(map[string]conversation_usecase.ContactDisplay, len(found))
			for _, contact := range found {
				if contact == nil {
					continue
				}
				out[contact.Handle()] = display(contact)
			}
			return out, nil
		},
	}
}

func unofficialWhatsAppHandler(c *Container) *uwhttp.Handler {
	if c.unofficialWhatsApp == nil || !c.unofficialWhatsApp.Enabled {
		return nil
	}
	return c.unofficialWhatsApp.Handler
}

func unofficialWhatsAppGroupHandler(c *Container) *uwhttp.GroupHandler {
	if c.unofficialWhatsApp == nil || !c.unofficialWhatsApp.Enabled {
		return nil
	}
	return c.unofficialWhatsApp.GroupHandler
}

func unofficialWhatsAppWebhookHandler(c *Container) *uwhttp.WebhookHandler {
	if c.unofficialWhatsApp == nil || !c.unofficialWhatsApp.Enabled {
		return nil
	}
	return c.unofficialWhatsApp.WebhookHandler
}
