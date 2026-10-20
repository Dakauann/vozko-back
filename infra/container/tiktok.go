package container

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	deliveryHttp "vozko/delivery/http"
	tiktokhttp "vozko/delivery/http/tiktok"
	ca "vozko/domain/audience"
	conversation_domain "vozko/domain/conversation"
	entrypurge_domain "vozko/domain/entrypurge"
	export_domain "vozko/domain/export"
	"vozko/domain/readiness"
	"vozko/domain/shared"
	ttdomain "vozko/domain/tiktok"
	"vozko/domain/workspace"
	"vozko/infra/netguard"
	"vozko/infra/remotefile"
	ca_repository "vozko/infra/repositories/audience"
	commentautomation_repository "vozko/infra/repositories/commentautomation"
	entrypurge_repository "vozko/infra/repositories/entrypurge"
	tiktok_repository "vozko/infra/repositories/tiktok"
	infratiktok "vozko/infra/tiktok"
	cauc "vozko/usecases/audience"
	conversation_usecase "vozko/usecases/conversation"
	entrypurge_usecase "vozko/usecases/entrypurge"
	readiness_usecase "vozko/usecases/readiness"
	"vozko/usecases/shared/automationrefs"
	"vozko/usecases/shared/oauthstate"
	ttuc "vozko/usecases/tiktok"
)

const (
	tiktokStatePrefix         = "tt:oauth"
	tiktokMediaTimeout        = 30 * time.Second
	tiktokMediaDownload       = 2 * time.Minute
	tiktokMediaMaxBytes       = 10 << 20
	tiktokConsumerConcurrency = 8
)

type tiktokBundle struct {
	Repos    ttuc.Repositories
	Services ttuc.Services
	Metrics  ttuc.Metrics

	States   *oauthstate.Issuer
	Alerts   ttuc.AccountAlerts
	Tokens   *ttuc.TokenSource
	Fetcher  ttuc.MediaFetcher
	Images   *ttuc.ImageStore
	Side     *ttuc.SideMessages
	Webhooks *ttuc.WebhookSubscriptions
	Comments *ttuc.Comments

	Consume       *ttuc.Consumers
	RefreshTokens *ttuc.RefreshTokensJob
	Health        *ttuc.HealthCheckJob
	Purge         *ttuc.PurgeJob
	CommentWorker *ttuc.CommentWorker

	Handler *tiktokhttp.Handler
	Webhook *tiktokhttp.WebhookHandler
}

