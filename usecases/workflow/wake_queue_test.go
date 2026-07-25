package workflow_usecase

import (
	"encoding/json"
	"testing"
	"time"

	"vozko/domain/messaging"
	"vozko/domain/workflow"
)

type mockQueuePub struct {
	topic   string
	payload []byte
	delay   time.Duration
	calls   int
	err     error
}

func (m *mockQueuePub) Publish(topic string, message []byte) error {
	m.topic = topic
	m.payload = append([]byte(nil), message...)
	m.delay = 0
	m.calls++
	return nil
}

func (m *mockQueuePub) PublishWithDelay(topic string, message []byte, delay time.Duration) error {
	m.topic = topic
	m.payload = append([]byte(nil), message...)
	m.delay = delay
	m.calls++
	return m.err
}

func (m *mockQueuePub) ValidateConnection() error {
	return nil
}

type mockQueueSub struct {
	topic   string
	handler func([]byte, messaging.MessageAck)
}

func (m *mockQueueSub) Subscribe(topic string, handler func(message []byte, ack messaging.MessageAck)) error {
	m.topic = topic
	m.handler = handler
	return nil
}

func (m *mockQueueSub) DeleteQueue(topic string) error {
	return nil
}

func (m *mockQueueSub) ValidateConnection() error {
	return nil
}

func (m *mockQueueSub) GetQueueLength(topic string) (int, error) {
	return 0, nil
}

type mockAck struct {
	acked  bool
	nacked bool
}

func (a *mockAck) Ack() error {
	a.acked = true
	return nil
}

func (a *mockAck) Nack(requeue bool) error {
	a.nacked = true
	return nil
}

func (a *mockAck) DeliveryCount() int {
	return 1
}

type mockWakeScheduler struct {
	runID  string
	wakeAt int64
	calls  int
	err    error
}

func (m *mockWakeScheduler) ScheduleRunWake(runID string, wakeAt int64) error {
	m.runID = runID
	m.wakeAt = wakeAt
	m.calls++
	return m.err
}

