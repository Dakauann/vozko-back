package container

import (
	"log"
	"strings"
	"time"

	audiencehttp "vozko/delivery/http/audience"
	ca "vozko/domain/audience"
	"vozko/domain/notification"
	"vozko/domain/shared"
	workspace_pricing_domain "vozko/domain/workspace/workspace_pricing"
	ca_repository "vozko/infra/repositories/audience"
	workspace_config_repository "vozko/infra/repositories/workspace_config"
	cauc "vozko/usecases/audience"
	convuc "vozko/usecases/conversation"
	iguc "vozko/usecases/instagram"
)

// audienceBundle is the comment-analysis engine wired as one unit, the
// same self-contained shape as the channel bundles: build it in one place,
// hand the jobs to the runner and the handler to the router, and keep the
// rest of the container unaware of its internals.
//
// It is channel-neutral. Instagram registers itself as a source below; a
// second channel would register another adapter and get the whole feature.
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

	// InstagramAdapter is the channel's side, handed to the webhook use case
	// as its AudienceEnqueuer.
	InstagramAdapter *iguc.AudienceAdapter

	// ConversationAdapter is the OTHER subject kind: one adapter serving every
	// channel that holds a transcript. The debounce sweep hands conversations to
	// it, and the engine reads them back through it at classification time.
	ConversationAdapter *convuc.AnalysisAdapter
}

// audienceRetention is how long analysed rows are kept. The full
// comment text is never here, only labels and a 200-rune excerpt, so this is
// dashboard history rather than a PII window.
const audienceRetention = 365 * 24 * time.Hour

