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
)

// unofficialWhatsAppBundle groups everything the channel needs, so it can be
// wired (or skipped) as one unit instead of threading a dozen fields through the
// container's god-structs. Same self-contained shape as the other two channels.
type unofficialWhatsAppBundle struct {
	// Enabled is false when the channel is switched off or misconfigured. Every
	// consumer checks it before using the bundle.
	Enabled bool

	Servers       uw.ServerRepository
	Instances     uw.InstanceRepository
	Contacts      uw.ContactRepository
	Conversations uw.ConversationRepository
	Groups        uw.GroupRepository

	Provider uw.ProviderAPI
	// Messaging is the send/media surface. Satisfied by the same client as
	// Provider, but held separately so the halves stay independently
	// substitutable — the health cron has no business sending.
	Messaging uw.MessagingAPI
	// GroupAPI is the group management surface, held separately for the same
	// reason and a sharper one: these calls evict people from the customer's own
	// WhatsApp groups, and nothing that only needs to send should be able to
	// reach them.
	GroupAPI uw.GroupAPI
	// Assets downloads provider-hosted profile pictures so they can be re-hosted
	// on our storage instead of linked.
	Assets uw.RemoteAssetFetcher

	// Entitlements answers how many numbers this workspace may connect.
	//
	// The concrete type, not the interface, because its source is attached in the
	// runtime pass and the boot log asserts that it was.
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
}

// initUnofficialWhatsApp builds the channel.
//
// The webhook base URL is required and validated here rather than at first use.
// That matters more than usual: the provider does not sign webhook bodies, so
// the delivery token in that URL is the channel's only authenticity control, and
// a base URL that is http, or carries a path, produces silence rather than an
// error.
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

	// The number allowance: a per-workspace grant a platform administrator sets,
	// plus whatever addons the workspace bought.
	//
	// Created here so the provisioning use case and the HTTP handler can hold it,
	// but its SOURCE is attached in the runtime pass — this function runs before
	// initUseCases, so the billing stack does not exist yet. Until then the
	// reader answers zero, which refuses provisioning rather than permitting it.
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
		StartConv: uwuc.NewStartConversationUseCase(
			bundle.Instances, bundle.Servers, bundle.Contacts, bundle.Conversations,
			bundle.Messaging, uwrepo.NewLeadLinker(c.repositories.lead)),
		Allowance: uwuc.NewGetAllowanceUseCase(bundle.Entitlements),
	})

	// The group panel. Wired here rather than in the runtime pass because it
	// depends on nothing from the conversation stack: a group's roster and admin
	// rules are the provider's state, not the CRM's.
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

	// Built with a nil publisher: the queue only exists once the usecases are
	// assembled, so the handler is rebuilt with both halves in the runtime pass.
	bundle.WebhookHandler = uwhttp.NewWebhookHandler(bundle.Instances, nil)

	bundle.CheckHealth = uwuc.NewCheckInstanceHealthUseCase(
		bundle.Instances, bundle.Servers, provider, c.cfg.UnofficialWhatsAppWebhookBaseURL)
	bundle.ReconcileCapacity = uwuc.NewReconcileServerCapacityUseCase(
		bundle.Servers, bundle.Instances, provider)
	bundle.PurgeEvents = uwuc.NewPurgeProcessedEventsUseCase(bundle.ProcessedEvts, 30*24*time.Hour)

	bundle.Enabled = true
	log.Printf("[unofficial-whatsapp] channel enabled (webhook base=%s)",
		c.cfg.UnofficialWhatsAppWebhookBaseURL)
	// Printed because a mismatch between this and what the provider was told is
	// the failure mode that produces silence rather than an error. The token
	// itself is never logged: it is the credential.
	log.Printf("[unofficial-whatsapp] webhook URL pattern: %s%s/{deliveryToken}",
		c.cfg.UnofficialWhatsAppWebhookBaseURL, uw.WebhookPathPrefix)
}

