package comment_analysis_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	ca "vozko/domain/comment_analysis"
	"vozko/domain/shared"
)

// The plan's second test block (§15): what the engine does with what the
// model returns, asserted on the rows that end up persisted.

func smallBudget() ca.Budget {
	b := ca.DefaultBudget()
	b.MaxBatchItems = 20
	return b
}

// The happy path: 20 comments, one call, 20 analysed rows carrying the
// labels for THEIR ref.
func TestEngine_ClassifiesABatch(t *testing.T) {
	h := newHarness(t, smallBudget())
	h.seed(20)
	h.classifier.push(func(req ca.ClassifyRequest) (*ca.ClassifyResult, error) {
		res := &ca.ClassifyResult{FinishReason: "stop", Model: "m", PromptTokens: 900, CompletionTokens: 400}
		for _, it := range req.Batch.Items {
			if it.Ref == 3 {
				res.Results = append(res.Results, hostileResult(3))
				continue
			}
			res.Results = append(res.Results, okResult(it.Ref))
		}
		return res, nil
	})

	res, err := h.engine.ProcessContainer(context.Background(), ref(), "ws-1", newCycle())
	if err != nil {
		t.Fatal(err)
	}
	if res.Analyzed != 20 || res.Batches != 1 || res.Released != 0 {
		t.Fatalf("result = %+v", res)
	}
	if h.repo.countStatus(ca.StatusAnalyzed) != 20 {
		t.Fatalf("analyzed rows = %d", h.repo.countStatus(ca.StatusAnalyzed))
	}
	// Ref 3 was the third item planned: the oldest-first order is c-1, c-2, c-3.
	third := h.repo.bySourceID("c-3")
	if third.Stance != ca.StanceHostile || third.Severity != 80 || !third.RequiresAction {
		t.Fatalf("labels did not land on the right comment: %+v", third)
	}
	if first := h.repo.bySourceID("c-1"); first.Stance != ca.StanceSupporter || first.TopicKey != "saude" {
		t.Fatalf("c-1 = %+v", first)
	}
	if len(h.batches.rows) != 1 || h.batches.rows[0].ItemCount != 20 || h.batches.rows[0].PromptTokens != 900 {
		t.Fatalf("receipt = %+v", h.batches.rows)
	}
	if len(h.charger.charges) != 1 || h.charger.charges[0] != 20 {
		t.Fatalf("surcharge charged for %v, want [20]", h.charger.charges)
	}
	if _, still := h.scheduler.hints[ref().Key()]; still {
		t.Fatal("a fully flushed container must drop its hint")
	}
	// The call carried the guards of §7.1 as far as the port can show them.
	call := h.classifier.Calls[0]
	if call.WorkspaceID != "ws-1" {
		t.Fatal("WorkspaceID must be on the request (token billing)")
	}
}

// THE reconciliation test. A response missing ref 7 leaves item 7 pending
// with attempts=1 and persists the other 19; a duplicated ref and an
// out-of-range ref are dropped, not applied to the wrong comment.
func TestEngine_ReconcilesRefs(t *testing.T) {
	h := newHarness(t, smallBudget())
	h.seed(20)
	h.classifier.push(func(req ca.ClassifyRequest) (*ca.ClassifyResult, error) {
		res := &ca.ClassifyResult{FinishReason: "stop", Model: "m"}
		for _, it := range req.Batch.Items {
			if it.Ref == 7 {
				continue // missing
			}
			res.Results = append(res.Results, okResult(it.Ref))
		}
		// A duplicate of ref 2 with hostile labels: must NOT overwrite c-2.
		res.Results = append(res.Results, hostileResult(2))
		// Refs the batch never had.
		res.Results = append(res.Results, hostileResult(21), hostileResult(0), hostileResult(-4))
		return res, nil
	})

	res, err := h.engine.ProcessContainer(context.Background(), ref(), "ws-1", newCycle())
	if err != nil {
		t.Fatal(err)
	}
	if res.Analyzed != 19 || res.Released != 1 {
		t.Fatalf("result = %+v", res)
	}
	seven := h.repo.bySourceID("c-7")
	if seven.Status != ca.StatusPending || seven.Attempts != 1 || seven.FailureReason != ca.ReasonMissingRef {
		t.Fatalf("c-7 = %+v", seven)
	}
	two := h.repo.bySourceID("c-2")
	if two.Stance != ca.StanceSupporter || two.Severity != 0 {
		t.Fatalf("the duplicate ref overwrote c-2: %+v", two)
	}
	if h.repo.countStatus(ca.StatusAnalyzed) != 19 {
		t.Fatalf("analyzed = %d", h.repo.countStatus(ca.StatusAnalyzed))
	}
	if _, still := h.scheduler.hints[ref().Key()]; !still {
		t.Fatal("with a row still pending the hint must stay for the next tick")
	}
}

