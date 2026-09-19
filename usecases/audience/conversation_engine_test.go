package audience_usecase

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	ca "vozko/domain/audience"
	"vozko/domain/shared"
)

// End to end for the second subject kind: a WhatsApp conversation goes through
// the same engine a comment does, and comes out with the conversation taxonomy
// on it.
//
// The point of these tests is that the machinery is genuinely shared. The
// durable queue, the budget planner, the claim, the spend receipt and the daily
// cap are not reimplemented for conversations; if they were, these tests would
// be asserting a second engine's behaviour rather than the same one's.

const convRefCampaign = "camp-1"

func convRef() ca.ContainerRef {
	return ca.ContainerRef{
		Kind: ca.SubjectKindConversation, Source: ca.SourceWhatsApp,
		AccountID: "acc-1", ContainerID: convRefCampaign,
	}
}

// fakeConversationAdapter answers the narrow conversation port.
type fakeConversationAdapter struct {
	mu          sync.Mutex
	transcripts map[string]ca.Transcript
	objective   string
	err         error
	readCalls   int
}

func (f *fakeConversationAdapter) ReadTranscripts(_ context.Context, _ ca.ContainerRef, ids []string) (map[string]ca.Transcript, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.readCalls++
	if f.err != nil {
		return nil, f.err
	}
	out := map[string]ca.Transcript{}
	for _, id := range ids {
		if t, ok := f.transcripts[id]; ok {
			out[id] = t
		}
	}
	return out, nil
}

func (f *fakeConversationAdapter) ReadContainerContext(_ context.Context, _ ca.ContainerRef) (ca.ContainerContext, error) {
	return ca.ContainerContext{Caption: f.objective}, nil
}

type conversationHarness struct {
	*harness
	conversations *fakeConversationAdapter
}

func newConversationHarness(t *testing.T, budget ca.Budget) *conversationHarness {
	t.Helper()

	settings := ca.NewSettings("ws-1", ca.SourceWhatsApp, "acc-1", ca.VerticalServices)
	settings.Enabled = true

	base := &harness{
		repo:        newFakeRepo(),
		settings:    newFakeSettings(&settings),
		batches:     &fakeBatches{},
		adapter:     &fakeAdapter{texts: map[string]string{}},
		classifier:  &fakeClassifier{},
		scheduler:   newFakeScheduler(),
		balance:     &fakeBalance{micros: 1_000_000},
		state:       newFakeState(),
		broadcaster: &fakeBroadcaster{},
	}
	conversations := &fakeConversationAdapter{
		transcripts: map[string]ca.Transcript{},
		objective:   "Agendar avaliação odontológica gratuita",
	}

	engine, err := NewEngine(EngineDeps{
		Repo: base.repo, Settings: NewSettingsResolver(base.settings), Batches: base.batches,
		Conversations: map[ca.Source]ca.ConversationAdapter{ca.SourceWhatsApp: conversations},
		Classifier:    base.classifier, Scheduler: base.scheduler,
		Usage:   NewUsageLimiter(base.state),
		Balance: base.balance, State: base.state, Clock: fixedClock{now}, Budget: budget,
		Broadcaster: base.broadcaster,
	})
	if err != nil {
		t.Fatal(err)
	}
	base.engine = engine
	return &conversationHarness{harness: base, conversations: conversations}
}

// row reads a stored row back, so assertions look at what was persisted rather
// than at an in-memory object the engine happened to still hold.
func (h *conversationHarness) row(t *testing.T, id string) *ca.Analysis {
	t.Helper()
	row, err := h.repo.FindByID(context.Background(), "ws-1", id)
	if err != nil {
		t.Fatalf("row %s not found: %v", id, err)
	}
	return row
}

// seed queues n conversations, each with a transcript.
func (h *conversationHarness) seed(n int, messagesEach int, lastAt time.Time) {
	for i := 1; i <= n; i++ {
		id := "entry-" + itoa(i)
		text := "cliente: quero marcar\natendente: claro, qual o melhor dia?"
		h.conversations.transcripts[id] = ca.Transcript{
			Text: text, MessageCount: messagesEach, LastMessageAt: lastAt,
		}
		row, _ := ca.NewPending(ca.NewInput{
			WorkspaceID: "ws-1", Container: convRef(), SubjectID: id,
			AuthorExternalID: "contact-" + itoa(i), Text: text,
			Now: now.Add(time.Duration(i) * time.Second),
		})
		row.ID = "row-" + itoa(i)
		_, _ = h.repo.Insert(context.Background(), row)
	}
	_ = h.scheduler.Stamp(context.Background(), convRef(), "ws-1", now.Add(-time.Hour))
}