func TestQueueWakeScheduler_PublishesDelayedMessage(t *testing.T) {
	pub := &mockQueuePub{}
	scheduler := NewQueueWakeScheduler(pub)

	wakeAt := time.Now().UTC().Add(15 * time.Second).UnixMilli()
	if err := scheduler.ScheduleRunWake("r1", wakeAt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pub.calls != 1 {
		t.Fatalf("expected one publish call, got %d", pub.calls)
	}
	if pub.topic != workflow.TopicRunWake {
		t.Fatalf("expected topic %s, got %s", workflow.TopicRunWake, pub.topic)
	}

	var msg workflow.RunWakeMessage
	if err := json.Unmarshal(pub.payload, &msg); err != nil {
		t.Fatalf("failed to decode message payload: %v", err)
	}
	if msg.RunID != "r1" || msg.WakeAt != wakeAt {
		t.Fatalf("unexpected payload: %+v", msg)
	}
	if pub.delay < 14*time.Second || pub.delay > 16*time.Second {
		t.Fatalf("unexpected delay: %s", pub.delay)
	}
}

func TestQueueWakeScheduler_PublishesImmediateForShortDelay(t *testing.T) {
	pub := &mockQueuePub{}
	scheduler := NewQueueWakeScheduler(pub)

	wakeAt := time.Now().UTC().Add(2 * time.Second).UnixMilli()
	if err := scheduler.ScheduleRunWake("r1", wakeAt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pub.calls != 1 {
		t.Fatalf("expected one publish call, got %d", pub.calls)
	}
	if pub.topic != workflow.TopicRunWake {
		t.Fatalf("expected topic %s, got %s", workflow.TopicRunWake, pub.topic)
	}
	if pub.delay != 0 {
		t.Fatalf("expected immediate publish for short delay, got delay=%s", pub.delay)
	}
}

func TestRunEngine_EnqueuesWakeOnWait(t *testing.T) {
	runRepo := NewMockWorkflowRunRepository()
	logRepo := NewMockWorkflowRunLogRepository()
	registry := NewNodeExecutorRegistry()
	RegisterDefaultExecutors(registry, ExecutorDeps{})
	engine := NewRunEngine(runRepo, logRepo, registry)

	scheduler := &mockWakeScheduler{}
	engine.SetWakeScheduler(scheduler)

	w := &workflow.Workflow{
		ID:          "w1",
		WorkspaceID: "ws1",
		TriggerType: workflow.TriggerManual,
		Status:      workflow.WorkflowStatusActive,
		Graph: workflow.Graph{
			Nodes: []workflow.Node{
				{ID: "t1", Type: workflow.NodeTypeTriggerManual},
				{ID: "wd", Type: workflow.NodeTypeWaitDuration, Config: map[string]interface{}{"seconds": float64(5)}},
				{ID: "e1", Type: workflow.NodeTypeEnd},
			},
			Edges: []workflow.Edge{
				{Source: "t1", Target: "wd"},
				{Source: "wd", Target: "e1", Label: "completed"},
			},
		},
	}

	now := time.Now().UTC()
	run := &workflow.WorkflowRun{
		ID:            "r1",
		WorkflowID:    "w1",
		WorkspaceID:   "ws1",
		EntryID:       "e1",
		EntryType:     "lead",
		Status:        workflow.RunStatusRunning,
		CurrentNodeID: "t1",
		State:         workflow.NewRunState(),
		StartedAt:     now,
		UpdatedAt:     now,
	}
	runRepo.runs[run.ID] = run

	if err := engine.Execute(run, w); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, _ := runRepo.FindByID("r1")
	if updated.Status != workflow.RunStatusWaiting {
		t.Fatalf("expected waiting status, got %s", updated.Status)
	}
	if updated.WakeAt == nil {
		t.Fatal("expected wake_at to be set")
	}
	if scheduler.calls != 1 {
		t.Fatalf("expected wake scheduler to be called once, got %d", scheduler.calls)
	}
	if scheduler.runID != "r1" || scheduler.wakeAt != *updated.WakeAt {
		t.Fatalf("unexpected scheduler args runID=%s wakeAt=%d expectedWakeAt=%d", scheduler.runID, scheduler.wakeAt, *updated.WakeAt)
	}
}

func TestConsumeRunWake_ResumesMatchingRun(t *testing.T) {
	wfRepo := NewMockWorkflowRepository()
	runRepo := NewMockWorkflowRunRepository()
	logRepo := NewMockWorkflowRunLogRepository()
	registry := NewNodeExecutorRegistry()
	RegisterDefaultExecutors(registry, ExecutorDeps{})
	engine := NewRunEngine(runRepo, logRepo, registry)

	w := &workflow.Workflow{
		ID:          "w1",
		WorkspaceID: "ws1",
		TriggerType: workflow.TriggerManual,
		Status:      workflow.WorkflowStatusActive,
		Graph: workflow.Graph{
			Nodes: []workflow.Node{
				{ID: "t1", Type: workflow.NodeTypeTriggerManual},
				{ID: "wd", Type: workflow.NodeTypeWaitDuration, Config: map[string]interface{}{"seconds": float64(5)}},
				{ID: "e1", Type: workflow.NodeTypeEnd},
			},
			Edges: []workflow.Edge{
				{Source: "t1", Target: "wd"},
				{Source: "wd", Target: "e1", Label: "completed"},
			},
		},
	}
	wfRepo.workflows[w.ID] = w

	pastWake := time.Now().UTC().Add(-1 * time.Second).UnixMilli()
	run := &workflow.WorkflowRun{
		ID:            "r1",
		WorkflowID:    "w1",
		WorkspaceID:   "ws1",
		EntryID:       "e1",
		EntryType:     "lead",
		Status:        workflow.RunStatusWaiting,
		CurrentNodeID: "wd",
		State:         workflow.NewRunState(),
		WakeAt:        &pastWake,
		StartedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}
	runRepo.runs[run.ID] = run

	sub := &mockQueueSub{}
	pub := &mockQueuePub{}
	uc := NewConsumeRunWakeUseCase(sub, pub, wfRepo, runRepo, engine)
	if err := uc.Start(); err != nil {
		t.Fatalf("unexpected start error: %v", err)
	}
	if sub.topic != workflow.TopicRunWake {
		t.Fatalf("expected subscription topic %s, got %s", workflow.TopicRunWake, sub.topic)
	}

	payload, _ := json.Marshal(workflow.RunWakeMessage{RunID: "r1", WakeAt: pastWake})
	ack := &mockAck{}
	sub.handler(payload, ack)

	if !ack.acked || ack.nacked {
		t.Fatalf("expected acked=true and nacked=false, got acked=%v nacked=%v", ack.acked, ack.nacked)
	}
	updated, _ := runRepo.FindByID("r1")
	if updated.Status != workflow.RunStatusCompleted {
		t.Fatalf("expected run to complete after wake consume, got status=%s", updated.Status)
	}
}

func TestConsumeRunWake_IgnoresStaleWakeMessage(t *testing.T) {
	wfRepo := NewMockWorkflowRepository()
	runRepo := NewMockWorkflowRunRepository()
	logRepo := NewMockWorkflowRunLogRepository()
	registry := NewNodeExecutorRegistry()
	RegisterDefaultExecutors(registry, ExecutorDeps{})
	engine := NewRunEngine(runRepo, logRepo, registry)

	w := &workflow.Workflow{
		ID:          "w1",
		WorkspaceID: "ws1",
		TriggerType: workflow.TriggerManual,
		Status:      workflow.WorkflowStatusActive,
		Graph:       workflow.Graph{Nodes: []workflow.Node{{ID: "wd", Type: workflow.NodeTypeWaitDuration}}},
	}
	wfRepo.workflows[w.ID] = w

	wakeAt := time.Now().UTC().Add(3 * time.Second).UnixMilli()
	run := &workflow.WorkflowRun{
		ID:            "r1",
		WorkflowID:    "w1",
		WorkspaceID:   "ws1",
		EntryID:       "e1",
		EntryType:     "lead",
		Status:        workflow.RunStatusWaiting,
		CurrentNodeID: "wd",
		State:         workflow.NewRunState(),
		WakeAt:        &wakeAt,
		StartedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}
	runRepo.runs[run.ID] = run

	sub := &mockQueueSub{}
	pub := &mockQueuePub{}
	uc := NewConsumeRunWakeUseCase(sub, pub, wfRepo, runRepo, engine)
	if err := uc.Start(); err != nil {
		t.Fatalf("unexpected start error: %v", err)
	}

	payload, _ := json.Marshal(workflow.RunWakeMessage{RunID: "r1", WakeAt: wakeAt - 1})
	ack := &mockAck{}
	sub.handler(payload, ack)

	if !ack.acked || ack.nacked {
		t.Fatalf("expected acked=true and nacked=false, got acked=%v nacked=%v", ack.acked, ack.nacked)
	}
	updated, _ := runRepo.FindByID("r1")
	if updated.Status != workflow.RunStatusWaiting {
		t.Fatalf("expected waiting status to remain on stale wake, got %s", updated.Status)
	}
}

var _ messaging.MessageQueueSub = (*mockQueueSub)(nil)
var _ messaging.MessageQueuePub = (*mockQueuePub)(nil)