// An out-of-set label is refused per item; the other items land.
func TestEngine_BadLabelsReleaseOnlyThatItem(t *testing.T) {
	h := newHarness(t, smallBudget())
	h.seed(3)
	h.classifier.push(func(req ca.ClassifyRequest) (*ca.ClassifyResult, error) {
		bad := okResult(2)
		bad.Stance = "hater"
		return &ca.ClassifyResult{FinishReason: "stop", Results: []ca.BatchResult{okResult(1), bad, okResult(3)}}, nil
	})
	res, err := h.engine.ProcessContainer(context.Background(), ref(), "ws-1", newCycle())
	if err != nil {
		t.Fatal(err)
	}
	if res.Analyzed != 2 || res.Released != 1 {
		t.Fatalf("result = %+v", res)
	}
	if r := h.repo.bySourceID("c-2"); r.Status != ca.StatusPending || r.FailureReason != ca.ReasonInvalidLabels {
		t.Fatalf("c-2 = %+v", r)
	}
}

// FinishReason "length": the truncated body is never parsed; the batch is
// halved and retried, and the halves succeed.
func TestEngine_LengthHalvesAndRetries(t *testing.T) {
	h := newHarness(t, smallBudget())
	h.seed(8)
	h.classifier.push(func(req ca.ClassifyRequest) (*ca.ClassifyResult, error) {
		return &ca.ClassifyResult{FinishReason: "length", Model: "m", CompletionTokens: 4000}, nil
	})
	// The two halves answer normally (default script).
	res, err := h.engine.ProcessContainer(context.Background(), ref(), "ws-1", newCycle())
	if err != nil {
		t.Fatal(err)
	}
	if res.Analyzed != 8 || res.Batches != 3 {
		t.Fatalf("result = %+v", res)
	}
	if len(h.classifier.Calls) != 3 || len(h.classifier.Calls[1].Batch.Items) != 4 || len(h.classifier.Calls[2].Batch.Items) != 4 {
		t.Fatalf("calls = %d, halves = %d/%d", len(h.classifier.Calls), len(h.classifier.Calls[1].Batch.Items), len(h.classifier.Calls[2].Batch.Items))
	}
	// Each half was renumbered 1..4 for the model.
	for _, it := range h.classifier.Calls[2].Batch.Items {
		if it.Ref > 4 {
			t.Fatalf("half not renumbered: ref %d", it.Ref)
		}
	}
	// And the receipts record the truncated call too, with its tokens.
	if len(h.batches.rows) != 3 || h.batches.rows[0].Outcome != ca.OutcomeLength || h.batches.rows[0].CompletionTokens != 4000 {
		t.Fatalf("receipts = %+v", h.batches.rows)
	}
}

// A single item that still does not fit after halving spends its attempt.
func TestEngine_LengthOnASingleSpendsTheAttempt(t *testing.T) {
	h := newHarness(t, smallBudget())
	h.seed(1)
	h.classifier.push(func(req ca.ClassifyRequest) (*ca.ClassifyResult, error) {
		return &ca.ClassifyResult{FinishReason: "length"}, nil
	})
	res, err := h.engine.ProcessContainer(context.Background(), ref(), "ws-1", newCycle())
	if err != nil {
		t.Fatal(err)
	}
	if res.Released != 1 {
		t.Fatalf("result = %+v", res)
	}
	if r := h.repo.bySourceID("c-1"); r.Status != ca.StatusPending || r.Attempts != 1 || r.FailureReason != ca.ReasonResponseTruncated {
		t.Fatalf("c-1 = %+v", r)
	}
}

