package container

import (
	"log"
	"time"

	"vozko/delivery/http/campaignlaunchhttp"
	balance_domain "vozko/domain/balance"
	wc_usecase "vozko/usecases/whatsapp_campaign"
)

func (c *Container) launchCheck() *wc_usecase.LaunchCheck {
	if c.launchCheckService != nil {
		return c.launchCheckService
	}
	caps, ok := c.repositories.monthlySendCaps.(balance_domain.MonthlySendCapUsageReader)
	if !ok {
		log.Fatalf("[launch-check] the monthly send cap repository cannot read what is left of the cap")
	}
	if c.useCases.consumeWhatsappTemplate == nil || c.services.cachedBalanceChecker == nil || c.repositories.whatsappQualityTimeline == nil {
		log.Fatalf("[launch-check] the template costs, the balance and the quality timeline must be wired before the launch check")
	}
	c.launchCheckService = wc_usecase.NewLaunchCheck(wc_usecase.LaunchCheckDeps{
		Campaigns:      c.repositories.wcCampaign,
		Counts:         c.repositories.wcEntry,
		Failures:       c.repositories.wcDispatchReport,
		Templates:      c.repositories.whatsappTemplate,
		Quality:        c.repositories.whatsappQualityTimeline,
		Phones:         c.repositories.businessPhone,
		PhoneAccess:    c.repositories.workspacePhoneAccess,
		TemplateGrants: c.repositories.workspaceTemplateAccess,
		Exits:          c.buttonActions(),
		Costs:          c.useCases.consumeWhatsappTemplate,
		Balances:       c.services.cachedBalanceChecker,
		Caps:           caps,
		Now:            time.Now,
	})
	return c.launchCheckService
}

func (c *Container) wireLaunchCheck() {
	gated, ok := c.useCases.dispatchWCCampaign.(interface {
		SetLaunchGate(wc_usecase.LaunchGate)
	})
	if !ok {
		log.Fatalf("[launch-check] the official campaign dispatch cannot take the launch gate")
	}
	gated.SetLaunchGate(c.launchCheck())
}

func (c *Container) campaignLaunchHandler() *campaignlaunchhttp.Handler {
	return campaignlaunchhttp.NewHandler(c.useCases.wcCampaignAccess, c.launchCheck())
}
