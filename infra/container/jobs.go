package container

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
	webhook_repository "vozko/infra/repositories/webhook"
	adsuc "vozko/usecases/advertising"
	webhook_usecase "vozko/usecases/webhook"

	"vozko/domain/shared"
	cronPackage "vozko/infra/cron"
	ia_repo "vozko/infra/repositories/inbox_assignment"
	workspace_config_repository "vozko/infra/repositories/workspace_config"
	conversation_usecase "vozko/usecases/conversation"
	ia_usecase "vozko/usecases/inbox_assignment"
	tools_usecase "vozko/usecases/tools"
	uwcuc "vozko/usecases/unofficial_whatsapp_campaign"
	whatsapp_campaign_usecase "vozko/usecases/whatsapp_campaign"
)

const reportRetentionSweepLimit = 500

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
		c.repositories.stage,
		c.useCases.listLeadMemories,
		c.services.conversationHub,
		c.services.cachedBalanceChecker,
	)
	channels := []analysisChannel{
		{shared.EntryTypeWhatsApp, conversation_usecase.NewWhatsAppAnalysisResolver(
			c.repositories.wcEntry, c.repositories.wcCampaign, c.repositories.lead)},
	}
	if c.instagram != nil && c.instagram.Enabled {
		channels = append(channels, analysisChannel{shared.EntryTypeInstagram, instagramAnalysisResolver(c.instagram)})
	}
	if c.telegram != nil && c.telegram.Enabled {
		channels = append(channels, analysisChannel{shared.EntryTypeTelegram, telegramAnalysisResolver(c.telegram)})
	}
	if c.facebook != nil && c.facebook.Enabled {
		channels = append(channels, analysisChannel{shared.EntryTypeFacebook, facebookAnalysisResolver(c.facebook)})
	}
	if c.webchat != nil {
		channels = append(channels, analysisChannel{shared.EntryTypeWebchat, webchatAnalysisResolver(c.webchat)})
	}
	if c.unofficialWhatsApp != nil && c.unofficialWhatsApp.Enabled {
		resolver := campaignAwareResolver(
			unofficialWhatsAppAnalysisResolver(c.unofficialWhatsApp),
			c.unofficialWhatsApp.Conversations,
			c.unofficialWhatsAppCampaigns,
		)
		channels = append(channels, analysisChannel{shared.EntryTypeUnofficialWhatsApp, resolver})
	}

	sinks := []analysisSubjectSink{c.services.liveSubjects}
	if setter, ok := analysisDebounceJob.(analysisSubjectSink); ok {
		sinks = append(sinks, setter)
	}

	if c.audience != nil && c.audience.ConversationAdapter != nil {
		c.audience.ConversationAdapter.SubjectContext = c.analysisSubjectContext
		sinks = append(sinks, adapterSink{c.audience.ConversationAdapter})
		if q, ok := analysisDebounceJob.(interface {
			SetAnalysisQueue(conversation_usecase.ConversationAnalysisEnqueuer)
		}); ok {
			q.SetAnalysisQueue(c.audience.ConversationAdapter)
		}
	}
	if p, ok := analysisDebounceJob.(interface {
		SetAnalysisDebouncePolicy(conversation_usecase.AnalysisDebouncePolicy)
	}); ok {
		p.SetAnalysisDebouncePolicy(workspace_config_repository.NewAudienceSettingsStore(c.db))
	}
	if d, ok := analysisDebounceJob.(interface {
		SetDealAutomation(conversation_usecase.DealAutomationSettings, tools_usecase.OpportunityManager)
	}); ok {
		d.SetDealAutomation(c.useCases.dealAutomation, c.useCases.opportunity)
	}
	if q, ok := analysisDebounceJob.(interface {
		SetQuietCascade(conversation_usecase.QuietCascade)
	}); ok {
		q.SetQuietCascade(c.services.liveGate)
	}
	if p, ok := analysisDebounceJob.(interface {
		SetProfileAgents(conversation_usecase.ProfileAgents)
	}); ok {
		p.SetProfileAgents(c.repositories.agent)
	}

	registerAnalysisChannels(channels, sinks...)

	autoCloseJob := conversation_usecase.NewAutoCloseJob(
		c.repositories.wcEntry,
		c.services.conversationStatusUpdater,
	)
	c.jobRunner = cronPackage.NewJobRunner(cron_job, startScheduledWhatsappCampaignsJob, c.redisProvider.SharedState(), analysisDebounceJob, autoCloseJob, c.useCases.workflowManager, c.useCases.expireSubscriptions, c.useCases.remindExpiringSubscriptions, c.useCases.monitorLowBalance, c.useCases.renewCalendarChannels, c.useCases.reconcileWhatsAppTemplates, c.useCases.reconcileWhatsAppEntitlements, c.useCases.emitMonthlyInvoices, c.useCases.cancelBillingSweep, c.useCases.vendorChannelReconciler, c.useCases.channelStatusReconciler, c.useCases.purgeShortLinkClicks)

	c.jobRunner.SetScheduledMessageJobs(c.useCases.sweepScheduledMessages, c.useCases.purgeScheduledMessages)

	if c.services.assignmentService != nil {
		rescue := ia_usecase.NewRescueJob(
			c.repositories.workspaceConfig,
			c.repositories.assignmentHistory,
			ia_repo.NewAttentionRepository(c.db),
			c.services.conversationStatusUpdater,
			c.services.assignmentService,
		)
		rescue.SetDepartmentSchedules(c.repositories.workspaceDepartment)
		c.jobRunner.SetAssignmentJobs(rescue)
	}

	if c.useCases.reconcileTemplateSends != nil {
		c.jobRunner.SetWhatsAppTemplateSendJobs(cronPackage.CtxJobFunc(func(ctx context.Context) error {
			_, err := c.useCases.reconcileTemplateSends.Execute(ctx)
			return err
		}))
	}

	if c.services.reportService != nil {
		reportService := c.services.reportService
		c.jobRunner.SetReportRetentionJobs(cronPackage.CtxJobFunc(func(context.Context) error {
			expired, err := reportService.ExpireOldFiles(reportRetentionSweepLimit)
			if err != nil {
				return err
			}
			if expired > 0 {
				log.Printf("[cron] report_retention: expired %d report(s)", expired)
			}
			return nil
		}))
	}

	if imports := c.leadImports(); imports != nil {
		c.jobRunner.SetLeadImportJobs(cronPackage.CtxJobFunc(imports.Sweep))
	}
	if sweeper := c.geocodingSweeper(); sweeper != nil {
		c.jobRunner.SetGeocodingJobs(cronPackage.CtxJobFunc(sweeper.Sweep))
	}
	if refine := c.geocodingDistrictRefine(); refine != nil {
		c.jobRunner.SetGeocodingRefineJobs(cronPackage.CtxJobFunc(refine.Run))
	}
	c.jobRunner.SetLeadActionJobs(cronPackage.CtxJobFunc(c.leadActions().Sweep))
	c.jobRunner.SetCallListJobs(cronPackage.CtxJobFunc(c.callLists().Sweep))

	c.jobRunner.SetWebhookEventPurgeJob(webhook_usecase.NewPurgeProcessedEventsUseCase(
		webhook_repository.NewProcessedEventRepository(c.db), 30*24*time.Hour))

	if c.instagram != nil && c.instagram.Enabled {
		c.jobRunner.SetInstagramJobs(c.instagram.RefreshTokens)
	}
	if c.facebook != nil && c.facebook.Enabled {
		c.jobRunner.SetFacebookJobs(c.facebook.Health, cronPackage.CtxJobFunc(c.facebook.Publisher.Reap))
	}
	sync := c.ads.Sync
	c.jobRunner.SetAdsJobs(
		cronPackage.CtxJobFunc(func(ctx context.Context) error { return sync.SyncAll(ctx, adsuc.RecentInsightDays) }),
		cronPackage.CtxJobFunc(func(ctx context.Context) error { return sync.SyncAll(ctx, adsuc.SettledInsightDays) }),
		cronPackage.CtxJobFunc(sync.RefreshReviews),
		cronPackage.CtxJobFunc(sync.WatchFunds),
		cronPackage.CtxJobFunc(c.ads.Publish.Resume),
		cronPackage.CtxJobFunc(c.ads.Forms.PollAll),
		cronPackage.CtxJobFunc(c.ads.Conversions.DispatchAll),
		cronPackage.CtxJobFunc(c.ads.GrantHealth.Execute),
	)
	c.jobRunner.SetMediaGenerationJobs(cronPackage.CtxJobFunc(c.mediaGeneration().Service.Reap))

	if c.audience != nil && c.audience.Enabled {
		b := c.audience
		c.jobRunner.SetAudienceJobs(b.Flush, b.Backstop, b.Rollup, b.Purge, b.Backfill)
	}
	if c.telegram != nil && c.telegram.Enabled {
		c.jobRunner.SetTelegramJobs(c.telegram.CheckHealth)
	}
	if c.unofficialWhatsAppCampaigns != nil && c.unofficialWhatsAppCampaigns.Enabled {
		c.jobRunner.SetUnofficialWhatsAppCampaignJobs(
			cronPackage.CtxJobFunc(func(context.Context) error {
				return c.unofficialWhatsAppCampaigns.ScheduleJob.StartScheduledCampaigns()
			}),
		)
	}
	if c.unofficialWhatsApp != nil && c.unofficialWhatsApp.Enabled {
		c.jobRunner.SetUnofficialWhatsAppJobs(
			c.unofficialWhatsApp.CheckHealth,
			cronPackage.CtxJobFunc(c.unofficialWhatsApp.CheckHealth.VerifyIntegrity),
			c.unofficialWhatsApp.ReconcileCapacity,
		)
		if c.unofficialWhatsApp.HistorySync != nil {
			c.jobRunner.SetUnofficialWhatsAppHistoryJob(c.unofficialWhatsApp.HistorySync)
		}
	}
}