func okConversationResult(ref int) ca.BatchResult {
	return ca.BatchResult{
		Ref:                ref,
		Sentiment:          string(shared.SentimentPositive),
		Interest:           string(ca.InterestInterested),
		Disposition:        string(ca.DispositionFillingInfo),
		Qualification:      string(ca.QualificationHotLead),
		NextAction:         string(ca.NextActionContinue),
		ProductInterest:    "avaliação odontológica",
		Summary:            "Cliente quer marcar e está escolhendo o dia.",
		Language:           "pt",
		GoalProgress:       string(shared.QualityLevelHigh),
		CustomerEngagement: string(shared.QualityLevelHigh),
		AgentConduct:       string(shared.QualityLevelMedium),
		Professionalism:    string(shared.QualityLevelHigh),
	}
}

func (h *conversationHarness) pushOK() {
	h.classifier.push(func(req ca.ClassifyRequest) (*ca.ClassifyResult, error) {
		res := &ca.ClassifyResult{FinishReason: "stop", Model: "m"}
		for _, it := range req.Batch.Items {
			res.Results = append(res.Results, okConversationResult(it.Ref))
		}
		return res, nil
	})
}

func TestEngine_ClassifiesConversationsEndToEnd(t *testing.T) {
	h := newConversationHarness(t, smallBudget())
	lastAt := now.Add(-2 * time.Hour)
	h.seed(3, 12, lastAt)
	h.pushOK()

	res, err := h.engine.ProcessContainer(context.Background(), convRef(), "ws-1", newCycle())
	if err != nil {
		t.Fatal(err)
	}
	if res.Analyzed != 3 {
		t.Fatalf("analysed = %d, want 3", res.Analyzed)
	}

	row := h.row(t, "row-1")
	if row.Status != ca.StatusAnalyzed {
		t.Fatalf("status = %q", row.Status)
	}
	if row.Kind() != ca.SubjectKindConversation {
		t.Fatalf("kind = %q", row.Kind())
	}

	// The conversation taxonomy landed.
	if row.Interest != ca.InterestInterested || row.Disposition != ca.DispositionFillingInfo {
		t.Errorf("conversation labels missing: %+v", row)
	}
	if row.Qualification != ca.QualificationHotLead || row.NextAction != ca.NextActionContinue {
		t.Errorf("conversation labels missing: %+v", row)
	}
	if row.Summary == "" || row.ProductInterest == "" {
		t.Errorf("free text not stored: summary=%q product=%q", row.Summary, row.ProductInterest)
	}

	// The score is COMPUTED from the ordinal ratings, not taken from the model.
	want := ca.ConversationQuality{
		GoalProgress: shared.QualityLevelHigh, CustomerEngagement: shared.QualityLevelHigh,
		AgentConduct: shared.QualityLevelMedium, Professionalism: shared.QualityLevelHigh,
	}.Score()
	if row.AttendanceQuality != want {
		t.Errorf("attendance quality = %d, want %d (computed from the rubric)", row.AttendanceQuality, want)
	}

	// Facts the transcript brought back are recorded.
	if row.MessageCount != 12 {
		t.Errorf("message count = %d, want 12", row.MessageCount)
	}
	if !row.OccurredAt.Equal(lastAt) {
		t.Errorf("row bucketed at %v, want the last message time %v", row.OccurredAt, lastAt)
	}

	// And none of the comment dimensions were invented.
	if row.Stance != "" || row.Intent != "" || row.TopicKey != "" || row.Severity != 0 {
		t.Errorf("comment dimensions leaked onto a conversation: %+v", row)
	}
}

