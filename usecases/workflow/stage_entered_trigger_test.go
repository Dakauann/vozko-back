package workflow_usecase

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"vozko/domain/messaging"
	"vozko/domain/stage"
	"vozko/domain/workflow"
	stage_usecase "vozko/usecases/stage"
)

type heardEvents struct {
	events []workflow.TriggerEvent
	fail   error
}

func (h *heardEvents) Evaluate(event workflow.TriggerEvent) { _ = h.EvaluateChecked(event) }

func (h *heardEvents) EvaluateChecked(event workflow.TriggerEvent) error {
	h.events = append(h.events, event)
	return h.fail
}

func stageWorkflow(id string, config map[string]interface{}) *workflow.Workflow {
	return &workflow.Workflow{
		ID:          id,
		WorkspaceID: "ws1",
		Graph: workflow.Graph{
			Nodes: []workflow.Node{{ID: "etapa", Type: workflow.NodeTypeTriggerStageAdded, Config: config}, {ID: "end", Type: workflow.NodeTypeEnd}},
			Edges: []workflow.Edge{{Source: "etapa", Target: "end"}},
		},
	}
}

type startedRuns struct {
	recordingRunRepo
	runs []*workflow.WorkflowRun
}

func (r *startedRuns) Create(run *workflow.WorkflowRun) error {
	r.runs = append(r.runs, run)
	return r.recordingRunRepo.Create(run)
}

func evaluatorOver(runs *startedRuns, workflows ...*workflow.Workflow) workflow.CheckedTriggerEvaluator {
	byID := map[string]*workflow.Workflow{}
	for _, w := range workflows {
		byID[w.ID] = w
	}
	return NewTriggerEvaluator(stubWorkflowRepo{byID: byID, active: workflows}, runs, NewRunEngine(runs, stubRunLogRepo{}, NewNodeExecutorRegistry()), nil)
}

func entered(cause stage.MoveCause) stage.Entered {
	return stage.Entered{MoveID: "row-1", WorkspaceID: "ws1", EntryID: "conv-1", EntryType: "whatsapp", StageID: "s1", FromStageID: "s0", Cause: cause}
}

type stageQueue struct {
	published []queuedMessage
	failWith  error
}

type queuedMessage struct {
	topic   string
	payload []byte
	delay   time.Duration
}

func (q *stageQueue) Publish(topic string, message []byte) error {
	return q.PublishWithDelay(topic, message, 0)
}

func (q *stageQueue) PublishWithDelay(topic string, message []byte, delay time.Duration) error {
	if q.failWith != nil {
		return q.failWith
	}
	q.published = append(q.published, queuedMessage{topic: topic, payload: append([]byte(nil), message...), delay: delay})
	return nil
}

func (q *stageQueue) ValidateConnection() error { return nil }

type settledAck struct {
	mu      sync.Mutex
	acked   bool
	nacked  bool
	requeue bool
	done    chan struct{}
}

func newSettledAck() *settledAck { return &settledAck{done: make(chan struct{})} }

func (a *settledAck) Ack() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.acked = true
	close(a.done)
	return nil
}

func (a *settledAck) Nack(requeue bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.nacked, a.requeue = true, requeue
	close(a.done)
	return nil
}

func (a *settledAck) settled() bool {
	select {
	case <-a.done:
		return true
	default:
		return false
	}
}

func (a *settledAck) wait(t *testing.T) {
	t.Helper()
	select {
	case <-a.done:
	case <-time.After(time.Second):
		t.Fatal("the message was never settled")
	}
}

func (a *settledAck) DeliveryCount() int { return 2 }

type stageConsumerFixture struct {
	sub   *mockQueueSub
	queue *stageQueue
	heard *heardEvents
}

func startStageConsumer(t *testing.T, evaluator workflow.CheckedTriggerEvaluator) *stageConsumerFixture {
	t.Helper()
	f := &stageConsumerFixture{sub: &mockQueueSub{}, queue: &stageQueue{}}
	if evaluator == nil {
		f.heard = &heardEvents{}
		evaluator = f.heard
	}
	if err := NewConsumeStageEnteredUseCase(f.sub, f.queue, evaluator).Start(); err != nil {
		t.Fatal(err)
	}
	if f.sub.topic != workflow.TopicStageEntered {
		t.Fatalf("subscribed to %q, want %q", f.sub.topic, workflow.TopicStageEntered)
	}
	return f
}

