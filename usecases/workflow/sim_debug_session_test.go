package workflow_usecase

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"vozko/domain/shared"
	"vozko/domain/workflow"
	"vozko/usecases/workflow/node_executors"
)

type countingAPI struct {
	server *httptest.Server
	hits   atomic.Int32
}

func newCountingAPI(t *testing.T) *countingAPI {
	t.Helper()
	api := &countingAPI{}
	api.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api.hits.Add(1)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(api.server.Close)
	return api
}

func debugWorkflow(apiURL string, pin *workflow.NodePin) *workflow.Workflow {
	return &workflow.Workflow{
		ID: "wf-1", WorkspaceID: "ws1", Name: "Matrícula", Type: workflow.WorkflowTypeMessages, Status: workflow.WorkflowStatusDraft,
		Graph: workflow.Graph{
			Nodes: []workflow.Node{
				{ID: "t", Type: workflow.NodeTypeTriggerWebhook},
				{ID: "login", Type: workflow.NodeTypeActionHTTPRequest, Config: map[string]interface{}{"method": "GET", "url": apiURL + "/login", "api_token": "abc", "capture_variable": "login"}, Pinned: pin},
				{ID: "ok", Type: workflow.NodeTypeActionSetVariable, Config: map[string]interface{}{"variable": "resultado", "value": "ok"}},
				{ID: "bad", Type: workflow.NodeTypeActionSetVariable, Config: map[string]interface{}{"variable": "resultado", "value": "falha"}},
				{ID: "end", Type: workflow.NodeTypeEnd},
			},
			Edges: []workflow.Edge{
				{Source: "t", Target: "login"},
				{Source: "login", Target: "ok", Label: "sucesso"},
				{Source: "login", Target: "bad", Label: "falha"},
				{Source: "ok", Target: "end"},
				{Source: "bad", Target: "end"},
			},
		},
	}
}

type runDetailStub struct {
	detail    *workflow.RunDetail
	err       error
	viewer    shared.Person
	pick      *workflow.StepPick
	fullReads int
}

func (s *runDetailStub) PickSteps(viewer shared.Person, workspaceID, runID string, pick workflow.StepPick) (*workflow.RunDetail, error) {
	s.pick = &pick
	return s.read(viewer, workspaceID, runID)
}

func (s *runDetailStub) Execute(viewer shared.Person, workspaceID, runID string) (*workflow.RunDetail, error) {
	s.fullReads++
	return s.read(viewer, workspaceID, runID)
}

func (s *runDetailStub) read(viewer shared.Person, workspaceID, runID string) (*workflow.RunDetail, error) {
	s.viewer = viewer
	if s.err != nil {
		return nil, s.err
	}
	if s.detail == nil || s.detail.Run.ID != runID || s.detail.Run.WorkspaceID != workspaceID {
		return nil, workflow.ErrRunNotFound
	}
	return s.detail, nil
}

type permissionStub struct{ allowed map[string]bool }

func (p permissionStub) HasWorkspacePermission(_, _, resource, action string, _ bool) bool {
	return p.allowed[resource+":"+action]
}

type debugSim struct {
	conn *websocket.Conn
	runs *runDetailStub
}

type debugSimOptions struct {
	workflow      *workflow.Workflow
	awaitStart    bool
	idle          time.Duration
	runs          *runDetailStub
	permissions   permissionStub
	triggerInputs TriggerInputReaders
}

func startDebugSimulation(t *testing.T, opts debugSimOptions) *debugSim {
	t.Helper()
	repo := NewMockWorkflowRepository()
	_ = repo.Create(opts.workflow)
	if opts.runs == nil {
		opts.runs = &runDetailStub{}
	}
	if opts.permissions.allowed == nil {
		opts.permissions = permissionStub{allowed: map[string]bool{"workflows:read": true}}
	}
	sim := NewWSWorkflowSimulationUseCase(WSWorkflowSimulationDeps{
		WorkflowRepo:     repo,
		HTTPEgress:       node_executors.HTTPEgress{Client: &http.Client{}},
		Runs:             opts.runs,
		Permissions:      opts.permissions,
		PauseIdleTimeout: opts.idle,
		TriggerInputs:    opts.triggerInputs,
	})
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_ = sim.HandleSession(context.Background(), conn, SimulationSession{
			WorkflowID: opts.workflow.ID, WorkspaceID: "ws1", Viewer: shared.Person{UserID: "u-1"}, AwaitStart: opts.awaitStart,
		})
	}))
	t.Cleanup(server.Close)
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return &debugSim{conn: conn, runs: opts.runs}
}