func (c *Container) initTikTok() {
	cfg := c.cfg
	if err := requirePath(cfg.TikTokRedirectURI, tiktokhttp.OAuthCallbackPath); err != nil {
		log.Fatalf("[tiktok] TIKTOK_REDIRECT_URI: %v", err)
	}
	if err := ttuc.ValidateCallbackURL(cfg.TikTokWebhookURL); err != nil {
		log.Fatalf("[tiktok] TIKTOK_WEBHOOK_URL: %v", err)
	}
	if err := requirePath(cfg.TikTokWebhookURL, tiktokhttp.WebhookPath); err != nil {
		log.Fatalf("[tiktok] TIKTOK_WEBHOOK_URL: %v", err)
	}
	if c.redisProvider == nil || c.services == nil || c.services.metrics == nil {
		log.Fatalf("[tiktok] redis and the metrics service must exist before the TikTok channel")
	}
	objects, ok := c.services.fileStorage.(ttuc.ObjectStore)
	if !ok {
		log.Fatalf("[tiktok] the file storage cannot delete files, so avatars and thumbnails could not be purged")
	}

	client, err := infratiktok.NewClient(infratiktok.Config{
		BaseURL:            cfg.TikTokAPIBaseURL,
		AppID:              cfg.TikTokAppID,
		AppSecret:          cfg.TikTokAppSecret,
		RedirectURI:        cfg.TikTokRedirectURI,
		AuthorizeURL:       cfg.TikTokAuthorizeURL,
		MediaHTTPClient:    netguard.NewHTTPClient(tiktokMediaDownload),
		RateLimiterFactory: c.redisProvider.RateLimiterFactory(),
	})
	if err != nil {
		log.Fatalf("[tiktok] client: %v", err)
	}
	nonces, err := oauthstate.NewNonceStore(c.redisProvider.SharedState(), tiktokStatePrefix)
	if err != nil {
		log.Fatalf("[tiktok] nonce store: %v", err)
	}
	issuer, err := oauthstate.NewIssuer(cfg.TikTokAppSecret, nonces, ttuc.AccountsPath)
	if err != nil {
		log.Fatalf("[tiktok] state issuer: %v", err)
	}

	b := &tiktokBundle{
		Repos: ttuc.Repositories{
			Accounts:      tiktok_repository.NewAccountRepository(c.db),
			Contacts:      tiktok_repository.NewContactRepository(c.db),
			Conversations: tiktok_repository.NewConversationRepository(c.db),
			Videos:        tiktok_repository.NewVideoRepository(c.db),
			Comments:      tiktok_repository.NewCommentRepository(c.db),
			Purge:         tiktok_repository.NewPurgeRepository(c.db),
		},
		Services: ttuc.Services{
			OAuth: client, Profiles: client, Messaging: client, Videos: client, Comments: client, Webhooks: client,
			Errors: infratiktok.Classifier{},
		},
		Metrics: c.services.metrics,
		States:  issuer,
		Alerts:  ttuc.NewAccountAlerts(c.notifications().Publisher),
		Fetcher: remotefile.NewFetcher(netguard.NewHTTPClient(tiktokMediaTimeout), tiktokMediaMaxBytes),
		Side:    ttuc.NewSideMessages(c.redisProvider.SharedState()),
	}
	b.Tokens = ttuc.NewTokenSource(b.Repos.Accounts, b.Services.OAuth, b.Services.Errors, b.Alerts, b.Metrics)
	b.Images = ttuc.NewImageStore(b.Fetcher, objects)
	b.Webhooks = ttuc.NewWebhookSubscriptions(b.Services.Webhooks, cfg.TikTokWebhookURL, b.Metrics)
	b.Comments = ttuc.NewComments(ttuc.CommentsDeps{
		Accounts: b.Repos.Accounts, Videos: b.Repos.Videos, Comments: b.Repos.Comments,
		Tokens: b.Tokens, Services: b.Services, Images: b.Images, Metrics: b.Metrics,
	})
	c.tiktok = b
	c.commentAutomation().Manager.Register(shared.EntryTypeTikTok, ttuc.NewAccountOwnership(b.Repos.Accounts))
	log.Printf("[tiktok] channel enabled (redirect_uri=%q and webhook=%q must match the TikTok app exactly)", cfg.TikTokRedirectURI, cfg.TikTokWebhookURL)
}

func requirePath(raw, want string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return err
	}
	if u.Path != want {
		return fmt.Errorf("the path is %q but the backend serves %q", u.Path, want)
	}
	return nil
}

func (c *Container) registerTikTokAudience(
	ingestor ca.Ingestor,
	tombstones ttuc.AudienceTombstoner,
	adapters map[ca.Source]ca.SourceAdapter,
	verifiers map[ca.Source]cauc.AccountVerifier,
	repliers map[ca.Source]ca.CommentReplier,
) {
	b := c.tiktok
	if b == nil {
		log.Fatalf("[tiktok] comment analysis was built before the TikTok channel")
	}
	adapters[ca.SourceTikTok] = ttuc.NewAudienceAdapter(ttuc.AudienceAdapterDeps{
		Ingestor: ingestor, Tombstones: tombstones,
		Accounts: b.Repos.Accounts, Videos: b.Repos.Videos, Comments: b.Repos.Comments,
		Tokens: b.Tokens, Service: b.Services.Comments, Metrics: b.Metrics,
	})
	verifiers[ca.SourceTikTok] = ttuc.NewAccountOwnership(b.Repos.Accounts)
	repliers[ca.SourceTikTok] = tiktokCommentReplier{comments: b.Comments}
}

type tiktokCommentReplier struct {
	comments *ttuc.Comments
}

func (r tiktokCommentReplier) ReplyToComment(ctx context.Context, workspaceID, accountID, commentID, text string) (string, error) {
	reply, err := r.comments.Reply(ctx, workspaceID, accountID, commentID, text)
	if err != nil {
		return "", err
	}
	return reply.CommentID, nil
}

