package container

import (
	"context"
	"log"
	"time"

	advertisinghttp "vozko/delivery/http/advertising"
	"vozko/delivery/http/metawebhook"
	"vozko/domain/advertising"
	"vozko/domain/copilot"
	fbinfra "vozko/infra/facebook"
	"vozko/infra/meta/marketing"
	"vozko/infra/netguard"
	"vozko/infra/remotefile"
	advertising_repository "vozko/infra/repositories/advertising"
	adsuc "vozko/usecases/advertising"
	"vozko/usecases/copilot/copilottools"
)

const (
	adCreativeDownloadTimeout = 30 * time.Second
	adCreativeMaxBytes        = 30 << 20
	adsWebhookAttempts        = 20
	adsWebhookTimeout         = 30 * time.Second
	adsWebhookRetryDelay      = 30 * time.Second
)

type adsBundle struct {
	Sync                *adsuc.SyncUseCase
	Publish             *adsuc.PublishUseCase
	Drafts              *adsuc.DraftsUseCase
	Readiness           *adsuc.ReadinessUseCase
	Report              *adsuc.ReportUseCase
	Manage              *adsuc.ManageUseCase
	Accounts            *adsuc.AccountsUseCase
	Assets              *adsuc.AssetsUseCase
	Live                *adsuc.LiveUseCase
	Audience            *adsuc.AudienceUseCase
	Forms               *adsuc.FormsUseCase
	Rules               *adsuc.RulesUseCase
	SplitTests          *adsuc.SplitTestUseCase
	Conversions         *adsuc.ConversionsUseCase
	Bulk                *adsuc.BulkUseCase
	SavedReports        *adsuc.SavedReportsUseCase
	Runs                *adsuc.ReportRunsUseCase
	Webhooks            *adsuc.WebhookConsumer
	GrantHealth         *adsuc.GrantHealthUseCase
	PublishWorker       interface{ Start() error }
	WebhookSubscription *adsuc.WebhookSubscriptionUseCase
	Handler             *advertisinghttp.Handler
	WebhookHandler      *metawebhook.Handler
}

func (c *Container) initAds() {
	if err := c.adsManager().Webhooks.Start(); err != nil {
		log.Printf("[ads] failed to start webhook consumers: %v", err)
	}
	if err := c.ads.PublishWorker.Start(); err != nil {
		log.Fatalf("[ads] failed to start the publish worker: %v", err)
	}
	if c.ads.WebhookSubscription != nil {
		go keepAdsWebhookSubscribed(c.ads.WebhookSubscription)
	}
}

func keepAdsWebhookSubscribed(subscription *adsuc.WebhookSubscriptionUseCase) {
	for attempt := 1; attempt <= adsWebhookAttempts; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), adsWebhookTimeout)
		changed, err := subscription.Ensure(ctx)
		cancel()
		if err == nil {
			log.Printf("[ads] ad account webhook subscription checked (changed: %v)", changed)
			return
		}
		log.Printf("[ads] ad account webhook subscription attempt %d/%d failed: %v", attempt, adsWebhookAttempts, err)
		time.Sleep(adsWebhookRetryDelay)
	}
	log.Printf("[ads] ad account webhook subscription gave up; ad status changes rely on the periodic sync until the next restart")
}