// seedUnofficialWhatsAppServer registers the configured platform host.
//
// The settings are required by config, so there is no absent-configuration
// branch here: boot has already failed with the missing variable's name by the
// time this runs. That is the same contract the other channels hold, and it
// trades a deployment that starts and then fails at the first customer's
// connect click for one that never starts.
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
		// Zero capacity means "unknown" and is treated as full, so placement
		// would refuse every connect. Said out loud because the symptom is a
		// connect button that always answers "no capacity".
		log.Printf("[unofficial-whatsapp] host %s has no capacity configured "+
			"(UNOFFICIAL_WHATSAPP_MAX_SESSIONS); no number can be connected to it", server.BaseURL)
	}
}

// initUnofficialWhatsAppRuntime wires the parts that depend on the conversation
// stack.
//
// Split from initUnofficialWhatsApp because the shared history manager is a
// local inside initUseCases rather than a container field, so it is passed in
// rather than reached for.
func (c *Container) initUnofficialWhatsAppRuntime(history conversation_domain.MessageHistoryManager) {
	bundle := c.unofficialWhatsApp
	if bundle == nil || !bundle.Enabled {
		return
	}
	if c.useCases == nil {
		log.Fatalf("[unofficial-whatsapp] runtime wiring ran before useCases were built")
	}

	// The entitlement source, now that the billing use cases exist.
	//
	// Without this the reader answers zero for every workspace and nobody can
	// connect a number, however many a platform administrator granted them. It
	// fails closed rather than open, which is the right direction, but it is
	// still broken — hence the capability line below.
	bundle.Entitlements.SetSource(c.useCases.getWorkspaceEntitlements)

	// Rebuilt now that the publisher exists. Without this the ingest endpoint
	// would accept events and drop them, which looks exactly like a channel
	// nobody is messaging.
	bundle.WebhookHandler = uwhttp.NewWebhookHandler(bundle.Instances, c.useCases.publishWebhook)

	// The dispatcher reuses the SHARED history manager, so this channel gets the
	// same persistence, dedup and websocket fan-out as every other rather than a
	// parallel implementation.
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

	// Every optional capability, named at boot.
	//
	// All of these are guarded with `!= nil` at the call site, so a missing one
	// degrades silently: no AI reply, no workflow trigger, no assignment. That is
	// the right runtime behaviour and the wrong thing to discover from a customer,
	// and it has already cost this channel twice — once with the conversation
	// authorizer, once with the AI service. One line at boot makes the difference
	// between "wired" and "silently inert" readable.
	logChannelCapabilities("unofficial-whatsapp", map[string]bool{
		"ai-reply":    c.services.channelAIReply != nil,
		"workflows":   c.useCases.triggerEvaluator != nil,
		"assignment":  c.services.assignmentService != nil,
		"broadcaster": c.services.conversationHub != nil,
		"media":       c.services.fileStorage != nil,
		// Named for the same reason as the rest: without storage or a fetcher
		// the channel still works, it just renders initials where every other
		// channel renders a face, and that is not something to learn from a
		// customer.
		"avatars": c.services.fileStorage != nil && bundle.Assets != nil,
		"groups":  bundle.Groups != nil && bundle.GroupAPI != nil,
		// Named because its absence does not degrade — it OPENS. An unwired
		// entitlement reader means provisioning is not gated at all, and slots on
		// hosts we pay for get handed out with nothing recording that they were
		// never authorised.
		"entitlement-gate": bundle.Entitlements.HasSource(),
	})

	bundle.Consume = uwuc.NewConsumeWebhookUseCase(
		c.services.webhookQueueSub,
		c.services.webhookQueuePub,
		c.redisProvider.SharedState(),
		bundle.ProcessedEvts,
		handler,
	)
}

