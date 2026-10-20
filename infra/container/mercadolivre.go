package container

import (
	"context"
	"log"
	"strings"
	"time"

	deliveryHttp "vozko/delivery/http"
	mercadolivrehttp "vozko/delivery/http/mercadolivre"
	conversation_domain "vozko/domain/conversation"
	mldomain "vozko/domain/mercadolivre"
	"vozko/domain/shared"
	mlinfra "vozko/infra/mercadolivre"
	"vozko/infra/netguard"
	mercadolivre_repository "vozko/infra/repositories/mercadolivre"
	conversation_usecase "vozko/usecases/conversation"
	mluc "vozko/usecases/mercadolivre"
	"vozko/usecases/shared/automationrefs"
	"vozko/usecases/shared/imagerehost"
	"vozko/usecases/shared/oauthstate"
)

const (
	mercadoLivreStatePrefix  = "ml:oauth"
	mercadoLivreMediaTimeout = 60 * time.Second
)

type mercadoLivreBundle struct {
	Accounts      mldomain.AccountRepository
	Contacts      mldomain.ContactRepository
	Conversations mldomain.ConversationRepository
	Listings      mldomain.ListingRepository
	Services      mluc.Services
	Metrics       mluc.Metrics

	States *oauthstate.SealedIssuer

	Thumbnails *imagerehost.Store

	Tokens    *mluc.TokenSource
	Threads   *mluc.ThreadSync
	Intake    *mluc.AcceptNotification
	Publisher *mluc.JobPublisher
	Consume   *mluc.ConsumeNotifications

	RefreshTokens *mluc.RefreshTokensJob
	Health        *mluc.HealthCheck
	Reconcile     *mluc.Reconciler
	MissedFeeds   *mluc.MissedFeedsJob
	Purge         *mluc.PurgeJob

	Handler       *mercadolivrehttp.Handler
	Notifications *mercadolivrehttp.NotificationHandler
	Catalog       *mercadolivrehttp.CatalogHandler
}

func (c *Container) initMercadoLivre() {
	cfg := c.cfg
	if err := mldomain.ValidateAppID(cfg.MercadoLivreAppID); err != nil {
		log.Fatalf("[mercadolivre] MERCADOLIVRE_APP_ID: %v", err)
	}
	if strings.TrimSpace(cfg.MercadoLivreClientSecret) == "" {
		log.Fatalf("[mercadolivre] MERCADOLIVRE_CLIENT_SECRET is required")
	}
	if err := mldomain.ValidateRedirectURI(cfg.MercadoLivreRedirectURI); err != nil {
		log.Fatalf("[mercadolivre] MERCADOLIVRE_REDIRECT_URI: %v", err)
	}
	if err := mldomain.ValidateNotificationToken(cfg.MercadoLivreNotificationToken); err != nil {
		log.Fatalf("[mercadolivre] MERCADOLIVRE_NOTIFICATION_TOKEN: %v", err)
	}
	if c.redisProvider == nil || c.services == nil || c.services.metrics == nil {
		log.Fatalf("[mercadolivre] redis and the metrics service must exist before the Mercado Livre channel")
	}

	client, err := mlinfra.NewClient(mlinfra.Config{BaseURL: cfg.MercadoLivreAPIBaseURL, RateLimiterFactory: c.redisProvider.RateLimiterFactory()})
	if err != nil {
		log.Fatalf("[mercadolivre] MERCADOLIVRE_API_BASE_URL or client: %v", err)
	}
	oauth, err := mlinfra.NewOAuthService(client, mlinfra.OAuthConfig{
		AppID: cfg.MercadoLivreAppID, ClientSecret: cfg.MercadoLivreClientSecret, RedirectURI: cfg.MercadoLivreRedirectURI,
		AuthorizeURL: cfg.MercadoLivreAuthURL,
	})
	if err != nil {
		log.Fatalf("[mercadolivre] MERCADOLIVRE_AUTH_URL or oauth: %v", err)
	}
	if cfg.MercadoLivreAPIBaseURL != "" || cfg.MercadoLivreAuthURL != "" {
		log.Printf("[mercadolivre] provider endpoints overridden (api=%q authorize=%q); unset both in production", cfg.MercadoLivreAPIBaseURL, cfg.MercadoLivreAuthURL)
	}
	users := mustML(mlinfra.NewUserService(client))
	questions := mustML(mlinfra.NewQuestionService(client))
	messages := mustML(mlinfra.NewMessageService(client))
	attachments := mustML(mlinfra.NewAttachmentService(client))
	sales := mustML(mlinfra.NewSalesService(client))
	missed := mustML(mlinfra.NewMissedFeedService(client))
	items := mustML(mlinfra.NewItemService(client))
	guide := mustML(mlinfra.NewActionGuideService(client))

	nonces, err := oauthstate.NewNonceStore(c.redisProvider.SharedState(), mercadoLivreStatePrefix)
	if err != nil {
		log.Fatalf("[mercadolivre] nonce store: %v", err)
	}
	issuer, err := oauthstate.NewIssuer(cfg.MercadoLivreClientSecret, nonces, mluc.AccountsPath)
	if err != nil {
		log.Fatalf("[mercadolivre] state issuer: %v", err)
	}
	sealed, err := oauthstate.NewSealedIssuer(issuer, c.redisProvider.SharedState(), mercadoLivreStatePrefix)
	if err != nil {
		log.Fatalf("[mercadolivre] sealed state: %v", err)
	}

	c.mercadoLivre = &mercadoLivreBundle{
		Accounts:      mercadolivre_repository.NewAccountRepository(c.db),
		Contacts:      mercadolivre_repository.NewContactRepository(c.db),
		Conversations: mercadolivre_repository.NewConversationRepository(c.db),
		Services: mluc.Services{
			OAuth: oauth, Users: users, Questions: questions, Messages: messages, Attachments: attachments,
			Packs: sales, Orders: sales, Shipments: sales, MissedFeeds: missed, Items: items, ActionGuide: guide, Errors: mlinfra.Classifier{},
		},
		Metrics: c.services.metrics,
		States:  sealed,
	}
	log.Printf("[mercadolivre] channel enabled (redirect_uri=%q must match the DevCenter application exactly)", cfg.MercadoLivreRedirectURI)
}