// MaxAttempts turns the row into a visible failure; the retry use case
// resets it.
func TestEngine_MaxAttemptsThenRetry(t *testing.T) {
	h := newHarness(t, smallBudget())
	h.seed(1)
	for i := 0; i < ca.MaxAttempts; i++ {
		h.classifier.push(func(req ca.ClassifyRequest) (*ca.ClassifyResult, error) {
			return &ca.ClassifyResult{FinishReason: "stop", Results: nil}, nil // never answers
		})
		if _, err := h.engine.ProcessContainer(context.Background(), ref(), "ws-1", newCycle()); err != nil {
			t.Fatal(err)
		}
	}
	r := h.repo.bySourceID("c-1")
	if r.Status != ca.StatusFailed || r.Attempts != ca.MaxAttempts || r.FailureReason != ca.ReasonMissingRef {
		t.Fatalf("after %d attempts: %+v", ca.MaxAttempts, r)
	}

	retried, err := NewRetryUseCase(h.repo, fixedClock{now}).Execute(context.Background(), "ws-1", r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if retried.Status != ca.StatusPending || retried.Attempts != 0 || retried.FailureReason != "" {
		t.Fatalf("after retry: %+v", retried)
	}
	// Another workspace cannot retry it.
	if _, err := NewRetryUseCase(h.repo, fixedClock{now}).Execute(context.Background(), "ws-2", r.ID); !errors.Is(err, ca.ErrNotFound) {
		t.Fatalf("cross-workspace retry: %v", err)
	}
}

// A provider outage hands every row back WITHOUT an attempt and stops the
// container for this tick.
func TestEngine_ProviderErrorUnclaims(t *testing.T) {
	h := newHarness(t, smallBudget())
	h.seed(5)
	h.classifier.push(func(req ca.ClassifyRequest) (*ca.ClassifyResult, error) {
		return nil, errors.New("502 from provider")
	})
	_, err := h.engine.ProcessContainer(context.Background(), ref(), "ws-1", newCycle())
	if err == nil {
		t.Fatal("a provider error must surface to the job")
	}
	if h.repo.countStatus(ca.StatusPending) != 5 {
		t.Fatalf("pending = %d, want 5", h.repo.countStatus(ca.StatusPending))
	}
	if r := h.repo.bySourceID("c-1"); r.Attempts != 0 {
		t.Fatalf("an outage must not consume an attempt: %+v", r)
	}
	if len(h.batches.rows) != 1 || h.batches.rows[0].Outcome != ca.OutcomeProviderError {
		t.Fatalf("receipts = %+v", h.batches.rows)
	}
}

// Balance below the floor: no AI call at all, rows untouched.
func TestEngine_BalanceFloorIsFailClosed(t *testing.T) {
	h := newHarness(t, smallBudget())
	h.seed(5)
	h.balance.micros = minBalanceFloor - 1
	res, err := h.engine.ProcessContainer(context.Background(), ref(), "ws-1", newCycle())
	if err != nil {
		t.Fatal(err)
	}
	if len(h.classifier.Calls) != 0 {
		t.Fatal("the AI service must never be invoked below the balance floor")
	}
	if !errors.Is(res.Stopped, ca.ErrBalanceBelowFloor) || h.repo.countStatus(ca.StatusPending) != 5 {
		t.Fatalf("result = %+v pending=%d", res, h.repo.countStatus(ca.StatusPending))
	}
	if r := h.repo.bySourceID("c-1"); r.Attempts != 0 {
		t.Fatal("no attempt may be counted when nothing was sent")
	}
}

// Daily cap exhausted: the cycle stops, rows stay pending, the hint stays.
func TestEngine_DailyCapStopsTheCycle(t *testing.T) {
	b := smallBudget()
	b.MaxBatchItems = 10
	h := newHarness(t, b)
	h.seed(30)
	h.charger.capLeft = 15 // one batch of 10 fits, the second does not

	res, err := h.engine.ProcessContainer(context.Background(), ref(), "ws-1", newCycle())
	if err != nil {
		t.Fatal(err)
	}
	if res.Analyzed != 10 || !errors.Is(res.Stopped, ca.ErrDailyCapReached) {
		t.Fatalf("result = %+v", res)
	}
	if len(h.classifier.Calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(h.classifier.Calls))
	}
	if h.repo.countStatus(ca.StatusPending) != 20 {
		t.Fatalf("pending = %d, want 20", h.repo.countStatus(ca.StatusPending))
	}
	if _, still := h.scheduler.hints[ref().Key()]; !still {
		t.Fatal("the hint must survive a cap stop")
	}
}

