package container

import (
	"log"

	emailcampaignhttp "vozko/delivery/http/emailcampaign"
	"vozko/infra/emailwiring"
	balance_usecase "vozko/usecases/balance"
	emailcampaign_usecase "vozko/usecases/emailcampaign"
	emailconsent_usecase "vozko/usecases/emailconsent"
	workspace_pricing_usecase "vozko/usecases/workspace_pricing"
)

type emailCampaignBundle struct {
	Campaigns      *emailcampaign_usecase.Service
	Consent        *emailconsent_usecase.Service
	Handler        *emailcampaignhttp.Handler
	ConsentHandler *emailcampaignhttp.ConsentHandler
	Public         *emailcampaignhttp.PublicHandler
}

func (c *Container) buildEmailCampaigns(sending *emailSendingBundle, hygiene *emailHygieneBundle, billing *balance_usecase.ConsumeEmailUseCase) *emailCampaignBundle {
	if hygiene == nil {
		log.Fatalf("[email-campaign] email hygiene must be built first")
	}
	if sending == nil || sending.Module == nil || sending.Codec == nil {
		log.Fatalf("[email-campaign] email sending must be built first")
	}
	if billing == nil {
		log.Fatalf("[email-campaign] email billing must be built first")
	}
	if c.db == nil || c.repositories == nil || c.repositories.lead == nil || c.repositories.balance == nil {
		log.Fatalf("[email-campaign] the database, the lead and the balance repositories must exist first")
	}
	if c.services == nil || c.services.wcQueuePub == nil || c.services.wcQueueSub == nil || c.redisProvider == nil {
		log.Fatalf("[email-campaign] the campaign queue and redis must exist first")
	}
	if c.useCases == nil || c.useCases.ensureActiveWorkspaceSubscription == nil {
		log.Fatalf("[email-campaign] the subscription checker must exist first")
	}
	built, err := emailwiring.NewCampaigns(c.db, emailwiring.CampaignAdapters{
		Sending:       sending.Module,
		Codec:         sending.Codec,
		Billing:       billing,
		Selection:     c.leadSelection(),
		Leads:         c.repositories.lead,
		Wallet:        c.repositories.balance,
		Rates:         workspace_pricing_usecase.NewGetExchangeRateUseCase(c.repositories.workspacePricing),
		QueuePub:      c.services.wcQueuePub,
		QueueSub:      c.services.wcQueueSub,
		Shared:        c.redisProvider.SharedState(),
		Memo:          c.analyticsMemo(),
		Notifications: c.notifications().Publisher,
		PublicBaseURL: c.cfg.Email.PublicBaseURL,
		Audience:      c.campaignAudience(),
		Hygiene:       hygiene.Service,
	})
	if err != nil {
		log.Fatalf("[email-campaign] %v", err)
	}
	return &emailCampaignBundle{
		Campaigns:      built.Service,
		Consent:        built.Consent,
		Handler:        emailcampaignhttp.NewHandler(built.Service),
		ConsentHandler: emailcampaignhttp.NewConsentHandler(built.Consent),
		Public:         emailcampaignhttp.NewPublicHandler(built.Consent),
	}
}

func (c *Container) startEmailCampaignConsumer(b *emailCampaignBundle) {
	if b == nil || b.Campaigns == nil {
		log.Fatalf("[email-campaign] the campaign consumer was not built")
	}
	if err := b.Campaigns.StartConsumer(); err != nil {
		log.Fatalf("[email-campaign] failed to start the campaign consumer: %v", err)
	}
}