func mustML[T any](service T, err error) T {
	if err != nil {
		log.Fatalf("[mercadolivre] %v", err)
	}
	return service
}

func (c *Container) initMercadoLivreRuntime(history conversation_domain.MessageHistoryManager) {
	b := c.mercadoLivre
	if b == nil {
		log.Fatalf("[mercadolivre] runtime wiring ran before the channel was built")
	}
	if c.useCases == nil || c.useCases.publishWebhook == nil || history == nil {
		log.Fatalf("[mercadolivre] the webhook publisher and the history manager must exist before the Mercado Livre runtime")
	}
	if c.services.assignmentService == nil || c.services.conversationHub == nil || c.services.webhookQueueSub == nil || c.services.webhookQueuePub == nil {
		log.Fatalf("[mercadolivre] assignments, the conversation hub and the webhook queue must exist before the Mercado Livre runtime")
	}
	watermarks, ok := c.repositories.conversation.(conversation_domain.DeliveryWatermarkRepository)
	if !ok {
		log.Fatalf("[mercadolivre] the message repository does not support delivery watermarks")
	}

	alerts := mluc.NewAccountAlerts(c.notifications().Publisher)
	b.Tokens = mluc.NewTokenSource(b.Accounts, b.Services.OAuth, b.Services.Errors, alerts, b.Metrics)
	publisher := mluc.NewJobPublisher(c.useCases.publishWebhook)
	b.Publisher = publisher
	b.Intake = mluc.NewAcceptNotification(c.cfg.MercadoLivreAppID, c.cfg.MercadoLivreNotificationToken, publisher, b.Metrics)

	c.buildMercadoLivreThumbnails(b)
	summaries := mluc.NewPackSummaries(mluc.PackSummariesDeps{
		Conversations: b.Conversations, Listings: b.Listings, Services: b.Services, Images: b.Thumbnails, Metrics: b.Metrics,
	})
	automation := conversation_usecase.NewInboundAutomation(
		c.useCases.triggerEvaluator,
		c.mustChannelAIReply(),
		nil,
		c.repositories.conversation,
	).WithDelegations(c.services.delegations)
	b.Threads = mluc.NewThreadSync(mluc.ThreadSyncDeps{
		Contacts:      b.Contacts,
		Conversations: b.Conversations,
		Services:      b.Services,
		Tokens:        b.Tokens,
		History:       history,
		Messages:      c.repositories.conversation,
		Media:         c.mediaStore(),
		Watermarks:    watermarks,
		Assignments:   c.services.assignmentService,
		Automation:    automation,
		Dispatches:    c.processedWebhookEvents(),
		Announcer:     c.services.conversationHub,
		PackSummaries: summaries,
		Metrics:       b.Metrics,
	})
	sales := mluc.NewSalesSync(b.Conversations, b.Services, b.Tokens, summaries, b.Threads)
	catalog := c.buildMercadoLivreCatalog(b, publisher, history)
	dispatcher := mluc.NewNotificationDispatcher(b.Accounts, b.Metrics, map[mldomain.JobKind]mluc.JobHandler{
		mldomain.JobFetchMessage:  b.Threads,
		mldomain.JobFetchThread:   b.Threads,
		mldomain.JobFetchOrder:    sales,
		mldomain.JobFetchShipment: sales,
		mldomain.JobRefreshPack:   sales,
		mldomain.JobFetchQuestion: catalog.questions,
		mldomain.JobFetchItem:     catalog.listings,
	})
	b.Consume = mluc.NewConsumeNotifications(mluc.ConsumeNotificationsDeps{
		QueueSub:    c.services.webhookQueueSub,
		QueuePub:    c.services.webhookQueuePub,
		SharedState: c.redisProvider.SharedState(),
		Durable:     c.processedWebhookEvents(),
		Dispatcher:  dispatcher,
		Errors:      b.Services.Errors,
		Metrics:     b.Metrics,
	})

	b.RefreshTokens = mluc.NewRefreshTokensJob(b.Accounts, b.Tokens)
	b.Health = mluc.NewHealthCheck(mluc.HealthCheckDeps{
		Accounts: b.Accounts, Contacts: b.Contacts, Services: b.Services, Tokens: b.Tokens, Alerts: alerts, Metrics: b.Metrics,
	})
	b.Reconcile = mluc.NewReconciler(b.Accounts, b.Tokens, b.Metrics,
		mluc.NewUnreadThreadsReconciler(b.Services.Messages, publisher),
		catalog.unanswered,
		catalog.listings,
	)
	b.MissedFeeds = mluc.NewMissedFeedsJob(b.Accounts, b.Tokens, b.Services.MissedFeeds, b.Intake)

	connect := mluc.NewConnectUseCase(mluc.ConnectDeps{
		Accounts: b.Accounts, Services: b.Services, States: b.States, PKCE: mlinfra.PKCE{}, Alerts: alerts, Metrics: b.Metrics,
	})
	accounts := mluc.NewAccounts(mluc.AccountsDeps{
		Accounts: b.Accounts,
		OAuth:    b.Services.OAuth,
		References: automationrefs.Finders{
			Agents:      c.repositories.agent,
			Workflows:   c.repositories.workflow,
			Pipelines:   c.repositories.pipeline,
			Departments: c.repositories.workspaceDepartment,
		},
		Alerts:  alerts,
		Metrics: b.Metrics,
	})
	b.Handler = mercadolivrehttp.NewHandler(mercadolivrehttp.HandlerDeps{
		Connect: connect, Accounts: accounts, Health: b.Health, FrontendBaseURL: c.cfg.FrontendBaseURL,
	})
	b.Notifications = mercadolivrehttp.NewNotificationHandler(b.Intake)
}