func (c *Container) adsManager() *adsBundle {
	if c.ads != nil {
		return c.ads
	}
	oauth, err := fbinfra.NewOAuthService(fbinfra.OAuthConfig{
		AppID:        c.cfg.MetaAdsAppID,
		AppSecret:    c.cfg.MetaAdsAppSecret,
		ConfigID:     c.cfg.MetaAdsLoginConfigID,
		RedirectURI:  c.cfg.MetaAdsRedirectURI,
		GraphVersion: c.cfg.MetaAdsGraphVersion,
		CallbackPath: advertising.OAuthCallbackPath,
	})
	if err != nil {
		log.Fatalf("[ads] oauth: %v", err)
	}
	gateway, err := marketing.NewGateway(marketing.Config{AppID: c.cfg.MetaAdsAppID, AppSecret: c.cfg.MetaAdsAppSecret, GraphVersion: c.cfg.MetaAdsGraphVersion})
	if err != nil {
		log.Fatalf("[ads] marketing gateway: %v", err)
	}
	if c.services.workspacePricer == nil || c.services.conversationAuth == nil || c.repositories.lead == nil {
		log.Fatalf("[ads] pricing, conversation access and leads must exist before the ads manager")
	}

	grants := advertising_repository.NewGrantRepository(c.db)
	accounts := advertising_repository.NewAccountRepository(c.db)
	objects := advertising_repository.NewObjectRepository(c.db)
	insights := advertising_repository.NewInsightRepository(c.db)
	attribution := advertising_repository.NewAttributionRepository(c.db)
	jobs := advertising_repository.NewPublishJobRepository(c.db)
	numbers := advertising_repository.NewNumberDirectory(c.db)

	fetcher := remotefile.NewFetcher(netguard.NewHTTPClient(adCreativeDownloadTimeout), adCreativeMaxBytes)
	media := adsuc.NewMediaSource(c.useCases.getMedia, fetcher)
	sync := adsuc.NewSyncUseCase(accounts, grants, gateway, objects, insights)
	fees := adsuc.NewFeeCharger(c.services.workspacePricer, c.repositories.balance, c.useCases.ensureActiveWorkspaceSubscription)

	bundle := &adsBundle{
		Sync:     sync,
		Accounts: adsuc.NewAccountsUseCase(accounts),
		Publish:  adsuc.NewPublishUseCase(sync, gateway, jobs, numbers, media, fees, adsuc.NewPublishQueue(c.services.adsPublishPub)),
		Report:   adsuc.NewReportUseCase(accounts, objects, insights, attribution),
		Manage:   adsuc.NewManageUseCase(sync, gateway, media),
		Assets:   adsuc.NewAssetsUseCase(sync, gateway, numbers),
		Live:     adsuc.NewLiveUseCase(sync, gateway),
		Audience: adsuc.NewAudienceUseCase(sync, gateway,
			adsuc.NewCRMCustomers(c.repositories.lead),
			adsuc.NewLibraryFiles(c.useCases.getMedia, fetcher),
			advertising_repository.NewSavedAudienceRepository(c.db)),
		Forms: adsuc.NewFormsUseCase(sync, gateway,
			advertising_repository.NewLeadFormRepository(c.db),
			advertising_repository.NewFormLeadRepository(c.db),
			c.repositories.lead),
		Rules:      adsuc.NewRulesUseCase(sync, gateway),
		SplitTests: adsuc.NewSplitTestUseCase(sync, gateway),
		Conversions: adsuc.NewConversionsUseCase(sync, gateway,
			advertising_repository.NewConversionSettingsRepository(c.db),
			advertising_repository.NewConversionOutbox(c.db),
			advertising_repository.NewWABADirectory(c.db)),
	}
	savedReports := advertising_repository.NewSavedReportRepository(c.db)
	bundle.Readiness = adsuc.NewReadinessUseCase(sync, gateway)
	bundle.Drafts = adsuc.NewDraftsUseCase(advertising_repository.NewDraftRepository(c.db), accounts, jobs, bundle.Publish)
	bundle.Bulk = adsuc.NewBulkUseCase(bundle.Manage)
	bundle.SavedReports = adsuc.NewSavedReportsUseCase(savedReports, accounts)
	bundle.Runs = adsuc.NewReportRunsUseCase(bundle.Live, bundle.Report, objects, savedReports, advertising_repository.NewReportExportRepository(c.db))
	bundle.Webhooks = adsuc.NewWebhookConsumer(adsuc.WebhookConsumerDeps{
		QueueSub:    c.services.webhookQueueSub,
		QueuePub:    c.services.webhookQueuePub,
		SharedState: c.redisProvider.SharedState(),
		Durable:     c.processedWebhookEvents(),
		Leads:       bundle.Forms,
		Accounts:    adsuc.NewAccountEventsUseCase(sync, gateway, c.redisProvider.SharedState()),
	})
	bundle.WebhookHandler = advertisinghttp.NewAdAccountWebhookHandler(
		c.useCases.publishWebhook,
		[]string{c.cfg.MetaAdsAppSecret},
		c.cfg.FacebookWebhookVerifyToken,
	)
	bundle.Handler = advertisinghttp.NewHandler(advertisinghttp.Deps{
		Connect:         adsuc.NewConnectUseCase(oauth, gateway, grants, accounts, c.mustOAuthStateIssuer("ads:oauth", c.cfg.MetaAdsAppSecret, "/dashboard/advertising")),
		Accounts:        bundle.Accounts,
		Sync:            sync,
		Readiness:       bundle.Readiness,
		Manage:          bundle.Manage,
		Comments:        adsuc.NewCommentsUseCase(sync, gateway),
		Report:          bundle.Report,
		Live:            bundle.Live,
		Assets:          bundle.Assets,
		Audience:        bundle.Audience,
		Forms:           bundle.Forms,
		Rules:           bundle.Rules,
		SplitTests:      bundle.SplitTests,
		Conversions:     bundle.Conversions,
		Publish:         bundle.Publish,
		Drafts:          bundle.Drafts,
		Bulk:            bundle.Bulk,
		Reports:         bundle.SavedReports,
		Runs:            bundle.Runs,
		Origins:         adsuc.NewOriginUseCase(c.services.conversationAuth, c.adOriginReader(), accounts, objects, insights, attribution),
		FrontendBaseURL: c.cfg.FrontendBaseURL,
	})
	bundle.GrantHealth = adsuc.NewGrantHealthUseCase(grants, accounts, oauth)
	if c.cfg.MetaAdsWebhookURL != "" {
		bundle.WebhookSubscription = adsuc.NewWebhookSubscriptionUseCase(gateway, c.cfg.MetaAdsWebhookURL, c.cfg.FacebookWebhookVerifyToken)
	}
	bundle.PublishWorker = adsuc.NewPublishConsumer(c.services.adsPublishSub, c.services.adsPublishPub, c.redisProvider.SharedState(), bundle.Publish)
	c.ads = bundle
	return bundle
}