// The per-cycle token ceiling defers whole batches to the next tick and
// never counts an attempt against a deferred row.
func TestEngine_CycleCeilingDefers(t *testing.T) {
	b := smallBudget()
	b.MaxBatchItems = 10
	h := newHarness(t, b)
	h.seed(25)

	// Spend the workspace's cycle down to exactly one batch's ESTIMATE (the
	// ceiling counts estimates, not maxima), the way earlier containers in
	// the same tick would have.
	var items []ca.Item
	for i := 1; i <= 25; i++ {
		items = append(items, ca.Item{ID: "row-" + itoa(i), Text: h.adapter.texts["c-"+itoa(i)]})
	}
	sys := BuildSystemPrompt(enabledSettings().Topics, ca.ContainerContext{Caption: h.adapter.caption}, "")
	plans, _ := ca.PlanBatches(items, shared.EstimateTokens(sys), b)
	oneBatch := plans[0].EstimatedTotalTokens()
	cyc := newCycle()
	cyc.add("ws-1", b.MaxTokensPerCycle-oneBatch)

	res, err := h.engine.ProcessContainer(context.Background(), ref(), "ws-1", cyc)
	if err != nil {
		t.Fatal(err)
	}
	if res.Analyzed != 10 || res.Deferred != 15 {
		t.Fatalf("result = %+v", res)
	}
	if r := h.repo.bySourceID("c-25"); r.Status != ca.StatusPending || r.Attempts != 0 {
		t.Fatalf("deferred row was touched: %+v", r)
	}
	// The same cycle has no room left for a second container of the
	// workspace either.
	if cyc.remaining("ws-1", b) > 0 {
		t.Fatalf("cycle should be spent, %d left", cyc.remaining("ws-1", b))
	}
}

// Turning the switch off stops all work: pending rows are skipped, visibly.
func TestEngine_DisabledSkipsPending(t *testing.T) {
	h := newHarness(t, smallBudget())
	h.seed(4)
	s := enabledSettings()
	s.Enabled = false
	_ = h.settings.Save(context.Background(), s)

	res, err := h.engine.ProcessContainer(context.Background(), ref(), "ws-1", newCycle())
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped != 4 || len(h.classifier.Calls) != 0 || h.repo.countStatus(ca.StatusSkipped) != 4 {
		t.Fatalf("result = %+v skipped=%d calls=%d", res, h.repo.countStatus(ca.StatusSkipped), len(h.classifier.Calls))
	}
}

// A comment deleted on the channel between ingest and flush is skipped and
// never sent; the others go through.
func TestEngine_VanishedTextIsSkipped(t *testing.T) {
	h := newHarness(t, smallBudget())
	h.seed(3)
	delete(h.adapter.texts, "c-2")
	res, err := h.engine.ProcessContainer(context.Background(), ref(), "ws-1", newCycle())
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped != 1 || res.Analyzed != 2 {
		t.Fatalf("result = %+v", res)
	}
	if r := h.repo.bySourceID("c-2"); r.Status != ca.StatusSkipped || r.FailureReason != ca.ReasonTextUnavailable {
		t.Fatalf("c-2 = %+v", r)
	}
	if len(h.classifier.Calls[0].Batch.Items) != 2 {
		t.Fatal("the vanished comment must not be sent")
	}
}

// Two replicas on one post: the lock makes the second a no-op.
func TestEngine_ContainerLockIsExclusive(t *testing.T) {
	h := newHarness(t, smallBudget())
	h.seed(2)
	if _, err := h.state.SetNX(containerLockPrefix+ref().Key(), "other-replica", time.Minute); err != nil {
		t.Fatal(err)
	}
	res, err := h.engine.ProcessContainer(context.Background(), ref(), "ws-1", newCycle())
	if err != nil {
		t.Fatal(err)
	}
	if res.Analyzed != 0 || len(h.classifier.Calls) != 0 {
		t.Fatal("a locked container must not be processed")
	}
}

// ---- jobs ----