func (c *Container) startMercadoLivreConsumers() {
	if c.mercadoLivre == nil || c.mercadoLivre.Consume == nil {
		log.Fatalf("[mercadolivre] the notification consumers were not built")
	}
	if err := c.mercadoLivre.Consume.Start(); err != nil {
		log.Fatalf("[mercadolivre] failed to start the notification consumers: %v", err)
	}
	log.Printf("[mercadolivre] notification consumers started")
}

func (c *Container) wireMercadoLivreConversationStack() {
	b := c.mercadoLivre
	if b == nil || b.Threads == nil || b.Tokens == nil {
		log.Fatalf("[mercadolivre] the conversation stack needs the channel runtime")
	}
	if c.services.conversationAutomation == nil || c.services.conversationAuthImpl == nil || c.services.conversationStatusService == nil {
		log.Fatalf("[mercadolivre] conversation services must be built before the Mercado Livre conversation stack")
	}
	conversations := b.Conversations

	c.registerChannelAdapter(mluc.NewChannelAdapter(mluc.ChannelAdapterDeps{
		Accounts:      b.Accounts,
		Contacts:      b.Contacts,
		Conversations: conversations,
		Services:      b.Services,
		Tokens:        b.Tokens,
		Threads:       b.Threads,
		Refresh:       b.Publisher,
		Media:         mlinfra.NewMediaFetcher(netguard.NewHTTPClient(mercadoLivreMediaTimeout)),
		Metrics:       b.Metrics,
	}))

	automationReader, ok := c.services.conversationHistory.(interface {
		SetAutomationReader(shared.EntryType, func(context.Context, string) (*bool, error))
	})
	if !ok {
		log.Fatalf("[mercadolivre] history provider exposes no SetAutomationReader")
	}
	automationReader.SetAutomationReader(shared.EntryTypeMercadoLivre, func(ctx context.Context, entryID string) (*bool, error) {
		conv, err := conversations.FindByID(ctx, entryID)
		if err != nil {
			return nil, err
		}
		return conv.AutomationEnabled, nil
	})
	c.services.conversationAutomation.Register(shared.EntryTypeMercadoLivre, conversations.SetAutomationEnabled)
	c.services.conversationAuthImpl.SetEntryAccessRepo(shared.EntryTypeMercadoLivre, conversations)
	c.services.conversationStatusService.SetConversationStatusStore(shared.EntryTypeMercadoLivre,
		conversationStatusFuncs{status: conversations.StatusForEntry, set: conversations.SetStatus})
	c.services.conversationStatusService.SetConversationCounter(shared.EntryTypeMercadoLivre, conversations.CountByStatus)

	owners, ok := c.services.campaignWorkspaceResolver.(interface {
		SetEntryOwnerResolver(shared.EntryType, conversation_usecase.EntryOwnerResolver)
	})
	if !ok {
		log.Fatalf("[mercadolivre] workspace resolver exposes no SetEntryOwnerResolver")
	}
	owners.SetEntryOwnerResolver(shared.EntryTypeMercadoLivre, conversations)

	identities, ok := c.services.conversationHistory.(interface {
		SetContactIdentityLookup(shared.EntryType, conversation_usecase.ContactIdentityLookup)
	})
	if !ok {
		log.Fatalf("[mercadolivre] history provider exposes no SetContactIdentityLookup")
	}
	identities.SetContactIdentityLookup(shared.EntryTypeMercadoLivre, mercadoLivreContactIdentity(b))
}

