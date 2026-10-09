package container

import (
	"log"

	geocoding_repository "vozko/infra/repositories/geocoding"
	georef_repository "vozko/infra/repositories/georef"
	lead_repository "vozko/infra/repositories/lead"
	geocoding_usecase "vozko/usecases/geocoding"
)

func (c *Container) geocodingSweeper() *geocoding_usecase.Sweeper {
	if c.cfg.GeocodingSweeperDisabled {
		log.Printf("[geocoding] the sweeper is switched off by GEOCODING_SWEEPER_DISABLED")
		return nil
	}
	if c.services.metrics == nil {
		log.Fatalf("[geocoding] metrics must be wired before the geocoding sweeper")
	}
	chain, err := geocoding_usecase.NewChain(geocoding_usecase.ChainDeps{Reference: georef_repository.NewReference(c.db)})
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

func (c *Container) geocodingPlatformCoverage() *geocoding_usecase.PlatformCoverage {
	if c.redisProvider == nil {
		log.Printf("[geocoding] no shared state: the platform address coverage view answers 503")
		return nil
	}
	platform := geocoding_repository.NewPlatformReader(c.db)
	service, err := geocoding_usecase.NewPlatformCoverage(geocoding_usecase.PlatformCoverageDeps{
		Directory: platform,
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