// The model must be offered the conversation rubric, never the comment one.
// Getting this wrong would not fail loudly: the model would answer with comment
// labels, every row would fail validation, and the symptom would look like a
// flaky provider.
func TestEngine_SendsTheConversationTaxonomy(t *testing.T) {
	h := newConversationHarness(t, smallBudget())
	h.seed(1, 4, now)

	var seen ca.ClassifyRequest
	h.classifier.push(func(req ca.ClassifyRequest) (*ca.ClassifyResult, error) {
		seen = req
		return &ca.ClassifyResult{
			FinishReason: "stop", Model: "m",
			Results: []ca.BatchResult{okConversationResult(req.Batch.Items[0].Ref)},
		}, nil
	})

	if _, err := h.engine.ProcessContainer(context.Background(), convRef(), "ws-1", newCycle()); err != nil {
		t.Fatal(err)
	}

	if seen.SubjectKind != ca.SubjectKindConversation {
		t.Fatalf("classify request carried subject kind %q", seen.SubjectKind)
	}

	prompt := BuildSystemPromptFor(seen.SubjectKind, seen.Topics, seen.Context, seen.Instructions)
	for _, want := range []string{"disposition", "qualification", "next_action", "goal_progress"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("conversation prompt never mentions %q", want)
		}
	}
	for _, unwanted := range []string{"stance", "toxicity", "personal_attack"} {
		if strings.Contains(prompt, unwanted) {
			t.Errorf("conversation prompt offers the comment field %q", unwanted)
		}
	}
	// The campaign objective reaches the model: every criterion in the rubric
	// is written relative to it.
	if !strings.Contains(prompt, "Agendar avaliação odontológica gratuita") {
		t.Error("the campaign objective never reached the prompt")
	}

	schema := ca.BatchResponseSchemaFor(seen.SubjectKind, seen.Topics)
	results, _ := schema["properties"].(map[string]any)[ca.SchemaKeyResults].(map[string]any)
	items, _ := results["items"].(map[string]any)
	props, _ := items["properties"].(map[string]any)
	if _, ok := props[ca.FieldDisposition]; !ok {
		t.Error("schema has no disposition")
	}
	if _, ok := props[ca.FieldStance]; ok {
		t.Error("schema offers the comment stance field on a conversation batch")
	}
}

// A conversation the model rates only partially must be released for retry, not
// stored with a score computed from the missing dimensions.
func TestEngine_ReleasesPartiallyRatedConversations(t *testing.T) {
	h := newConversationHarness(t, smallBudget())
	h.seed(1, 4, now)

	h.classifier.push(func(req ca.ClassifyRequest) (*ca.ClassifyResult, error) {
		r := okConversationResult(req.Batch.Items[0].Ref)
		r.AgentConduct = "" // one dimension missing
		return &ca.ClassifyResult{FinishReason: "stop", Model: "m", Results: []ca.BatchResult{r}}, nil
	})

	res, err := h.engine.ProcessContainer(context.Background(), convRef(), "ws-1", newCycle())
	if err != nil {
		t.Fatal(err)
	}
	if res.Analyzed != 0 {
		t.Fatalf("analysed = %d, want 0", res.Analyzed)
	}
	row := h.row(t, "row-1")
	if row.Status == ca.StatusAnalyzed {
		t.Error("a partially rated conversation must not be stored as analysed")
	}
	if row.AttendanceQuality != 0 {
		t.Errorf("a score was computed from an incomplete assessment: %d", row.AttendanceQuality)
	}
}