func (f *stageConsumerFixture) deliver(t *testing.T, payload []byte) *settledAck {
	t.Helper()
	ack := newSettledAck()
	f.sub.handler(payload, ack)
	ack.wait(t)
	return ack
}

func publishedMove(t *testing.T, e stage.Entered) []byte {
	t.Helper()
	queue := &stageQueue{}
	NewStageEnteredPublisher(queue).StageEntered(e)
	if len(queue.published) != 1 {
		t.Fatalf("published %d messages, want 1", len(queue.published))
	}
	return queue.published[0].payload
}

type placements struct {
	stage.Repository
	current map[string]string
}

func (p *placements) AssignStage(et *stage.EntryStage) (string, error) {
	previous := p.current[et.EntryID]
	p.current[et.EntryID] = et.StageID
	return previous, nil
}

func TestACommittedStageChangeIsPublishedDurablyOnTheStageTopic(t *testing.T) {
	queue := &stageQueue{}
	repo := stage_usecase.AnnounceEntries(&placements{current: map[string]string{"conv-1": "s0"}}, NewStageEnteredPublisher(queue))
	cause := stage.MoveCause{WorkflowID: "wf-a", RunID: "run-1", Chain: 1}

	if _, err := repo.AssignStage(&stage.EntryStage{ID: "row-1", WorkspaceID: "ws1", EntryID: "conv-1", EntryType: "whatsapp", StageID: "s1", Cause: cause}); err != nil {
		t.Fatal(err)
	}

	if len(queue.published) != 1 || queue.published[0].topic != workflow.TopicStageEntered || queue.published[0].delay != 0 {
		t.Fatalf("published %+v, want one immediate message on %s", queue.published, workflow.TopicStageEntered)
	}
	var body struct {
		Entered stage.Entered `json:"entered"`
		Attempt int           `json:"attempt"`
	}
	if err := json.Unmarshal(queue.published[0].payload, &body); err != nil {
		t.Fatal(err)
	}
	if body.Entered != entered(cause) || body.Attempt != 0 {
		t.Fatalf("message = %+v, want the move with its cause at attempt 0", body)
	}
}

func TestAStageChangeStaysCommittedWhenTheQueueRefusesTheMessage(t *testing.T) {
	queue := &stageQueue{failWith: errors.New("broker down")}
	repo := stage_usecase.AnnounceEntries(&placements{current: map[string]string{}}, NewStageEnteredPublisher(queue))

	if _, err := repo.AssignStage(&stage.EntryStage{ID: "row-1", WorkspaceID: "ws1", EntryID: "conv-1", EntryType: "whatsapp", StageID: "s1"}); err != nil {
		t.Fatalf("the move was written, so the caller must not see the queue failure: %v", err)
	}
}

func TestAPublisherWithoutAQueueRefusesQuietlyInsteadOfPanicking(t *testing.T) {
	NewStageEnteredPublisher(nil).StageEntered(entered(stage.MoveCause{}))
}

func TestTheConsumerTurnsAQueuedMoveIntoAStageAddedEvent(t *testing.T) {
	f := startStageConsumer(t, nil)

	ack := f.deliver(t, publishedMove(t, entered(stage.MoveCause{})))

	if !ack.acked || len(f.heard.events) != 1 {
		t.Fatalf("ack %+v, heard %d", ack, len(f.heard.events))
	}
	event := f.heard.events[0]
	if event.TriggerType != workflow.TriggerStageAdded || event.EntryID != "conv-1" || event.Data[workflow.DataKeyStageID] != "s1" || event.OccurrenceKey() != "stage/row-1" {
		t.Fatalf("event = %+v", event)
	}
}

func TestTheConsumerAppliesTheLoopGuardBeforeLookingUpWorkflows(t *testing.T) {
	f := startStageConsumer(t, nil)

	ack := f.deliver(t, publishedMove(t, entered(stage.MoveCause{WorkflowID: "wf-a", RunID: "run-1", Chain: workflow.MaxStageChain})))

	if !ack.acked || len(f.heard.events) != 0 {
		t.Fatalf("ack %+v, heard %+v: a move past the chain limit is settled without evaluation", ack, f.heard.events)
	}
}

func TestAnUnreadableMessageIsDroppedWithoutRequeue(t *testing.T) {
	f := startStageConsumer(t, nil)

	ack := f.deliver(t, []byte("{not json"))

	if !ack.nacked || ack.requeue || ack.acked || len(f.heard.events) != 0 {
		t.Fatalf("ack %+v, heard %d: a malformed payload must leave the queue for good", ack, len(f.heard.events))
	}
}

