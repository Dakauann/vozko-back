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
		charger:     newFakeCharger(),
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
		Classifier:    base.classifier, Scheduler: base.scheduler, Charger: base.charger,
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

// A conversation that no longer resolves is skipped, exactly as a deleted
// comment is: not failed, and never sent to the model.
func TestEngine_SkipsVanishedConversations(t *testing.T) {
	h := newConversationHarness(t, smallBudget())
	h.seed(2, 4, now)
	delete(h.conversations.transcripts, "entry-2")
	h.pushOK()

	res, err := h.engine.ProcessContainer(context.Background(), convRef(), "ws-1", newCycle())
	if err != nil {
		t.Fatal(err)
	}
	if res.Analyzed != 1 || res.Skipped != 1 {
		t.Fatalf("analysed=%d skipped=%d, want 1 and 1", res.Analyzed, res.Skipped)
	}
	if got := h.row(t, "row-2").Status; got != ca.StatusSkipped {
		t.Errorf("vanished conversation is %q, want skipped", got)
	}
}

// The shared machinery is genuinely shared: a conversation batch books a spend
// receipt and charges, the same as a comment batch. If conversations had been
// given their own path, this is what would have been quietly lost, which is
// exactly what happened in the engine this replaces.
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
	if len(h.charger.charges) == 0 {
		t.Error("a conversation batch charged nothing")
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