func (c *Container) adsTools() []copilot.Tool {
	bundle := c.adsManager()
	ads := copilottools.AdsDeps{
		Accounts:  bundle.Accounts,
		Reports:   bundle.Report,
		Manage:    bundle.Manage,
		Assets:    bundle.Assets,
		Publish:   bundle.Publish,
		Live:      bundle.Live,
		Drafts:    bundle.Drafts,
		Readiness: bundle.Readiness,
		Targeting: bundle.Assets,
		Forms:     bundle.Forms,
		Editor:    bundle.Manage,
		Bulk:      bundle.Bulk,
		Sources:   bundle.Assets,
	}
	tools := copilottools.AdsTools(ads)
	tools = append(tools, copilottools.AdManageTools(copilottools.AdManageDeps{
		Bulk: bundle.Bulk, SpendCap: bundle.Manage, Runs: bundle.Runs, Reports: bundle.SavedReports, Now: time.Now,
	}, ads)...)
	tools = append(tools, copilottools.AdGrowthTools(copilottools.AdGrowthDeps{
		Audiences: bundle.Audience, Rules: bundle.Rules, Tests: bundle.SplitTests, Conversions: bundle.Conversions, Pixels: bundle.Assets, Now: time.Now,
	}, ads)...)
	return append(tools, copilottools.NewConnectAdAccountTool())
}
