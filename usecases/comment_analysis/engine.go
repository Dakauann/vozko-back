package comment_analysis_usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"

	"vozko/brand"
	"vozko/domain/balance"
	"vozko/domain/cache"
	ca "vozko/domain/comment_analysis"
	"vozko/domain/metrics"
	"vozko/domain/notification"
	"vozko/domain/shared"
)

// The engine: hints → containers → lock → list → plan → claim → classify →
// reconcile → persist → bill (plan §6, §7, §8, §9). One code path, entered
// by the debounce flush and by the DB-only backstop, which differ only in
// how they found the container.

const (
	containerLockPrefix = "lock:comment_analysis:"
	containerLockTTL    = 5 * time.Minute

	// minBalanceFloor is the same floor the conversation analysis job uses,
	// fail-closed: below it no call is made.
	minBalanceFloor int64 = 10_000

	// maxSplitDepth: two halvings, then singles (plan §7.2).
	maxSplitDepth = 2

	capNotificationTemplate = "comment_analysis_cap_reached.html"
)

// EngineDeps groups the engine's collaborators. Optional ones (balance,
// metrics, notifier) may be nil; the engine degrades to "no guard" for the
// balance check ONLY when the checker is absent by construction, exactly
// as the conversation job does.
type EngineDeps struct {
	Repo ca.Repository
	// Settings resolves the effective settings for a post: the account's with
	// the post's override on top. The engine never reads the repository
	// directly, so the fallback rule cannot be re-derived here.
	Settings   ca.SettingsResolver
	Batches    ca.BatchRepository
	Adapters   map[ca.Source]ca.SourceAdapter
	Classifier ca.Classifier
	Scheduler  ca.Scheduler
	Charger    ca.Charger
	Balance    balance.CachedBalanceChecker
	State      cache.SharedState
	Metrics    metrics.CommentAnalysisMetricsRecorder
	Notifier   notification.Notifier
	// Broadcaster feeds the live view (§7). Optional: a deployment without a
	// socket classifies exactly the same.
	Broadcaster ca.AnalysisBroadcaster
	// Alerts fires the configured rules. Optional, and best effort: a rule that
	// cannot be evaluated must never fail an analysis.
	Alerts ca.AlertEvaluator
	Clock  ca.Clock

	Budget       ca.Budget
	Debounce     ca.DebouncePolicy
	DashboardURL string
}

// Engine processes containers. Safe for concurrent use.
type Engine struct {
	EngineDeps
	// guard is the shared balance floor, the same one the reply, briefing and
	// author-role paths answer to.
	guard balanceGuard
}

func NewEngine(deps EngineDeps) (*Engine, error) {
	switch {
	case deps.Repo == nil, deps.Settings == nil, deps.Batches == nil, deps.Classifier == nil,
		deps.Scheduler == nil, deps.Charger == nil, deps.State == nil:
		return nil, errors.New("comment analysis engine: missing required dependency")
	}
	if deps.Clock == nil {
		deps.Clock = shared.SystemClock{}
	}
	deps.Budget.Normalize()
	if err := deps.Budget.Validate(); err != nil {
		return nil, err
	}
	deps.Debounce.Normalize()
	if deps.Adapters == nil {
		deps.Adapters = map[ca.Source]ca.SourceAdapter{}
	}
	return &Engine{EngineDeps: deps, guard: newBalanceGuard(deps.Balance, "comment pass")}, nil
}

// RegisterSource attaches a channel. Without one, that source's comments
// are simply never classified, visibly (they stay pending on the gauge).
func (e *Engine) RegisterSource(source ca.Source, adapter ca.SourceAdapter) {
	if adapter != nil {
		e.Adapters[source] = adapter
	}
}

// cycle is the per-tick ledger: tokens planned per workspace against the
// budget's per-cycle ceiling.
type cycle struct {
	tokens map[string]int
}

func newCycle() *cycle { return &cycle{tokens: map[string]int{}} }

func (c *cycle) remaining(workspaceID string, b ca.Budget) int {
	return b.MaxTokensPerCycle - c.tokens[workspaceID]
}

func (c *cycle) add(workspaceID string, n int) { c.tokens[workspaceID] += n }

// containerResult is what one pass over a container did, for logs and
// for the tests.
type containerResult struct {
	Analyzed, Released, Skipped, Deferred int
	Batches                               int
	Stopped                               error // ErrDailyCapReached, ErrBalanceBelowFloor, or nil
}

