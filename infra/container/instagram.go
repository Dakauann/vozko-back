package container

import (
	"context"
	"log"
	"time"

	instagramhttp "vozko/delivery/http/instagram"
	conversation_domain "vozko/domain/conversation"
	igdomain "vozko/domain/instagram"
	"vozko/domain/shared"
	iginfra "vozko/infra/instagram"
	instagram_repository "vozko/infra/repositories/instagram"
	conversation_usecase "vozko/usecases/conversation"
	iguc "vozko/usecases/instagram"
)

// instagramBundle groups everything the Instagram channel needs, so the channel
// can be wired (or skipped) as one unit instead of threading a dozen fields
// through the container's god-structs.
//
// It follows the same self-contained-bundle shape as agent_mcp.go: build it in one
// place, hand the pieces to the router and the job runner, and keep the rest of
// the container unaware of the channel's internals.
type instagramBundle struct {
	// Enabled is false when the channel is switched off or misconfigured. Every
	// consumer checks it before using the bundle.
	Enabled bool

	Accounts        igdomain.AccountRepository
	Contacts        igdomain.ContactRepository
	Conversations   igdomain.ConversationRepository
	Media           igdomain.MediaRepository
	Comments        igdomain.CommentRepository
	PrivateReplies  igdomain.PrivateReplyRepository
	ProcessedEvent  igdomain.ProcessedEventRepository
	CommentRules    igdomain.CommentRuleRepository
	CommentRuleEval *iguc.EvaluateCommentRulesUseCase
	PrivateReplyUC  *iguc.SendPrivateReplyUseCase
	ManageRules     *iguc.ManageCommentRulesUseCase

	OAuth        igdomain.OAuthService
	Messaging    igdomain.MessagingService
	MediaService igdomain.MediaService
	CommentSvc   igdomain.CommentService
	Subscription igdomain.SubscriptionService

	Handler        *instagramhttp.Handler
	WebhookHandler *instagramhttp.WebhookHandler

	Consume       *iguc.ConsumeWebhookUseCase
	RefreshTokens *iguc.RefreshTokensUseCase
	PurgeEvents   *iguc.PurgeProcessedEventsUseCase

	// WebhookSecrets is the accepted signing-secret list. The Instagram API setup
	// has its own app secret, and the docs do not say unambiguously which one
	// signs Instagram webhooks, so both are accepted rather than guessed.
	WebhookSecrets []string
}

