package container

import (
	"log"

	"vozko/delivery/http/handlers"
	whatsappbusinessphonehttp "vozko/delivery/http/whatsappbusinessphone"
	wc_domain "vozko/domain/whatsapp_campaign"
	wc_usecase "vozko/usecases/whatsapp_campaign"
)

func withCampaignAccess(c *Container, h *handlers.WhatsAppCampaignHandler) *handlers.WhatsAppCampaignHandler {
	h.SetCampaignAccess(c.useCases.wcCampaignAccess)
	start, ok := c.useCases.startWCCampaign.(wc_domain.ConfirmedStartUseCase)
	if !ok {
		log.Fatalf("[launch-check] the official campaign start cannot carry the confirmed launch check")
	}
	h.SetStartCampaign(start)
	return h
}

func withWorkspacePhones(c *Container, h *whatsappbusinessphonehttp.WhatsAppBusinessPhoneHandler) *whatsappbusinessphonehttp.WhatsAppBusinessPhoneHandler {
	h.SetWorkspacePhones(c.useCases.workspacePhones)
	h.SetReceptive(c.numberReceptive())
	return h
}

func (c *Container) numberReceptive() *wc_usecase.NumberReceptive {
	return wc_usecase.NewNumberReceptive(c.repositories.businessPhone, c.repositories.wcCampaign, c.useCases.ensureReceptiveContainer)
}
