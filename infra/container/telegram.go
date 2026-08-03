package container

import (
	"context"
	"log"
	"strconv"
	"time"

	telegramhttp "vozko/delivery/http/telegram"
	conversation_domain "vozko/domain/conversation"
	"vozko/domain/shared"
	tgdomain "vozko/domain/telegram"
	telegram_repository "vozko/infra/repositories/telegram"
	tginfra "vozko/infra/telegram"
	conversation_usecase "vozko/usecases/conversation"
	tguc "vozko/usecases/telegram"
)

// telegramBundle groups everything the Telegram channel needs, so the channel can
// be wired (or skipped) as one unit instead of threading a dozen fields through
// the container's god-structs. Same self-contained shape as instagramBundle.
type telegramBundle struct {
	// Enabled is false when the channel is switched off or misconfigured. Every
	// consumer checks it before using the bundle.
	Enabled bool

	Accounts      tgdomain.AccountRepository
	Contacts      tgdomain.ContactRepository
	Conversations tgdomain.ConversationRepository
	DeepLinks     tgdomain.DeepLinkRepository
	Files         tgdomain.FileCacheRepository
	ProcessedEvts tgdomain.ProcessedEventRepository

	API tgdomain.BotAPI

	Handler        *telegramhttp.Handler
	WebhookHandler *telegramhttp.WebhookHandler

	Consume     *tguc.ConsumeWebhookUseCase
	CheckHealth *tguc.CheckWebhookHealthUseCase
	PurgeEvents *tguc.PurgeProcessedEventsUseCase
}

// initTelegram builds the Telegram channel.
//
// The configuration is required (LoadConfig fails fast without it), so a
// misconfigured webhook base URL is a genuine boot failure rather than a reason
// to silently serve 404s on every Telegram route, the same rule Instagram
// follows.
//
// Validating the URL here matters more than usual: Telegram's constraints on it
// fail SILENTLY. A wrong scheme or an unsupported port produces no error at
// registration time, only messages that never arrive.
func (c *Container) initTelegram() {
	bundle := &telegramBundle{}
	c.telegram = bundle

	if err := tgdomain.ValidateWebhookBaseURL(c.cfg.TelegramWebhookBaseURL); err != nil {
		log.Fatalf("[telegram] %v", err)
	}

	// The client is decorated with Telegram's two published send budgets, one
	// message per second per chat, ~30 per second per bot, through the shared
	// Redis limiter, so they hold across replicas rather than per process.
	api := tginfra.NewThrottled(
		tginfra.NewClient(tginfra.Config{BaseURL: c.cfg.TelegramBotAPIBaseURL}),
		c.redisProvider.RateLimiterFactory(),
	)
	bundle.API = api

	bundle.Accounts = telegram_repository.NewAccountRepository(c.db)
	bundle.Contacts = telegram_repository.NewContactRepository(c.db)
	bundle.Conversations = telegram_repository.NewConversationRepository(c.db)
	bundle.DeepLinks = telegram_repository.NewDeepLinkRepository(c.db)
	bundle.Files = telegram_repository.NewFileCacheRepository(c.db)
	bundle.ProcessedEvts = telegram_repository.NewProcessedEventRepository(c.db)

	bundle.Handler = telegramhttp.NewHandler(telegramhttp.HandlerDeps{
		Connect:    tguc.NewConnectAccountUseCase(bundle.Accounts, api, c.cfg.TelegramWebhookBaseURL),
		Reregister: tguc.NewReregisterWebhookUseCase(bundle.Accounts, api, c.cfg.TelegramWebhookBaseURL),
		List:       tguc.NewListAccountsUseCase(bundle.Accounts),
		Get:        tguc.NewGetAccountUseCase(bundle.Accounts),
		UpdateCfg:  tguc.NewUpdateAccountConfigUseCase(bundle.Accounts),
		Disconnect: tguc.NewDisconnectAccountUseCase(bundle.Accounts, api),
		CreateLink: tguc.NewCreateDeepLinkUseCase(bundle.Accounts, bundle.DeepLinks),
		ListLinks:  tguc.NewListDeepLinksUseCase(bundle.Accounts, bundle.DeepLinks),
		DeleteLink: tguc.NewDeleteDeepLinkUseCase(bundle.Accounts, bundle.DeepLinks),
	})
	bundle.WebhookHandler = telegramhttp.NewWebhookHandler(bundle.Accounts, nil)

	bundle.CheckHealth = tguc.NewCheckWebhookHealthUseCase(bundle.Accounts, api)
	bundle.PurgeEvents = tguc.NewPurgeProcessedEventsUseCase(bundle.ProcessedEvts, 30*24*time.Hour)

	bundle.Enabled = true
	log.Printf("[telegram] channel enabled (webhook base=%s)", c.cfg.TelegramWebhookBaseURL)
	// Printed because a mismatch between this and what Telegram was told is the
	// failure mode that produces silence rather than an error.
	log.Printf("[telegram] webhook URL pattern: %s%s/{accountId}",
		c.cfg.TelegramWebhookBaseURL, tgdomain.WebhookPathPrefix)
}

