package container

import (
	"context"
	"log"
	"time"

	smartcampaignhttp "vozko/delivery/http/smartcampaignhttp"
	balance_domain "vozko/domain/balance"
	wc_domain "vozko/domain/whatsapp_campaign"
	wcw_repository "vozko/infra/repositories/whatsapp_campaign_wave"
	"vozko/usecases/campaignqueue"
	smartcampaign_usecase "vozko/usecases/smartcampaign"
	businessphone_usecase "vozko/usecases/whatsapp/business_phone"
	whatsapp_template_usecase "vozko/usecases/whatsapp/template"
	wc_usecase "vozko/usecases/whatsapp_campaign"
)

type smartCampaignWiring struct {
	entries      *smartcampaign_usecase.ObservedEntries
	engine       *smartcampaign_usecase.Engine
	configurator *smartcampaign_usecase.Configurator
	signals      *smartcampaign_usecase.Signals
}

func (c *Container) observedCampaignEntries() *smartcampaign_usecase.ObservedEntries {
	if c.smartCampaigns.entries == nil {
		c.smartCampaigns.entries = smartcampaign_usecase.ObserveFailures(c.repositories.wcEntry)
	}
	return c.smartCampaigns.entries
}

func (c *Container) wireSmartCampaigns(consumer wc_domain.MessageConsumerUseCase, dispatch wc_domain.DispatchCampaignUseCase, dashboardURL string) {
	caps, ok := c.repositories.monthlySendCaps.(balance_domain.MonthlySendCapUsageReader)
	if !ok {
		log.Fatalf("[smart-campaign] the monthly send cap repository cannot read what is left of the cap")
	}
	consumerSetter, ok := consumer.(interface {
		SetSmartCampaigns(wc_usecase.SmartCampaigns)
	})
	if !ok {
		log.Fatalf("[smart-campaign] the campaign consumer cannot leave completion to the wave engine")
	}
	dispatchSetter, ok := dispatch.(interface {
		SetSmartCampaigns(wc_usecase.SmartStarter)
	})
	if !ok {
		log.Fatalf("[smart-campaign] the campaign dispatch cannot hand starts to the wave engine")
	}

	waves := wcw_repository.NewRepository(c.db)
	probe := businessphone_usecase.NewPollPhoneQualityUseCase(c.repositories.businessPhone, c.services.businessPhoneMetaAPI,
		c.repositories.messageStatusSending, c.repositories.whatsappQualityHistory, c.repositories.whatsappQuality)
	engine := smartcampaign_usecase.NewEngine(smartcampaign_usecase.Deps{
		Campaigns: c.repositories.wcCampaign,
		Entries:   c.repositories.wcEntry,
		Policies:  waves,
		States:    waves,
		Waves:     waves,
		Health:    smartcampaign_usecase.NewHealthReader(c.repositories.whatsappTemplate, c.repositories.businessPhone, c.repositories.whatsappQualityTimeline, probe),
		Consumer:  consumer,
		Queue:     campaignqueue.NewDispatcher(c.services.wcQueuePub, c.redisProvider.SharedState(), wc_domain.QueueNamespace),
		Caps:      caps,
		Notifier:  smartcampaign_usecase.NewHoldNotice(c.notifications().Publisher, dashboardURL),
		Locks:     c.redisProvider.SharedState(),
		Now:       time.Now,
	})

	c.observedCampaignEntries().Attach(engine)
	consumerSetter.SetSmartCampaigns(engine)
	dispatchSetter.SetSmartCampaigns(engine)
	c.smartCampaigns.engine = engine
	c.smartCampaigns.configurator = smartcampaign_usecase.NewConfigurator(waves, c.repositories.workspaceConfig, c.repositories.workspaceDepartment)
	c.smartCampaigns.signals = smartcampaign_usecase.NewSignals(c.repositories.whatsappTemplate, engine)
}

func (c *Container) signalTemplateHealth(webhook interface{}) {
	signalling, ok := webhook.(whatsapp_template_usecase.HealthSignalling)
	if !ok {
		log.Fatalf("[smart-campaign] the template webhook cannot report health changes")
	}
	signalling.SetHealthSignals(c.smartCampaigns.signals)
}

func (c *Container) smartCampaignTick(ctx context.Context) error {
	return c.smartCampaigns.engine.Tick(ctx)
}

func (c *Container) signallingPhones() *smartcampaign_usecase.SignallingPhones {
	return smartcampaign_usecase.SignalPhoneChanges(c.repositories.businessPhone, c.smartCampaigns.signals)
}

func (c *Container) smartCampaignHandler() *smartcampaignhttp.Handler {
	return smartcampaignhttp.NewHandler(c.useCases.wcCampaignAccess, c.smartCampaigns.engine, c.smartCampaigns.configurator)
}

func (c *Container) createWithWavePolicy(create wc_domain.CreateCampaignUseCase, remove wc_domain.DeleteCampaignUseCase) wc_domain.CreateCampaignUseCase {
	return smartcampaign_usecase.CreateWithPolicy(create, c.smartCampaigns.configurator, remove)
}