func (s *debugSim) send(t *testing.T, msgType string, data interface{}) {
	t.Helper()
	msg := map[string]interface{}{"type": msgType}
	if data != nil {
		msg["data"] = data
	}
	if err := s.conn.WriteJSON(msg); err != nil {
		t.Fatalf("send %s: %v", msgType, err)
	}
}

type streamedStep struct {
	StepIndex int                    `json:"stepIndex"`
	NodeID    string                 `json:"nodeId"`
	NodeType  string                 `json:"nodeType"`
	Status    string                 `json:"status"`
	Attempt   int                    `json:"attempt"`
	Input     map[string]interface{} `json:"input"`
	Output    map[string]interface{} `json:"output"`
	Error     string                 `json:"error"`
	ErrorKind string                 `json:"errorKind"`
	Branch    string                 `json:"branch"`
	Pinned    bool                   `json:"pinned"`
	StartedAt *time.Time             `json:"startedAt"`
	HTTP      []json.RawMessage      `json:"http"`
}

func stepsIn(frames []simFrame) []streamedStep {
	var out []streamedStep
	for _, f := range frames {
		if f.Type != "node_executed" {
			continue
		}
		var s streamedStep
		_ = json.Unmarshal(f.Payload, &s)
		out = append(out, s)
	}
	return out
}

func decode[T any](t *testing.T, frame simFrame) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(frame.Payload, &v); err != nil {
		t.Fatalf("decode %s: %v", frame.Type, err)
	}
	return v
}

type pausedFrame struct {
	NodeID    string                 `json:"nodeId"`
	StepIndex int                    `json:"stepIndex"`
	Reason    string                 `json:"reason"`
	State     map[string]interface{} `json:"state"`
}

type startedFrame struct {
	RunID         string   `json:"runId"`
	RealCallNodes []string `json:"realCallNodes"`
	Reproducing   *struct {
		RunID       string   `json:"runId"`
		PinnedNodes []string `json:"pinnedNodes"`
	} `json:"reproducing"`
}

func TestTheSimulationStreamsTheSameRedactedStepRecordAsARunWithAStepIndex(t *testing.T) {
	api := newCountingAPI(t)
	sim := startDebugSimulation(t, debugSimOptions{workflow: debugWorkflow(api.server.URL, nil), awaitStart: true})
	readUntil(t, sim.conn, "sim_ready")
	sim.send(t, "confirm_real_calls", nil)
	sim.send(t, "start", nil)

	_, frames := readUntil(t, sim.conn, "run_completed")
	steps := stepsIn(frames)
	if len(steps) < 3 {
		t.Fatalf("steps = %+v", steps)
	}
	for i, s := range steps {
		if s.StepIndex != i {
			t.Fatalf("step %d has index %d, want a running index", i, s.StepIndex)
		}
	}
	login := steps[1]
	if login.NodeID != "login" || login.Status != "executed" || login.Attempt != 1 || login.StartedAt == nil || login.Branch != "sucesso" {
		t.Fatalf("login step = %+v, want the full step record", login)
	}
	if login.Input["url"] != api.server.URL+"/login" || login.Input["api_token"] == "abc" {
		t.Fatalf("parameters used = %v, want the resolved values with credentials masked", login.Input)
	}
	if len(login.HTTP) != 1 {
		t.Fatalf("the HTTP exchange must stream too, got %d", len(login.HTTP))
	}
}

func TestARealCallWithoutConfirmationFailsClosed(t *testing.T) {
	api := newCountingAPI(t)
	sim := startDebugSimulation(t, debugSimOptions{workflow: debugWorkflow(api.server.URL, nil)})

	started, _ := readUntil(t, sim.conn, "sim_started")
	if got := decode[startedFrame](t, started).RealCallNodes; len(got) != 1 || got[0] != "login" {
		t.Fatalf("realCallNodes = %v, want the unpinned HTTP node", got)
	}
	_, frames := readUntil(t, sim.conn, "run_error")
	if api.hits.Load() != 0 {
		t.Fatal("no request may leave without confirmation")
	}
	steps := stepsIn(frames)
	last := steps[len(steps)-1]
	if last.NodeID != "login" || last.ErrorKind != string(workflow.ErrorKindRealCallNotConfirmed) || last.Error == "" {
		t.Fatalf("step = %+v, want the refusal with its kind and message", last)
	}
}