// initInstagram builds the Instagram channel.
//
// The configuration is required (LoadConfig fails fast without it), so a client
// that cannot be constructed is a genuine boot failure rather than a reason to
// silently serve 404s on every Instagram route.
func (c *Container) initInstagram() {
	bundle := &instagramBundle{}
	c.instagram = bundle

	// Validate the redirect URI against the path this build actually serves.
	//
	// The path is a code constant (igdomain.OAuthCallbackPath) shared with the
	// router, so a deployment can only get the HOST wrong — and that failure now
	// surfaces at boot with a precise message rather than as an opaque
	// "Invalid redirect_uri" from Instagram halfway through onboarding.
	if err := igdomain.ValidateRedirectURI(c.cfg.InstagramRedirectURI); err != nil {
		log.Fatalf("[instagram] %v", err)
	}

	graphCfg := iginfra.GraphConfig{
		GraphVersion: c.cfg.InstagramGraphVersion,
		AppSecret:    c.cfg.InstagramAppSecret,
	}

	mediaSvc, err := iginfra.NewMediaService(graphCfg)
	if err != nil {
		log.Fatalf("[instagram] media client: %v", err)
	}
	commentSvc, err := iginfra.NewCommentService(graphCfg)
	if err != nil {
		log.Fatalf("[instagram] comment client: %v", err)
	}
	subscriptionSvc, err := iginfra.NewSubscriptionService(graphCfg)
	if err != nil {
		log.Fatalf("[instagram] subscription client: %v", err)
	}
	messagingSvc, err := iginfra.NewMessagingService(iginfra.MessagingConfig{
		GraphVersion: c.cfg.InstagramGraphVersion,
		AppSecret:    c.cfg.InstagramAppSecret,
		// The shared Redis-backed limiter keeps the per-account send quotas
		// (100/s text, 10/s media, 2/s conversations) honest across replicas.
		RateLimiterFactory: c.redisProvider.RateLimiterFactory(),
	})
	if err != nil {
		log.Fatalf("[instagram] messaging client: %v", err)
	}

	bundle.OAuth = iginfra.NewOAuthService(iginfra.OAuthConfig{
		AppID:        c.cfg.InstagramAppID,
		AppSecret:    c.cfg.InstagramAppSecret,
		RedirectURI:  c.cfg.InstagramRedirectURI,
		GraphVersion: c.cfg.InstagramGraphVersion,
	})
	bundle.Messaging = messagingSvc
	bundle.MediaService = mediaSvc
	bundle.CommentSvc = commentSvc
	bundle.Subscription = subscriptionSvc

	bundle.Accounts = instagram_repository.NewAccountRepository(c.db)
	bundle.Contacts = instagram_repository.NewContactRepository(c.db)
	bundle.Conversations = instagram_repository.NewConversationRepository(c.db)
	bundle.Media = instagram_repository.NewMediaRepository(c.db)
	bundle.Comments = instagram_repository.NewCommentRepository(c.db)
	bundle.PrivateReplies = instagram_repository.NewPrivateReplyRepository(c.db)
	bundle.ProcessedEvent = instagram_repository.NewProcessedEventRepository(c.db)
	bundle.CommentRules = instagram_repository.NewCommentRuleRepository(c.db)

	// Instagram webhooks are signed with the Instagram app secret, but the docs
	// only say "your app's App Secret". Accepting both removes the ambiguity
	// without betting on either.
	bundle.WebhookSecrets = append([]string{c.cfg.InstagramAppSecret}, c.cfg.MetaAppSecret)
	bundle.WebhookSecrets = append(bundle.WebhookSecrets, c.cfg.MetaAppSecretsExtra...)

	connect := iguc.NewConnectAccountUseCase(
		bundle.OAuth,
		bundle.Subscription,
		bundle.Messaging,
		bundle.Accounts,
		c.redisProvider.SharedState(),
		c.cfg.InstagramAppSecret,
		"/dashboard/instagram-accounts",
	)

	// Built once and shared: the HTTP handler exposes them as operator actions,
	// and the comment-rule evaluator performs the same operations automatically.
	replyComment := iguc.NewReplyToCommentUseCase(bundle.Accounts, commentSvc, bundle.Comments)
	moderateComment := iguc.NewModerateCommentUseCase(bundle.Accounts, commentSvc, bundle.Comments)
	privateReply := iguc.NewSendPrivateReplyUseCase(
		bundle.Accounts, bundle.Messaging, bundle.Comments,
		bundle.PrivateReplies, bundle.Contacts, bundle.Conversations,
	)
	bundle.PrivateReplyUC = privateReply
	bundle.CommentRuleEval = iguc.NewEvaluateCommentRulesUseCase(
		bundle.CommentRules,
		iguc.NewCommentActionRunner(replyComment, privateReply, moderateComment),
	)
	bundle.ManageRules = iguc.NewManageCommentRulesUseCase(bundle.CommentRules, bundle.Accounts)

	bundle.Handler = instagramhttp.NewHandler(instagramhttp.HandlerDeps{
		Connect:           connect,
		List:              iguc.NewListAccountsUseCase(bundle.Accounts),
		Get:               iguc.NewGetAccountUseCase(bundle.Accounts),
		UpdateCfg:         iguc.NewUpdateAccountConfigUseCase(bundle.Accounts),
		Disconnect:        iguc.NewDisconnectAccountUseCase(bundle.Accounts, bundle.Subscription),
		ListMedia:         iguc.NewListMediaUseCase(bundle.Accounts, mediaSvc, bundle.Media),
		GetMedia:          iguc.NewGetMediaUseCase(bundle.Accounts, mediaSvc, bundle.Media),
		ProxyMedia:        iguc.NewProxyMediaUseCase(bundle.Accounts, mediaSvc),
		ProxyAvatar:       iguc.NewProxyAvatarUseCase(bundle.Accounts, bundle.OAuth, mediaSvc),
		CreateMedia:       iguc.NewCreateMediaUseCase(bundle.Accounts, mediaSvc, bundle.Media),
		SetCommentEnabled: iguc.NewSetCommentEnabledUseCase(bundle.Accounts, mediaSvc, bundle.Media),
		ListComments:      iguc.NewListCommentsUseCase(bundle.Accounts, commentSvc, bundle.Comments),
		ReplyComment:      replyComment,
		Moderate:          moderateComment,
		PrivateReply:      privateReply,
		ManageRules:       bundle.ManageRules,
		FrontendBaseURL:   c.cfg.FrontendBaseURL,
	})

	bundle.RefreshTokens = iguc.NewRefreshTokensUseCase(bundle.Accounts, bundle.OAuth)
	bundle.PurgeEvents = iguc.NewPurgeProcessedEventsUseCase(bundle.ProcessedEvent, 30*24*time.Hour)

	bundle.Enabled = true
	log.Printf("[instagram] channel enabled (graph=%s/%s)", iginfra.GraphHost, iginfra.DefaultGraphVersion)

	// Print the redirect URI exactly as it will be sent to Instagram.
	//
	// "Invalid redirect_uri" is the most common onboarding failure and it is always
	// a byte-level mismatch against the list registered in the App Dashboard under
	//   Instagram > API setup with Instagram login > 3. Set up Instagram business
	//   login > Set up > Redirect URL
	// (NOT the Facebook Login for Business OAuth list, which belongs to WhatsApp
	// Embedded Signup and has no effect here). Meta also warns that the dashboard
	// may silently append a trailing slash to a saved URI, so the two strings can
	// differ by one invisible character. Logging it makes that diffable instead of
	// guesswork.
	log.Printf("[instagram] redirect_uri sent to Instagram: %q (must match the dashboard byte-for-byte, including any trailing slash)",
		c.cfg.InstagramRedirectURI)
}

