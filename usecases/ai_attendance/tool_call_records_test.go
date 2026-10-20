package ai_attendance_usecase

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"vozko/domain/ai"
	aa "vozko/domain/ai_attendance"
	"vozko/domain/conversation"
	"vozko/domain/redact"
	"vozko/domain/shared"
	"vozko/domain/tools"
)

type toolCallLogSpy struct {
	mu        sync.Mutex
	batches   [][]aa.ToolCall
	recordErr error
}

func (s *toolCallLogSpy) Record(calls []aa.ToolCall) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.batches = append(s.batches, calls)
	return s.recordErr
}

func (s *toolCallLogSpy) ListByEntry(workspaceID, entryID, entryType string, p shared.Pagination) (*shared.PaginatedResult[*aa.ToolCall], error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := []*aa.ToolCall{}
	for _, batch := range s.batches {
		for i := range batch {
			c := batch[i]
			if c.WorkspaceID == workspaceID && c.EntryID == entryID && c.EntryType == entryType {
				items = append(items, &c)
			}
		}
	}
	return shared.NewPaginatedResult(items, p, int64(len(items))), nil
}

func (s *toolCallLogSpy) DeleteCreatedBefore(time.Time, int) (int64, error) { return 0, nil }

type toolMetricsSpy struct {
	mu    sync.Mutex
	calls []string
}

func (s *toolMetricsSpy) IncAIToolCall(tool, outcome string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, tool+"/"+outcome)
}

func syncDispatch(fn func()) { fn() }

func counting(inner ai.Service, sessions aa.ToolCallRecorder) ai.Service {
	return countToolCalls(inner, sessions, &toolCallLogSpy{}, &toolMetricsSpy{}, syncDispatch)
}

func executedTurn() *ai.GenerateOutput {
	return &ai.GenerateOutput{ToolCalls: []ai.ToolCall{
		{Name: "http_request", Arguments: map[string]interface{}{"url": "https://api", "headers": map[string]interface{}{"X-Webhook-Token": "tok-9"}, "cpf": "52998224725"}, Result: &tools.ExecutionResult{Result: map[string]interface{}{"ok": true}}, DurationMs: 120},
		{Name: "transfer_to_human", Arguments: map[string]interface{}{}, Result: &tools.ExecutionResult{Result: "tool transfer_to_human failed: 409 O workflow não está ativo", IsError: true}, DurationMs: 30},
		{Name: "route_to_sales", Arguments: map[string]interface{}{"reason": "quer comprar"}},
	}}
}

func turnWithTools() ai.GenerateInput {
	in := agentTurnInput()
	in.SessionID = "sess-1"
	in.Tools = []tools.Definition{{Name: "http_request"}, {Name: "transfer_to_human"}, {Name: "route_to_sales"}}
	return in
}

func TestEveryToolCallOfAnAgentTurnIsRecordedOnceWithItsOutcome(t *testing.T) {
	log, metrics, sessions := &toolCallLogSpy{}, &toolMetricsSpy{}, &toolCallSpy{}
	counted := countToolCalls(scriptedAI{out: executedTurn()}, sessions, log, metrics, syncDispatch)

	if _, err := counted.Generate(context.Background(), turnWithTools()); err != nil {
		t.Fatal(err)
	}
	if len(sessions.calls) != 1 || sessions.calls[0].count != 3 {
		t.Fatalf("the session counter must count the same three calls once, got %+v", sessions.calls)
	}
	if len(log.batches) != 1 || len(log.batches[0]) != 3 {
		t.Fatalf("batches = %+v", log.batches)
	}
	got := log.batches[0]
	first := got[0]
	if first.WorkspaceID != "ws-1" || first.EntryID != "entry-1" || first.EntryType != "instagram" || first.AgentID != "agent-1" || first.SessionID != "sess-1" {
		t.Fatalf("identity = %+v", first)
	}
	if first.ToolName != "http_request" || first.Outcome != aa.ToolCallOK || first.DurationMs != 120 || first.ID == "" || first.CreatedAt.IsZero() {
		t.Fatalf("first = %+v", first)
	}
	headers, _ := first.Args["headers"].(map[string]interface{})
	if headers["X-Webhook-Token"] != redact.Mask || first.Args["cpf"] != "•••••••••25" || first.Args["url"] != "https://api" {
		t.Fatalf("args not redacted: %+v", first.Args)
	}
	if got[1].Outcome != aa.ToolCallError || !strings.Contains(got[1].Error, "não está ativo") {
		t.Fatalf("failed call = %+v", got[1])
	}
	if got[2].Outcome != aa.ToolCallRequested {
		t.Fatalf("a call the model made but nobody executed here = %+v", got[2])
	}
	if strings.Join(metrics.calls, ",") != "http_request/ok,transfer_to_human/error,route_to_sales/requested" {
		t.Fatalf("metrics = %v", metrics.calls)
	}
}