// initCommentAnalysis builds the engine. It runs inside initUseCases once the
// AI service, the pricer, the balance checker and the notifier exist, and
// BEFORE the Instagram runtime half, whose webhook use case takes the
// enqueuer built here.
func (c *Container) initCommentAnalysis(pricer workspace_pricing_domain.Pricer, notifier notification.Notifier, dashboardURL string) {
	bundle := &audienceBundle{}
	c.audience = bundle

	if c.instagram == nil || !c.instagram.Enabled {
		// The only source today is Instagram; without it the engine would
		// have nothing to classify. Leaving the bundle disabled registers no
		// routes and no jobs.
		log.Printf("[comment-analysis] no comment source enabled; feature not wired")
		return
	}

	clock := shared.SystemClock{}
	state := c.redisProvider.SharedState()

	repo := ca_repository.NewRepository(c.db)
	settings := ca_repository.NewSettingsRepository(c.db)
	// The workspace's own analysis settings, as opposed to each account's. Both
	// the engine and the dashboard resolve the ceiling through
	// ca.ResolveDailyCap over this, which is what stops the number a person sets
	// and the number that stops a pass from drifting apart.
	workspaceSettings := workspace_config_repository.NewAudienceSettingsStore(c.db)
	authors := ca_repository.NewAuthorRepository(c.db)
	rollups := ca_repository.NewRollupRepository(c.db)
	batches := ca_repository.NewBatchRepository(c.db)
	backfills := ca_repository.NewBackfillRepository(c.db)

	scheduler := cauc.NewScheduler(state)
	// One limiter, shared: the engine claims against it and the dashboard reads
	// it, so the ceiling that stops a pass is the ceiling a person sees.
	usageLimiter := cauc.NewUsageLimiter(state)
	// Post override -> account settings -> disabled defaults. The resolver is
	// the only place that fallback lives; the engine and the ingestor read
	// effective settings through it and never touch the repository directly.
	resolver := cauc.NewSettingsResolver(settings)
	bundle.Ingestor = cauc.NewIngestUseCase(repo, resolver, scheduler, c.services.metrics, clock)
	// Queuing a conversation is half the signal: the CRM is told an analysis is
	// coming here, and told it landed from the engine. Wired after construction
	// because the socket is built later and must not become an argument every
	// channel threads through.
	if live, ok := bundle.Ingestor.(interface {
		SetLive(ca.ConversationAnalysisLive)
	}); ok && c.services.conversationHub != nil {
		live.SetLive(c.services.conversationHub)
	}

	// Instagram's side: the same adapter is the webhook's enqueuer and the
	// engine's source adapter.
	ig := c.instagram
	bundle.InstagramAdapter = iguc.NewAudienceAdapter(
		bundle.Ingestor, ig.Comments, ig.Media, ig.Accounts, ig.CommentSvc, repo,
	)
	if ig.ModerateComment != nil {
		ig.ModerateComment.SetCommentAnalysis(bundle.InstagramAdapter)
	}
	adapters := map[ca.Source]ca.SourceAdapter{ca.SourceInstagram: bundle.InstagramAdapter}

	// Alerts. The dispatcher composes the two outbound use cases that already
	// exist; nothing here is a new send path. A workspace with neither channel
	// configured simply never dispatches, and the rules stay inert.
	alertRules := ca_repository.NewAlertRuleRepository(c.db)
	alertDispatcher := commentAlertDispatcher{
		official: c.useCases.startOfficialConversation,
		send:     c.services.liveOperatorSend,
		// So an alert fills the variables the customer's own template declares,
		// rather than a shape we guessed.
		templates: c.repositories.whatsappTemplate,
	}
	if c.unofficialWhatsApp != nil && c.unofficialWhatsApp.Enabled {
		alertDispatcher.unofficial = c.unofficialWhatsApp.StartConv
	}

	// What the workspace can actually SEND on, asked of the workspace instead
	// of assumed from a constant. The picker and the save both read this, so
	// they cannot disagree about whether a channel works.
	senderDirectory := commentAlertSenderDirectory{phones: c.useCases.listBusinessPhones}
	if c.unofficialWhatsApp != nil && c.unofficialWhatsApp.Enabled {
		senderDirectory.unofficialEnabled = true
		senderDirectory.instances = c.unofficialWhatsApp.Instances
	}
	alertEvaluator := cauc.NewAlertEvaluator(cauc.AlertDeps{
		Rules: alertRules, Repo: repo, Dispatcher: alertDispatcher, Clock: clock,
		// So an alert can name the account and link the post instead of
		// printing internal ids at whoever it wakes up.
		Adapters: adapters,
		Settings: settings,
		// The model's reading of what fired. Opt-in per rule, under the same
		// balance floor and on the same spend page as every other model call.
		Briefer: cauc.NewAlertBriefer(c.services.ai, c.cfg.OpenRouterDefaultModel),
		Balance: c.services.cachedBalanceChecker,
		Batches: batches,
	})
	verifiers := map[ca.Source]cauc.AccountVerifier{ca.SourceInstagram: iguc.NewAudienceAccountVerifier(ig.Accounts)}

	// Conversations. One adapter, every channel: the transcript comes from
	// conversation_messages, which each channel already writes to.
	bundle.ConversationAdapter = convuc.NewAnalysisAdapter(bundle.Ingestor, c.repositories.conversation)
	conversationAdapters := map[ca.Source]ca.ConversationAdapter{}
	for _, e := range shared.ConversationAnalysableEntryTypes() {
		conversationAdapters[ca.SourceOf(e)] = bundle.ConversationAdapter
	}

	engine, err := cauc.NewEngine(cauc.EngineDeps{
		Repo:          repo,
		Settings:      resolver,
		Batches:       batches,
		Adapters:      adapters,
		Conversations: conversationAdapters,
		Classifier:    cauc.NewClassifier(c.services.ai, c.cfg.OpenRouterDefaultModel),
		Scheduler:     scheduler,
		Charger:       cauc.NewCharger(c.repositories.balance, pricer, state),
		// The rolling volume budget, distinct from the surcharge above.
		Usage:           usageLimiter,
		WorkspaceLimits: workspaceSettings,
		Balance:         c.services.cachedBalanceChecker,
		State:           state,
		Metrics:         c.services.metrics,
		Notifier:        notifier,
		// The live feed (§7). Nil when the socket is not up; the engine then
		// classifies exactly the same and nothing is broadcast.
		Broadcaster: c.services.conversationHub,
		Timeline:    c.services.crmTelemetryEmitter,
		// The per-conversation signal the CRM listens on. Same hub, different
		// channel from Broadcaster above: that one is the workspace audience
		// feed under audience:read, this one is one conversation, seen by
		// whoever can open it.
		Live:     c.services.conversationHub,
		Alerts:   alertEvaluator,
		Clock:    clock,
		Budget:   ca.DefaultBudget(),
		Debounce: ca.DefaultDebouncePolicy(),

		DashboardURL: dashboardURL,
	})
	if err != nil {
		log.Fatalf("[comment-analysis] %v", err)
	}
	bundle.Engine = engine
	bundle.Flush = cauc.NewFlushJob(engine)
	bundle.Backstop = cauc.NewBackstopJob(engine)
	bundle.Rollup = cauc.NewRollupJob(repo, settings, authors, rollups, state, clock)
	// The §5 author pass rides the rollup: it is the one moment the corpus
	// sizes it decides on are known to be current. Capped inside, billed under
	// its own kind, and skipped entirely without a model.
	bundle.Rollup.SetRoleInference(cauc.NewRoleInferenceJob(cauc.RoleInferenceDeps{
		Authors: authors, Repo: repo, Settings: settings, Adapters: adapters,
		Inferrer: cauc.NewRoleInferrer(c.services.ai, c.cfg.OpenRouterDefaultModel),
		Batches:  batches, Charger: cauc.NewCharger(c.repositories.balance, pricer, state),
		Balance: c.services.cachedBalanceChecker, Clock: clock,
	}))
	bundle.Purge = cauc.NewPurgeJob(repo, audienceRetention, clock)

	backfillDeps := cauc.BackfillDeps{
		Backfills: backfills, Settings: settings, Ingestor: bundle.Ingestor,
		Adapters: adapters, Verifiers: verifiers, Pricer: pricer, State: state, Clock: clock,
	}
	bundle.Backfill = cauc.NewBackfillJob(backfillDeps)
	estimate, start, get, cancel := cauc.NewBackfillUseCases(backfillDeps)
	getSettings, updateSettings := cauc.NewSettingsUseCases(settings, verifiers, clock)

	// Replying (§6). The repliers map is empty for a channel that cannot post,
	// and the use case refuses rather than pretending it sent something. The
	// drafter is the same ai.Service the classifier already bills against.
	repliers := map[ca.Source]ca.CommentReplier{}
	if c.instagram != nil && c.instagram.Enabled && c.instagram.ReplyComment != nil {
		repliers[ca.SourceInstagram] = instagramCommentReplier{uc: c.instagram.ReplyComment}
	}
	suggestReply, postReply := cauc.NewReplyUseCases(cauc.ReplyDeps{
		Repo: repo, Settings: settings, Adapters: adapters,
		Drafter:  cauc.NewReplyDrafter(c.services.ai, c.cfg.OpenRouterDefaultModel),
		Repliers: repliers,
		// Drafting is a model call: same floor, same spend page.
		Balance: c.services.cachedBalanceChecker,
		Batches: batches,
		Clock:   clock,
	})
	getContainer, putContainer, delContainer, listAccounts := cauc.NewContainerSettingsUseCases(settings, resolver, verifiers, clock)

	bundle.Handler = audiencehttp.NewHandler(audiencehttp.Deps{
		List:       cauc.NewListUseCase(repo),
		Stats:      cauc.NewStatsUseCase(repo),
		Trends:     cauc.NewTrendsUseCase(repo, rollups),
		Authors:    cauc.NewListAuthorsUseCase(authors),
		Author:     cauc.NewGetAuthorUseCase(authors, repo),
		Containers: cauc.NewListAuthorContainersUseCase(authors, repo),
		// Forwarding rides the live composer through the late-binding wrapper,
		// exactly as the scheduled-message dispatcher does: one send
		// implementation, whichever channel the conversation runs on.
		Escalate:   cauc.NewEscalateCommentUseCase(repo, adapters, commentEscalationSender{send: c.services.liveOperatorSend}),
		Recipients: cauc.NewListEscalationRecipientsUseCase(commentEscalationRecipients{inbox: c.services.conversationHistory}),
		Alerts:     cauc.NewManageAlertRulesUseCase(alertRules, clock).WithSenderDirectory(senderDirectory),
		Channels:   cauc.NewGetAlertChannelsUseCase(senderDirectory),
		TestAlert:  cauc.NewTestAlertRuleUseCase(alertRules, alertDispatcher, clock),
		Suggest:    suggestReply,
		PostReply:  postReply,
		Moderate:   cauc.NewSetModerationStateUseCase(authors, clock),
		GetSet:     getSettings,
		UpdateSet:  updateSettings,
		Retry:      cauc.NewRetryUseCase(repo, clock),
		Spend:      cauc.NewSpendUseCase(batches, clock),
		// The same limiter the engine claims against, read-only, and the same
		// ceiling store the engine resolves against. One object each, so the
		// number the dashboard shows and the ceiling that stops a pass can never
		// be two different things. The backlog reader is what turns a bare
		// number into "and this is what it is costing you".
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
	// Report what is actually registered, not a fixed string. The previous line
	// said "sources: instagram" on every boot whatever was wired, which reads as
	// a diagnosis and is not one.
	commentSources, conversationSources := engine.RegisteredSources()
	log.Printf("[comment-analysis] engine enabled (comments: %s; conversations: %s; model default %q)",
		sourceList(commentSources), sourceList(conversationSources), c.cfg.OpenRouterDefaultModel)
}

// audienceHandler returns the API handler, or nil when the feature is
// not wired; the router treats nil as "register no routes".
func audienceHandler(c *Container) *audiencehttp.Handler {
	if c.audience == nil || !c.audience.Enabled {
		return nil
	}
	return c.audience.Handler
}

// audienceEnqueuer returns Instagram's enqueuer, or nil when the
// feature is not wired; the webhook use case treats nil as "no engine".
func audienceEnqueuer(c *Container) iguc.AudienceEnqueuer {
	if c.audience == nil || !c.audience.Enabled || c.audience.InstagramAdapter == nil {
		return nil
	}
	return c.audience.InstagramAdapter
}

// sourceList renders a registered-source list for the startup line.
//
// "none" rather than an empty gap: a channel set that is empty is exactly the
// misconfiguration this line exists to reveal, and it has to be readable as
// such at a glance.
func sourceList(sources []string) string {
	if len(sources) == 0 {
		return "none"
	}
	return strings.Join(sources, ", ")
}
