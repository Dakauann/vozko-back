package audience_usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/google/uuid"

	"vozko/brand"
	ca "vozko/domain/audience"
	"vozko/domain/balance"
	"vozko/domain/cache"
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

	// minBalanceFloor is the product-wide AI floor, fail-closed: below it no
	// call is made. Read from the domain rather than written out again, so
	// every path that spends on a model agrees on one number.
	minBalanceFloor int64 = balance.MinAIFloorMicros

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
	Settings ca.SettingsResolver
	Batches  ca.BatchRepository
	Adapters map[ca.Source]ca.SourceAdapter
	// Conversations is the second registry: the same channel vocabulary, a
	// narrower port. A channel can appear in one and not the other.
	Conversations map[ca.Source]ca.ConversationAdapter
	Classifier    ca.Classifier
	Scheduler     ca.Scheduler
	// Usage is the rolling volume budget: not money, but how much work a
	// workspace may do. Optional; nil means no ceiling.
	Usage ca.UsageLimiter
	// WorkspaceLimits is where an operator's ceiling is set, which is a
	// workspace-level decision and not an account-level one. Read through
	// ca.ResolveDailyCap so the number that stops a pass here is the same one
	// the dashboard reports; they used to be resolved separately, which is how
	// a screen ends up naming a limit nobody is enforcing. Optional: without it
	// the per-account ceiling stands, exactly as before.
	WorkspaceLimits ca.WorkspaceSettingsStore
	Balance         balance.CachedBalanceChecker
	State           cache.SharedState
	Metrics         metrics.AudienceMetricsRecorder
	Notifier        notification.Notifier
	// Broadcaster feeds the live view (§7). Optional: a deployment without a
	// socket classifies exactly the same.
	Broadcaster ca.AnalysisBroadcaster
	Timeline    ca.ConversationAnalysisObserver
	// Live is the per-conversation signal the CRM listens on. Optional, and
	// distinct from Broadcaster, which feeds the workspace-wide audience view
	// under a different permission.
	Live ca.ConversationAnalysisLive
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
		deps.Scheduler == nil, deps.State == nil:
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
	if deps.Conversations == nil {
		deps.Conversations = map[ca.Source]ca.ConversationAdapter{}
	}
	return &Engine{EngineDeps: deps, guard: newBalanceGuard(deps.Balance, "comment pass")}, nil
}

// subjectReader is what ProcessContainer actually needs from a channel: the
// text of each pending subject, and the context it sits in. Both kinds of
// subject can answer that, so the hot path below stays ONE path instead of
// branching on the kind at every step.
type subjectReader interface {
	ReadSubjects(ctx context.Context, ref ca.ContainerRef, ids []string) (map[string]subjectText, error)
	ReadContainerContext(ctx context.Context, ref ca.ContainerRef) (ca.ContainerContext, error)
}

// subjectText is one subject as the engine reads it. A comment brings only its
// words; a conversation also brings how many messages were read and when the
// last one arrived. Returning one shape for both is what keeps the loop below
// free of type assertions on the reader.
type subjectText struct {
	Text         string
	MessageCount int
	OccurredAt   time.Time
}

// commentReader adapts the original SourceAdapter, whose texts carry nothing
// beyond the words.
type commentReader struct{ adapter ca.SourceAdapter }

func (c commentReader) ReadSubjects(ctx context.Context, ref ca.ContainerRef, ids []string) (map[string]subjectText, error) {
	texts, err := c.adapter.ReadTexts(ctx, ref, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[string]subjectText, len(texts))
	for id, t := range texts {
		out[id] = subjectText{Text: t}
	}
	return out, nil
}

func (c commentReader) ReadContainerContext(ctx context.Context, ref ca.ContainerRef) (ca.ContainerContext, error) {
	return c.adapter.ReadContainerContext(ctx, ref)
}

// conversationReader adapts a ConversationAdapter to the same shape.
type conversationReader struct{ adapter ca.ConversationAdapter }

func (c conversationReader) ReadSubjects(ctx context.Context, ref ca.ContainerRef, ids []string) (map[string]subjectText, error) {
	transcripts, err := c.adapter.ReadTranscripts(ctx, ref, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[string]subjectText, len(transcripts))
	for id, t := range transcripts {
		out[id] = subjectText{Text: t.Text, MessageCount: t.MessageCount, OccurredAt: t.LastMessageAt}
	}
	return out, nil
}

func (c conversationReader) ReadContainerContext(ctx context.Context, ref ca.ContainerRef) (ca.ContainerContext, error) {
	return c.adapter.ReadContainerContext(ctx, ref)
}

