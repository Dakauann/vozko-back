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
	// The resolvers are registered on BOTH the sweep and the analysis adapter.
	//
	// They answer the same question ("what is this conversation, and is it
	// configured to be analysed"), and registering a channel on one but not the
	// other is exactly the silent gap this whole area already had once: an
	// EnableAnalysis switch in the UI that nothing behind it ever read.
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

	// Hand the sweep the analysis engine. Without this, conversations are never
	// queued and only auto-staging and auto-memory run.
	if c.audience != nil && c.audience.ConversationAdapter != nil {
		c.audience.ConversationAdapter.SubjectContext = c.analysisSubjectContext
		sinks = append(sinks, adapterSink{c.audience.ConversationAdapter})
		if q, ok := analysisDebounceJob.(interface {
			SetAnalysisQueue(conversation_usecase.ConversationAnalysisEnqueuer)
		}); ok {
			q.SetAnalysisQueue(c.audience.ConversationAdapter)
		}
	}
	// The per-workspace quiet period. Without it every workspace waits the
	// product default, which is what this sweep did when the window was a
	// constant in the binary.
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

// agentPromptContextRunes bounds how much of an agent's configured prompt is
// frozen alongside a transcript. It is reference data for the classifier, not
// the conversation itself, so an operator with a very long prompt must not be
// able to push the transcript out of the model's window with it.
const agentPromptContextRunes = 6000

// analysisSubjectContext describes what a conversation was configured to DO,
// resolved once when the conversation is queued and frozen with its transcript.
//
// Freezing it is the point. Renaming a campaign or rewriting an agent's
// instructions afterwards must not silently re-score analyses that were made
// under the old configuration; it produces a new revision, judged on the new
// terms, alongside the old one.
//
// The campaign's name is offered as what it is — a name — because it is the
// only thing the product actually stores about a campaign's purpose. Presenting
// it as a stated objective would have the model score conversations against a
// goal nobody wrote down.
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
		// Retried by the caller rather than queued without its context: an
		// analysis is scored against this, so classifying without it would
		// judge the conversation on different terms from its neighbours.
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

// analysisChannel pairs a channel with the resolver that loads a conversation's
// analysis subject on it.
type analysisChannel struct {
	entry    shared.EntryType
	resolver conversation_usecase.AnalysisSubjectResolver
}

// analysisSubjectSink is something that needs the per-channel resolvers.
//
// There are two, the inactivity sweep and the analysis adapter, and they must
// receive the SAME set. A channel wired into one and not the other fails
// silently in a way no log records: the operator's EnableAnalysis switch is
// read by half the pipeline, so conversations are either enriched but never
// classified, or classified with the channel's defaults instead of the
// campaign's. Naming the shape lets registerAnalysisChannels fan out once, and
// lets a test hold both sinks and assert they agree.
type analysisSubjectSink interface {
	SetAnalysisSubjectResolver(shared.EntryType, conversation_usecase.AnalysisSubjectResolver)
}

// adapterSink lets the analysis adapter answer to analysisSubjectSink, whose
// method it spells differently.
type adapterSink struct {
	adapter *conversation_usecase.AnalysisAdapter
}

func (a adapterSink) SetAnalysisSubjectResolver(entry shared.EntryType, resolver conversation_usecase.AnalysisSubjectResolver) {
	a.adapter.RegisterResolver(entry, resolver)
}

// registerAnalysisChannels gives every sink every channel.
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

// campaignAwareResolver wraps a channel's resolver so a conversation that
// belongs to a campaign is configured by that CAMPAIGN rather than by the
// instance it happens to run on.
//
// Without it an operator who switches analysis on for one campaign gets
// nothing, because the resolver underneath only ever read the instance. The
// wrapping is skipped when campaigns are not available, which is the same
// deployment where there are no campaigns to own a conversation.
func campaignAwareResolver(
	base conversation_usecase.AnalysisSubjectResolver,
	campaigns *unofficialWhatsAppCampaignBundle,
) conversation_usecase.AnalysisSubjectResolver {
	if campaigns == nil || campaigns.Entries == nil || campaigns.Campaigns == nil {
		return base
	}
	return uwcuc.NewAutomationSource(campaigns.Entries, campaigns.Campaigns).AnalysisResolver(base)
}
