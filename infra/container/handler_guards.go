package container

import (
	"vozko/delivery/http/handlers"
	whatsappbusinessphonehttp "vozko/delivery/http/whatsappbusinessphone"
)

func withCampaignAccess(c *Container, h *handlers.WhatsAppCampaignHandler) *handlers.WhatsAppCampaignHandler {
	h.SetCampaignAccess(c.useCases.wcCampaignAccess)
	h.SetStartCampaign(c.useCases.startWCCampaign)
	return h
}

func withWorkspacePhones(c *Container, h *whatsappbusinessphonehttp.WhatsAppBusinessPhoneHandler) *whatsappbusinessphonehttp.WhatsAppBusinessPhoneHandler {
	h.SetWorkspacePhones(c.useCases.workspacePhones)
	return h
}