// The flush job only touches DUE hints; the backstop finds work without
// any hint at all and resets rows a dead replica left in flight.
func TestFlushAndBackstopJobs(t *testing.T) {
	h := newHarness(t, smallBudget())
	h.seed(3)
	// Make the hint look fresh: last comment seconds ago, not due.
	_ = h.scheduler.Clear(context.Background(), ref())
	_ = h.scheduler.Stamp(context.Background(), ref(), "ws-1", now.Add(-10*time.Second))

	if err := NewFlushJob(h.engine).Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(h.classifier.Calls) != 0 {
		t.Fatal("a post still receiving comments must wait")
	}

	// A row a dead replica left in flight an hour ago.
	stuck, _ := ca.NewPending(ca.NewInput{WorkspaceID: "ws-1", Container: ref(), SourceCommentID: "c-stuck",
		AuthorExternalID: "u", Text: "x", Now: now.Add(-2 * time.Hour)})
	stuck.ID = "row-stuck"
	_ = stuck.Claim(now.Add(-time.Hour))
	_ = h.repo.Save(context.Background(), stuck)
	h.adapter.texts["c-stuck"] = "x"
	// And drop the hint entirely: the backstop must not need it.
	_ = h.scheduler.Clear(context.Background(), ref())

	// The seeded rows were created "now" and the backstop only sweeps rows
	// older than MaxAge; move the clock forward.
	h.engine.Clock = fixedClock{now.Add(time.Hour)}
	if err := NewBackstopJob(h.engine).Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.repo.countStatus(ca.StatusAnalyzed) != 4 { // 3 seeded + the reset one
		t.Fatalf("analyzed = %d, want 4", h.repo.countStatus(ca.StatusAnalyzed))
	}
	if r := h.repo.bySourceID("c-stuck"); r.Attempts != 2 { // 1 from the dead claim, 1 from this one
		t.Fatalf("stuck row attempts = %d, want 2", r.Attempts)
	}
}

// ---- ingest ----

func TestIngest_IdempotentAndSkipsOurs(t *testing.T) {
	h := newHarness(t, smallBudget())
	ingest := NewIngestUseCase(h.repo, NewSettingsResolver(h.settings), h.scheduler, nil, fixedClock{now})
	in := ca.IngestInput{WorkspaceID: "ws-1", Container: ref(), SourceCommentID: "c-1", AuthorExternalID: "u-1", Text: "oi"}

	for i := 0; i < 3; i++ { // the same webhook, redelivered
		if err := ingest.Enqueue(context.Background(), in); err != nil {
			t.Fatal(err)
		}
	}
	if h.repo.countStatus(ca.StatusPending) != 1 {
		t.Fatalf("pending = %d, want 1", h.repo.countStatus(ca.StatusPending))
	}
	if h.scheduler.hints[ref().Key()].Count != 1 {
		t.Fatalf("hint count = %d, want 1 (redeliveries must not inflate it)", h.scheduler.hints[ref().Key()].Count)
	}

	ours := in
	ours.SourceCommentID, ours.IsOurs = "c-ours", true
	if err := ingest.Enqueue(context.Background(), ours); err != nil {
		t.Fatal(err)
	}
	if h.repo.bySourceID("c-ours") != nil {
		t.Fatal("our own reply must never be enqueued")
	}

	// An account with analysis off (or never configured) enqueues nothing.
	other := in
	other.SourceCommentID = "c-other"
	other.Container.AccountID = "acc-unconfigured"
	if err := ingest.Enqueue(context.Background(), other); err != nil {
		t.Fatal(err)
	}
	if h.repo.bySourceID("c-other") != nil {
		t.Fatal("an unconfigured account must not enqueue")
	}

	// Blank text is recorded as skipped and never scheduled.
	blank := in
	blank.SourceCommentID, blank.Text = "c-blank", "   "
	if err := ingest.Enqueue(context.Background(), blank); err != nil {
		t.Fatal(err)
	}
	if r := h.repo.bySourceID("c-blank"); r == nil || r.Status != ca.StatusSkipped {
		t.Fatalf("blank = %+v", r)
	}
	if h.scheduler.hints[ref().Key()].Count != 1 {
		t.Fatal("a skipped row must not stamp the hint")
	}
}

// ---- scheduler encoding ----