// initInstagramRuntime wires the parts that depend on the conversation stack.
//
// The history manager is a local inside initUseCases rather than a container
// field, so it is passed in instead of reached for.
func (c *Container) initInstagramRuntime(history conversation_domain.MessageHistoryManager) {
	bundle := c.instagram
	if bundle == nil || !bundle.Enabled {
		return
	}

	// A private reply opens a conversation; recording it in the transcript is what
	// makes that conversation appear in the inbox, since the inbox lists
	// conversations by their last message.
	if bundle.PrivateReplyUC != nil {
		bundle.PrivateReplyUC.SetHistoryManager(history)
	}
	// Guard the wiring order explicitly: this half depends on the useCases struct
	// literal having been assigned, and a nil here used to panic at boot rather
	// than saying what was wrong.
	if c.useCases == nil {
		log.Fatalf("[instagram] runtime wiring ran before useCases were built")
	}

	bundle.WebhookHandler = instagramhttp.NewWebhookHandler(
		c.useCases.publishWebhook,
		bundle.WebhookSecrets,
		c.cfg.InstagramWebhookVerifyToken,
	)

	// The webhook dispatcher reuses the SHARED history manager, so Instagram gets
	// the same persistence, dedup and websocket fan-out as every other channel
	// rather than a parallel implementation.
	handler := iguc.NewHandleWebhookUseCase(iguc.HandleWebhookDeps{
		Accounts:      bundle.Accounts,
		Contacts:      bundle.Contacts,
		Conversations: bundle.Conversations,
		Comments:      bundle.Comments,
		Media:         bundle.Media,
		Messaging:     bundle.Messaging,
		MediaFetcher:  bundle.MediaService,
		History:       history,
		Messages:      c.repositories.conversation,
		ConvMedia:     c.repositories.conversationMedia,
		FileStorage:   c.services.fileStorage,
		Broadcaster:   c.services.conversationHub,
		Assignments:   c.services.assignmentService,
		AIReply:       c.services.channelAIReply,
		Workflows:     c.useCases.triggerEvaluator,
		CommentRules:  bundle.CommentRuleEval,
		Analysis:      conversation_usecase.NewAnalysisScheduler(c.redisProvider.SharedState()),
	})

	bundle.Consume = iguc.NewConsumeWebhookUseCase(
		c.services.webhookQueueSub,
		c.services.webhookQueuePub,
		c.redisProvider.SharedState(),
		bundle.ProcessedEvent,
		handler,
	)
}

