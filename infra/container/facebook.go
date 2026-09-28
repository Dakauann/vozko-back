package container

import (
	"log"

	facebookhttp "vozko/delivery/http/facebook"
	"vozko/delivery/http/metawebhook"
	fbdomain "vozko/domain/facebook"
	"vozko/domain/shared"
	fbinfra "vozko/infra/facebook"
	facebook_repository "vozko/infra/repositories/facebook"
	fbuc "vozko/usecases/facebook"
)

type facebookBundle struct {
	Enabled bool

	Grants        fbdomain.GrantRepository
	Pages         fbdomain.PageRepository
	Contacts      fbdomain.ContactRepository
	Conversations fbdomain.ConversationRepository
	Posts         fbdomain.PostRepository
	Comments      fbdomain.CommentRepository
	PublishJobs   fbdomain.PublishJobRepository

	OAuth        fbdomain.OAuthService
	Subscription fbdomain.SubscriptionService
	Messaging    fbdomain.MessagingService
	Routing      fbdomain.RoutingService
	PostService  fbdomain.PostService
	CommentSvc   fbdomain.CommentService
	Pictures     *fbuc.PictureStore

	Connect       *fbuc.ConnectPagesUseCase
	PagesUC       *fbuc.PageUseCases
	Health        *fbuc.HealthCheckUseCase
	ThreadControl *fbuc.ThreadControlUseCase
	PostsUC       *fbuc.PostUseCases
	CommentsUC    *fbuc.CommentUseCases
	ProfileUC     *fbuc.MessengerProfileUseCases
	Publisher     *fbuc.PublishJobWorker
	AppUsers      *fbuc.AppUserHandler

	Handler        *facebookhttp.Handler
	WebhookHandler *metawebhook.Handler
	Consume        *fbuc.ConsumeWebhookUseCase
	PublishRunner  interface{ Start() error }
}

func (c *Container) initFacebook() {
	bundle := &facebookBundle{}
	c.facebook = bundle

	graph := fbinfra.GraphConfig{GraphVersion: c.cfg.FacebookGraphVersion, AppSecret: c.cfg.MetaAppSecret}

	oauth, err := fbinfra.NewOAuthService(fbinfra.OAuthConfig{
		AppID:        c.cfg.MetaAppID,
		AppSecret:    c.cfg.MetaAppSecret,
		ConfigID:     c.cfg.FacebookLoginConfigID,
		RedirectURI:  c.cfg.FacebookRedirectURI,
		GraphVersion: c.cfg.FacebookGraphVersion,
	})
	if err != nil {
		log.Fatalf("[facebook] %v", err)
	}
	subscription, err := fbinfra.NewSubscriptionService(graph)
	if err != nil {
		log.Fatalf("[facebook] subscription client: %v", err)
	}
	fetcher, err := fbinfra.NewMediaFetcher(graph)
	if err != nil {
		log.Fatalf("[facebook] media fetcher: %v", err)
	}

	messaging, err := fbinfra.NewMessagingService(fbinfra.MessagingConfig{Graph: graph, RateLimiterFactory: c.redisProvider.RateLimiterFactory()})
	if err != nil {
		log.Fatalf("[facebook] messaging client: %v", err)
	}
	routing, err := fbinfra.NewRoutingService(graph)
	if err != nil {
		log.Fatalf("[facebook] routing client: %v", err)
	}

	posts, err := fbinfra.NewPostService(graph)
	if err != nil {
		log.Fatalf("[facebook] post client: %v", err)
	}
	if c.services.facebookPublishPub == nil || c.services.facebookPublishSub == nil {
		log.Fatalf("[facebook] the publish queue must exist before the facebook channel")
	}

	bundle.OAuth = oauth
	bundle.PostService = posts
	bundle.CommentSvc, err = fbinfra.NewCommentService(graph)
	if err != nil {
		log.Fatalf("[facebook] comment client: %v", err)
	}
	profiles, err := fbinfra.NewProfileService(fbinfra.ProfileConfig{Graph: graph, RateLimiterFactory: c.redisProvider.RateLimiterFactory()})
	if err != nil {
		log.Fatalf("[facebook] messenger profile client: %v", err)
	}
	bundle.Subscription = subscription
	bundle.Messaging = messaging
	bundle.Routing = routing

	bundle.Pictures = fbuc.NewPictureStore(fetcher, c.services.fileStorage)

	bundle.Grants = facebook_repository.NewGrantRepository(c.db)
	bundle.Pages = facebook_repository.NewPageRepository(c.db)
	bundle.Contacts = facebook_repository.NewContactRepository(c.db)
	bundle.Conversations = facebook_repository.NewConversationRepository(c.db)
	bundle.Posts = facebook_repository.NewPostRepository(c.db)
	bundle.Comments = facebook_repository.NewCommentRepository(c.db)
	bundle.PublishJobs = facebook_repository.NewPublishJobRepository(c.db)

	postDeps := fbuc.PostDeps{
		Pages: bundle.Pages, Posts: bundle.Posts, Jobs: bundle.PublishJobs,
		Service: posts, Queue: fbuc.NewPublishQueue(c.services.facebookPublishPub),
	}
	bundle.PostsUC = fbuc.NewPostUseCases(postDeps)
	bundle.ProfileUC = fbuc.NewMessengerProfileUseCases(bundle.Pages, profiles)
	bundle.Publisher = fbuc.NewPublishJobWorker(postDeps)

	bundle.Connect = fbuc.NewConnectPagesUseCase(
		oauth, subscription, bundle.Grants, bundle.Pages, bundle.Pictures,
		c.mustOAuthStateIssuer("fb:oauth", c.cfg.MetaAppSecret, "/dashboard/facebook-pages"),
	)
	bundle.PagesUC = fbuc.NewPageUseCases(bundle.Pages, bundle.Grants, subscription)
	if c.services.conversationAuth == nil {
		log.Fatalf("[facebook] the conversation authorizer must exist before the facebook channel")
	}
	bundle.ThreadControl = fbuc.NewThreadControlUseCase(fbuc.ThreadControlDeps{
		Access:        c.services.conversationAuth,
		Pages:         bundle.Pages,
		Contacts:      bundle.Contacts,
		Conversations: bundle.Conversations,
		Routing:       routing,
		OurAppID:      c.cfg.MetaAppID,
	})
	bundle.Health = fbuc.NewHealthCheckUseCase(oauth, subscription, bundle.Grants, bundle.Pages, bundle.ThreadControl)
	bundle.AppUsers = fbuc.NewAppUserHandler(bundle.Grants, bundle.Pages)

	c.commentAutomation().Manager.Register(shared.EntryTypeFacebook, fbuc.NewPageOwnership(bundle.Pages))

	bundle.Enabled = true
	log.Printf("[facebook] channel enabled (redirect_uri=%q must match the Facebook Login for Business settings exactly)",
		c.cfg.FacebookRedirectURI)
}