func (c *Container) tiktokAudience() *ttuc.AudienceAdapter {
	audience, ok := commentAudience[*ttuc.AudienceAdapter](c.audience, ca.SourceTikTok)
	if !ok {
		log.Fatalf("[tiktok] comment analysis has no TikTok source, so comments would never reach the audience")
	}
	return audience
}

func (c *Container) initTikTokRuntime(history conversation_domain.MessageHistoryManager) {
	b := c.tiktok
	if b == nil {
		log.Fatalf("[tiktok] runtime wiring ran before the channel was built")
	}
	if c.useCases == nil || c.useCases.publishWebhook == nil || history == nil {
		log.Fatalf("[tiktok] the webhook publisher and the history manager must exist before the TikTok runtime")
	}
	if c.services.assignmentService == nil || c.services.conversationHub == nil || c.services.webhookQueueSub == nil || c.services.webhookQueuePub == nil {
		log.Fatalf("[tiktok] assignments, the conversation hub and the webhook queue must exist before the TikTok runtime")
	}
	watermarks, ok := c.repositories.conversation.(conversation_domain.DeliveryWatermarkRepository)
	if !ok {
		log.Fatalf("[tiktok] the message repository does not support delivery watermarks")
	}
	files, ok := c.services.fileStorage.(entrypurge_domain.Files)
	if !ok {
		log.Fatalf("[tiktok] the file storage cannot delete files, so the purge could not remove personal data")
	}
	audience := c.tiktokAudience()
	automation := c.commentAutomation()

	handler := ttuc.NewHandleWebhook(ttuc.HandleWebhookDeps{
		Repos:       b.Repos,
		Tokens:      b.Tokens,
		Messaging:   b.Services.Messaging,
		Errors:      b.Services.Errors,
		History:     history,
		Messages:    c.repositories.conversation,
		Announcer:   c.services.conversationHub,
		Watermarks:  watermarks,
		Media:       c.mediaStore(),
		Images:      b.Images,
		Assignments: c.services.assignmentService,
		Automation: conversation_usecase.NewInboundAutomation(
			c.useCases.triggerEvaluator,
			c.mustChannelAIReply(),
			c.services.analysisScheduler,
			c.repositories.conversation,
		).WithDelegations(c.services.delegations),
		Comments: audience,
		Side:     b.Side,
		Metrics:  b.Metrics,
	})
	b.Consume = ttuc.NewConsumers(ttuc.ConsumerDeps{
		QueueSub:    c.services.webhookQueueSub,
		QueuePub:    c.services.webhookQueuePub,
		SharedState: c.redisProvider.SharedState(),
		Durable:     c.processedWebhookEvents(),
		Messages:    handler,
		Comments:    handler,
		Errors:      b.Services.Errors,
		Metrics:     b.Metrics,
		Concurrency: tiktokConsumerConcurrency,
	})
	b.Webhook = tiktokhttp.NewWebhookHandler(c.cfg.TikTokAppSecret, c.useCases.publishWebhook, b.Metrics)

	b.CommentWorker = ttuc.NewCommentWorker(ttuc.CommentWorkerDeps{
		Accounts: b.Repos.Accounts, Contacts: b.Repos.Contacts, Comments: b.Repos.Comments,
		Tokens: b.Tokens, Service: b.Services.Comments, Errors: b.Services.Errors,
		Rules: automation.Evaluator, Actions: ttuc.NewRuleActions(b.Repos.Accounts, b.Comments),
		Audience: audience, Metrics: b.Metrics,
	})
	b.RefreshTokens = ttuc.NewRefreshTokensJob(b.Repos.Accounts, b.Tokens)
	b.Health = ttuc.NewHealthCheckJob(ttuc.HealthCheckDeps{
		Accounts: b.Repos.Accounts, Services: b.Services, Tokens: b.Tokens, Webhooks: b.Webhooks, Alerts: b.Alerts, Metrics: b.Metrics,
	})
	b.Purge = ttuc.NewPurgeJob(ttuc.PurgeDeps{
		Accounts: b.Repos.Accounts,
		Purge:    b.Repos.Purge,
		Entries:  entrypurge_usecase.New(entrypurge_repository.NewStore(c.db), files),
		Rules:    commentautomation_repository.NewAccountPurger(c.db),
		Audience: ca_repository.NewAccountPurger(c.db),
		Metrics:  b.Metrics,
	})

	connect := ttuc.NewConnectUseCase(ttuc.ConnectDeps{
		Accounts: b.Repos.Accounts, Services: b.Services, States: b.States, Webhooks: b.Webhooks,
		Images: b.Images, Alerts: b.Alerts, Metrics: b.Metrics,
	})
	accounts := ttuc.NewAccounts(ttuc.AccountsDeps{
		Accounts: b.Repos.Accounts,
		OAuth:    b.Services.OAuth,
		References: automationrefs.Finders{
			Agents:      c.repositories.agent,
			Workflows:   c.repositories.workflow,
			Pipelines:   c.repositories.pipeline,
			Departments: c.repositories.workspaceDepartment,
		},
		Alerts:  b.Alerts,
		Metrics: b.Metrics,
	})
	b.Handler = tiktokhttp.NewHandler(tiktokhttp.HandlerDeps{
		Connect:         connect,
		Accounts:        accounts,
		Health:          b.Health,
		Comments:        b.Comments,
		Rules:           automation.Manager,
		Images:          b.Images,
		RateLimited:     infratiktok.IsRateLimited,
		FrontendBaseURL: c.cfg.FrontendBaseURL,
	})
}