// A conversation that does not resolve is RETRIED before it is given up on,
// and never fails the batch its peers are in.
//
// A deleted comment is genuinely gone, so it is skipped on the spot. A
// conversation is ours: we queued it from a row in our own database, so a miss
// is far more likely a database having a bad minute than a vanished subject.
// Skipping on the first miss is how a transient failure used to stamp
// text_unavailable on a perfectly good conversation, permanently. The attempt
// is still counted, so an entry that really is gone gives up rather than
// sitting pending forever.
func TestEngine_RetriesUnreadableConversationsBeforeSkipping(t *testing.T) {
	h := newConversationHarness(t, smallBudget())
	h.seed(2, 4, now)
	delete(h.conversations.transcripts, "entry-2")
	h.pushOK()

	res, err := h.engine.ProcessContainer(context.Background(), convRef(), "ws-1", newCycle())
	if err != nil {
		t.Fatal(err)
	}
	if res.Analyzed != 1 || res.Skipped != 0 {
		t.Fatalf("analysed=%d skipped=%d, want 1 analysed and nothing skipped yet", res.Analyzed, res.Skipped)
	}
	if got := h.row(t, "row-2").Status; got != ca.StatusPending {
		t.Fatalf("unreadable conversation is %q after one miss, want pending for a retry", got)
	}

	// It stays unreadable: the attempts run out and the row gives up.
	for i := 0; i < ca.MaxAttempts; i++ {
		h.pushOK()
		if _, err := h.engine.ProcessContainer(context.Background(), convRef(), "ws-1", newCycle()); err != nil {
			t.Fatal(err)
		}
	}
	row := h.row(t, "row-2")
	if row.Status != ca.StatusSkipped {
		t.Errorf("permanently unreadable conversation is %q, want skipped", row.Status)
	}
	if row.FailureReason != ca.ReasonTextUnavailable {
		t.Errorf("failure reason = %q, want text_unavailable", row.FailureReason)
	}
}

// The shared machinery is genuinely shared: a conversation batch books a usage
// receipt and carries the workspace on the classify call, the same as a comment
// batch. That workspace id is the whole of token billing, so if conversations
// had been given their own path this is the revenue that would have been
// quietly lost, which is exactly what happened in the engine this replaces.
func TestEngine_BooksSpendForConversations(t *testing.T) {
	h := newConversationHarness(t, smallBudget())
	h.seed(2, 6, now)
	h.pushOK()

	if _, err := h.engine.ProcessContainer(context.Background(), convRef(), "ws-1", newCycle()); err != nil {
		t.Fatal(err)
	}

	if len(h.batches.rows) == 0 {
		t.Fatal("a conversation batch wrote no spend receipt")
	}
	receipt := h.batches.rows[0]
	if receipt.ItemCount != 2 {
		t.Errorf("receipt covers %d items, want 2", receipt.ItemCount)
	}
	if receipt.WorkspaceID != "ws-1" || receipt.Source != ca.SourceWhatsApp {
		t.Errorf("receipt misattributed: %+v", receipt)
	}
	if len(h.classifier.Calls) == 0 || h.classifier.Calls[0].WorkspaceID != "ws-1" {
		t.Error("WorkspaceID must be on the conversation request too (token billing)")
	}
}

// Without a registered conversation adapter the container fails loudly rather
// than silently classifying nothing.
func TestEngine_RefusesConversationsWithoutAnAdapter(t *testing.T) {
	h := newConversationHarness(t, smallBudget())
	h.seed(1, 3, now)
	h.engine.Conversations = map[ca.Source]ca.ConversationAdapter{}

	if _, err := h.engine.ProcessContainer(context.Background(), convRef(), "ws-1", newCycle()); err == nil {
		t.Fatal("expected an error when no conversation adapter is registered")
	}
}