func TestHintEncodingRoundTrip(t *testing.T) {
	h := ca.Hint{Ref: ref(), WorkspaceID: "ws-1", FirstSeen: now, LastSeen: now.Add(time.Minute), Count: 42}
	back, err := parseHint(ref(), encodeHint(h))
	if err != nil {
		t.Fatal(err)
	}
	if back.WorkspaceID != "ws-1" || !back.FirstSeen.Equal(now) || !back.LastSeen.Equal(now.Add(time.Minute)) || back.Count != 42 {
		t.Fatalf("round trip = %+v", back)
	}
	for _, bad := range []string{"", "v0|ws|x|y|1", "v1|ws|notatime|2026-09-02T12:00:00Z|1", "v1|ws|2026-09-02T12:00:00Z|2026-09-02T12:00:00Z|x"} {
		if _, err := parseHint(ref(), bad); err == nil {
			t.Errorf("parseHint(%q) should fail", bad)
		}
	}
}

func TestRedisScheduler_StampHintsClear(t *testing.T) {
	state := newFakeState()
	s := NewScheduler(state)
	ctx := context.Background()
	if err := s.Stamp(ctx, ref(), "ws-1", now); err != nil {
		t.Fatal(err)
	}
	if err := s.Stamp(ctx, ref(), "ws-1", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	// A foreign field in the hash is dropped, not fatal.
	_ = state.HSet(hintHashKey, "garbage", "v1|x")
	hints, err := s.Hints(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(hints) != 1 || hints[0].Count != 2 || !hints[0].FirstSeen.Equal(now) {
		t.Fatalf("hints = %+v", hints)
	}
	if err := s.Clear(ctx, ref()); err != nil {
		t.Fatal(err)
	}
	if hints, _ := s.Hints(ctx); len(hints) != 0 {
		t.Fatal("cleared hint still listed")
	}
}

// The live feed (§7): one broadcast per BATCH, not per comment, and only after
// the rows are stored. Per-comment events during a backfill are the failure
// this coalescing exists to prevent.
func TestEngine_BroadcastsOneEventPerBatch(t *testing.T) {
	h := newHarness(t, smallBudget())
	h.seed(20)
	h.classifier.push(func(req ca.ClassifyRequest) (*ca.ClassifyResult, error) {
		res := &ca.ClassifyResult{FinishReason: "stop", Model: "m"}
		for _, it := range req.Batch.Items {
			res.Results = append(res.Results, okResult(it.Ref))
		}
		return res, nil
	})

	if _, err := h.engine.ProcessContainer(context.Background(), ref(), "ws-1", newCycle()); err != nil {
		t.Fatal(err)
	}

	events := h.broadcaster.all()
	if len(events) != 1 {
		t.Fatalf("broadcasts = %d, want exactly one for one batch of 20", len(events))
	}
	e := events[0]
	if e.WorkspaceID != "ws-1" || e.ContainerID != ref().ContainerID {
		t.Fatalf("scope = %+v", e)
	}
	if len(e.Items) == 0 {
		t.Fatal("an empty broadcast is not worth sending")
	}
	// Everything broadcast must be a row that actually reached the store.
	for _, item := range e.Items {
		row, err := h.repo.FindByID(context.Background(), "ws-1", item.CommentID)
		if err != nil {
			t.Fatalf("broadcast row %s is not stored: %v", item.CommentID, err)
		}
		if row.Status != ca.StatusAnalyzed {
			t.Fatalf("broadcast row %s is %q", item.CommentID, row.Status)
		}
	}
}

// A deployment with no socket classifies exactly the same. The broadcaster is
// optional, and a nil one must not be a nil-pointer panic in the hot path.
func TestEngine_WithoutABroadcasterStillClassifies(t *testing.T) {
	h := newHarness(t, smallBudget())
	h.engine.Broadcaster = nil
	h.seed(5)
	h.classifier.push(func(req ca.ClassifyRequest) (*ca.ClassifyResult, error) {
		res := &ca.ClassifyResult{FinishReason: "stop", Model: "m"}
		for _, it := range req.Batch.Items {
			res.Results = append(res.Results, okResult(it.Ref))
		}
		return res, nil
	})

	res, err := h.engine.ProcessContainer(context.Background(), ref(), "ws-1", newCycle())
	if err != nil {
		t.Fatal(err)
	}
	if res.Analyzed != 5 {
		t.Fatalf("analysed = %d, want 5", res.Analyzed)
	}
}