func (c *Container) startTikTokConsumers() {
	if c.tiktok == nil || c.tiktok.Consume == nil {
		log.Fatalf("[tiktok] the webhook consumers were not built")
	}
	if err := c.tiktok.Consume.Start(); err != nil {
		log.Fatalf("[tiktok] failed to start the webhook consumers: %v", err)
	}
	log.Printf("[tiktok] webhook consumers started")
}

func (c *Container) wireTikTokConversationStack() {
	b := c.tiktok
	if b == nil || b.Tokens == nil {
		log.Fatalf("[tiktok] the conversation stack needs the channel runtime")
	}
	if c.services.conversationAutomation == nil || c.services.conversationAuthImpl == nil || c.services.conversationStatusService == nil {
		log.Fatalf("[tiktok] conversation services must be built before the TikTok conversation stack")
	}
	conversations := b.Repos.Conversations

	c.registerChannelAdapter(ttuc.NewChannelAdapter(ttuc.ChannelAdapterDeps{
		Accounts:      b.Repos.Accounts,
		Contacts:      b.Repos.Contacts,
		Conversations: conversations,
		Tokens:        b.Tokens,
		Messaging:     b.Services.Messaging,
		Errors:        b.Services.Errors,
		Fetcher:       b.Fetcher,
		Side:          b.Side,
		Metrics:       b.Metrics,
	}))

	automationReader, ok := c.services.conversationHistory.(interface {
		SetAutomationReader(shared.EntryType, func(context.Context, string) (*bool, error))
	})
	if !ok {
		log.Fatalf("[tiktok] history provider exposes no SetAutomationReader")
	}
	automationReader.SetAutomationReader(shared.EntryTypeTikTok, func(ctx context.Context, entryID string) (*bool, error) {
		conv, err := conversations.FindByID(ctx, entryID)
		if err != nil {
			return nil, err
		}
		return conv.AutomationEnabled, nil
	})
	c.services.conversationAutomation.Register(shared.EntryTypeTikTok, conversations.SetAutomationEnabled)
	c.services.conversationAuthImpl.SetEntryAccessRepo(shared.EntryTypeTikTok, conversations)
	c.services.conversationStatusService.SetConversationStatusStore(shared.EntryTypeTikTok,
		conversationStatusFuncs{status: conversations.StatusForEntry, set: conversations.SetStatus})
	c.services.conversationStatusService.SetConversationCounter(shared.EntryTypeTikTok, conversations.CountByStatus)

	owners, ok := c.services.campaignWorkspaceResolver.(interface {
		SetEntryOwnerResolver(shared.EntryType, conversation_usecase.EntryOwnerResolver)
	})
	if !ok {
		log.Fatalf("[tiktok] workspace resolver exposes no SetEntryOwnerResolver")
	}
	owners.SetEntryOwnerResolver(shared.EntryTypeTikTok, conversations)

	identities, ok := c.services.conversationHistory.(interface {
		SetContactIdentityLookup(shared.EntryType, conversation_usecase.ContactIdentityLookup)
	})
	if !ok {
		log.Fatalf("[tiktok] history provider exposes no SetContactIdentityLookup")
	}
	identities.SetContactIdentityLookup(shared.EntryTypeTikTok, tiktokContactIdentity(b))
}

