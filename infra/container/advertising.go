package container

import (
	"log"
	"time"

	advertisinghttp "vozko/delivery/http/advertising"
	"vozko/delivery/http/metawebhook"
	"vozko/domain/advertising"
	"vozko/domain/copilot"
	"vozko/infra/ai/aibilling"
	fbinfra "vozko/infra/facebook"
	"vozko/infra/meta/marketing"
	"vozko/infra/netguard"
	"vozko/infra/openaiimage"
	"vozko/infra/remotefile"
	advertising_repository "vozko/infra/repositories/advertising"
	adsuc "vozko/usecases/advertising"
	"vozko/usecases/copilot/copilottools"
)

const (
	adCreativeDownloadTimeout = 30 * time.Second
	adCreativeMaxBytes        = 30 << 20
)

type adsBundle struct {
	Sync           *adsuc.SyncUseCase
	Publish        *adsuc.PublishUseCase
	Report         *adsuc.ReportUseCase
	Manage         *adsuc.ManageUseCase
	Images         *adsuc.ImageUseCase
	Accounts       *adsuc.AccountsUseCase
	Assets         *adsuc.AssetsUseCase
	Live           *adsuc.LiveUseCase
	Audience       *adsuc.AudienceUseCase
	Forms          *adsuc.FormsUseCase
	Rules          *adsuc.RulesUseCase
	SplitTests     *adsuc.SplitTestUseCase
	Conversions    *adsuc.ConversionsUseCase
	Webhooks       *adsuc.WebhookConsumer
	Handler        *advertisinghttp.Handler
	WebhookHandler *metawebhook.Handler
}

func (c *Container) initAds() {
	if err := c.adsManager().Webhooks.Start(); err != nil {
		log.Printf("[ads] failed to start webhook consumers: %v", err)
	}
}

func (c *Container) adsManager() *adsBundle {
	if c.ads != nil {
		return c.ads
	}
	if c.cfg.AdsImageCostCeilingMicros <= 0 {
		log.Fatalf("[ads] ADS_IMAGE_COST_CEILING_MICROS must be positive")
	}
	oauth, err := fbinfra.NewOAuthService(fbinfra.OAuthConfig{
		AppID:        c.cfg.MetaAdsAppID,
		AppSecret:    c.cfg.MetaAdsAppSecret,
		ConfigID:     c.cfg.FacebookAdsLoginConfigID,
		RedirectURI:  c.cfg.FacebookAdsRedirectURI,
		GraphVersion: c.cfg.MetaAdsGraphVersion,
		CallbackPath: advertising.OAuthCallbackPath,
	})
	if err != nil {
		log.Fatalf("[ads] oauth: %v", err)
	}
	gateway, err := marketing.NewGateway(marketing.Config{AppSecret: c.cfg.MetaAdsAppSecret, GraphVersion: c.cfg.MetaAdsGraphVersion})
	if err != nil {
		log.Fatalf("[ads] marketing gateway: %v", err)
	}
	generator, err := openaiimage.New(openaiimage.Config{APIKey: c.cfg.OpenRouterAPIKey, Model: c.cfg.AdsImageModel})
	if err != nil {
		log.Fatalf("[ads] image generator: %v", err)
	}
	if c.services.workspacePricer == nil || c.useCases.chatFunds == nil || c.services.conversationAuth == nil || c.repositories.lead == nil {
		log.Fatalf("[ads] pricing, funds gate, conversation access and leads must exist before the ads manager")
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
		Publish:  adsuc.NewPublishUseCase(sync, gateway, jobs, numbers, media, fees),
		Report:   adsuc.NewReportUseCase(accounts, objects, insights, attribution),
		Manage:   adsuc.NewManageUseCase(sync, gateway, media),
		Images:   adsuc.NewImageUseCase(generator, c.useCases.chatFunds, aibilling.NewPublisher(c.services.billingQueuePub), c.useCases.uploadMedia, int64(c.cfg.AdsImageCostCeilingMicros)),
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
	bundle.Webhooks = adsuc.NewWebhookConsumer(adsuc.WebhookConsumerDeps{
		QueueSub:    c.services.webhookQueueSub,
		QueuePub:    c.services.webhookQueuePub,
		SharedState: c.redisProvider.SharedState(),
		Durable:     c.processedWebhookEvents(),
		Leads:       bundle.Forms,
		Accounts:    sync,
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
		Manage:          bundle.Manage,
		Report:          bundle.Report,
		Live:            bundle.Live,
		Assets:          bundle.Assets,
		Audience:        bundle.Audience,
		Forms:           bundle.Forms,
		Rules:           bundle.Rules,
		SplitTests:      bundle.SplitTests,
		Conversions:     bundle.Conversions,
		Publish:         bundle.Publish,
		Images:          bundle.Images,
		Origins:         adsuc.NewOriginUseCase(c.services.conversationAuth, c.adOriginReader(), accounts, objects, insights, attribution),
		FrontendBaseURL: c.cfg.FrontendBaseURL,
	})
	c.ads = bundle
	return bundle
}

func (c *Container) adsTools() []copilot.Tool {
	bundle := c.adsManager()
	return copilottools.AdsTools(copilottools.AdsDeps{
		Accounts: bundle.Accounts,
		Reports:  bundle.Report,
		Manage:   bundle.Manage,
		Assets:   bundle.Assets,
		Publish:  bundle.Publish,
		Images:   bundle.Images,
		Live:     bundle.Live,
	})
}