// readerFor picks the adapter for the container's subject kind. The two
// registries are separate because they answer to different ports, and a
// channel may serve one and not the other: Instagram has both, WhatsApp has
// only conversations.
func (e *Engine) readerFor(ref ca.ContainerRef) (subjectReader, error) {
	if ref.Normalized().Kind == ca.SubjectKindConversation {
		adapter, ok := e.Conversations[ref.Source]
		if !ok || adapter == nil {
			return nil, fmt.Errorf("comment analysis: no conversation adapter registered for source %q", ref.Source)
		}
		return conversationReader{adapter: adapter}, nil
	}
	adapter, ok := e.Adapters[ref.Source]
	if !ok || adapter == nil {
		return nil, fmt.Errorf("comment analysis: no adapter registered for source %q", ref.Source)
	}
	return commentReader{adapter: adapter}, nil
}

// RegisterConversationSource wires a channel's conversation reader.
func (e *Engine) RegisterConversationSource(source ca.Source, adapter ca.ConversationAdapter) {
	if adapter != nil {
		e.Conversations[source] = adapter
	}
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
	// caps memoizes the resolved daily ceiling per workspace.
	//
	// A tick can walk many containers of the same workspace, and the ceiling is
	// one small read that would otherwise repeat for each of them. Holding it
	// for the tick also means every container in one pass is judged against the
	// same number, which is what an operator would assume anyway.
	caps map[string]int
}