// initTelegramRuntime wires the parts that depend on the conversation stack.
//
// The history manager is a local inside initUseCases rather than a container
// field, so it is passed in instead of reached for.
func (c *Container) initTelegramRuntime(history conversation_domain.MessageHistoryManager) {
	bundle := c.telegram
	if bundle == nil || !bundle.Enabled {
		return
	}
	if c.useCases == nil {
		log.Fatalf("[telegram] runtime wiring ran before useCases were built")
	}

	// The webhook handler needs the publisher, which only exists once the
	// usecases are built, so it is rebuilt here with both halves.
	bundle.WebhookHandler = telegramhttp.NewWebhookHandler(bundle.Accounts, c.useCases.publishWebhook)

	// The dispatcher reuses the SHARED history manager, so Telegram gets the same
	// persistence, dedup and websocket fan-out as every other channel rather than
	// a parallel implementation.
	handler := tguc.NewHandleWebhookUseCase(tguc.HandleWebhookDeps{
		Accounts:      bundle.Accounts,
		Contacts:      bundle.Contacts,
		Conversations: bundle.Conversations,
		DeepLinks:     bundle.DeepLinks,
		API:           bundle.API,
		History:       history,
		Messages:      c.repositories.conversation,
		ConvMedia:     c.repositories.conversationMedia,
		FileStorage:   c.services.fileStorage,
		Broadcaster:   c.services.conversationHub,
		Assignments:   c.services.assignmentService,
		AIReply:       c.services.channelAIReply,
		Workflows:     c.useCases.triggerEvaluator,
		Leads:         telegram_repository.NewLeadLinker(c.repositories.lead),
		Analysis:      conversation_usecase.NewAnalysisScheduler(c.redisProvider.SharedState()),
	})

	bundle.Consume = tguc.NewConsumeWebhookUseCase(
		c.services.webhookQueueSub,
		c.services.webhookQueuePub,
		c.redisProvider.SharedState(),
		bundle.ProcessedEvts,
		handler,
	)
}

