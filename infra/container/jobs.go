package container

import (
	"context"
	"fmt"
	"strings"

	"vozko/domain/shared"
	cronPackage "vozko/infra/cron"
	ia_repo "vozko/infra/repositories/inbox_assignment"
	workspace_config_repository "vozko/infra/repositories/workspace_config"
	conversation_usecase "vozko/usecases/conversation"
	ia_usecase "vozko/usecases/inbox_assignment"
	uwcuc "vozko/usecases/unofficial_whatsapp_campaign"
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
	if c.unofficialWhatsApp != nil && c.unofficialWhatsApp.Enabled {
		resolver := campaignAwareResolver(
			unofficialWhatsAppAnalysisResolver(c.unofficialWhatsApp),
			c.unofficialWhatsAppCampaigns,
		)
		channels = append(channels, analysisChannel{shared.EntryTypeUnofficialWhatsApp, resolver})
	}

	sinks := []analysisSubjectSink{}
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

	if c.instagram != nil && c.instagram.Enabled {
		c.jobRunner.SetInstagramJobs(c.instagram.RefreshTokens, c.instagram.PurgeEvents)
	}
	if c.audience != nil && c.audience.Enabled {
		b := c.audience
		c.jobRunner.SetAudienceJobs(b.Flush, b.Backstop, b.Rollup, b.Purge, b.Backfill)
	}
	if c.telegram != nil && c.telegram.Enabled {
		c.jobRunner.SetTelegramJobs(c.telegram.CheckHealth, c.telegram.PurgeEvents)
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
			c.unofficialWhatsApp.PurgeEvents,
		)
	}
}

func unofficialWhatsAppAnalysisResolver(bundle *unofficialWhatsAppBundle) conversation_usecase.AnalysisSubjectResolver {
	return func(ctx context.Context, entryID string) (*conversation_usecase.AnalysisSubject, error) {
		conv, err := bundle.Conversations.FindByID(ctx, entryID)
		if err != nil || conv == nil {
			return nil, err
		}
		instance, err := bundle.Instances.FindByID(ctx, conv.InstanceID)
		if err != nil || instance == nil {
			return nil, err
		}

		label := instance.Label()
		var leadID string
		if contact, err := bundle.Contacts.FindByID(ctx, conv.ContactID); err == nil && contact != nil {
			label = contact.DisplayName()
			leadID = derefID(contact.LeadID)
		}

		return &conversation_usecase.AnalysisSubject{
			EntryID:           conv.ID,
			EntryType:         shared.EntryTypeUnofficialWhatsApp,
			WorkspaceID:       conv.WorkspaceID,
			ContainerID:       instance.ID,
			ContainerName:     instance.Label(),
			ContactLabel:      label,
			LeadID:            leadID,
			AgentID:           derefID(instance.AgentID),
			EnableAnalysis:    instance.EnableAnalysis,
			EnableAutoStaging: instance.EnableAutoStaging,
			EnableAutoMemory:  instance.EnableAutoMemory,
		}, nil
	}
}

func instagramAnalysisResolver(bundle *instagramBundle) conversation_usecase.AnalysisSubjectResolver {
	return func(ctx context.Context, entryID string) (*conversation_usecase.AnalysisSubject, error) {
		conv, err := bundle.Conversations.FindByID(ctx, entryID)
		if err != nil || conv == nil {
			return nil, err
		}
		account, err := bundle.Accounts.FindByID(ctx, conv.IGAccountID)
		if err != nil || account == nil {
			return nil, err
		}

		label := "@" + account.Username
		var leadID string
		if contact, err := bundle.Contacts.FindByID(ctx, conv.ContactID); err == nil && contact != nil {
			label = contact.DisplayName()
			leadID = derefID(contact.LeadID)
		}

		return &conversation_usecase.AnalysisSubject{
			EntryID:           conv.ID,
			EntryType:         shared.EntryTypeInstagram,
			WorkspaceID:       conv.WorkspaceID,
			ContainerID:       account.ID,
			ContainerName:     "@" + account.Username,
			ContactLabel:      label,
			LeadID:            leadID,
			AgentID:           derefID(account.AgentID),
			EnableAnalysis:    account.EnableAnalysis,
			EnableAutoStaging: account.EnableAutoStaging,
			EnableAutoMemory:  account.EnableAutoMemory,
		}, nil
	}
}

func telegramAnalysisResolver(bundle *telegramBundle) conversation_usecase.AnalysisSubjectResolver {
	return func(ctx context.Context, entryID string) (*conversation_usecase.AnalysisSubject, error) {
		conv, err := bundle.Conversations.FindByID(ctx, entryID)
		if err != nil || conv == nil {
			return nil, err
		}
		account, err := bundle.Accounts.FindByID(ctx, conv.AccountID)
		if err != nil || account == nil {
			return nil, err
		}

		label := account.DisplayName()
		var leadID string
		if contact, err := bundle.Contacts.FindByID(ctx, conv.ContactID); err == nil && contact != nil {
			label = contact.DisplayName()
			leadID = derefID(contact.LeadID)
		}

		return &conversation_usecase.AnalysisSubject{
			EntryID:           conv.ID,
			EntryType:         shared.EntryTypeTelegram,
			WorkspaceID:       conv.WorkspaceID,
			ContainerID:       account.ID,
			ContainerName:     account.DisplayName(),
			ContactLabel:      label,
			LeadID:            leadID,
			AgentID:           derefID(account.AgentID),
			EnableAnalysis:    account.EnableAnalysis,
			EnableAutoStaging: account.EnableAutoStaging,
			EnableAutoMemory:  account.EnableAutoMemory,
		}, nil
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
	campaigns *unofficialWhatsAppCampaignBundle,
) conversation_usecase.AnalysisSubjectResolver {
	if campaigns == nil || campaigns.Entries == nil || campaigns.Campaigns == nil {
		return base
	}
	return uwcuc.NewAutomationSource(campaigns.Entries, campaigns.Campaigns).AnalysisResolver(base)
}