func TestTheRecordAndTheReturnedCallShareOneToolCallID(t *testing.T) {
	log := &toolCallLogSpy{}
	out := &ai.GenerateOutput{ToolCalls: []ai.ToolCall{
		{ID: "call_model_1", Name: "http_request", Result: &tools.ExecutionResult{Result: "ok"}},
		{Name: "transfer_to_human", Result: &tools.ExecutionResult{Result: "ok"}},
	}}
	counted := countToolCalls(scriptedAI{out: out}, nil, log, &toolMetricsSpy{}, syncDispatch)

	got, err := counted.Generate(context.Background(), turnWithTools())
	if err != nil {
		t.Fatal(err)
	}
	records := log.batches[0]
	if records[0].ToolCallID != "call_model_1" || got.ToolCalls[0].ID != "call_model_1" {
		t.Fatalf("the model's id must reach the record unchanged: record %q, call %q", records[0].ToolCallID, got.ToolCalls[0].ID)
	}
	if records[1].ToolCallID == "" || records[1].ToolCallID != got.ToolCalls[1].ID {
		t.Fatalf("a call without a model id gets one id for both: record %q, call %q", records[1].ToolCallID, got.ToolCalls[1].ID)
	}
	if records[1].ID == records[1].ToolCallID {
		t.Fatal("the record keeps its own row id")
	}
}

func TestAToolNameTheAgentWasNotOfferedIsNotAMetricLabel(t *testing.T) {
	metrics := &toolMetricsSpy{}
	out := &ai.GenerateOutput{ToolCalls: []ai.ToolCall{{Name: "made_up_tool_42"}}}
	counted := countToolCalls(scriptedAI{out: out}, nil, &toolCallLogSpy{}, metrics, syncDispatch)
	if _, err := counted.Generate(context.Background(), turnWithTools()); err != nil {
		t.Fatal(err)
	}
	if len(metrics.calls) != 1 || metrics.calls[0] != unlistedToolLabel+"/requested" {
		t.Fatalf("metrics = %v", metrics.calls)
	}
}

