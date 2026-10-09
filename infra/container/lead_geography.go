package container

import (
	"log"

	leadhttp "vozko/delivery/http/lead"
	domainCache "vozko/domain/cache"
	georef_repository "vozko/infra/repositories/georef"
	lead_repository "vozko/infra/repositories/lead"
	leadarea_repository "vozko/infra/repositories/leadarea"
	geocoding_usecase "vozko/usecases/geocoding"
	lead_usecase "vozko/usecases/lead"
	leadarea_usecase "vozko/usecases/leadarea"
)

func (c *Container) leadGenerations() domainCache.Versions {
	if c.redisProvider == nil {
		log.Fatalf("[lead-map] the shared state must be wired before the lead map")
	}
	return lead_repository.NewAggregateGenerations(c.redisProvider.SharedState())
}

func (c *Container) leadAreas() *leadarea_usecase.Service {
	if c.leadAreaService != nil {
		return c.leadAreaService
	}
	if c.services.conversationAuthImpl == nil {
		log.Fatalf("[lead-areas] the authorizer must be wired before the lead areas")
	}
	areas, err := leadarea_usecase.New(leadarea_usecase.Deps{
		Areas:       leadarea_repository.NewRepository(c.db),
		Permissions: c.services.conversationAuthImpl,
	})
	if err != nil {
		log.Fatalf("[lead-areas] %v", err)
	}
	c.leadAreaService = areas
	return areas
}

func (c *Container) leadGeographyHandler() *leadhttp.GeographyHandler {
	areas := c.leadAreas()
	maps, err := lead_usecase.NewMapSections(lead_usecase.MapDeps{
		Reader:      lead_repository.NewMapReader(c.db),
		Areas:       areas,
		Leads:       c.repositories.lead,
		Contacts:    c.repositories.lead,
		Permissions: c.services.conversationAuthImpl,
		Definitions: c.repositories.customField,
		Zones:       c.attendanceScheduleResolver(),
		Caching: lead_usecase.SectionCaching{
			Memo:     c.analyticsMemo(),
			Gate:     c.sharedAnalyticsGate(),
			Versions: c.leadGenerations(),
			TTL:      lead_usecase.DefaultSectionTTL,
		},
	})
	if err != nil {
		log.Fatalf("[lead-map] %v", err)
	}
	return leadhttp.NewGeographyHandler(maps, areas, nil)
}

func (c *Container) leadReferencePointHandler() *leadhttp.ReferencePointHandler {
	points, err := geocoding_usecase.NewReferencePoints(georef_repository.NewReference(c.db), nil)
	if err != nil {
		log.Fatalf("[lead-map] %v", err)
	}
	places, err := geocoding_usecase.NewPlaceSuggestions(geocoding_usecase.PlaceSuggestionDeps{
		Index: georef_repository.NewPlaces(c.db),
		Memo:  c.analyticsMemo(),
		Gate:  c.sharedAnalyticsGate(),
	})
	if err != nil {
		log.Fatalf("[lead-places] %v", err)
	}
	return leadhttp.NewReferencePointHandler(points).WithPlaces(places)
}
