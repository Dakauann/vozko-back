package container

import (
	"log"

	lead_usecase "vozko/usecases/lead"
)

func (c *Container) leadLocations() *lead_usecase.Locations {
	if c.services.conversationAuthImpl == nil || c.repositories.conversation == nil {
		log.Fatalf("[leads] the authorizer and the message repository must be wired before lead locations")
	}
	locations, err := lead_usecase.NewLocations(lead_usecase.LocationDeps{
		Store:       c.repositories.lead,
		Permissions: c.services.conversationAuthImpl,
		Definitions: c.repositories.customField,
		Notifier:    c.leadChangeNotifier(),
		Messages:    c.repositories.conversation,
		Access:      c.services.conversationAuthImpl,
		EntryLeads:  c.repositories.lead,
	})
	if err != nil {
		log.Fatalf("[leads] %v", err)
	}
	return locations
}
