package container

import (
	"log"

	"vozko/domain/cep"
	lead_usecase "vozko/usecases/lead"
)

type leadProfileBundle struct {
	service *lead_usecase.Profiles
}

func (c *Container) initLeadProfiles(cepSearch cep.CEPSearchUseCase) *lead_usecase.Profiles {
	service, err := lead_usecase.NewProfiles(lead_usecase.ProfileDeps{
		Store:       c.repositories.lead,
		Notifier:    c.leadChangeNotifier(),
		Definitions: c.repositories.customField,
		EntryLeads:  c.repositories.lead,
		CEP:         cepSearch,
	})
	if err != nil {
		log.Fatalf("[lead-profiles] %v", err)
	}
	c.leadProfileBundle = &leadProfileBundle{service: service}
	return service
}

func (c *Container) leadProfiles() *lead_usecase.Profiles {
	if c.leadProfileBundle == nil {
		log.Fatalf("[lead-profiles] lead profiles must be built with the use cases before anything uses them")
	}
	return c.leadProfileBundle.service
}