// ProcessContainer runs one container through the engine under its lock.
// Returning nil with work left behind is normal: the next tick, or the
// backstop, continues from the database.
func (e *Engine) ProcessContainer(ctx context.Context, ref ca.ContainerRef, workspaceID string, cyc *cycle) (containerResult, error) {
	var res containerResult
	if err := ref.Validate(); err != nil {
		return res, err
	}
	lockKey := containerLockPrefix + ref.Key()
	acquired, err := e.State.SetNX(lockKey, "1", containerLockTTL)
	if err != nil {
		return res, err
	}
	if !acquired {
		return res, nil
	}
	defer func() { _ = e.State.Del(lockKey) }()

	now := e.Clock.Now()

	settings, err := e.Settings.Resolve(ctx, ref)
	if err != nil {
		return res, err
	}
	if !settings.Enabled {
		// Switched off after ingest: the rows are skipped, visibly, and the
		// hint goes away. Turning the switch off stops all work immediately.
		return e.skipAll(ctx, ref, ca.ReasonAnalysisDisabled, now)
	}
	if workspaceID == "" {
		workspaceID = settings.WorkspaceID
	}

	adapter, ok := e.Adapters[ref.Source]
	if !ok {
		return res, fmt.Errorf("comment analysis: no adapter registered for source %q", ref.Source)
	}

	rows, err := e.Repo.ListPending(ctx, ref, e.Budget.MaxBatchItems*e.Budget.MaxBatchesPerCycle)
	if err != nil {
		return res, err
	}
	if len(rows) == 0 {
		_ = e.Scheduler.Clear(ctx, ref)
		return res, nil
	}

	ids := make([]string, len(rows))
	byID := make(map[string]*ca.CommentAnalysis, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
		byID[r.ID] = r
	}
	texts, err := adapter.ReadTexts(ctx, ref, sourceIDs(rows))
	if err != nil {
		return res, err
	}

	// A comment deleted on the channel between ingest and now has nothing
	// to classify. Skipped, not failed, and never sent.
	var vanished []*ca.CommentAnalysis
	items := make([]ca.Item, 0, len(rows))
	for _, r := range rows {
		text, ok := texts[r.SourceCommentID]
		if !ok {
			if err := r.MarkSkipped(ca.ReasonTextUnavailable, now); err == nil {
				vanished = append(vanished, r)
			}
			continue
		}
		items = append(items, ca.Item{ID: r.ID, Text: text})
	}
	if len(vanished) > 0 {
		if err := e.Repo.SaveMany(ctx, vanished); err != nil {
			return res, err
		}
		res.Skipped += len(vanished)
		e.addItems(metrics.CommentItemOutcomeSkipped, len(vanished))
	}
	if len(items) == 0 {
		_ = e.Scheduler.Clear(ctx, ref)
		return res, nil
	}

	containerCtx, err := adapter.ReadContainerContext(ctx, ref)
	if err != nil {
		// The caption helps; its absence is not a reason to stop.
		log.Printf("[comment-analysis] container context for %s unavailable: %v", ref.Key(), err)
		containerCtx = ca.ContainerContext{}
	}
	systemPrompt := BuildSystemPrompt(settings.Topics, containerCtx, settings.Instructions)
	budget := e.Budget.WithCycleAllowance(cyc.remaining(workspaceID, e.Budget))
	plans, remainder := ca.PlanBatches(items, shared.EstimateTokens(systemPrompt), budget)
	if len(remainder.Unplanned) > 0 {
		res.Deferred += len(remainder.Unplanned)
		e.addItems(metrics.CommentItemOutcomeDeferred, len(remainder.Unplanned))
		if remainder.Reason == ca.RemainderCycleTokens {
			e.capHit(metrics.CommentCapCycle)
		}
	}

	for _, plan := range plans {
		if stop := e.guards(ctx, workspaceID, settings, len(plan.Items), now); stop != nil {
			res.Stopped = stop
			break
		}

		claimed, err := e.Repo.ClaimByIDs(ctx, plan.IDs(), now)
		if err != nil {
			return res, err
		}
		plan = keepClaimed(plan, claimed)
		if len(plan.Items) == 0 {
			continue
		}
		batchRows := make([]*ca.CommentAnalysis, 0, len(plan.Items))
		for _, it := range plan.Items {
			row := byID[it.ID]
			_ = row.Claim(now) // mirrors what ClaimByIDs did in the database
			batchRows = append(batchRows, row)
		}
		cyc.add(workspaceID, plan.EstimatedTotalTokens())
		e.addTruncated(plan)

		outcome := e.runPlan(ctx, ref, workspaceID, settings, containerCtx, plan, byID, 0, now)
		res.Analyzed += outcome.analyzed
		res.Released += outcome.released
		res.Batches += outcome.batches

		if err := e.Repo.SaveMany(ctx, batchRows); err != nil {
			return res, err
		}
		// After the rows are STORED, never before: a live row a viewer sees
		// must be one that survived the write. Best effort, and deliberately
		// last, so a socket having a bad day cannot fail an analysis that was
		// already paid for.
		e.broadcastAnalyzed(batchRows)
		// Same point, same posture: alerts read facts that are on disk.
		if e.Alerts != nil {
			e.Alerts.EvaluateBatch(ctx, ref, workspaceID, batchRows)
		}
		if outcome.providerErr != nil {
			// The rows were handed back without an attempt; nothing more this
			// tick. Logged by the caller, retried next tick.
			return res, outcome.providerErr
		}
	}

	// The hint stays whenever anything is still pending here: deferred by
	// the cycle, stopped by a cap, or released back for a retry. Only a
	// container with nothing left drops it.
	if len(remainder.Unplanned) == 0 && res.Stopped == nil && res.Released == 0 {
		_ = e.Scheduler.Clear(ctx, ref)
	}
	return res, nil
}

