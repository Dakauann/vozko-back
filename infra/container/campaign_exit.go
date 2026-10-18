package container

import (
	"log"

	campaignexithttp "vozko/delivery/http/campaignexit"
	lead_repository "vozko/infra/repositories/lead"
	campaignexit_usecase "vozko/usecases/campaignexit"
	wc_usecase "vozko/usecases/whatsapp_campaign"
)

func (c *Container) wireCampaignExit() {
	updating, ok := c.useCases.updateWCCampaign.(interface {
		SetExitRiskGate(wc_usecase.ExitRiskGate)
	})
	if !ok {
		log.Fatalf("[campaign-exit] the official campaign update cannot take the exit risk gate")
	}
	updating.SetExitRiskGate(c.buttonActions())
}

func (c *Container) campaignExitHandler() *campaignexithttp.Handler {
	service, err := campaignexit_usecase.NewService(campaignexit_usecase.Deps{
		Facts:    lead_repository.NewSendFacts(c.db),
		Memo:     c.analyticsMemo(),
		Gate:     c.sharedAnalyticsGate(),
		Versions: c.leadGenerations(),
	})
	if err != nil {
		log.Fatalf("[campaign-exit] %v", err)
	}
	return campaignexithttp.NewHandler(service)
}