func TestARunStartedByAToolIsLinkedOnItsRecordAndABoundToolCountsUnderItsHandler(t *testing.T) {
	log, metrics := &toolCallLogSpy{}, &toolMetricsSpy{}
	out := &ai.GenerateOutput{ToolCalls: []ai.ToolCall{
		{Name: "run_workflow_wf-1", Arguments: map[string]interface{}{}, Result: &tools.ExecutionResult{Result: "Fluxo iniciado.", RunID: "run-9"}},
		{Name: "http_request", Arguments: map[string]interface{}{}, Result: &tools.ExecutionResult{Result: "ok"}},
	}}
	in := turnWithTools()
	in.Tools = append(in.Tools, tools.Definition{Name: "run_workflow_wf-1", HandlerName: "run_workflow"})
	counted := countToolCalls(scriptedAI{out: out}, nil, log, metrics, syncDispatch)

	if _, err := counted.Generate(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	got := log.batches[0]
	if got[0].RunID != "run-9" || got[0].ToolName != "run_workflow_wf-1" || got[1].RunID != "" {
		t.Fatalf("records = %+v", got)
	}
	if strings.Join(metrics.calls, ",") != "run_workflow/ok,http_request/ok" {
		t.Fatalf("metrics = %v", metrics.calls)
	}
}

func TestAFailedRecordNeverFailsTheReply(t *testing.T) {
	log := &toolCallLogSpy{recordErr: errors.New("db down")}
	out := executedTurn()
	counted := countToolCalls(scriptedAI{out: out}, nil, log, &toolMetricsSpy{}, syncDispatch)
	got, err := counted.Generate(context.Background(), turnWithTools())
	if err != nil || got != out {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestToolCallsOutsideAnAgentTurnAreNotRecorded(t *testing.T) {
	log := &toolCallLogSpy{}
	in := turnWithTools()
	in.AgentTurn = ai.AgentTurn{}
	counted := countToolCalls(scriptedAI{out: executedTurn()}, nil, log, &toolMetricsSpy{}, syncDispatch)
	if _, err := counted.Generate(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if len(log.batches) != 0 {
		t.Fatalf("batches = %+v", log.batches)
	}
}

func TestRecordingRunsOffTheReplyPath(t *testing.T) {
	release := make(chan struct{})
	done := make(chan struct{})
	log := &blockingLog{release: release, done: done}
	counted := CountToolCalls(scriptedAI{out: executedTurn()}, nil, log, &toolMetricsSpy{})

	finished := make(chan struct{})
	go func() {
		_, _ = counted.Generate(context.Background(), turnWithTools())
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("the reply waited for the tool call record")
	}
	close(release)
	<-done
}

type blockingLog struct {
	toolCallLogSpy
	release chan struct{}
	done    chan struct{}
}

func (b *blockingLog) Record([]aa.ToolCall) error {
	<-b.release
	close(b.done)
	return nil
}

func TestToolCallsAreListedForAConversationTheCallerCanSee(t *testing.T) {
	log := &toolCallLogSpy{}
	counted := countToolCalls(scriptedAI{out: executedTurn()}, nil, log, &toolMetricsSpy{}, syncDispatch)
	if _, err := counted.Generate(context.Background(), turnWithTools()); err != nil {
		t.Fatal(err)
	}
	person := shared.Person{UserID: "u-1"}
	cases := []struct {
		name    string
		rule    conversationRule
		visible bool
	}{
		{"visible", conversationRule{departments: []string{"dept-sales"}, department: "dept-sales"}, true},
		{"hidden by department", conversationRule{departments: []string{"dept-support"}, viewOthers: true, department: "dept-sales"}, false},
		{"hidden by ownership without view_others", conversationRule{department: "dept-sales", assignee: "u-2"}, false},
		{"hidden when the lookup fails", conversationRule{err: errors.New("department scope unreadable")}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			page, err := NewListToolCallsUseCase(log, tc.rule).Execute(person, "ws-1", "instagram", "entry-1", shared.Pagination{Page: 1, PageSize: 20})
			if tc.visible && (err != nil || len(page.Items) != 3) {
				t.Fatalf("page = %+v err = %v", page, err)
			}
			if !tc.visible && (!errors.Is(err, ErrConversationNotVisible) || page != nil) {
				t.Fatalf("a conversation the caller cannot see must be refused, got %+v, %v", page, err)
			}
		})
	}
	if _, err := NewListToolCallsUseCase(log, conversationRule{}).Execute(person, "", "instagram", "entry-1", shared.Pagination{}); !errors.Is(err, ErrConversationNotVisible) {
		t.Fatalf("no workspace must be refused, got %v", err)
	}
}

type conversationRule struct {
	departments []string
	viewOthers  bool
	department  string
	assignee    string
	err         error
}

func (r conversationRule) CanView(viewer shared.Person, workspaceID, _, _ string) (bool, error) {
	if r.err != nil {
		return false, r.err
	}
	if workspaceID == "" {
		return false, nil
	}
	if r.assignee != "" && r.assignee != viewer.UserID && !r.viewOthers {
		return false, nil
	}
	return r.departments == nil || slices.Contains(r.departments, r.department) || r.assignee == viewer.UserID, nil
}

func (r conversationRule) Scope(shared.Person, string) (conversation.VisibilityScope, error) {
	return conversation.VisibilityScope{}, r.err
}

func TestARefusalCodeIsRecordedSoTheRuntimeAlertCanTellAnInactiveWorkflowApart(t *testing.T) {
	log := &toolCallLogSpy{}
	out := &ai.GenerateOutput{ToolCalls: []ai.ToolCall{
		{Name: "run_workflow_wf-1", Arguments: map[string]interface{}{}, Result: &tools.ExecutionResult{Result: "Fluxo inativo.", IsError: true, ErrorCode: "workflow_not_active"}},
	}}
	counted := countToolCalls(scriptedAI{out: out}, nil, log, &toolMetricsSpy{}, syncDispatch)
	if _, err := counted.Generate(context.Background(), turnWithTools()); err != nil {
		t.Fatal(err)
	}
	if got := log.batches[0][0]; got.Outcome != aa.ToolCallError || got.ErrorCode != "workflow_not_active" {
		t.Fatalf("record = %+v", got)
	}
}
