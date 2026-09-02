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
		Clock:      clock,
		Budget:     ca.DefaultBudget(),
		Debounce:   ca.DefaultDebouncePolicy(),

		DashboardURL: dashboardURL,
	})
	if err != nil {
		log.Fatalf("[comment-analysis] %v", err)
	}
	bundle.Engine = engine
	bundle.Flush = cauc.NewFlushJob(engine)
	bundle.Backstop = cauc.NewBackstopJob(engine)
	bundle.Rollup = cauc.NewRollupJob(repo, settings, authors, rollups, state, clock)
	bundle.Purge = cauc.NewPurgeJob(repo, commentAnalysisRetention, clock)

	backfillDeps := cauc.BackfillDeps{
		Backfills: backfills, Settings: settings, Ingestor: bundle.Ingestor,
		Adapters: adapters, Verifiers: verifiers, Pricer: pricer, State: state, Clock: clock,
	}
	bundle.Backfill = cauc.NewBackfillJob(backfillDeps)
	estimate, start, get, cancel := cauc.NewBackfillUseCases(backfillDeps)
	getSettings, updateSettings := cauc.NewSettingsUseCases(settings, verifiers, clock)
	getContainer, putContainer, delContainer, listAccounts := cauc.NewContainerSettingsUseCases(settings, resolver, verifiers, clock)

	bundle.Handler = commentanalysishttp.NewHandler(commentanalysishttp.Deps{
		List:      cauc.NewListUseCase(repo),
		Stats:     cauc.NewStatsUseCase(repo),
		Trends:    cauc.NewTrendsUseCase(rollups),
		Authors:   cauc.NewListAuthorsUseCase(authors),
		Author:    cauc.NewGetAuthorUseCase(authors, repo),
		Moderate:  cauc.NewSetModerationStateUseCase(authors, clock),
		GetSet:    getSettings,
		UpdateSet: updateSettings,
		Retry:     cauc.NewRetryUseCase(repo, clock),
		Spend:     cauc.NewSpendUseCase(batches, clock),
		Estimate:  estimate,
		Start:     start,
		Backfill:  get,
		Cancel:    cancel,

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
