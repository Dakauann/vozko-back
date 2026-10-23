package container

import (
	"log"

	deliveryHttp "vozko/delivery/http"
	"vozko/delivery/http/campaigndrafthttp"
	"vozko/delivery/http/handlers"
	uwhttp "vozko/delivery/http/unofficial_whatsapp"
	"vozko/domain/campaigndraft"
	wc_domain "vozko/domain/whatsapp_campaign"
	campaigndraft_repository "vozko/infra/repositories/campaigndraft"
	campaignaudience_usecase "vozko/usecases/campaignaudience"
	campaigndraft_usecase "vozko/usecases/campaigndraft"
	uwcuc "vozko/usecases/unofficial_whatsapp_campaign"
	wc_usecase "vozko/usecases/whatsapp_campaign"
)

func (c *Container) officialCampaignCreate() wc_domain.CreateCampaignUseCase {
	if c.officialCampaignCreateUseCase == nil {
		c.officialCampaignCreateUseCase = c.createWithWavePolicy(campaignaudience_usecase.OfficialCreate(c.useCases.createWCCampaign, c.campaignAudience()), c.useCases.deleteWCCampaign)
	}
	return c.officialCampaignCreateUseCase
}

func campaignDraftRoutes(c *Container) deliveryHttp.CampaignDraftRoutes {
	drafts, err := campaigndraft_usecase.New(campaigndraft_repository.NewRepository(c.db))
	if err != nil {
		log.Fatalf("[campaign-drafts] %v", err)
	}
	if c.handlers.whatsappCampaign == nil || c.useCases.dealAutomation == nil {
		log.Fatalf("[campaign-drafts] the official campaign handler and deal automation must be built before campaign drafts")
	}
	start, ok := c.useCases.startWCCampaign.(wc_domain.ConfirmedStartUseCase)
	if !ok {
		log.Fatalf("[campaign-drafts] the official campaign start cannot carry the confirmed launch check")
	}
	official := wc_usecase.NewDraftLaunch(wc_usecase.DraftLaunchDeps{
		Create:   c.officialCampaignCreate(),
		Start:    start,
		Check:    c.launchCheck(),
		Audience: campaignaudience_usecase.NewOfficialCounter(c.campaignAudience()),
		Rates:    c.walletRate(),
	})
	routes := deliveryHttp.CampaignDraftRoutes{
		WhatsApp: campaigndrafthttp.NewHandler(drafts, campaigndraft.ChannelWhatsApp,
			handlers.NewWhatsAppDraftFinisher(c.handlers.whatsappCampaign, official, c.useCases.dealAutomation, c.buttonActions())),
	}
	if finisher := unofficialDraftFinisher(c); finisher != nil {
		routes.Unofficial = campaigndrafthttp.NewHandler(drafts, campaigndraft.ChannelUnofficialWhatsApp, finisher)
	}
	return routes
}

func unofficialDraftFinisher(c *Container) *uwhttp.DraftFinisher {
	bundle := c.unofficialWhatsAppCampaigns
	if bundle == nil || !bundle.Enabled || bundle.Handler == nil {
		return nil
	}
	launch := uwcuc.NewDraftLaunch(uwcuc.DraftLaunchDeps{
		Create:    bundle.CreateFlow,
		Actions:   bundle.Actions,
		Estimate:  bundle.Estimate,
		Audience:  campaignaudience_usecase.NewUnofficialCounter(c.campaignAudience()),
		Preflight: bundle.Preflight,
	})
	return uwhttp.NewDraftFinisher(bundle.Handler, launch, c.useCases.dealAutomation)
}