// A conversation has a TIMELINE of analyses, not one verdict for all time.
//
// The queue used to be keyed on the conversation alone, so the first analysis
// was the only one it could ever have: every later inactivity window hit the
// conflict clause and was dropped. Keyed on the revision, each snapshot of the
// transcript becomes its own row, and the two are classified independently.
//
// Each row is classified from the transcript frozen when it was queued, which
// is why the adapter is never asked to re-read: by the time the second window
// elapses the channel can only render the conversation as it is NOW, and the
// first analysis would silently be about the wrong text.
func TestEngine_AnalysesEachRevisionOfAConversation(t *testing.T) {
	h := newConversationHarness(t, smallBudget())
	first := now.Add(-3 * time.Hour)
	second := now.Add(-1 * time.Hour)

	h.seedRevision("row-1", "entry-1", "rev-a", "cliente: quanto custa?", 4, first)
	h.seedRevision("row-2", "entry-1", "rev-b", "cliente: quanto custa?\natendente: R$ 300\ncliente: fechado", 9, second)
	h.pushOK()

	res, err := h.engine.ProcessContainer(context.Background(), convRef(), "ws-1", newCycle())
	if err != nil {
		t.Fatal(err)
	}
	if res.Analyzed != 2 {
		t.Fatalf("analysed = %d, want both revisions of the conversation", res.Analyzed)
	}
	if h.conversations.readCalls != 0 {
		t.Errorf("the channel was re-read %d times for rows that carry their own snapshot", h.conversations.readCalls)
	}

	early, late := h.row(t, "row-1"), h.row(t, "row-2")
	if early.Status != ca.StatusAnalyzed || late.Status != ca.StatusAnalyzed {
		t.Fatalf("statuses = %q and %q, want both analysed", early.Status, late.Status)
	}
	if early.MessageCount != 4 || late.MessageCount != 9 {
		t.Errorf("message counts = %d and %d, want each revision's own 4 and 9", early.MessageCount, late.MessageCount)
	}
	// The timestamps are what put the two rows in order on the timeline, and
	// what bucket them into the right day of the trend.
	if !early.OccurredAt.Equal(first) || !late.OccurredAt.Equal(second) {
		t.Errorf("occurredAt = %v and %v, want %v and %v", early.OccurredAt, late.OccurredAt, first, second)
	}

	sent := map[string]bool{}
	for _, req := range h.classifier.Calls {
		for _, it := range req.Batch.Items {
			sent[it.Text] = true
		}
	}
	if !sent["cliente: quanto custa?"] {
		t.Error("the earlier revision was not classified from the transcript it was queued with")
	}
	if !sent["cliente: quanto custa?\natendente: R$ 300\ncliente: fechado"] {
		t.Error("the later revision was not classified from the transcript it was queued with")
	}
}

// seedRevision queues ONE snapshot of a conversation: the transcript exactly as
// it read when that inactivity window elapsed, and the revision that identifies
// it. Two of these on the same entry id are two analyses on one timeline.
func (h *conversationHarness) seedRevision(rowID, entryID, revision, text string, messages int, lastAt time.Time) {
	row, _ := ca.NewPending(ca.NewInput{
		WorkspaceID: "ws-1", Container: convRef(), SubjectID: entryID,
		AuthorExternalID: "contact-1", Text: text, Now: lastAt,
	})
	row.ID = rowID
	row.Revision, row.Transcript, row.MessageCount = revision, text, messages
	_, _ = h.repo.Insert(context.Background(), row)
	_ = h.scheduler.Stamp(context.Background(), convRef(), "ws-1", now.Add(-time.Hour))
}

// Repeat analysis is the one path in this engine that can spend money in a
// loop, so the guard on it is pinned here rather than left to the index.
//
// A conversation is queued every time it goes quiet. Without a floor, a chat
// that stops and starts all afternoon buys an analysis per lull, and a burst
// stacks several snapshots of the same conversation in the queue at once, all
// but the last of them obsolete before they are even sent.
func TestIngest_DoesNotPayTwiceForTheSameConversation(t *testing.T) {
	h := newConversationHarness(t, smallBudget())
	ingest := NewIngestUseCase(h.repo, NewSettingsResolver(h.settings), h.scheduler, nil, fixedClock{now})

	queue := func(revision string, messages int, at time.Time) {
		t.Helper()
		if err := ingest.Enqueue(context.Background(), ca.IngestInput{
			WorkspaceID: "ws-1", Container: convRef(), SubjectID: "entry-1",
			AuthorExternalID: "contact-1", Text: "cliente: " + revision,
			Revision: revision, MessageCount: messages, OccurredAt: at,
		}); err != nil {
			t.Fatalf("Enqueue %s: %v", revision, err)
		}
	}
	pending := func() int { return h.repo.countStatus(ca.StatusPending) }

	queue("rev-a", 4, now)
	if pending() != 1 {
		t.Fatalf("first snapshot queued %d rows, want 1", pending())
	}

	// Still waiting to be classified: a newer snapshot of the SAME conversation
	// must not stack behind it. Paying for both buys one analysis and one that
	// was obsolete before it was sent.
	queue("rev-b", 9, now.Add(time.Hour))
	if pending() != 1 {
		t.Fatalf("a second snapshot stacked behind an unclassified one: pending = %d", pending())
	}

	h.pushOK()
	if _, err := h.engine.ProcessContainer(context.Background(), convRef(), "ws-1", newCycle()); err != nil {
		t.Fatal(err)
	}

	// Analysed now, so the conversation may be analysed again — but only once
	// it has actually moved. One more message is not an exchange.
	queue("rev-c", 5, now.Add(2*time.Hour))
	if pending() != 0 {
		t.Fatalf("a one-message change bought a second analysis: pending = %d", pending())
	}

	queue("rev-d", 4+ca.MinMessagesBetweenAnalyses, now.Add(3*time.Hour))
	if pending() != 1 {
		t.Fatalf("a real exchange did not queue a new analysis: pending = %d", pending())
	}
}

