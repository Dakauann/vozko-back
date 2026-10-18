package container

import (
	"log"

	"vozko/domain/tools"
	"vozko/usecases/agentturn"
	tools_usecase "vozko/usecases/tools"
)

func (c *Container) consentToolHandlers() []tools.Handler {
	if c.repositories.conversation == nil {
		log.Fatalf("[consent-tools] the conversation message repository must be wired before agents can cite messages")
	}
	consent := c.leadConsent()
	optOut, err := tools_usecase.NewRecordOptOutTool(consent, c.repositories.conversation)
	if err != nil {
		log.Fatalf("[consent-tools] record_opt_out: %v", err)
	}
	grant, err := tools_usecase.NewRecordConsentTool(consent, c.repositories.conversation)
	if err != nil {
		log.Fatalf("[consent-tools] record_consent: %v", err)
	}
	return []tools.Handler{optOut, grant}
}

func (c *Container) citeConsentMessages(assembler *agentturn.Assembler) {
	if c.repositories.conversation == nil {
		log.Fatalf("[consent-tools] the conversation message repository must be wired before agents can cite messages")
	}
	assembler.CiteMessagesFrom(c.repositories.conversation)
}