// guards is every reason NOT to make the next call (plan §8): balance floor
// (fail-closed) and the daily cap.
func (e *Engine) guards(ctx context.Context, workspaceID string, settings *ca.Settings, items int, now time.Time) error {
	// The floor itself lives in balanceGuard, shared with every other path that
	// spends tokens; the metric is this caller's own.
	if err := e.guard.Allow(workspaceID); err != nil {
		e.capHit(metrics.CommentCapBalance)
		return err
	}
	ok, err := e.Charger.ReserveDaily(ctx, workspaceID, items, settings.DailyCap, now)
	if err != nil {
		log.Printf("[comment-analysis] daily cap check for workspace %s failed, skipping (fail-closed): %v", workspaceID, err)
		return ca.ErrDailyCapReached
	}
	if !ok {
		e.capHit(metrics.CommentCapDaily)
		e.notifyCapReached(workspaceID, settings, now)
		return ca.ErrDailyCapReached
	}
	return nil
}

type planOutcome struct {
	analyzed, released, batches int
	providerErr                 error
}

// runPlan classifies one batch, halving on a truncated response (plan
// §7.2) and reconciling refs (plan §7.3). Rows are mutated in memory; the
// caller persists them.
func (e *Engine) runPlan(
	ctx context.Context,
	ref ca.ContainerRef,
	workspaceID string,
	settings *ca.Settings,
	containerCtx ca.ContainerContext,
	plan ca.BatchPlan,
	byID map[string]*ca.CommentAnalysis,
	depth int,
	now time.Time,
) planOutcome {
	var out planOutcome
	batchID := uuid.NewString()
	started := time.Now()
	result, err := e.Classifier.Classify(ctx, ca.ClassifyRequest{
		WorkspaceID:  workspaceID,
		Model:        settings.Model,
		Topics:       settings.Topics,
		Context:      containerCtx,
		Instructions: settings.Instructions,
		Batch:        plan,
	})
	e.observeLatency(time.Since(started))
	out.batches++

	model := settings.Model
	if result != nil && result.Model != "" {
		model = result.Model
	}
	receipt := ca.Batch{
		ID: batchID, WorkspaceID: workspaceID, Source: ref.Source, AccountID: ref.AccountID, ContainerID: ref.ContainerID,
		Model: model, ItemCount: len(plan.Items), Outcome: ca.OutcomeOK, CreatedAt: now,
	}
	if result != nil {
		receipt.PromptTokens, receipt.CompletionTokens, receipt.RequestID = result.PromptTokens, result.CompletionTokens, result.RequestID
		e.addTokens(model, result.PromptTokens, result.CompletionTokens)
	}

	switch {
	case err != nil && result == nil:
		// The provider did not answer. Not the comments' fault: hand every
		// row back without an attempt and stop the container this tick.
		receipt.Outcome = ca.OutcomeProviderError
		for _, it := range plan.Items {
			byID[it.ID].Unclaim(now)
		}
		e.recordBatch(ctx, receipt)
		out.providerErr = fmt.Errorf("comment analysis: classify %s: %w", ref.Key(), err)
		return out

	case result.FinishReason == "length":
		receipt.Outcome = ca.OutcomeLength
		e.recordBatch(ctx, receipt)
		if len(plan.Items) == 1 || depth >= maxSplitDepth+1 {
			// Even alone it does not fit: this attempt is spent.
			for _, it := range plan.Items {
				byID[it.ID].Release(ca.ReasonResponseTruncated, now)
			}
			out.released += len(plan.Items)
			e.addItems(metrics.CommentItemOutcomeFailed, len(plan.Items))
			return out
		}
		var parts []ca.BatchPlan
		if depth < maxSplitDepth {
			parts = plan.Split()
		} else {
			// Two halvings done: singles.
			for _, it := range plan.Items {
				parts = append(parts, plan.Subset([]ca.PlannedItem{it}))
			}
		}
		for _, part := range parts {
			sub := e.runPlan(ctx, ref, workspaceID, settings, containerCtx, part, byID, depth+1, now)
			out.analyzed += sub.analyzed
			out.released += sub.released
			out.batches += sub.batches
			if sub.providerErr != nil {
				out.providerErr = sub.providerErr
				return out
			}
		}
		return out

	case err != nil:
		// Answered, but not with JSON we can read. The attempt is spent.
		receipt.Outcome = ca.OutcomeParseError
		e.recordBatch(ctx, receipt)
		for _, it := range plan.Items {
			byID[it.ID].Release(ca.ReasonUnparseableResponse, now)
		}
		out.released += len(plan.Items)
		e.addItems(metrics.CommentItemOutcomeFailed, len(plan.Items))
		return out
	}

	analyzed := e.reconcile(plan, result.Results, settings, byID, ca.Provenance{BatchID: batchID, Model: model}, now)
	out.analyzed = analyzed.analyzed
	out.released = analyzed.released

	if analyzed.analyzed > 0 {
		price, err := e.Charger.ChargeBatch(ctx, workspaceID, batchID, analyzed.analyzed)
		if err != nil {
			// Loud: the classification happened and was token-billed; a
			// missing surcharge is revenue, not correctness.
			log.Printf("[comment-analysis] CRITICAL: surcharge for batch %s (workspace %s, %d comments) failed: %v", batchID, workspaceID, analyzed.analyzed, err)
		}
		receipt.PriceMicros = price
	}
	e.recordBatch(ctx, receipt)
	return out
}