func mercadoLivreContactIdentity(b *mercadoLivreBundle) conversation_usecase.ContactIdentityLookup {
	contacts, conversations := b.Contacts, b.Conversations
	display := func(ct *mldomain.Contact) conversation_usecase.ContactDisplay {
		out := conversation_usecase.ContactDisplay{
			ContactID: ct.ID,
			Ref:       ct.MLUserID,
			Handle:    ct.Nickname,
			Name:      ct.DisplayName(),
		}
		if ct.LeadID != nil {
			out.LeadID = *ct.LeadID
		}
		return out
	}
	return contactIdentityFuncs{
		byIDs: func(ctx context.Context, ids []string) (map[string]conversation_usecase.ContactDisplay, error) {
			found, err := contacts.FindByIDs(ctx, ids)
			if err != nil {
				return nil, err
			}
			out := make(map[string]conversation_usecase.ContactDisplay, len(found))
			for _, ct := range found {
				out[ct.ID] = display(ct)
			}
			return out, nil
		},
		forConversation: func(ctx context.Context, conversationID string) (conversation_usecase.ContactDisplay, string, error) {
			conv, err := conversations.FindByID(ctx, conversationID)
			if err != nil {
				return conversation_usecase.ContactDisplay{}, "", err
			}
			ct, err := contacts.FindByID(ctx, conv.ContactID)
			if err != nil {
				return conversation_usecase.ContactDisplay{}, conv.WorkspaceID, err
			}
			return display(ct), conv.WorkspaceID, nil
		},
	}
}

func mercadoLivreRoutes(c *Container) deliveryHttp.MercadoLivreRoutes {
	if c.mercadoLivre == nil || c.mercadoLivre.Handler == nil || c.mercadoLivre.Notifications == nil {
		log.Fatalf("[mercadolivre] routes requested before the channel runtime was built")
	}
	if c.mercadoLivre.Catalog == nil {
		log.Fatalf("[mercadolivre] routes requested before the listings and questions were built")
	}
	return deliveryHttp.MercadoLivreRoutes{Handler: c.mercadoLivre.Handler, Notifications: c.mercadoLivre.Notifications, Catalog: c.mercadoLivre.Catalog}
}
