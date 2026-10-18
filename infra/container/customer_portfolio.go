package container

import (
	"log"

	customerportfoliohttp "vozko/delivery/http/customerportfolio"
	customerportfolio_repository "vozko/infra/repositories/customerportfolio"
	customerportfolio_usecase "vozko/usecases/customerportfolio"
)

func (c *Container) customerPortfolioEntries() *customerportfolio_repository.Store {
	if c.customerPortfolioStore == nil {
		c.customerPortfolioStore = customerportfolio_repository.New(c.db)
	}
	return c.customerPortfolioStore
}

func (c *Container) customerPortfolio() *customerportfolio_usecase.Service {
	if c.customerPortfolioService != nil {
		return c.customerPortfolioService
	}
	if c.services.conversationAuthImpl == nil || c.useCases.memberVisibility == nil || c.repositories.lead == nil {
		log.Fatalf("[customer-portfolio] the authorizer, member visibility and leads must be wired before the carteira")
	}
	store := c.customerPortfolioEntries()
	service, err := customerportfolio_usecase.New(customerportfolio_usecase.Deps{
		Entries:     store,
		Contacts:    store,
		EntryLeads:  c.repositories.lead,
		Permissions: c.services.conversationAuthImpl,
		Receivers:   c.services.conversationAuthImpl,
		Access:      c.services.conversationAuthImpl,
		Reach:       c.useCases.memberVisibility,
		Names:       c.actorNames(),
	})
	if err != nil {
		log.Fatalf("[customer-portfolio] %v", err)
	}
	c.customerPortfolioService = service
	return service
}

func (c *Container) customerPortfolioHandler() *customerportfoliohttp.Handler {
	return customerportfoliohttp.NewHandler(c.customerPortfolio())
}
