package container

import (
	"log"

	opportunity_repository "vozko/infra/repositories/opportunity"
	lead_usecase "vozko/usecases/lead"
)

func (c *Container) leadTimeline() *lead_usecase.Timeline {
	if c.services.conversationAuthImpl == nil || c.useCases.personDeals == nil || c.callRouting.TransferLog == nil {
		log.Fatalf("[leads] the authorizer, the deal scope and the call transfers must be wired before the lead timeline")
	}
	timeline, err := lead_usecase.NewTimeline(lead_usecase.TimelineDeps{
		Leads:       c.repositories.lead,
		Source:      c.repositories.lead,
		Deals:       opportunity_repository.NewLeadDealReader(c.db),
		Access:      c.services.conversationAuthImpl,
		Permissions: c.services.conversationAuthImpl,
		Definitions: c.repositories.customField,
		CallAccess:  c.useCases.checkWsAccess,
		Transfers:   c.callRouting.TransferLog,
		DealScopes:  c.useCases.personDeals,
		Names:       c.actorNames(),
	})
	if err != nil {
		log.Fatalf("[leads] %v", err)
	}
	return timeline
}
