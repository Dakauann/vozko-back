package container

import (
	"context"
	"log"
	"time"

	carriercampaignhttp "vozko/delivery/http/carriercampaign"
	cc "vozko/domain/carriercampaign"
	cronPackage "vozko/infra/cron"
	carriercampaign_repository "vozko/infra/repositories/carriercampaign"
	carriermessaging_repository "vozko/infra/repositories/carriermessaging"
	"vozko/usecases/campaignlive"
	"vozko/usecases/campaignqueue"
	ccuc "vozko/usecases/carriercampaign"
	cmuc "vozko/usecases/carriermessaging"
)

const (
	carrierCampaignNamespace    = "carrier_campaign"
	carrierCampaignPauseRequeue = time.Minute
	carrierCampaignScheduleSize = 100
)

type carrierCampaignBundle struct {
	Service *ccuc.Service
	Runner  *campaignqueue.Runner
	Handler *carriercampaignhttp.Handler
}

func (c *Container) initCarrierCampaigns() {
	carrier := c.carrier
	if carrier == nil || carrier.Service == nil {
		log.Fatalf("[carrier-campaigns] the RCS and SMS service must be built first")
	}
	if c.services.wcQueuePub == nil || c.services.wcQueueSub == nil || c.redisProvider == nil {
		log.Fatalf("[carrier-campaigns] the campaign queue and redis must exist first")
	}
	timeline, err := campaignlive.NewTimeline(campaignlive.TimelineDeps{
		Namespace: carrierCampaignNamespace,
		Source:    carriercampaign_repository.NewTimelineReader(c.db),
		Locator:   c.attendanceScheduleResolver(),
		Memo:      c.analyticsMemo(),
		Gate:      c.sharedAnalyticsGate(),
	})
	if err != nil {
		log.Fatalf("[carrier-campaigns] timeline: %v", err)
	}
	funnels := carriercampaign_repository.NewFunnelReader(c.db)
	service, err := ccuc.New(ccuc.Deps{
		Campaigns:     carriercampaign_repository.NewCampaignRepository(c.db),
		Runs:          carriercampaign_repository.NewRunRepository(c.db),
		Recipients:    carriercampaign_repository.NewRecipientRepository(c.db),
		Funnels:       funnels,
		Selections:    c.leadSelection(),
		Leads:         c.repositories.lead,
		Contacts:      c.repositories.lead,
		Names:         c.actorNames(),
		Definitions:   c.repositories.customField,
		Suppressions:  carriermessaging_repository.NewSuppressionRepository(c.db),
		Senders:       carriermessaging_repository.NewSenderRepository(c.db),
		Routes:        carriermessaging_repository.NewRouteRepository(c.db),
		Messages:      carrier.Messages,
		Carrier:       carrier.Service,
		SenderPauser:  carrier.Service,
		Settings:      carrier.Service,
		Prices:        carrier.Service.Charges,
		Balance:       c.services.cachedBalanceChecker,
		Spend:         carrier.Spend,
		Enqueuer:      campaignqueue.NewDispatcher(c.services.wcQueuePub, c.redisProvider.SharedState(), cc.QueueNamespace),
		Attempts:      c.redisProvider.SharedState(),
		Stats:         funnels,
		Timeline:      timeline,
		RecipientRows: carriercampaign_repository.NewRecipientRowReader(c.db),
		Overview:      carrier.Overview,
		WhatsAppSpend: carriercampaign_repository.NewWhatsAppSpendReader(c.db),
		Timezones:     cmuc.NewWorkspaceTimezones(c.repositories.workspaceConfig),
		Logf: func(format string, args ...any) {
			log.Printf("[carrier-campaigns] "+format, args...)
		},
	})
	if err != nil {
		log.Fatalf("[carrier-campaigns] service: %v", err)
	}
	runner := campaignqueue.New(
		c.services.wcQueueSub, c.services.wcQueuePub, c.redisProvider.SharedState(),
		service, service,
		campaignqueue.Config{
			Namespace:         cc.QueueNamespace,
			PauseRequeueDelay: carrierCampaignPauseRequeue,
			Logf: func(format string, args ...any) {
				log.Printf("[carrier-campaigns] "+format, args...)
			},
		},
		service.Handle,
	)
	service.AttachQueue(runner)
	service.SetAudience(c.campaignAudience())
	c.campaignAudience().AddChannel(service.AudienceChannel())
	if err := runner.Start(); err != nil {
		log.Fatalf("[carrier-campaigns] failed to start the queue runner: %v", err)
	}
	c.carrierCampaigns = &carrierCampaignBundle{
		Service: service,
		Runner:  runner,
		Handler: carriercampaignhttp.NewHandler(service),
	}
}

func (c *Container) carrierJobs() (expire, syncSenders, schedules func(context.Context) error) {
	if c.carrier == nil || c.carrierCampaigns == nil {
		log.Fatalf("[carrier] jobs requested before RCS and SMS were built")
	}
	messaging, campaigns := c.carrier.Service, c.carrierCampaigns.Service
	expire = func(ctx context.Context) error {
		_, err := messaging.ExpireOverdue(ctx, carrierExpiryBatch)
		return err
	}
	syncSenders = func(ctx context.Context) error {
		_, err := messaging.SyncSenders(ctx, carrierSenderSyncSize)
		return err
	}
	schedules = func(ctx context.Context) error {
		_, err := campaigns.RunSchedules(ctx, carrierCampaignScheduleSize)
		return err
	}
	return expire, syncSenders, schedules
}

func (c *Container) registerCarrierJobs() {
	expire, syncSenders, schedules := c.carrierJobs()
	c.jobRunner.SetCarrierJobs(cronPackage.CtxJobFunc(expire), cronPackage.CtxJobFunc(syncSenders), cronPackage.CtxJobFunc(schedules))
}