func TestAPinnedNodeReturnsItsPinAndNeverCallsOut(t *testing.T) {
	api := newCountingAPI(t)
	pin := &workflow.NodePin{Output: map[string]interface{}{"status_code": 500.0, "json": map[string]interface{}{"erro": "senha"}}, Edge: "falha", Source: workflow.PinSourceSimulation, PinnedAt: pinTimeForTests()}
	sim := startDebugSimulation(t, debugSimOptions{workflow: debugWorkflow(api.server.URL, pin)})

	started, _ := readUntil(t, sim.conn, "sim_started")
	if got := decode[startedFrame](t, started).RealCallNodes; len(got) != 0 {
		t.Fatalf("realCallNodes = %v, a pinned node makes no real call", got)
	}
	completed, frames := readUntil(t, sim.conn, "run_completed")
	if api.hits.Load() != 0 {
		t.Fatal("a pinned HTTP node must not call out")
	}
	vars := decode[statePayload](t, completed).Vars
	if captured, _ := vars["login"].(map[string]interface{}); captured["erro"] != "senha" {
		t.Fatalf("login = %v, want the capture variable filled from the pinned answer", vars["login"])
	}
	steps := stepsIn(frames)
	if !steps[1].Pinned || steps[1].Output["status_code"] != 500.0 || steps[1].Branch != "falha" || steps[2].NodeID != "bad" {
		t.Fatalf("steps = %+v, want the pinned output followed by the pinned edge", steps)
	}
}

func TestEveryStateTheSimulationStreamsIsRedacted(t *testing.T) {
	api := newCountingAPI(t)
	pin := &workflow.NodePin{Output: map[string]interface{}{"status_code": 200.0, "json": map[string]interface{}{"access_token": "tok-secret-1"}}, Edge: "sucesso", Source: workflow.PinSourceSimulation, PinnedAt: pinTimeForTests()}
	sim := startDebugSimulation(t, debugSimOptions{workflow: debugWorkflow(api.server.URL, pin)})

	completed, frames := readUntil(t, sim.conn, "run_completed")
	states := 0
	for _, frame := range append(frames, completed) {
		if frame.Type != "state_update" && frame.Type != "run_completed" {
			continue
		}
		states++
		if strings.Contains(string(frame.Payload), "tok-secret-1") {
			t.Fatalf("%s carries the raw token: %s", frame.Type, frame.Payload)
		}
	}
	if states == 0 {
		t.Fatal("expected state frames")
	}
}

func TestABreakpointPausesBeforeTheNodeThenStepAndContinueResume(t *testing.T) {
	api := newCountingAPI(t)
	sim := startDebugSimulation(t, debugSimOptions{workflow: debugWorkflow(api.server.URL, nil), awaitStart: true})

	ready, _ := readUntil(t, sim.conn, "sim_ready")
	if got := decode[startedFrame](t, ready).RealCallNodes; len(got) != 1 {
		t.Fatalf("sim_ready must list the real calls before the run starts, got %v", got)
	}
	sim.send(t, "set_breakpoints", map[string]interface{}{"nodeIds": []string{"login"}})
	sim.send(t, "confirm_real_calls", nil)
	sim.send(t, "start", nil)

	paused, before := readUntil(t, sim.conn, "paused")
	p := decode[pausedFrame](t, paused)
	if p.NodeID != "login" || p.Reason != "breakpoint" || p.StepIndex != 1 || p.State == nil {
		t.Fatalf("paused = %+v", p)
	}
	for _, s := range stepsIn(before) {
		if s.NodeID == "login" {
			t.Fatal("the breakpoint node must not run before the pause")
		}
	}
	if api.hits.Load() != 0 {
		t.Fatal("paused before the node means no request yet")
	}

	sim.send(t, "step", nil)
	next, frames := readUntil(t, sim.conn, "paused")
	if steps := stepsIn(frames); len(steps) != 1 || steps[0].NodeID != "login" {
		t.Fatalf("step must run exactly one node, got %+v", steps)
	}
	if n := decode[pausedFrame](t, next); n.NodeID != "ok" || n.Reason != "step" || n.StepIndex != 2 {
		t.Fatalf("paused = %+v", n)
	}

	sim.send(t, "continue", nil)
	readUntil(t, sim.conn, "run_completed")
}

