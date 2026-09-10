package container

import (
	"log"
	"time"

	commentanalysishttp "vozko/delivery/http/commentanalysis"
	ca "vozko/domain/comment_analysis"
	"vozko/domain/notification"
	"vozko/domain/shared"
	workspace_pricing_domain "vozko/domain/workspace/workspace_pricing"
	ca_repository "vozko/infra/repositories/comment_analysis"
	cauc "vozko/usecases/comment_analysis"
	iguc "vozko/usecases/instagram"
)

// commentAnalysisBundle is the comment-analysis engine wired as one unit, the
// same self-contained shape as the channel bundles: build it in one place,
// hand the jobs to the runner and the handler to the router, and keep the
// rest of the container unaware of its internals.
//
// It is channel-neutral. Instagram registers itself as a source below; a
// second channel would register another adapter and get the whole feature.
type commentAnalysisBundle struct {
	Enabled bool

	Engine   *cauc.Engine
	Ingestor ca.Ingestor
	Handler  *commentanalysishttp.Handler

	Flush    *cauc.FlushJob
	Backstop *cauc.BackstopJob
	Rollup   *cauc.RollupJob
	Purge    *cauc.PurgeJob
	Backfill *cauc.BackfillJob

	// InstagramAdapter is the channel's side, handed to the webhook use case
	// as its CommentAnalysisEnqueuer.
	InstagramAdapter *iguc.CommentAnalysisAdapter
}

// commentAnalysisRetention is how long analysed rows are kept. The full
// comment text is never here, only labels and a 200-rune excerpt, so this is
// dashboard history rather than a PII window.
const commentAnalysisRetention = 365 * 24 * time.Hour

// initCommentAnalysis builds the engine. It runs inside initUseCases once the
// AI service, the pricer, the balance checker and the notifier exist, and
// BEFORE the Instagram runtime half, whose webhook use case takes the
// enqueuer built here.
func (c *Container) initCommentAnalysis(pricer workspace_pricing_domain.Pricer, notifier notification.Notifier, dashboardURL string) {
	bundle := &commentAnalysisBundle{}
	c.commentAnalysis = bundle

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
	authors := ca_repository.NewAuthorRepository(c.db)
	rollups := ca_repository.NewRollupRepository(c.db)
	batches := ca_repository.NewBatchRepository(c.db)
	backfills := ca_repository.NewBackfillRepository(c.db)

	scheduler := cauc.NewScheduler(state)
	// Post override -> account settings -> disabled defaults. The resolver is
	// the only place that fallback lives; the engine and the ingestor read
	// effective settings through it and never touch the repository directly.
	resolver := cauc.NewSettingsResolver(settings)
	bundle.Ingestor = cauc.NewIngestUseCase(repo, resolver, scheduler, c.services.metrics, clock)

	// Instagram's side: the same adapter is the webhook's enqueuer and the
	// engine's source adapter.
	ig := c.instagram
	bundle.InstagramAdapter = iguc.NewCommentAnalysisAdapter(
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
	verifiers := map[ca.Source]cauc.AccountVerifier{ca.SourceInstagram: iguc.NewCommentAnalysisAccountVerifier(ig.Accounts)}

	engine, err := cauc.NewEngine(cauc.EngineDeps{
		Repo:       repo,
		Settings:   resolver,
		Batches:    batches,
		Adapters:   adapters,
		Classifier: cauc.NewClassifier(c.services.ai, c.cfg.OpenRouterDefaultModel),
		Scheduler:  scheduler,
		Charger:    cauc.NewCharger(c.repositories.balance, pricer, state),
		Balance:    c.services.cachedBalanceChecker,
		State:      state,
		Metrics:    c.services.metrics,
		Notifier:   notifier,
		// The live feed (§7). Nil when the socket is not up; the engine then
		// classifies exactly the same and nothing is broadcast.
		Broadcaster: c.services.conversationHub,
		Alerts:      alertEvaluator,
		Clock:       clock,
		Budget:      ca.DefaultBudget(),
		Debounce:    ca.DefaultDebouncePolicy(),

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
	bundle.Purge = cauc.NewPurgeJob(repo, commentAnalysisRetention, clock)

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

	bundle.Handler = commentanalysishttp.NewHandler(commentanalysishttp.Deps{
		List:       cauc.NewListUseCase(repo),
		Stats:      cauc.NewStatsUseCase(repo),
		Trends:     cauc.NewTrendsUseCase(rollups),
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
		Estimate:   estimate,
		Start:      start,
		Backfill:   get,
		Cancel:     cancel,

		Accounts:     listAccounts,
		GetContainer: getContainer,
		PutContainer: putContainer,
		DelContainer: delContainer,
	})

	bundle.Enabled = true
	log.Printf("[comment-analysis] engine enabled (sources: instagram; model default %q)", c.cfg.OpenRouterDefaultModel)
}

// commentAnalysisHandler returns the API handler, or nil when the feature is
// not wired; the router treats nil as "register no routes".
func commentAnalysisHandler(c *Container) *commentanalysishttp.Handler {
	if c.commentAnalysis == nil || !c.commentAnalysis.Enabled {
		return nil
	}
	return c.commentAnalysis.Handler
}

// commentAnalysisEnqueuer returns Instagram's enqueuer, or nil when the
// feature is not wired; the webhook use case treats nil as "no engine".
func commentAnalysisEnqueuer(c *Container) iguc.CommentAnalysisEnqueuer {
	if c.commentAnalysis == nil || !c.commentAnalysis.Enabled || c.commentAnalysis.InstagramAdapter == nil {
		return nil
	}
	return c.commentAnalysis.InstagramAdapter
}