func tiktokContactIdentity(b *tiktokBundle) conversation_usecase.ContactIdentityLookup {
	contacts, conversations, images := b.Repos.Contacts, b.Repos.Conversations, b.Images
	display := func(ct *ttdomain.Contact) conversation_usecase.ContactDisplay {
		out := conversation_usecase.ContactDisplay{
			ContactID: ct.ID,
			Ref:       ct.UniqueIdentifier,
			Handle:    ct.Username,
			Name:      ct.Name(),
		}
		if ct.AvatarKey != "" {
			out.PictureURL = images.URL(ct.AvatarKey)
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

func tiktokAnalysisResolver(b *tiktokBundle) conversation_usecase.AnalysisSubjectResolver {
	return channelAnalysisResolver(shared.EntryTypeTikTok,
		func(ctx context.Context, entryID string) (*analysisConversation, error) {
			conv, err := b.Repos.Conversations.FindByID(ctx, entryID)
			if err != nil || conv == nil {
				return nil, err
			}
			account, err := b.Repos.Accounts.FindByID(ctx, conv.AccountID)
			if err != nil || account == nil {
				return nil, err
			}
			return &analysisConversation{
				ID: conv.ID, WorkspaceID: conv.WorkspaceID, ContactID: conv.ContactID,
				Container: analysisContainer{ID: account.ID, Name: account.Name(), Automation: account.Automation()},
			}, nil
		},
		func(ctx context.Context, contactID string) (*analysisContact, error) {
			contact, err := b.Repos.Contacts.FindByID(ctx, contactID)
			if err != nil || contact == nil {
				return nil, err
			}
			return &analysisContact{Label: contact.Name(), LeadID: contact.LeadID}, nil
		})
}

func (c *Container) tiktokReadinessProbe(access workspace.CheckAccessUseCase) readiness.Probe {
	if c.tiktok == nil {
		log.Fatalf("[tiktok] readiness was built before the TikTok channel")
	}
	return readiness_usecase.NewCountProbe(readiness.TikTok, workspace.ResourceTikTokAccounts,
		workspaceTotal(c.tiktok.Repos.Accounts.ListByWorkspace, func(workspaceID string) ttdomain.ListAccountsInput {
			return ttdomain.ListAccountsInput{WorkspaceID: workspaceID, Options: countOnly}
		}), 0, access)
}

func (c *Container) registerTikTokJobs() {
	b := c.tiktok
	if b == nil || b.RefreshTokens == nil || b.Health == nil || b.Purge == nil || b.CommentWorker == nil {
		log.Fatalf("[tiktok] the channel jobs were not built")
	}
	c.jobRunner.SetTikTokJobs(b.RefreshTokens, b.Health, b.Purge, b.CommentWorker)
}

func (c *Container) tiktokAnalysisChannel() analysisChannel {
	if c.tiktok == nil {
		log.Fatalf("[tiktok] conversation analysis was built before the TikTok channel")
	}
	return analysisChannel{shared.EntryTypeTikTok, tiktokAnalysisResolver(c.tiktok)}
}

func (c *Container) tiktokExportLister() export_domain.ChannelEntryLister {
	return tiktok_repository.NewExportRepository(c.db)
}

func tiktokRoutes(c *Container) deliveryHttp.TikTokRoutes {
	if c.tiktok == nil || c.tiktok.Handler == nil || c.tiktok.Webhook == nil {
		log.Fatalf("[tiktok] routes requested before the channel runtime was built")
	}
	return deliveryHttp.TikTokRoutes{Handler: c.tiktok.Handler, Webhook: c.tiktok.Webhook}
}