func derefID(id *string) string {
	if id == nil {
		return ""
	}
	return *id
}

const agentPromptContextRunes = 6000

func (c *Container) analysisSubjectContext(_ context.Context, subject *conversation_usecase.AnalysisSubject) (string, error) {
	text := fmt.Sprintf(
		"Campaign/account: %s. A name alone does not establish a conversion objective.",
		subject.ContainerName,
	)
	if subject.AgentID == "" || c.repositories.agent == nil {
		return text, nil
	}

	agent, err := c.repositories.agent.FindByID(subject.AgentID)
	if err != nil {
		return "", err
	}
	if agent == nil {
		return text, nil
	}
	prompt := []rune(strings.TrimSpace(agent.MessagingPrompt))
	if len(prompt) > agentPromptContextRunes {
		prompt = prompt[:agentPromptContextRunes]
	}
	if len(prompt) == 0 {
		return text, nil
	}
	return text + "\nConfigured agent purpose and guidance:\n" + string(prompt), nil
}

type analysisChannel struct {
	entry    shared.EntryType
	resolver conversation_usecase.AnalysisSubjectResolver
}

type analysisSubjectSink interface {
	SetAnalysisSubjectResolver(shared.EntryType, conversation_usecase.AnalysisSubjectResolver)
}

type adapterSink struct {
	adapter *conversation_usecase.AnalysisAdapter
}

func (a adapterSink) SetAnalysisSubjectResolver(entry shared.EntryType, resolver conversation_usecase.AnalysisSubjectResolver) {
	a.adapter.RegisterResolver(entry, resolver)
}

func registerAnalysisChannels(channels []analysisChannel, sinks ...analysisSubjectSink) {
	for _, sink := range sinks {
		if sink == nil {
			continue
		}
		for _, ch := range channels {
			sink.SetAnalysisSubjectResolver(ch.entry, ch.resolver)
		}
	}
}

func campaignAwareResolver(
	base conversation_usecase.AnalysisSubjectResolver,
	conversations uwcuc.ConversationCampaigns,
	campaigns *unofficialWhatsAppCampaignBundle,
) conversation_usecase.AnalysisSubjectResolver {
	if conversations == nil || campaigns == nil || campaigns.Campaigns == nil {
		return base
	}
	return uwcuc.NewAutomationSource(conversations, campaigns.Campaigns).AnalysisResolver(base)
}
