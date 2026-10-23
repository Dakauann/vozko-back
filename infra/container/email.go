package container

import (
	"log"

	deliveryHttp "vozko/delivery/http"
	"vozko/delivery/http/emailapihttp"
	emailhygienehttp "vozko/delivery/http/emailhygiene"
	emailoutreachhttp "vozko/delivery/http/emailoutreach"
	"vozko/delivery/http/emailpricing"
	cronPackage "vozko/infra/cron"
	balance_usecase "vozko/usecases/balance"
	workspace_pricing_usecase "vozko/usecases/workspace_pricing"
)

type emailBundle struct {
	Sending   *emailSendingBundle
	Hygiene   *emailHygieneBundle
	Campaigns *emailCampaignBundle
	Billing   *balance_usecase.ConsumeEmailUseCase
	Prices    *balance_usecase.EmailPriceList
	Pricing   *emailpricing.Handler
	Outreach  *emailoutreachhttp.Handler
}

func (c *Container) initEmail() {
	if c.repositories == nil || c.repositories.balance == nil || c.repositories.workspacePricing == nil {
		log.Fatalf("[email] the balance and pricing repositories must exist first")
	}
	if c.services == nil || c.services.workspacePricer == nil || c.useCases == nil || c.useCases.ensureActiveWorkspaceSubscription == nil {
		log.Fatalf("[email] the pricer and the subscription checker must exist first")
	}
	billing := balance_usecase.NewConsumeEmailUseCase(c.repositories.balance, c.services.workspacePricer, c.useCases.ensureActiveWorkspaceSubscription)
	sending := c.buildEmailSending()
	hygiene := c.emailHygiene()
	campaigns := c.buildEmailCampaigns(sending, hygiene, billing)
	rates := workspace_pricing_usecase.NewGetExchangeRateUseCase(c.repositories.workspacePricing)
	prices := balance_usecase.NewEmailPriceList(c.services.workspacePricer, rates)
	c.email = &emailBundle{
		Sending:   sending,
		Hygiene:   hygiene,
		Campaigns: campaigns,
		Billing:   billing,
		Prices:    prices,
		Pricing:   emailpricing.NewHandler(prices),
	}
	c.startEmailSendingConsumer(sending)
	c.startEmailHygiene(hygiene)
	c.startEmailCampaignConsumer(campaigns)
}

func (c *Container) registerEmailJobs() {
	if c.email == nil || c.email.Sending == nil || c.email.Campaigns == nil || c.email.Hygiene == nil {
		log.Fatalf("[email] jobs requested before email was built")
	}
	m := c.email.Sending.Module
	c.jobRunner.SetEmailSendingJobs(m.Verification, m.Promotion, m.Deliverability)
	c.jobRunner.SetEmailTenantRetirementJob(m.Dormancy)
	c.jobRunner.SetEmailUnusedReleaseJob(m.UnusedRelease)
	c.jobRunner.SetEmailReplySweepJob(m.ReplySweep)
	c.registerEmailAPIJobs()
	c.jobRunner.SetEmailSESLimitJobs(m.SendQuota, m.Capacity, m.HealthSweep)
	campaigns := c.email.Campaigns.Campaigns
	c.jobRunner.SetEmailCampaignJobs(cronPackage.CtxJobFunc(campaigns.StartScheduled), cronPackage.CtxJobFunc(campaigns.Tick))
	hygiene := c.email.Hygiene
	c.jobRunner.SetEmailHygieneJobs(hygiene.Domains, hygiene.Disposable, hygiene.Pruner)
}

func emailRoutes(c *Container) deliveryHttp.EmailRoutes {
	if c.email == nil || c.email.Sending == nil || c.email.Campaigns == nil || c.email.Pricing == nil || c.email.Outreach == nil || c.email.Hygiene == nil {
		log.Fatalf("[email] routes requested before email was built")
	}
	return deliveryHttp.EmailRoutes{
		Sending:      c.email.Sending.Handler,
		SendingAdmin: c.email.Sending.Admin,
		Webhooks:     c.email.Sending.Webhooks,
		Campaigns:    c.email.Campaigns.Handler,
		Consent:      c.email.Campaigns.ConsentHandler,
		Public:       c.email.Campaigns.Public,
		Pricing:      c.email.Pricing,
		Inboxes:      emailInboxHandler(c.emailInbox),
		Outreach:     c.email.Outreach,
		API:          emailAPIHandler(c),
		Hygiene:      emailhygienehttp.NewHandler(c.email.Hygiene.Service),
	}
}

func emailAPIHandler(c *Container) *emailapihttp.Handler {
	if c.emailAPI == nil {
		log.Fatalf("[email-api] routes requested before the email API was built")
	}
	return c.emailAPI.Handler
}