type reconcileOutcome struct{ analyzed, released int }

// reconcile is plan §7.3, and the test the literature demands: every ref
// exactly once, in range, labels in the rubric. Anything else is counted
// and never applied to the wrong comment.
func (e *Engine) reconcile(plan ca.BatchPlan, results []ca.BatchResult, settings *ca.Settings, byID map[string]*ca.CommentAnalysis, prov ca.Provenance, now time.Time) reconcileOutcome {
	var out reconcileOutcome
	byRef := make(map[int]*ca.CommentAnalysis, len(plan.Items))
	for _, it := range plan.Items {
		byRef[it.Ref] = byID[it.ID]
	}
	seen := make(map[int]bool, len(results))
	for _, r := range results {
		row, ok := byRef[r.Ref]
		if !ok || seen[r.Ref] {
			e.addItems(metrics.CommentItemOutcomeInvalidRef, 1)
			continue
		}
		seen[r.Ref] = true
		c := r.Classification()
		if err := c.Validate(settings.Topics); err != nil {
			row.Release(ca.ReasonInvalidLabels, now)
			out.released++
			e.addItems(metrics.CommentItemOutcomeBadLabels, 1)
			continue
		}
		if err := row.Apply(c, settings.ActionPolicy, prov, now); err != nil {
			row.Release(ca.ReasonInvalidLabels, now)
			out.released++
			e.addItems(metrics.CommentItemOutcomeFailed, 1)
			continue
		}
		out.analyzed++
	}
	for ref, row := range byRef {
		if !seen[ref] && row.Status == ca.StatusInFlight {
			row.Release(ca.ReasonMissingRef, now)
			out.released++
			e.addItems(metrics.CommentItemOutcomeMissingRef, 1)
		}
	}
	e.addItems(metrics.CommentItemOutcomeAnalyzed, out.analyzed)
	return out
}