// wireUnofficialWhatsAppConversationStack registers the channel with the shared
// conversation services.
//
// Each of these is a per-channel lookup the conversation stack keys on
// (entry_id, entry_type) and cannot resolve generically. Registering them here,
// rather than adding a `case` inside each of those files, is what keeps the
// channel additive.
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
	// Audio is re-encoded to ogg/opus on the way out: WhatsApp voice notes are
	// opus and the CRM recorder emits WAV, so without this every voice note is
	// refused as an unaccepted MIME type.
	if setter, ok := adapter.(interface{ SetVoiceTranscoder(uw.VoiceTranscoder) }); ok {
		setter.SetVoiceTranscoder(uazapi.NewVoiceTranscoder())
	}

	// registerChannelAdapter, never SetChannelAdapters: adapters accumulate, and
	// replacing the registry would silently disable Instagram's and Telegram's
	// send paths — a failure that reads downstream as "those channels cannot
	// send" rather than as a wiring bug.
	c.registerChannelAdapter(adapter)

	// The per-conversation automation override, read and write. Without the
	// reader the header reports automation as running even after an operator
	// paused it; without the writer the toggle has no setter and the service
	// refuses it by name.
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
		// Never silently: a missing reader is indistinguishable from
		// "automation is on" at the UI, which is the exact bug this fixes.
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

	// The websocket authorizer's ownership check. Without it, opening a
	// conversation on this channel is refused for every operator, because the
	// authorizer has no reader for the entry type and fails closed.
	//
	// The KEYED setter, never a per-channel one: the Instagram- and
	// Telegram-shaped setters still exist as deprecated aliases onto the same
	// map, and a third channel reaching for that shape is how the map ends up
	// with one hand-written accessor per channel again.
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
		setter.SetEntryOwnerResolver(shared.EntryTypeUnofficialWhatsApp, bundle.Conversations)
	}

	if setter, ok := c.services.conversationHistory.(interface {
		SetContactIdentityLookup(shared.EntryType, conversation_usecase.ContactIdentityLookup)
	}); ok {
		setter.SetContactIdentityLookup(
			shared.EntryTypeUnofficialWhatsApp, unofficialWhatsAppContactIdentity(bundle))
	}
}

// unofficialWhatsAppContactIdentity adapts the channel's repositories onto the
// conversation usecase's sender-identity port.
//
// Registered even though this channel's contacts ARE leads, unlike Instagram's
// and Telegram's: a contact is only linked to a lead once one resolves, and
// between the first inbound message and that resolution the inbox would
// otherwise render a bare JID where a name belongs.
func unofficialWhatsAppContactIdentity(bundle *unofficialWhatsAppBundle) conversation_usecase.ContactIdentityLookup {
	contacts, conversations := bundle.Contacts, bundle.Conversations

	display := func(c *uw.Contact) conversation_usecase.ContactDisplay {
		return conversation_usecase.ContactDisplay{
			ContactID: c.ID,
			// Message rows carry the JID as the sender, so that is what the
			// hydration compares against when deciding whether a label is a raw
			// provider id leaking into the UI.
			Ref:        c.JID,
			Handle:     c.Handle(),
			Name:       c.DisplayName(),
			PictureURL: c.PictureURL,
			// The one channel that has groups, so far. Carried on the identity
			// lookup rather than through the inbox SQL registry, so declaring it
			// costs this adapter one line and the other channels nothing.
			IsGroup: c.IsGroup,
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
				out[contact.ID] = display(contact)
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
	}
}

// unofficialWhatsAppHandler returns the channel's HTTP handler, or nil when the
// channel is disabled; the router treats nil as "register no routes".
func unofficialWhatsAppHandler(c *Container) *uwhttp.Handler {
	if c.unofficialWhatsApp == nil || !c.unofficialWhatsApp.Enabled {
		return nil
	}
	return c.unofficialWhatsApp.Handler
}

// unofficialWhatsAppGroupHandler returns the group panel's handler, or nil when
// the channel is disabled.
func unofficialWhatsAppGroupHandler(c *Container) *uwhttp.GroupHandler {
	if c.unofficialWhatsApp == nil || !c.unofficialWhatsApp.Enabled {
		return nil
	}
	return c.unofficialWhatsApp.GroupHandler
}

// unofficialWhatsAppWebhookHandler returns the public ingest handler, or nil
// when the channel is disabled.
func unofficialWhatsAppWebhookHandler(c *Container) *uwhttp.WebhookHandler {
	if c.unofficialWhatsApp == nil || !c.unofficialWhatsApp.Enabled {
		return nil
	}
	return c.unofficialWhatsApp.WebhookHandler
}