// wireTelegramConversationStack registers the Telegram channel with the shared
// conversation services.
//
// Each of these is a per-channel lookup the conversation stack keys on
// (entry_id, entry_type) and therefore cannot resolve generically: the send
// adapter, the WS authorizer's ownership check, the workspace/department
// resolver, the conversation-status writer and the sender-identity lookup.
// Registering them here, rather than adding another `case "telegram"` inside
// each of those files, is what keeps the channel additive.
func (c *Container) wireTelegramConversationStack() {
	bundle := c.telegram
	if bundle == nil || !bundle.Enabled {
		return
	}

	adapter := tguc.NewChannelAdapter(
		bundle.Accounts,
		bundle.Contacts,
		bundle.Conversations,
		bundle.Files,
		bundle.API,
	)

	// Use registerChannelAdapter, never SetChannelAdapters: adapters accumulate,
	// and replacing the registry would silently disable Instagram's send path.
	c.registerChannelAdapter(adapter)

	// The per-conversation automation override. Without this the toggle has no
	// setter for Telegram and the service refuses it by name rather than
	// silently doing nothing.
	// The matching READER. GetEntryInfo returned a hard true for every
	// adapter-backed channel, so the header reported automation as running even
	// after it had been paused.
	if setter, ok := c.services.conversationHistory.(interface {
		SetAutomationReader(shared.EntryType, func(context.Context, string) (*bool, error))
	}); ok {
		conversations := bundle.Conversations
		setter.SetAutomationReader(shared.EntryTypeTelegram, func(ctx context.Context, entryID string) (*bool, error) {
			conv, err := conversations.FindByID(ctx, entryID)
			if err != nil {
				return nil, err
			}
			return conv.AutomationEnabled, nil
		})
	} else {
		// Never silently. A missing reader is indistinguishable from "automation
		// is on" at the UI, which is the exact bug this registration fixes.
		log.Printf("[telegram] history provider exposes no SetAutomationReader; the toggle will read as always-on")
	}

	if c.services.conversationAutomation != nil {
		conversations := bundle.Conversations
		c.services.conversationAutomation.Register(
			shared.EntryTypeTelegram,
			func(ctx context.Context, entryID string, enabled *bool) error {
				return conversations.SetAutomationEnabled(ctx, entryID, enabled)
			},
		)
	}

	if c.services.conversationAuthImpl != nil {
		c.services.conversationAuthImpl.SetTelegramEntryRepo(bundle.Conversations)
	}
	if c.services.conversationStatusService != nil {
		c.services.conversationStatusService.SetConversationStatusStore(
			shared.EntryTypeTelegram,
			conversationStatusFuncs{
				status: bundle.Conversations.StatusForEntry,
				set:    bundle.Conversations.SetStatus,
			},
		)
		c.services.conversationStatusService.SetConversationCounter(
			shared.EntryTypeTelegram, bundle.Conversations.CountByStatus)
	}
	if setter, ok := c.services.campaignWorkspaceResolver.(interface {
		SetEntryOwnerResolver(shared.EntryType, conversation_usecase.EntryOwnerResolver)
	}); ok {
		setter.SetEntryOwnerResolver(shared.EntryTypeTelegram, bundle.Conversations)
	}
	if setter, ok := c.services.conversationHistory.(interface {
		SetContactIdentityLookup(shared.EntryType, conversation_usecase.ContactIdentityLookup)
	}); ok {
		setter.SetContactIdentityLookup(shared.EntryTypeTelegram, telegramContactIdentity(bundle))
	}
}

// telegramContactIdentity adapts the Telegram repositories onto the conversation
// usecase's sender-identity port, so the CRM can label a Telegram DM without the
// conversation package importing the Telegram domain.
func telegramContactIdentity(bundle *telegramBundle) conversation_usecase.ContactIdentityLookup {
	contacts, conversations := bundle.Contacts, bundle.Conversations

	display := func(c *tgdomain.Contact) conversation_usecase.ContactDisplay {
		return conversation_usecase.ContactDisplay{
			ContactID: c.ID,
			// The message rows carry the numeric user id as the sender, so that is
			// what the hydration compares against when deciding whether a label is
			// a raw provider id leaking into the UI.
			Ref:        strconv.FormatInt(c.TGUserID, 10),
			Handle:     c.Handle(),
			Name:       c.DisplayName(),
			PictureURL: c.PhotoURL,
		}
	}

	return contactIdentityFuncs{
		byIDs: func(ctx context.Context, ids []string) (map[string]conversation_usecase.ContactDisplay, error) {
			found, err := contacts.FindByIDs(ctx, ids)
			if err != nil {
				return nil, err
			}
			out := make(map[string]conversation_usecase.ContactDisplay, len(found))
			for _, c := range found {
				if c == nil {
					continue
				}
				out[c.ID] = display(c)
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

// telegramHandler returns the channel's HTTP handler, or nil when the channel is
// disabled, the router treats nil as "register no routes".
func telegramHandler(c *Container) *telegramhttp.Handler {
	if c.telegram == nil || !c.telegram.Enabled {
		return nil
	}
	return c.telegram.Handler
}

// telegramWebhookHandler returns the webhook handler, or nil when disabled.
func telegramWebhookHandler(c *Container) *telegramhttp.WebhookHandler {
	if c.telegram == nil || !c.telegram.Enabled {
		return nil
	}
	return c.telegram.WebhookHandler
}
