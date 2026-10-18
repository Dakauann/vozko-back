package container

import (
	"log"

	wc_usecase "vozko/usecases/whatsapp_campaign"
)

func (c *Container) grantTemplatesOnUpdate(update any) {
	granting, ok := update.(interface {
		SetTemplateGrants(wc_usecase.TemplateGrants)
	})
	if !ok {
		log.Fatalf("[campaigns] the official campaign update cannot check template grants")
	}
	granting.SetTemplateGrants(c.repositories.workspaceTemplateAccess)
}