func (e *Engine) skipAll(ctx context.Context, ref ca.ContainerRef, reason string, now time.Time) (containerResult, error) {
	var res containerResult
	for {
		rows, err := e.Repo.ListPending(ctx, ref, 500)
		if err != nil {
			return res, err
		}
		if len(rows) == 0 {
			break
		}
		for _, r := range rows {
			_ = r.MarkSkipped(reason, now)
		}
		if err := e.Repo.SaveMany(ctx, rows); err != nil {
			return res, err
		}
		res.Skipped += len(rows)
		e.addItems(metrics.CommentItemOutcomeSkipped, len(rows))
	}
	_ = e.Scheduler.Clear(ctx, ref)
	return res, nil
}

// broadcastAnalyzed publishes one batch to the live feed. One event per batch
// rather than per comment is the coalescing the plan asks for: the engine
// already works in batches, so this is where a backfill's thousands of rows
// become tens of messages instead of thousands.
func (e *Engine) broadcastAnalyzed(rows []*ca.CommentAnalysis) {
	if e.Broadcaster == nil {
		return
	}
	event := ca.NewAnalysisBatchAnalyzed(rows)
	if event == nil {
		return
	}
	e.Broadcaster.BroadcastCommentsAnalyzed(*event)
}

func (e *Engine) recordBatch(ctx context.Context, b ca.Batch) {
	writeBatch(ctx, e.Batches, &b)
	if e.Metrics != nil {
		e.Metrics.IncCommentBatches(b.Model, string(b.Outcome))
	}
}

func (e *Engine) notifyCapReached(workspaceID string, settings *ca.Settings, now time.Time) {
	if e.Notifier == nil {
		return
	}
	day := now.UTC().Format("2006-01-02")
	if err := e.Notifier.Notify(notification.Notification{
		WorkspaceID: workspaceID,
		Subject:     "Limite diário de análise de comentários atingido - " + brand.Active().Name,
		Template:    capNotificationTemplate,
		Placeholders: map[string]interface{}{
			"DailyCap":     settings.DailyCap,
			"Date":         day,
			"DashboardURL": e.DashboardURL,
		},
		DedupKey: "comment_analysis_cap:" + workspaceID + ":" + day,
		DedupTTL: 24 * time.Hour,
	}); err != nil {
		log.Printf("[comment-analysis] cap notification for workspace %s failed: %v", workspaceID, err)
	}
}

// ---- helpers ----

func sourceIDs(rows []*ca.CommentAnalysis) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.SourceCommentID
	}
	return out
}

// keepClaimed drops the items another replica claimed first. Refs are kept
// as planned; gaps are fine, the model only has to echo what it received.
func keepClaimed(plan ca.BatchPlan, claimed []string) ca.BatchPlan {
	if len(claimed) == len(plan.Items) {
		return plan
	}
	ok := make(map[string]bool, len(claimed))
	for _, id := range claimed {
		ok[id] = true
	}
	kept := make([]ca.PlannedItem, 0, len(claimed))
	for _, it := range plan.Items {
		if ok[it.ID] {
			kept = append(kept, it)
		}
	}
	return plan.Subset(kept)
}

func (e *Engine) addItems(outcome string, n int) {
	if e.Metrics != nil && n > 0 {
		e.Metrics.AddCommentItems(outcome, n)
	}
}

func (e *Engine) addTokens(model string, prompt, completion int) {
	if e.Metrics == nil {
		return
	}
	e.Metrics.AddCommentTokens(model, metrics.CommentTokenKindPrompt, prompt)
	e.Metrics.AddCommentTokens(model, metrics.CommentTokenKindCompletion, completion)
}

func (e *Engine) addTruncated(plan ca.BatchPlan) {
	if e.Metrics == nil {
		return
	}
	n := 0
	for _, it := range plan.Items {
		if it.Truncated {
			n++
		}
	}
	e.Metrics.AddCommentTruncated(n)
}

func (e *Engine) observeLatency(d time.Duration) {
	if e.Metrics != nil {
		e.Metrics.ObserveCommentBatchLatency(d)
	}
}

func (e *Engine) capHit(cap string) {
	if e.Metrics != nil {
		e.Metrics.IncCommentCapHit(cap)
	}
}