func TestRunUntilPausesOnceAtTheChosenNode(t *testing.T) {
	api := newCountingAPI(t)
	sim := startDebugSimulation(t, debugSimOptions{workflow: debugWorkflow(api.server.URL, nil), awaitStart: true})
	readUntil(t, sim.conn, "sim_ready")
	sim.send(t, "confirm_real_calls", nil)
	sim.send(t, "run_until", map[string]interface{}{"nodeId": "ok"})
	sim.send(t, "start", nil)

	paused, _ := readUntil(t, sim.conn, "paused")
	if p := decode[pausedFrame](t, paused); p.NodeID != "ok" || p.Reason != "run_until" {
		t.Fatalf("paused = %+v", p)
	}
	sim.send(t, "continue", nil)
	readUntil(t, sim.conn, "run_completed")
}

func TestCancellingWhilePausedEndsTheRun(t *testing.T) {
	api := newCountingAPI(t)
	sim := startDebugSimulation(t, debugSimOptions{workflow: debugWorkflow(api.server.URL, nil), awaitStart: true})
	readUntil(t, sim.conn, "sim_ready")
	sim.send(t, "set_breakpoints", map[string]interface{}{"nodeIds": []string{"login"}})
	sim.send(t, "start", nil)
	readUntil(t, sim.conn, "paused")
	sim.send(t, "cancel", nil)

	cancelled, _ := readUntil(t, sim.conn, "run_cancelled")
	if reason := decode[map[string]string](t, cancelled)["reason"]; reason != "cancelled" {
		t.Fatalf("reason = %q", reason)
	}
}

func TestAHandshakeNobodyStartsEndsWithStartTimeout(t *testing.T) {
	api := newCountingAPI(t)
	sim := startDebugSimulation(t, debugSimOptions{workflow: debugWorkflow(api.server.URL, nil), awaitStart: true, idle: 50 * time.Millisecond})
	readUntil(t, sim.conn, "sim_ready")

	cancelled, _ := readUntil(t, sim.conn, "run_cancelled")
	if reason := decode[map[string]string](t, cancelled)["reason"]; reason != "start_timeout" {
		t.Fatalf("reason = %q, want start_timeout", reason)
	}
	if api.hits.Load() != 0 {
		t.Fatal("a session that never started must not run anything")
	}
}

func TestAnAbandonedPauseEndsWithPausedTimeout(t *testing.T) {
	api := newCountingAPI(t)
	sim := startDebugSimulation(t, debugSimOptions{workflow: debugWorkflow(api.server.URL, nil), awaitStart: true, idle: 50 * time.Millisecond})
	readUntil(t, sim.conn, "sim_ready")
	sim.send(t, "set_breakpoints", map[string]interface{}{"nodeIds": []string{"login"}})
	sim.send(t, "start", nil)
	readUntil(t, sim.conn, "paused")

	cancelled, _ := readUntil(t, sim.conn, "run_cancelled")
	if reason := decode[map[string]string](t, cancelled)["reason"]; reason != "paused_timeout" {
		t.Fatalf("reason = %q, want paused_timeout", reason)
	}
	if api.hits.Load() != 0 {
		t.Fatal("an abandoned pause must not run the node")
	}
}

func recordedRun(triggerData map[string]interface{}) *workflow.RunDetail {
	return &workflow.RunDetail{
		Run: &workflow.WorkflowRun{ID: "run-9", WorkflowID: "wf-1", WorkspaceID: "ws1", Status: workflow.RunStatusError, TriggerData: triggerData},
		Logs: []*workflow.WorkflowRunLog{
			{NodeID: "t", NodeType: workflow.NodeTypeTriggerWebhook, Status: workflow.LogStatusExecuted},
			{NodeID: "login", NodeType: workflow.NodeTypeActionHTTPRequest, Status: workflow.LogStatusExecuted, Output: map[string]interface{}{"status_code": 401.0}, Branch: "falha"},
			{NodeID: "bad", NodeType: workflow.NodeTypeActionSetVariable, Status: workflow.LogStatusExecuted},
		},
	}
}

