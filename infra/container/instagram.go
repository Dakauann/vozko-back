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

type instagramBundle struct {
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
	ModerateComment *iguc.ModerateCommentUseCase
	ReplyComment    *iguc.ReplyToCommentUseCase

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

	WebhookSecrets []string
}

func (c *Container) initInstagram() {
	bundle := &instagramBundle{}
	c.instagram = bundle

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
		GraphVersion:       c.cfg.InstagramGraphVersion,
		AppSecret:          c.cfg.InstagramAppSecret,
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

	replyComment := iguc.NewReplyToCommentUseCase(bundle.Accounts, commentSvc, bundle.Comments)
	moderateComment := iguc.NewModerateCommentUseCase(bundle.Accounts, commentSvc, bundle.Comments)
	privateReply := iguc.NewSendPrivateReplyUseCase(
		bundle.Accounts, bundle.Messaging, bundle.Comments,
		bundle.PrivateReplies, bundle.Contacts, bundle.Conversations,
	)
	bundle.PrivateReplyUC = privateReply
	bundle.ModerateComment = moderateComment
	bundle.ReplyComment = replyComment
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

	log.Printf("[instagram] redirect_uri sent to Instagram: %q (must match the dashboard byte-for-byte, including any trailing slash)",
		c.cfg.InstagramRedirectURI)
}

func (c *Container) initInstagramRuntime(history conversation_domain.MessageHistoryManager) {
	bundle := c.instagram
	if bundle == nil || !bundle.Enabled {
		return
	}

	if bundle.PrivateReplyUC != nil {
		bundle.PrivateReplyUC.SetHistoryManager(history)
	}
	if c.useCases == nil {
		log.Fatalf("[instagram] runtime wiring ran before useCases were built")
	}

	bundle.WebhookHandler = instagramhttp.NewWebhookHandler(
		c.useCases.publishWebhook,
		bundle.WebhookSecrets,
		c.cfg.InstagramWebhookVerifyToken,
	)

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
		AIReply:       c.mustChannelAIReply(),
		Workflows:     c.useCases.triggerEvaluator,
		CommentRules:  bundle.CommentRuleEval,
		Analysis:      conversation_usecase.NewAnalysisScheduler(c.redisProvider.SharedState()),
		Audience:      audienceEnqueuer(c),
	})

	bundle.Consume = iguc.NewConsumeWebhookUseCase(
		c.services.webhookQueueSub,
		c.services.webhookQueuePub,
		c.redisProvider.SharedState(),
		bundle.ProcessedEvent,
		handler,
	)
}

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

	if setter, ok := c.services.conversationHistory.(interface {
		SetAutomationReader(shared.EntryType, func(context.Context, string) (*bool, error))
	}); ok {
		conversations := bundle.Conversations
		setter.SetAutomationReader(shared.EntryTypeInstagram, func(ctx context.Context, entryID string) (*bool, error) {
			conv, err := conversations.FindByID(ctx, entryID)
			if err != nil {
				return nil, err
			}
			return conv.AutomationEnabled, nil
		})
	} else {
		log.Printf("[instagram] history provider exposes no SetAutomationReader; the toggle will read as always-on")
	}

	if c.services.conversationAutomation != nil {
		conversations := bundle.Conversations
		c.services.conversationAutomation.Register(
			shared.EntryTypeInstagram,
			func(ctx context.Context, entryID string, enabled *bool) error {
				return conversations.SetAutomationEnabled(ctx, entryID, enabled)
			},
		)
	}
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
		SetEntryOwnerResolver(shared.EntryType, conversation_usecase.EntryOwnerResolver)
	}); ok {
		setter.SetEntryOwnerResolver(shared.EntryTypeInstagram, bundle.Conversations)
	} else {
		log.Printf("[instagram] workspace resolver exposes no SetEntryOwnerResolver; inbound messages will not resolve a workspace")
	}
	if setter, ok := c.services.conversationHistory.(interface {
		SetContactIdentityLookup(shared.EntryType, conversation_usecase.ContactIdentityLookup)
	}); ok {
		setter.SetContactIdentityLookup(shared.EntryTypeInstagram, instagramContactIdentity(bundle))
	}
}

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

func instagramHandler(c *Container) *instagramhttp.Handler {
	if c.instagram == nil || !c.instagram.Enabled {
		return nil
	}
	return c.instagram.Handler
}

func instagramWebhookHandler(c *Container) *instagramhttp.WebhookHandler {
	if c.instagram == nil || !c.instagram.Enabled {
		return nil
	}
	return c.instagram.WebhookHandler
}
