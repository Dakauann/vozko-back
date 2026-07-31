package container

import (
	cronPackage "vozko/infra/cron"
	conversation_usecase "vozko/usecases/conversation"
	whatsapp_campaign_usecase "vozko/usecases/whatsapp_campaign"
)

func (c *Container) initJobRunner() {
	cron_job := cronPackage.NewOrderCleanupJob(c.useCases.cancelExpiredOrder)
	startScheduledWhatsappCampaignsJob := whatsapp_campaign_usecase.NewScheduleStartJob(c.useCases.dispatchWCCampaign, c.repositories.wcCampaign)
	analysisDebounceJob := conversation_usecase.NewAnalysisDebounceJob(
		c.redisProvider.SharedState(),
		c.repositories.conversation,
		c.repositories.wcEntry,
		c.repositories.wcCampaign,
		c.repositories.lead,
		c.services.ai,
		c.services.toolRegistry,
		c.repositories.analysis,
		c.repositories.stage,
		c.services.conversationHub,
		c.services.cachedBalanceChecker,
	)
	autoCloseJob := conversation_usecase.NewAutoCloseJob(
		c.repositories.wcEntry,
		c.services.conversationStatusUpdater,
	)
	c.jobRunner = cronPackage.NewJobRunner(cron_job, startScheduledWhatsappCampaignsJob, c.redisProvider.SharedState(), analysisDebounceJob, autoCloseJob, c.useCases.workflowManager, c.useCases.expireSubscriptions, c.useCases.remindExpiringSubscriptions, c.useCases.monitorLowBalance, c.useCases.renewCalendarChannels, c.useCases.reconcileWhatsAppTemplates, c.useCases.reconcileWhatsAppEntitlements, c.useCases.emitMonthlyInvoices, c.useCases.cancelBillingSweep, c.useCases.vendorChannelReconciler, c.useCases.channelStatusReconciler, c.useCases.purgeShortLinkClicks)

	// Instagram jobs are optional and registered after construction, so the
	// channel can be absent without touching the constructor.
	if c.instagram != nil && c.instagram.Enabled {
		c.jobRunner.SetInstagramJobs(c.instagram.RefreshTokens, c.instagram.PurgeEvents)
	}
}
