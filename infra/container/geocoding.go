package container

import (
	"log"
	"time"

	domainCache "vozko/domain/cache"
	"vozko/domain/geo"
	"vozko/domain/geocoding"
	"vozko/infra/opencage"
	geocoding_repository "vozko/infra/repositories/geocoding"
	georef_repository "vozko/infra/repositories/georef"
	lead_repository "vozko/infra/repositories/lead"
	geocoding_usecase "vozko/usecases/geocoding"
	workspace_config_usecase "vozko/usecases/workspace_config"
)

const openCageLimiterPrefix = "geocoding:opencage"

type geocodingBundle struct {
	providers map[geocoding.Provider]geo.Geocoder
	settings  *geocoding_repository.SettingsStore
	usage     *geocoding_repository.UsageStore
	answers   *geocoding_repository.AnswerStore
	pauses    *geocoding_repository.PauseStore
}

func (c *Container) geocoding() *geocodingBundle {
	if c.geocodingBundle == nil {
		c.geocodingBundle = &geocodingBundle{
			providers: c.geocodingProviders(),
			settings:  geocoding_repository.NewSettingsStore(c.db),
			usage:     geocoding_repository.NewUsageStore(c.db),
			answers:   geocoding_repository.NewAnswerStore(c.db),
			pauses:    geocoding_repository.NewPauseStore(c.geocodingSharedState()),
		}
	}
	return c.geocodingBundle
}

func (c *Container) geocodingProviders() map[geocoding.Provider]geo.Geocoder {
	providers := map[geocoding.Provider]geo.Geocoder{}
	if c.cfg.OpenCageAPIKey == "" {
		log.Printf("[geocoding] OPENCAGE_API_KEY is not set: addresses are located from the IBGE reference only")
		return providers
	}
	if c.cfg.OpenCageRatePerSecond <= 0 {
		log.Fatalf("[geocoding] OPENCAGE_RATE_PER_SECOND must be above 0 when OPENCAGE_API_KEY is set")
	}
	if c.redisProvider == nil {
		log.Fatalf("[geocoding] the OpenCage provider needs Redis for its rate limiter")
	}
	limiter := c.redisProvider.RateLimiterFactory()(openCageLimiterPrefix, c.cfg.OpenCageRatePerSecond, time.Second)
	client, err := opencage.NewClient(opencage.Config{APIKey: c.cfg.OpenCageAPIKey, Limiter: limiter})
	if err != nil {
		log.Fatalf("[geocoding] %v", err)
	}
	providers[geocoding.ProviderOpenCage] = client
	return providers
}

func (c *Container) geocodingProviderNames() []geocoding.Provider {
	names := make([]geocoding.Provider, 0, len(c.geocoding().providers))
	for name := range c.geocoding().providers {
		names = append(names, name)
	}
	return names
}

func (c *Container) geocodingSettingsService() *workspace_config_usecase.GeocodingSettingsUseCase {
	if c.repositories.workspace == nil {
		log.Fatalf("[geocoding] the workspace repository must be wired before the geocoding settings")
	}
	bundle := c.geocoding()
	service, err := workspace_config_usecase.NewGeocodingSettingsUseCase(workspace_config_usecase.GeocodingSettingsDeps{
		Store: bundle.settings, Usage: bundle.usage, Pauses: bundle.pauses, Workspaces: c.repositories.workspace,
		Names: c.actorNames(), Providers: c.geocodingProviderNames(),
	})
	if err != nil {
		log.Fatalf("[geocoding] %v", err)
	}
	return service
}

func (c *Container) geocodingSweeper() *geocoding_usecase.Sweeper {
	if c.cfg.GeocodingSweeperDisabled {
		log.Printf("[geocoding] the sweeper is switched off by GEOCODING_SWEEPER_DISABLED")
		return nil
	}
	if c.services.metrics == nil {
		log.Fatalf("[geocoding] metrics must be wired before the geocoding sweeper")
	}
	bundle := c.geocoding()
	chain, err := geocoding_usecase.NewChain(geocoding_usecase.ChainDeps{
		Reference: georef_repository.NewReference(c.db),
		Providers: bundle.providers,
		Settings:  bundle.settings,
		Usage:     bundle.usage,
		Answers:   bundle.answers,
		Pauses:    bundle.pauses,
		Metrics:   c.services.metrics,
	})
	if err != nil {
		log.Fatalf("[geocoding] %v", err)
	}
	sweeper, err := geocoding_usecase.NewSweeper(geocoding_usecase.SweeperDeps{
		Queue:    lead_repository.NewGeocodeQueue(c.db, c.redisProvider.SharedState()),
		Chain:    chain,
		Metrics:  c.services.metrics,
		Notifier: c.leadChangeNotifier(),
	})
	if err != nil {
		log.Fatalf("[geocoding] %v", err)
	}
	return sweeper
}

func (c *Container) geocodingSharedState() domainCache.SharedState {
	if c.redisProvider == nil {
		log.Printf("[geocoding] no shared state: the provider pause cannot be read, so no external call is made")
		return nil
	}
	return c.redisProvider.SharedState()
}

func (c *Container) geocodingPlatformUsage() *geocoding_usecase.PlatformUsage {
	if c.redisProvider == nil {
		log.Printf("[geocoding] no shared state: the platform geocoding view answers 503")
		return nil
	}
	bundle := c.geocoding()
	platform := geocoding_repository.NewPlatformReader(c.db)
	service, err := geocoding_usecase.NewPlatformUsage(geocoding_usecase.PlatformUsageDeps{
		Directory: platform,
		Settings:  bundle.settings,
		Usage:     bundle.usage,
		Coverage:  platform,
		Memo:      c.analyticsMemo(),
		Gate:      c.sharedAnalyticsGate(),
	})
	if err != nil {
		log.Fatalf("[geocoding] %v", err)
	}
	return service
}

func (c *Container) geocodingDistrictRefine() *geocoding_usecase.DistrictRefine {
	if c.redisProvider == nil {
		log.Printf("[geocoding] no shared state: the nightly bairro refinement cannot record its runs and does not run")
		return nil
	}
	runs, err := georef_repository.NewRefineRuns(c.redisProvider.SharedState())
	if err != nil {
		log.Fatalf("[geocoding] %v", err)
	}
	refinements := georef_repository.NewRefinements(c.db)
	refine, err := geocoding_usecase.NewDistrictRefine(geocoding_usecase.DistrictRefineDeps{
		Addresses: refinements,
		Cities:    georef_repository.NewReference(c.db),
		Districts: refinements,
		Runs:      runs,
	})
	if err != nil {
		log.Fatalf("[geocoding] %v", err)
	}
	return refine
}
