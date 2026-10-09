package container

import (
	"log"

	lead_usecase "vozko/usecases/lead"
	savedview_usecase "vozko/usecases/savedview"
)

func (c *Container) savedViewAccess() savedview_usecase.Access {
	if c.services.conversationAuth == nil {
		log.Fatalf("[saved-views] the authorizer must be wired before saved views")
	}
	checks, err := lead_usecase.NewFilterChecks(c.services.conversationAuth, c.repositories.customField)
	if err != nil {
		log.Fatalf("[saved-views] %v", err)
	}
	return savedview_usecase.Access{Permissions: c.services.conversationAuth, LeadFilters: checks}
}