func newCycle() *cycle { return &cycle{tokens: map[string]int{}, caps: map[string]int{}} }

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

	adapter, err := e.readerFor(ref)
	if err != nil {
		return res, err
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
	byID := make(map[string]*ca.Analysis, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
		byID[r.ID] = r
	}
	// A conversation queued since revisions exist carries its own transcript,
	// frozen when its inactivity window elapsed. Those rows are classified from
	// the snapshot, so they need no read at all: the channel cannot be asked to
	// re-render a conversation that has moved on since, and a read that fails
	// cannot cost them their analysis.
	needRead := unsnapshottedIDs(rows)
	subjects := map[string]subjectText{}
	if len(needRead) > 0 {
		subjects, err = adapter.ReadSubjects(ctx, ref, needRead)
		if err != nil {
			return res, err
		}
	}

	// A comment deleted on the channel between ingest and now has nothing
	// to classify. Skipped, not failed, and never sent.
	var vanished, missed []*ca.Analysis
	items := make([]ca.Item, 0, len(rows))
	for _, r := range rows {
		var subject subjectText
		ok := r.HasSnapshot()
		if ok {
			// Each queued revision is judged on the exact snapshot captured
			// when its inactivity window elapsed, even though the conversation
			// has since moved on. Later messages are a later revision.
			subject = subjectText{Text: r.Transcript, MessageCount: r.MessageCount, OccurredAt: r.OccurredAt}
		} else {
			subject, ok = subjects[r.SubjectID]
		}
		if !ok {
			// A conversation we queued ourselves is expected to still exist, so
			// a miss is read as a failed read and retried. Only a comment, which
			// really can be deleted on the channel, is skipped on the spot.
			if r.Kind() == ca.SubjectKindConversation {
				if r.MissedRead(ca.ReasonTextUnavailable, now) {
					vanished = append(vanished, r)
				} else {
					missed = append(missed, r)
				}
				continue
			}
			if err := r.MarkSkipped(ca.ReasonTextUnavailable, now); err == nil {
				vanished = append(vanished, r)
			}
			continue
		}
		// Facts the read brought back, recorded before classification so they
		// survive even a batch that fails: how much of the conversation was
		// actually read, and when it last moved. The timestamp is what buckets
		// the row in the daily rollups, so a conversation that started last
		// week but ended today lands on today.
		if subject.MessageCount > 0 {
			r.MessageCount = subject.MessageCount
		}
		if !subject.OccurredAt.IsZero() {
			r.OccurredAt = subject.OccurredAt
		}
		items = append(items, ca.Item{ID: r.ID, Text: subject.Text})
	}
	if len(vanished) > 0 {
		if err := e.Repo.SaveMany(ctx, vanished); err != nil {
			return res, err
		}
		res.Skipped += len(vanished)
		e.addItems(metrics.CommentItemOutcomeSkipped, len(vanished))
	}
	// Still pending, with one more attempt spent. Saved so the count survives
	// this process, which is what bounds the retries.
	if len(missed) > 0 {
		if err := e.Repo.SaveMany(ctx, missed); err != nil {
			return res, err
		}
		res.Deferred += len(missed)
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
	systemPrompt := BuildSystemPromptFor(ref.Normalized().Kind, settings.Topics, containerCtx, settings.Instructions)
	budget := e.Budget.WithCycleAllowance(cyc.remaining(workspaceID, e.Budget))
	plans, remainder := ca.PlanBatches(items, shared.EstimateTokens(systemPrompt), budget, ref.Normalized().Kind)
	if len(remainder.Unplanned) > 0 {
		res.Deferred += len(remainder.Unplanned)
		e.addItems(metrics.CommentItemOutcomeDeferred, len(remainder.Unplanned))
		if remainder.Reason == ca.RemainderCycleTokens {
			e.capHit(metrics.CommentCapCycle)
		}
	}

	for _, plan := range plans {
		if stop := e.guards(ctx, workspaceID, e.dailyCap(ctx, workspaceID, settings, cyc), len(plan.Items), now); stop != nil {
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
		batchRows := make([]*ca.Analysis, 0, len(plan.Items))
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
		for _, row := range batchRows {
			if row.Kind() != ca.SubjectKindConversation || row.Status != ca.StatusAnalyzed {
				continue
			}
			if e.Timeline != nil {
				e.Timeline.AnalysisCreated(row.WorkspaceID, row.SubjectID, string(row.Source), row.ID, string(row.Disposition), row.AttendanceQuality)
			}
			// The other half of the pair the ingest path opened: whoever is
			// looking at this conversation was told an analysis was coming, so
			// they are told when it lands rather than being left with a
			// spinner until they reload.
			if e.Live != nil {
				verdict := *row
				e.Live.AnalysisStateChanged(ca.ConversationAnalysisState{
					EntryID:   row.SubjectID,
					EntryType: string(row.Source),
					Pending:   false,
					Analysis:  &verdict,
				})
			}
		}
		// Same point, same posture: alerts read facts that are on disk.
		if e.Alerts != nil {
			e.Alerts.EvaluateBatch(ctx, ref, workspaceID, batchRows)
		}
		if outcome.providerErr != nil {
			// The rows were handed back without an attempt; nothing more this
			// tick. Logged by the caller, retried next tick.
			//
			// Their budget goes back with them. The claim is made before the
			// call, which is what stops two replicas overspending together, so
			// a provider that drops the batch has already been counted for work
			// nobody got. Under the counter this replaced, that stayed counted
			// until midnight.
			e.releaseUsage(ctx, workspaceID, len(plan.Items)-outcome.analyzed, now)
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

// guards is every reason NOT to make the next call (plan §8): the balance
// floor (fail-closed) and the rolling volume budget.
func (e *Engine) guards(ctx context.Context, workspaceID string, dailyCap, items int, now time.Time) error {
	// The floor itself lives in balanceGuard, shared with every other path that
	// spends tokens; the metric is this caller's own.
	if err := e.guard.Allow(workspaceID); err != nil {
		e.capHit(metrics.CommentCapBalance)
		return err
	}
	if e.Usage == nil {
		return nil
	}
	ok, err := e.Usage.Claim(ctx, workspaceID, items, dailyCap, now)
	if err != nil {
		log.Printf("[comment-analysis] usage budget check for workspace %s failed, skipping (fail-closed): %v", workspaceID, err)
		return ca.ErrDailyCapReached
	}
	if !ok {
		e.capHit(metrics.CommentCapDaily)
		e.notifyCapReached(workspaceID, dailyCap, now)
		return ca.ErrDailyCapReached
	}
	return nil
}

// dailyCap is the ceiling in force for this workspace, resolved once per tick.
//
// The workspace's own number wins; the account's is what every workspace
// configured before the workspace-level control existed still runs under. A
// read failure falls through to the account ceiling rather than to no ceiling:
// the alternative is that a database hiccup silently removes the spend limit.
func (e *Engine) dailyCap(ctx context.Context, workspaceID string, settings *ca.Settings, cyc *cycle) int {
	accountCap := 0
	if settings != nil {
		accountCap = settings.DailyCap
	}
	if cyc == nil || e.WorkspaceLimits == nil {
		return ca.ResolveDailyCap(0, accountCap)
	}
	if cap, ok := cyc.caps[workspaceID]; ok {
		return ca.ResolveDailyCap(cap, accountCap)
	}
	settingsRow, err := e.WorkspaceLimits.Get(ctx, workspaceID)
	workspaceCap := settingsRow.DailyCap
	if err != nil {
		log.Printf("[comment-analysis] workspace ceiling for %s unavailable, falling back to the account cap: %v", workspaceID, err)
		workspaceCap = 0
	}
	cyc.caps[workspaceID] = workspaceCap
	return ca.ResolveDailyCap(workspaceCap, accountCap)
}

// releaseUsage gives budget back for items that were claimed and then never
// classified.
//
// The claim happens before the model call, which is the only order that can
// stop two replicas overspending together. The consequence is that a batch the
// provider drops has already been counted, and under the old counter it stayed
// counted: a bad afternoon upstream could exhaust the budget having produced
// nothing. Best effort, and never allowed to fail the pass that is already
// handling an error.
func (e *Engine) releaseUsage(ctx context.Context, workspaceID string, items int, now time.Time) {
	if e.Usage == nil || items <= 0 {
		return
	}
	if err := e.Usage.Release(ctx, workspaceID, items, now); err != nil {
		log.Printf("[comment-analysis] could not release %d unused analyses for workspace %s: %v", items, workspaceID, err)
	}
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
	byID map[string]*ca.Analysis,
	depth int,
	now time.Time,
) planOutcome {
	var out planOutcome
	batchID := uuid.NewString()
	started := time.Now()
	result, err := e.Classifier.Classify(ctx, ca.ClassifyRequest{
		WorkspaceID:  workspaceID,
		SubjectKind:  ref.Normalized().Kind,
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

	e.recordBatch(ctx, receipt)
	return out
}

type reconcileOutcome struct{ analyzed, released int }

// reconcile is plan §7.3, and the test the literature demands: every ref
// exactly once, in range, labels in the rubric. Anything else is counted
// and never applied to the wrong comment.
func (e *Engine) reconcile(plan ca.BatchPlan, results []ca.BatchResult, settings *ca.Settings, byID map[string]*ca.Analysis, prov ca.Provenance, now time.Time) reconcileOutcome {
	var out reconcileOutcome
	byRef := make(map[int]*ca.Analysis, len(plan.Items))
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
		// Decoded and validated against the ROW's kind, not the batch's: a batch
		// is one container and one kind, and reading it per row means a mixed
		// batch could never silently validate under the wrong taxonomy.
		kind := row.Kind()
		c := r.ClassificationFor(kind)
		if err := c.ValidateFor(kind, settings.Topics); err != nil {
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
func (e *Engine) broadcastAnalyzed(rows []*ca.Analysis) {
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

func (e *Engine) notifyCapReached(workspaceID string, dailyCap int, now time.Time) {
	if e.Notifier == nil {
		return
	}
	day := now.UTC().Format("2006-01-02")
	if err := e.Notifier.Notify(notification.Notification{
		WorkspaceID: workspaceID,
		Subject:     "Limite diário de análise de comentários atingido - " + brand.Active().Name,
		Template:    capNotificationTemplate,
		Placeholders: map[string]interface{}{
			"DailyCap":     dailyCap,
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

// unsnapshottedIDs names the subjects whose text still has to be read, once
// each: a conversation can now have several pending revisions at a time, and
// asking the channel for the same entry three times would render the same
// transcript three times.
func unsnapshottedIDs(rows []*ca.Analysis) []string {
	out := make([]string, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, r := range rows {
		if r.HasSnapshot() {
			continue
		}
		if _, dup := seen[r.SubjectID]; dup {
			continue
		}
		seen[r.SubjectID] = struct{}{}
		out = append(out, r.SubjectID)
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

// RegisteredSources names what this engine can actually classify, split by the
// two subject kinds, sorted so the output is stable between boots.
//
// It exists because the startup line that reported this was a hardcoded string
// reading "sources: instagram" on every boot regardless of what was wired. That
// is worse than no log at all: it cost an afternoon of suspecting the
// conversation channels were unregistered when they were fine, and it would
// have said the same thing on a deployment where they genuinely were not.
//
// A channel appears here only if an adapter was actually registered for it, so
// the line answers the question an operator is really asking when they read it:
// what will this process classify?
func (e *Engine) RegisteredSources() (comments, conversations []string) {
	if e == nil {
		return nil, nil
	}
	for source, adapter := range e.Adapters {
		if adapter != nil {
			comments = append(comments, string(source))
		}
	}
	for source, adapter := range e.Conversations {
		if adapter != nil {
			conversations = append(conversations, string(source))
		}
	}
	sort.Strings(comments)
	sort.Strings(conversations)
	return comments, conversations
}
