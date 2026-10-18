package container

import (
	"log"

	lead_usecase "vozko/usecases/lead"
)

func (c *Container) leadConsent() *lead_usecase.ConsentCommands {
	if c.repositories.lead == nil {
		log.Fatalf("[leads] the lead repository must be wired before consent can be recorded")
	}
	consent, err := lead_usecase.NewConsentCommands(lead_usecase.ConsentDeps{
		Store:    c.repositories.lead,
		Notifier: c.leadChangeNotifier(),
	})
	if err != nil {
		log.Fatalf("[leads] %v", err)
	}
	return consent
}
