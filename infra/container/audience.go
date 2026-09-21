package container

import (
	"log"
	"strings"
	"time"

	audiencehttp "vozko/delivery/http/audience"
	ca "vozko/domain/audience"
	"vozko/domain/notification"
	"vozko/domain/shared"
	ca_repository "vozko/infra/repositories/audience"
	workspace_config_repository "vozko/infra/repositories/workspace_config"
	cauc "vozko/usecases/audience"
	convuc "vozko/usecases/conversation"
	iguc "vozko/usecases/instagram"
)

type audienceBundle struct {
	Enabled bool

	Engine   *cauc.Engine
	Ingestor ca.Ingestor
	Handler  *audiencehttp.Handler

	Flush    *cauc.FlushJob
	Backstop *cauc.BackstopJob
	Rollup   *cauc.RollupJob
	Purge    *cauc.PurgeJob
	Backfill *cauc.BackfillJob

	InstagramAdapter *iguc.AudienceAdapter

	AlertConsumer       *cauc.AlertConsumer
	ConversationAdapter *convuc.AnalysisAdapter
}

const audienceRetention = 365 * 24 * time.Hour

func (c *Container) initCommentAnalysis(notifier notification.Notifier, dashboardURL string) {
	bundle := &audienceBundle{}
	c.audience = bundle

	if c.instagram == nil || !c.instagram.Enabled {
		log.Printf("[comment-analysis] no comment source enabled; feature not wired")
		return
	}

	clock := shared.SystemClock{}
	state := c.redisProvider.SharedState()

	repo := ca_repository.NewRepository(c.db)
	settings := ca_repository.NewSettingsRepository(c.db)
	workspaceSettings := workspace_config_repository.NewAudienceSettingsStore(c.db)
	authors := ca_repository.NewAuthorRepository(c.db)
	rollups := ca_repository.NewRollupRepository(c.db)
	batches := ca_repository.NewBatchRepository(c.db)
	backfills := ca_repository.NewBackfillRepository(c.db)

	scheduler := cauc.NewScheduler(state)
	usageLimiter := cauc.NewUsageLimiter(state)
	resolver := cauc.NewSettingsResolver(settings)
	bundle.Ingestor = cauc.NewIngestUseCase(repo, resolver, scheduler, c.services.metrics, clock)
	if live, ok := bundle.Ingestor.(interface {
		SetLive(ca.ConversationAnalysisLive)
	}); ok && c.services.conversationHub != nil {
		live.SetLive(c.services.conversationHub)
	}

	ig := c.instagram
	bundle.InstagramAdapter = iguc.NewAudienceAdapter(
		bundle.Ingestor, ig.Comments, ig.Media, ig.Accounts, ig.CommentSvc, repo,
	)
	if ig.ModerateComment != nil {
		ig.ModerateComment.SetCommentAnalysis(bundle.InstagramAdapter)
	}
	adapters := map[ca.Source]ca.SourceAdapter{ca.SourceInstagram: bundle.InstagramAdapter}

	alertRules := ca_repository.NewAlertRuleRepository(c.db)

	senderDirectory := commentAlertSenderDirectory{phones: c.useCases.listBusinessPhones}
	if c.unofficialWhatsApp != nil && c.unofficialWhatsApp.Enabled {
		senderDirectory.unofficialEnabled = true
		senderDirectory.instances = c.unofficialWhatsApp.Instances
	}

	alertDispatcher := commentAlertDispatcher{
		official:  c.useCases.startOfficialConversation,
		send:      c.services.liveOperatorSend,
		templates: c.repositories.whatsappTemplate,
		senders:   senderDirectory,
	}
	if c.unofficialWhatsApp != nil && c.unofficialWhatsApp.Enabled {
		alertDispatcher.unofficial = c.unofficialWhatsApp.StartConv
	}
	bundle.AlertConsumer = cauc.NewAlertConsumer(cauc.AlertConsumerDeps{
		Subscriber: c.services.audienceAlertSub,
		Dispatcher: alertDispatcher,
		Rules:      alertRules,
		Briefer:    cauc.NewAlertBriefer(c.services.ai, c.cfg.OpenRouterDefaultModel),
		Settings:   settings,
		Balance:    c.services.cachedBalanceChecker,
		Batches:    batches,
		Clock:      clock,
	})
	if err := bundle.AlertConsumer.Start(); err != nil {
		log.Printf("[comment-analysis] alert consumer failed to start, alerts will send inline: %v", err)
	}

	alertEvaluator := cauc.NewAlertEvaluator(cauc.AlertDeps{
		Rules: alertRules, Repo: repo, Clock: clock,
		Adapters:  adapters,
		Publisher: c.services.audienceAlertPub,
		Sender:    bundle.AlertConsumer,
		State:     state,
	})
	verifiers := map[ca.Source]cauc.AccountVerifier{ca.SourceInstagram: iguc.NewAudienceAccountVerifier(ig.Accounts)}

	bundle.ConversationAdapter = convuc.NewAnalysisAdapter(bundle.Ingestor, c.repositories.conversation)
	conversationAdapters := map[ca.Source]ca.ConversationAdapter{}
	for _, e := range shared.ConversationAnalysableEntryTypes() {
		conversationAdapters[ca.SourceOf(e)] = bundle.ConversationAdapter
	}

	engine, err := cauc.NewEngine(cauc.EngineDeps{
		Repo:            repo,
		Settings:        resolver,
		Batches:         batches,
		Adapters:        adapters,
		Conversations:   conversationAdapters,
		Classifier:      cauc.NewClassifier(c.services.ai, c.cfg.OpenRouterDefaultModel),
		Scheduler:       scheduler,
		Usage:           usageLimiter,
		WorkspaceLimits: workspaceSettings,
		Balance:         c.services.cachedBalanceChecker,
		State:           state,
		Metrics:         c.services.metrics,
		Notifier:        notifier,
		Broadcaster:     c.services.conversationHub,
		Timeline:        c.services.crmTelemetryEmitter,
		Live:            c.services.conversationHub,
		Alerts:          alertEvaluator,
		Clock:           clock,
		Budget:          ca.DefaultBudget(),
		Debounce:        ca.DefaultDebouncePolicy(),

		DashboardURL: dashboardURL,
	})
	if err != nil {
		log.Fatalf("[comment-analysis] %v", err)
	}
	bundle.Engine = engine
	bundle.Flush = cauc.NewFlushJob(engine)
	bundle.Backstop = cauc.NewBackstopJob(engine)
	bundle.Rollup = cauc.NewRollupJob(repo, settings, authors, rollups, state, clock)
	bundle.Rollup.SetRoleInference(cauc.NewRoleInferenceJob(cauc.RoleInferenceDeps{
		Authors: authors, Repo: repo, Settings: settings, Adapters: adapters,
		Inferrer: cauc.NewRoleInferrer(c.services.ai, c.cfg.OpenRouterDefaultModel),
		Batches:  batches,
		Balance:  c.services.cachedBalanceChecker, Clock: clock,
	}))
	bundle.Purge = cauc.NewPurgeJob(repo, audienceRetention, clock)

	backfillDeps := cauc.BackfillDeps{
		Backfills: backfills, Settings: settings, Ingestor: bundle.Ingestor,
		Adapters: adapters, Verifiers: verifiers, State: state, Clock: clock,
	}
	bundle.Backfill = cauc.NewBackfillJob(backfillDeps)
	estimate, start, get, cancel := cauc.NewBackfillUseCases(backfillDeps)
	getSettings, updateSettings := cauc.NewSettingsUseCases(settings, verifiers, clock)

	repliers := map[ca.Source]ca.CommentReplier{}
	if c.instagram != nil && c.instagram.Enabled && c.instagram.ReplyComment != nil {
		repliers[ca.SourceInstagram] = instagramCommentReplier{uc: c.instagram.ReplyComment}
	}
	suggestReply, postReply := cauc.NewReplyUseCases(cauc.ReplyDeps{
		Repo: repo, Settings: settings, Adapters: adapters,
		Drafter:  cauc.NewReplyDrafter(c.services.ai, c.cfg.OpenRouterDefaultModel),
		Repliers: repliers,
		Balance:  c.services.cachedBalanceChecker,
		Batches:  batches,
		Clock:    clock,
	})
	getContainer, putContainer, delContainer, listAccounts := cauc.NewContainerSettingsUseCases(settings, resolver, verifiers, clock)

	bundle.Handler = audiencehttp.NewHandler(audiencehttp.Deps{
		List:              cauc.NewListUseCase(repo),
		Stats:             cauc.NewStatsUseCase(repo),
		Trends:            cauc.NewTrendsUseCase(repo, rollups),
		Authors:           cauc.NewListAuthorsUseCase(authors),
		Author:            cauc.NewGetAuthorUseCase(authors, repo),
		Containers:        cauc.NewListAuthorContainersUseCase(authors, repo),
		Escalate:          cauc.NewEscalateCommentUseCase(repo, adapters, commentEscalationSender{send: c.services.liveOperatorSend}),
		Recipients:        cauc.NewListEscalationRecipientsUseCase(commentEscalationRecipients{inbox: c.services.conversationHistory}),
		Alerts:            cauc.NewManageAlertRulesUseCase(alertRules, clock).WithSenderDirectory(senderDirectory),
		Channels:          cauc.NewGetAlertChannelsUseCase(senderDirectory),
		TestAlert:         cauc.NewTestAlertRuleUseCase(alertRules, alertDispatcher, clock),
		Suggest:           suggestReply,
		PostReply:         postReply,
		Moderate:          cauc.NewSetModerationStateUseCase(authors, clock),
		GetSet:            getSettings,
		UpdateSet:         updateSettings,
		Retry:             cauc.NewRetryUseCase(repo, clock),
		Spend:             cauc.NewSpendUseCase(batches, clock),
		Usage:             cauc.NewUsageUseCase(usageLimiter, workspaceSettings, settings, ca_repository.NewBacklogReader(c.db), clock),
		WorkspaceSettings: cauc.NewWorkspaceSettingsUseCase(workspaceSettings),
		Estimate:          estimate,
		Start:             start,
		Backfill:          get,
		Cancel:            cancel,

		Accounts:     listAccounts,
		GetContainer: getContainer,
		PutContainer: putContainer,
		DelContainer: delContainer,
	})

	bundle.Enabled = true
	commentSources, conversationSources := engine.RegisteredSources()
	log.Printf("[comment-analysis] engine enabled (comments: %s; conversations: %s; model default %q)",
		sourceList(commentSources), sourceList(conversationSources), c.cfg.OpenRouterDefaultModel)
}

func audienceHandler(c *Container) *audiencehttp.Handler {
	if c.audience == nil || !c.audience.Enabled {
		return nil
	}
	return c.audience.Handler
}

func audienceEnqueuer(c *Container) iguc.AudienceEnqueuer {
	if c.audience == nil || !c.audience.Enabled || c.audience.InstagramAdapter == nil {
		return nil
	}
	return c.audience.InstagramAdapter
}

func sourceList(sources []string) string {
	if len(sources) == 0 {
		return "none"
	}
	return strings.Join(sources, ", ")
}