func TestAMessageMissingTheMoveIsSettledWithoutEvaluation(t *testing.T) {
	f := startStageConsumer(t, nil)

	ack := f.deliver(t, []byte(`{"entered":{"workspace_id":"ws1","entry_id":"conv-1","stage_id":"s1"}}`))

	if !ack.acked || len(f.heard.events) != 0 {
		t.Fatalf("ack %+v, heard %d", ack, len(f.heard.events))
	}
}

func TestAFailedEvaluationIsRetriedLaterWithTheAttemptInTheMessage(t *testing.T) {
	f := startStageConsumer(t, nil)
	f.heard.fail = errors.New("database down")

	ack := f.deliver(t, publishedMove(t, entered(stage.MoveCause{WorkflowID: "wf-a", RunID: "run-1", Chain: 1})))

	if !ack.acked || ack.nacked {
		t.Fatalf("ack %+v, the original is settled once its retry is queued", ack)
	}
	if len(f.queue.published) != 1 || f.queue.published[0].topic != workflow.TopicStageEntered || f.queue.published[0].delay != retryBackoff(1) {
		t.Fatalf("retries %+v, want one delayed copy after %s", f.queue.published, retryBackoff(1))
	}
	var body struct {
		Entered stage.Entered `json:"entered"`
		Attempt int           `json:"attempt"`
	}
	if err := json.Unmarshal(f.queue.published[0].payload, &body); err != nil {
		t.Fatal(err)
	}
	if body.Attempt != 1 || body.Entered.Cause.RunID != "run-1" || body.Entered.MoveID != "row-1" {
		t.Fatalf("retry = %+v, want attempt 1 carrying the same move and cause", body)
	}
}

func TestAnEvaluationThatKeepsFailingIsDroppedAtTheLastAttempt(t *testing.T) {
	f := startStageConsumer(t, nil)
	f.heard.fail = errors.New("database down")
	payload, err := json.Marshal(map[string]interface{}{"entered": entered(stage.MoveCause{}), "attempt": messaging.MaxRetries - 1})
	if err != nil {
		t.Fatal(err)
	}

	ack := f.deliver(t, payload)

	if !ack.acked || len(f.queue.published) != 0 {
		t.Fatalf("ack %+v, retries %d: the last attempt is dropped, not requeued", ack, len(f.queue.published))
	}
}

func TestARetryThatCannotBeQueuedReturnsTheMessageToTheQueue(t *testing.T) {
	f := startStageConsumer(t, nil)
	f.heard.fail = errors.New("database down")
	f.queue.failWith = errors.New("broker down")

	ack := f.deliver(t, publishedMove(t, entered(stage.MoveCause{})))

	if !ack.nacked || !ack.requeue || ack.acked {
		t.Fatalf("ack %+v, the move must not be lost when its retry cannot be queued", ack)
	}
}

func TestTheConsumerRefusesToStartWithoutItsDependencies(t *testing.T) {
	cases := map[string]*ConsumeStageEnteredUseCase{
		"no subscriber": NewConsumeStageEnteredUseCase(nil, &stageQueue{}, &heardEvents{}),
		"no publisher":  NewConsumeStageEnteredUseCase(&mockQueueSub{}, nil, &heardEvents{}),
		"no evaluator":  NewConsumeStageEnteredUseCase(&mockQueueSub{}, &stageQueue{}, nil),
	}
	for name, consumer := range cases {
		t.Run(name, func(t *testing.T) {
			if err := consumer.Start(); err == nil {
				t.Fatal("a consumer that cannot evaluate or retry must not subscribe")
			}
		})
	}
}

func TestAStageMoveStartsEveryMatchingWorkflowButTheOneThatMadeIt(t *testing.T) {
	runs := &startedRuns{}
	maker, other, elsewhere := stageWorkflow("wf-a", nil), stageWorkflow("wf-b", map[string]interface{}{workflow.DataKeyStageID: "s1"}), stageWorkflow("wf-c", map[string]interface{}{workflow.DataKeyStageID: "s9"})
	f := startStageConsumer(t, evaluatorOver(runs, maker, other, elsewhere))

	f.deliver(t, publishedMove(t, entered(stage.MoveCause{WorkflowID: "wf-a", RunID: "run-0"})))

	if len(runs.runs) != 1 || runs.runs[0].WorkflowID != "wf-b" {
		t.Fatalf("started %v, want only wf-b (wf-a made the move, wf-c filters another stage)", runs.createdFor)
	}
	if got := workflow.StageChainOf(runs.runs[0].State); got != 1 {
		t.Fatalf("chain in the new run = %d, want 1", got)
	}
}

