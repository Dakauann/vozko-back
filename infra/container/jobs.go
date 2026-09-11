package container

import (
	"context"

	"vozko/domain/shared"
	cronPackage "vozko/infra/cron"
	ia_repo "vozko/infra/repositories/inbox_assignment"
	conversation_usecase "vozko/usecases/conversation"
	ia_usecase "vozko/usecases/inbox_assignment"
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
	// The resolvers are registered on BOTH the sweep and the analysis adapter.
	//
	// They answer the same question ("what is this conversation, and is it
	// configured to be analysed"), and registering a channel on one but not the
	// other is exactly the silent gap this whole area already had once: an
	// EnableAnalysis switch in the UI that nothing behind it ever read.
	type analysisChannel struct {
		entry    shared.EntryType
		resolver conversation_usecase.AnalysisSubjectResolver
	}
	channels := []analysisChannel{
		// WhatsApp is registered like every other channel rather than special-cased
		// inside the sweep. It has no Enabled flag because it is not an optional
		// integration: it is the channel this feature was built for.
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
		channels = append(channels, analysisChannel{shared.EntryTypeUnofficialWhatsApp, unofficialWhatsAppAnalysisResolver(c.unofficialWhatsApp)})
	}

	if setter, ok := analysisDebounceJob.(interface {
		SetAnalysisSubjectResolver(shared.EntryType, conversation_usecase.AnalysisSubjectResolver)
	}); ok {
		for _, ch := range channels {
			setter.SetAnalysisSubjectResolver(ch.entry, ch.resolver)
		}
	}

	// Hand the sweep the analysis engine. Without this, conversations are never
	// queued and only auto-staging and auto-memory run.
	if c.audience != nil && c.audience.ConversationAdapter != nil {
		for _, ch := range channels {
			c.audience.ConversationAdapter.RegisterResolver(ch.entry, ch.resolver)
		}
		if q, ok := analysisDebounceJob.(interface {
			SetAnalysisQueue(conversation_usecase.ConversationAnalysisEnqueuer)
		}); ok {
			q.SetAnalysisQueue(c.audience.ConversationAdapter)
		}
	}

	autoCloseJob := conversation_usecase.NewAutoCloseJob(
		c.repositories.wcEntry,
		c.services.conversationStatusUpdater,
	)
	c.jobRunner = cronPackage.NewJobRunner(cron_job, startScheduledWhatsappCampaignsJob, c.redisProvider.SharedState(), analysisDebounceJob, autoCloseJob, c.useCases.workflowManager, c.useCases.expireSubscriptions, c.useCases.remindExpiringSubscriptions, c.useCases.monitorLowBalance, c.useCases.renewCalendarChannels, c.useCases.reconcileWhatsAppTemplates, c.useCases.reconcileWhatsAppEntitlements, c.useCases.emitMonthlyInvoices, c.useCases.cancelBillingSweep, c.useCases.vendorChannelReconciler, c.useCases.channelStatusReconciler, c.useCases.purgeShortLinkClicks)

	// Scheduled messages are not optional and not per-channel: the sweep is what
	// makes delivery correct when the delayed queue loses a message.
	c.jobRunner.SetScheduledMessageJobs(c.useCases.sweepScheduledMessages, c.useCases.purgeScheduledMessages)

	// Roulette rescue. Registered unconditionally: it filters itself down to the
	// workspaces running the last_seen mode with rescue on, so a deployment with
	// none of them pays one indexed read a minute.
	if c.services.assignmentService != nil {
		rescue := ia_usecase.NewRescueJob(
			c.repositories.workspaceConfig,
			c.repositories.assignmentHistory,
			ia_repo.NewAttentionRepository(c.db),
			c.services.conversationStatusUpdater,
			c.services.assignmentService,
		)
		// Department-level working hours. Without this the sweep still honours
		// each workspace's schedule; departments simply all inherit it.
		rescue.SetDepartmentSchedules(c.repositories.workspaceDepartment)
		c.jobRunner.SetAssignmentJobs(rescue)
	}

	// Paid-template reconciliation is likewise not optional: it is the sweep that
	// returns money taken for sends that never completed. Without it those
	// charges are simply kept.
	if c.useCases.reconcileTemplateSends != nil {
		c.jobRunner.SetWhatsAppTemplateSendJobs(cronPackage.CtxJobFunc(func(ctx context.Context) error {
			_, err := c.useCases.reconcileTemplateSends.Execute(ctx)
			return err
		}))
	}

	// Instagram jobs are optional and registered after construction, so the
	// channel can be absent without touching the constructor.
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

// unofficialWhatsAppAnalysisResolver loads a conversation's analysis subject.
//
// The instance is the container, and the contact label is a real phone number —
// which is why the job's original phone-number precondition, the thing that
// excluded Instagram and Telegram, is satisfied here without special-casing.
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

// instagramAnalysisResolver loads an Instagram conversation's analysis subject.
//
// The account is the container, the role whatsapp_campaigns plays for WhatsApp,
// and the @handle is the contact label. Instagram contacts have no phone
// number, which is precisely why the job's old phone-number precondition
// excluded them.
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

// telegramAnalysisResolver loads a Telegram conversation's analysis subject.
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

// derefID flattens the nullable ids the channel entities use (*string) into
// the plain strings AnalysisSubject carries; nil means "not linked".
func derefID(id *string) string {
	if id == nil {
		return ""
	}
	return *id
}
