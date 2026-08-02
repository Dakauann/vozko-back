package container

import (
	"context"

	"vozko/domain/shared"
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
	// Register each channel's analysis subject resolver.
	//
	// Without one a channel's conversations are never analysed — the silent gap
	// Instagram carried for months while its EnableAnalysis switch sat in the UI
	// doing nothing.
	if setter, ok := analysisDebounceJob.(interface {
		SetAnalysisSubjectResolver(shared.EntryType, conversation_usecase.AnalysisSubjectResolver)
	}); ok {
		if c.instagram != nil && c.instagram.Enabled {
			setter.SetAnalysisSubjectResolver(shared.EntryTypeInstagram, instagramAnalysisResolver(c.instagram))
		}
		if c.telegram != nil && c.telegram.Enabled {
			setter.SetAnalysisSubjectResolver(shared.EntryTypeTelegram, telegramAnalysisResolver(c.telegram))
		}
	}

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
	if c.telegram != nil && c.telegram.Enabled {
		c.jobRunner.SetTelegramJobs(c.telegram.CheckHealth, c.telegram.PurgeEvents)
	}
}

// instagramAnalysisResolver loads an Instagram conversation's analysis subject.
//
// The account is the container — the role whatsapp_campaigns plays for WhatsApp
// — and the @handle is the contact label. Instagram contacts have no phone
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
		if contact, err := bundle.Contacts.FindByID(ctx, conv.ContactID); err == nil && contact != nil {
			label = contact.DisplayName()
		}

		return &conversation_usecase.AnalysisSubject{
			EntryID:           conv.ID,
			EntryType:         shared.EntryTypeInstagram,
			WorkspaceID:       conv.WorkspaceID,
			ContainerID:       account.ID,
			ContainerName:     "@" + account.Username,
			ContactLabel:      label,
			EnableAnalysis:    account.EnableAnalysis,
			EnableAutoStaging: account.EnableAutoStaging,
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
		if contact, err := bundle.Contacts.FindByID(ctx, conv.ContactID); err == nil && contact != nil {
			label = contact.DisplayName()
		}

		return &conversation_usecase.AnalysisSubject{
			EntryID:           conv.ID,
			EntryType:         shared.EntryTypeTelegram,
			WorkspaceID:       conv.WorkspaceID,
			ContainerID:       account.ID,
			ContainerName:     account.DisplayName(),
			ContactLabel:      label,
			EnableAnalysis:    account.EnableAnalysis,
			EnableAutoStaging: account.EnableAutoStaging,
		}, nil
	}
}