// wireInstagramConversationStack registers the Instagram channel with the shared
// conversation services.
//
// Each of these is a per-channel lookup that the conversation stack keys on
// (entry_id, entry_type) and therefore cannot resolve generically: the send
// adapter, the WS authorizer's ownership check, the workspace/department resolver,
// and the conversation-status writer. Registering them here — rather than adding
// another `case "instagram"` inside each of those files — is what keeps the
// channel additive.
func (c *Container) wireInstagramConversationStack() {
	bundle := c.instagram
	if bundle == nil || !bundle.Enabled {
		return
	}

	adapter := iguc.NewChannelAdapter(
		bundle.Accounts,
		bundle.Contacts,
		bundle.Conversations,
		bundle.Messaging,
	)

	c.registerChannelAdapter(adapter)
	if c.services.conversationAuthImpl != nil {
		c.services.conversationAuthImpl.SetInstagramEntryRepo(bundle.Conversations)
	}
	if c.services.conversationStatusService != nil {
		c.services.conversationStatusService.SetConversationStatusStore(
			shared.EntryTypeInstagram,
			conversationStatusFuncs{
				status: bundle.Conversations.StatusForEntry,
				set:    bundle.Conversations.SetStatus,
			},
		)
		c.services.conversationStatusService.SetConversationCounter(
			shared.EntryTypeInstagram, bundle.Conversations.CountByStatus)
	}
	if setter, ok := c.services.campaignWorkspaceResolver.(interface {
		SetInstagramEntryResolver(interface {
			WorkspaceIDForEntry(ctx context.Context, entryID string) (string, error)
			DepartmentIDForEntry(ctx context.Context, entryID string) (string, error)
		})
	}); ok {
		setter.SetInstagramEntryResolver(bundle.Conversations)
	}
	// The history provider is held as the domain interface, so the optional
	// identity port is attached by assertion — the same pattern the resolver
	// above uses.
	if setter, ok := c.services.conversationHistory.(interface {
		SetContactIdentityLookup(shared.EntryType, conversation_usecase.ContactIdentityLookup)
	}); ok {
		setter.SetContactIdentityLookup(shared.EntryTypeInstagram, instagramContactIdentity(bundle))
	}
}

// instagramContactIdentity adapts the Instagram repositories onto the
// conversation usecase's sender-identity port, so the CRM can label an Instagram
// DM without the conversation package importing the Instagram domain.
func instagramContactIdentity(bundle *instagramBundle) conversation_usecase.ContactIdentityLookup {
	contacts, conversations := bundle.Contacts, bundle.Conversations

	display := func(c *igdomain.Contact) conversation_usecase.ContactDisplay {
		return conversation_usecase.ContactDisplay{
			ContactID:  c.ID,
			Ref:        c.IGSID,
			Handle:     c.Username,
			Name:       c.Name,
			PictureURL: c.ProfilePictureURL,
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

// instagramHandler returns the channel's HTTP handler, or nil when the channel is
// disabled — the router treats nil as "register no routes".
func instagramHandler(c *Container) *instagramhttp.Handler {
	if c.instagram == nil || !c.instagram.Enabled {
		return nil
	}
	return c.instagram.Handler
}

// instagramWebhookHandler returns the webhook handler, or nil when disabled.
func instagramWebhookHandler(c *Container) *instagramhttp.WebhookHandler {
	if c.instagram == nil || !c.instagram.Enabled {
		return nil
	}
	return c.instagram.WebhookHandler
}