func TestTheSameMoveDeliveredTwiceNamesTheSameRun(t *testing.T) {
	runs := &startedRuns{}
	f := startStageConsumer(t, evaluatorOver(runs, stageWorkflow("wf-a", nil)))
	payload := publishedMove(t, entered(stage.MoveCause{}))

	f.deliver(t, payload)
	f.deliver(t, payload)

	if len(runs.runs) != 2 || runs.runs[0].ID != runs.runs[1].ID {
		t.Fatalf("run ids = %v, want one id per move so the store refuses the duplicate", runs.runs)
	}
}

type unreadableWorkflows struct{ stubWorkflowRepo }

func (unreadableWorkflows) FindActiveByTrigger(string, workflow.TriggerType) ([]*workflow.Workflow, error) {
	return nil, errors.New("database down")
}

func TestAnEvaluationThatCannotReadTheWorkflowsReportsItSoTheMoveIsRetried(t *testing.T) {
	runs := &startedRuns{}
	evaluator := NewTriggerEvaluator(unreadableWorkflows{}, runs, NewRunEngine(runs, stubRunLogRepo{}, NewNodeExecutorRegistry()), nil)
	event, _ := workflow.StageAddedEvent(workflow.StageEntry{MoveID: "row-1", WorkspaceID: "ws1", EntryID: "conv-1", StageID: "s1"})

	if err := evaluator.EvaluateChecked(event); err == nil {
		t.Fatal("nothing was evaluated, so the caller must learn it failed")
	}
	if len(runs.runs) != 0 {
		t.Fatalf("started %d runs", len(runs.runs))
	}
}

func TestAChainOfWorkflowsMovingTheCardStopsAtTheLimit(t *testing.T) {
	chain := 0
	starts := 0
	for {
		event, ok := workflow.StageAddedEvent(workflow.StageEntry{MoveID: "m", WorkspaceID: "ws1", EntryID: "conv-1", StageID: "s1", ByWorkflowID: "wf-ping", ByChain: chain})
		if !ok {
			break
		}
		starts++
		state := workflow.NewRunState()
		for k, v := range event.Data {
			state.Set(k, v)
		}
		chain = workflow.StageChainOf(state)
		if starts > workflow.MaxStageChain+1 {
			t.Fatal("the chain never ends")
		}
	}
	if starts != workflow.MaxStageChain {
		t.Fatalf("starts = %d, want %d", starts, workflow.MaxStageChain)
	}
}

type heldEvaluations struct {
	started chan string
	release chan struct{}
}

func (h *heldEvaluations) Evaluate(event workflow.TriggerEvent) { _ = h.EvaluateChecked(event) }

func (h *heldEvaluations) EvaluateChecked(event workflow.TriggerEvent) error {
	h.started <- event.Occurrence
	<-h.release
	return nil
}

func TestStageMovesAreEvaluatedConcurrentlyAndAckedOnlyWhenTheirEvaluationEnds(t *testing.T) {
	held := &heldEvaluations{started: make(chan string, workflow.StageEnteredWorkers), release: make(chan struct{})}
	f := startStageConsumer(t, held)

	acks := make([]*settledAck, workflow.StageEnteredWorkers)
	for i := range acks {
		move := entered(stage.MoveCause{})
		move.MoveID = "row-" + string(rune('a'+i))
		acks[i] = newSettledAck()
		f.sub.handler(publishedMove(t, move), acks[i])
	}
	seen := map[string]bool{}
	for len(seen) < workflow.StageEnteredWorkers {
		select {
		case id := <-held.started:
			seen[id] = true
		case <-time.After(time.Second):
			t.Fatalf("%d of %d moves are being evaluated at once", len(seen), workflow.StageEnteredWorkers)
		}
	}
	for i, ack := range acks {
		if ack.settled() {
			t.Fatalf("move %d was settled while its evaluation was still running, so a crash would lose it", i)
		}
	}
	close(held.release)
	for _, ack := range acks {
		ack.wait(t)
		if !ack.acked {
			t.Fatalf("ack %+v", ack)
		}
	}
}
