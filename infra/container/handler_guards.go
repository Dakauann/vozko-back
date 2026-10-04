package container

import (
	"vozko/delivery/http/handlers"
	whatsappbusinessphonehttp "vozko/delivery/http/whatsappbusinessphone"
	wc_usecase "vozko/usecases/whatsapp_campaign"
)

func withCampaignAccess(c *Container, h *handlers.WhatsAppCampaignHandler) *handlers.WhatsAppCampaignHandler {
	h.SetCampaignAccess(c.useCases.wcCampaignAccess)
	h.SetStartCampaign(c.useCases.startWCCampaign)
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