func TestReproducingARunReplaysItsRecordedCallsStepByStepOnTheDraft(t *testing.T) {
	api := newCountingAPI(t)
	runs := &runDetailStub{detail: recordedRun(map[string]interface{}{"cpf_final": "25"})}
	sim := startDebugSimulation(t, debugSimOptions{workflow: debugWorkflow(api.server.URL, nil), awaitStart: true, runs: runs})
	readUntil(t, sim.conn, "sim_ready")
	sim.send(t, "reproduce_run", map[string]interface{}{"runId": "run-9"})

	started, _ := readUntil(t, sim.conn, "sim_started")
	s := decode[startedFrame](t, started)
	if s.Reproducing == nil || s.Reproducing.RunID != "run-9" || len(s.Reproducing.PinnedNodes) != 1 || s.Reproducing.PinnedNodes[0] != "login" {
		t.Fatalf("sim_started = %+v, want the reproduced run and its pinned nodes", s)
	}
	if len(s.RealCallNodes) != 0 {
		t.Fatalf("realCallNodes = %v, the recorded call is pinned", s.RealCallNodes)
	}
	if runs.viewer.UserID != "u-1" {
		t.Fatal("the run must be read as the person, so the visibility rule applies")
	}
	if runs.fullReads != 0 || runs.pick == nil || !runs.pick.Trigger || !slices.Contains(runs.pick.LatestOfNodes, "login") || slices.Contains(runs.pick.LatestOfNodes, "bad") {
		t.Fatalf("pick = %+v, full reads = %d; a reproduction reads only the trigger and the latest step of each replayed node", runs.pick, runs.fullReads)
	}

	paused, _ := readUntil(t, sim.conn, "paused")
	p := decode[pausedFrame](t, paused)
	if p.StepIndex != 0 || p.State["cpf_final"] != "25" {
		t.Fatalf("paused = %+v, want a stop at the first step with the run's trigger data", p)
	}
	sim.send(t, "continue", nil)
	_, frames := readUntil(t, sim.conn, "run_completed")
	steps := stepsIn(frames)
	var login *streamedStep
	for i := range steps {
		if steps[i].NodeID == "login" {
			login = &steps[i]
		}
	}
	if login == nil || !login.Pinned || login.Branch != "falha" || api.hits.Load() != 0 {
		t.Fatalf("login = %+v hits = %d, want the recorded answer without a real call", login, api.hits.Load())
	}
}

func TestReproductionIsRefusedClearlyAndTheSessionStaysOpen(t *testing.T) {
	cases := []struct {
		name        string
		runs        *runDetailStub
		permissions permissionStub
		runID       string
		want        string
	}{
		{"no workflows read", &runDetailStub{detail: recordedRun(map[string]interface{}{"a": "b"})}, permissionStub{allowed: map[string]bool{}}, "run-9", "permissão"},
		{"run not visible", &runDetailStub{err: workflow.ErrRunNotFound}, permissionStub{}, "run-9", "não encontrada"},
		{"run of another workflow", &runDetailStub{detail: func() *workflow.RunDetail {
			d := recordedRun(map[string]interface{}{"a": "b"})
			d.Run.WorkflowID = "wf-2"
			return d
		}()}, permissionStub{}, "run-9", "outro fluxo"},
		{"older run without trigger data", &runDetailStub{detail: recordedRun(nil)}, permissionStub{}, "run-9", "dados do gatilho"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api := newCountingAPI(t)
			sim := startDebugSimulation(t, debugSimOptions{workflow: debugWorkflow(api.server.URL, nil), awaitStart: true, runs: tc.runs, permissions: tc.permissions})
			readUntil(t, sim.conn, "sim_ready")
			sim.send(t, "reproduce_run", map[string]interface{}{"runId": tc.runID})
			refused, _ := readUntil(t, sim.conn, "command_refused")
			got := decode[map[string]string](t, refused)
			if got["command"] != "reproduce_run" || !strings.Contains(got["error"], tc.want) {
				t.Fatalf("refusal = %v, want it to mention %q", got, tc.want)
			}
			sim.send(t, "cancel", nil)
			readUntil(t, sim.conn, "run_cancelled")
		})
	}
}