// A conversation whose first attempt failed is not held back by the floor: the
// failure bought nothing, so the next snapshot is still its first analysis.
func TestIngest_RetriesAConversationWhoseAnalysisFailed(t *testing.T) {
	h := newConversationHarness(t, smallBudget())
	ingest := NewIngestUseCase(h.repo, NewSettingsResolver(h.settings), h.scheduler, nil, fixedClock{now})

	first, _ := ca.NewPending(ca.NewInput{
		WorkspaceID: "ws-1", Container: convRef(), SubjectID: "entry-1",
		AuthorExternalID: "contact-1", Text: "cliente: oi", Now: now,
	})
	first.ID, first.Revision, first.Transcript, first.MessageCount = "row-1", "rev-a", "cliente: oi", 4
	_ = first.Fail(ca.ReasonProviderError, now)
	_, _ = h.repo.Insert(context.Background(), first)

	if err := ingest.Enqueue(context.Background(), ca.IngestInput{
		WorkspaceID: "ws-1", Container: convRef(), SubjectID: "entry-1",
		AuthorExternalID: "contact-1", Text: "cliente: oi de novo",
		Revision: "rev-b", MessageCount: 5, OccurredAt: now.Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if h.repo.countStatus(ca.StatusPending) != 1 {
		t.Fatal("a conversation whose only analysis failed was never re-queued")
	}
}

// recordingLive captures the per-conversation live signal.
type recordingLive struct {
	mu     sync.Mutex
	states []ca.ConversationAnalysisState
}

func (r *recordingLive) AnalysisStateChanged(state ca.ConversationAnalysisState) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.states = append(r.states, state)
}

// The CRM is told an analysis is coming, and told when it lands.
//
// Only the pair is useful. A "queued" with no "finished" leaves a spinner on
// screen until someone reloads, and a "finished" with no "queued" means the
// minutes between a conversation going quiet and its batch running look
// exactly like nothing happening at all.
func TestEngine_AnnouncesAnalysisQueuedThenFinished(t *testing.T) {
	h := newConversationHarness(t, smallBudget())
	live := &recordingLive{}
	h.engine.Live = live

	ingest := NewIngestUseCase(h.repo, NewSettingsResolver(h.settings), h.scheduler, nil, fixedClock{now})
	ingest.(interface {
		SetLive(ca.ConversationAnalysisLive)
	}).SetLive(live)

	if err := ingest.Enqueue(context.Background(), ca.IngestInput{
		WorkspaceID: "ws-1", Container: convRef(), SubjectID: "entry-1",
		AuthorExternalID: "contact-1", Text: "cliente: quanto custa?",
		Revision: "rev-a", MessageCount: 4, OccurredAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	if len(live.states) != 1 {
		t.Fatalf("queueing announced %d states, want 1", len(live.states))
	}
	queued := live.states[0]
	if !queued.Pending || queued.EntryID != "entry-1" || queued.EntryType != string(ca.SourceWhatsApp) {
		t.Fatalf("queued state = %+v", queued)
	}
	// Nothing is claimed about the verdict yet, because there is not one.
	if queued.Analysis != nil {
		t.Error("a queued conversation announced an analysis that does not exist yet")
	}

	h.pushOK()
	if _, err := h.engine.ProcessContainer(context.Background(), convRef(), "ws-1", newCycle()); err != nil {
		t.Fatal(err)
	}

	if len(live.states) != 2 {
		t.Fatalf("after classifying, %d states announced, want 2", len(live.states))
	}
	done := live.states[1]
	if done.Pending {
		t.Error("a finished analysis was still announced as pending")
	}
	if done.Analysis == nil || done.Analysis.Status != ca.StatusAnalyzed {
		t.Fatalf("finished state carried no verdict: %+v", done.Analysis)
	}
	// The verdict travels WITH the event, so a listening client needs no
	// follow-up read to render it.
	if done.Analysis.Disposition == "" || done.Analysis.AttendanceQuality == 0 {
		t.Errorf("the announced verdict is missing its labels: %+v", done.Analysis)
	}
}

// A conversation the guard refuses to re-analyse must not announce anything:
// a pending marker that never resolves is worse than no marker, because the
// only way out of it is a reload that shows the same thing.
func TestEngine_DoesNotAnnounceAnAnalysisItRefusedToQueue(t *testing.T) {
	h := newConversationHarness(t, smallBudget())
	live := &recordingLive{}
	ingest := NewIngestUseCase(h.repo, NewSettingsResolver(h.settings), h.scheduler, nil, fixedClock{now})
	ingest.(interface {
		SetLive(ca.ConversationAnalysisLive)
	}).SetLive(live)

	queue := func(revision string, messages int, at time.Time) {
		t.Helper()
		if err := ingest.Enqueue(context.Background(), ca.IngestInput{
			WorkspaceID: "ws-1", Container: convRef(), SubjectID: "entry-1",
			AuthorExternalID: "contact-1", Text: "cliente: " + revision,
			Revision: revision, MessageCount: messages, OccurredAt: at,
		}); err != nil {
			t.Fatal(err)
		}
	}

	queue("rev-a", 4, now)
	// Stacked behind an unclassified snapshot: refused by the billing guard,
	// so nothing is queued and nothing may be announced.
	queue("rev-b", 9, now.Add(time.Hour))

	if len(live.states) != 1 {
		t.Fatalf("announced %d states, want only the one that was actually queued", len(live.states))
	}
}

// The startup line has to report what is REGISTERED.
//
// It used to be a hardcoded "sources: instagram", printed identically whether
// every channel was wired or none were. A log that says the same thing in both
// cases is worse than silence: it reads as a diagnosis, and it sent an
// afternoon of debugging after conversation channels that were fine.
func TestEngineReportsWhatIsActuallyRegistered(t *testing.T) {
	h := newConversationHarness(t, smallBudget())

	comments, conversations := h.engine.RegisteredSources()
	// The conversation harness registers WhatsApp conversations and no comment
	// source, which is exactly the shape the old line could not express.
	if len(comments) != 0 {
		t.Errorf("reported comment sources %v, want none registered", comments)
	}
	if len(conversations) != 1 || conversations[0] != string(ca.SourceWhatsApp) {
		t.Errorf("reported conversation sources %v, want [whatsapp]", conversations)
	}

	// A channel registered afterwards is reported too, so the line cannot go
	// stale against a registry that is still being filled.
	h.engine.RegisterSource(ca.SourceInstagram, h.adapter)
	h.engine.RegisterConversationSource(ca.SourceTelegram, h.conversations)

	comments, conversations = h.engine.RegisteredSources()
	if len(comments) != 1 || comments[0] != string(ca.SourceInstagram) {
		t.Errorf("comment sources = %v, want [instagram]", comments)
	}
	// Sorted, so two boots of the same deployment print the same line.
	if len(conversations) != 2 || conversations[0] != string(ca.SourceTelegram) || conversations[1] != string(ca.SourceWhatsApp) {
		t.Errorf("conversation sources = %v, want [telegram whatsapp] sorted", conversations)
	}
}

// A nil adapter in the map is not a registered channel: reporting it would put
// a channel on the startup line that the engine refuses to process.
func TestEngineDoesNotReportNilAdapters(t *testing.T) {
	h := newConversationHarness(t, smallBudget())
	h.engine.Conversations[ca.SourceInstagram] = nil
	h.engine.Adapters[ca.SourceTelegram] = nil

	comments, conversations := h.engine.RegisteredSources()
	for _, s := range append(comments, conversations...) {
		if s == string(ca.SourceInstagram) || s == string(ca.SourceTelegram) {
			t.Errorf("reported %q, which has a nil adapter", s)
		}
	}
}
