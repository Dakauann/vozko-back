package copilot_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"vozko/domain/ai"
	"vozko/domain/copilot"
)

func chartTool() *fakeTool {
	return &fakeTool{name: "render_chart", meta: readMeta, result: copilot.Result{
		Status: copilot.StatusOK,
		Data:   map[string]interface{}{"rendered": "bar"},
		Chart:  &copilot.Chart{Type: copilot.ChartBar, Title: "Volume", Categories: []string{"a"}},
	}}
}

func TestService_ChartIsStreamedAndKeptWithTheAnswer(t *testing.T) {
	th, ms := &fakeThreads{thread: testThread()}, &fakeMessages{}
	prov := &scriptAI{turns: [][]ai.ToolCall{{call("render_chart", nil)}, {}}, texts: []string{"", "Segue o gráfico."}}
	var charts []interface{}
	emit := func(tp string, payload interface{}) {
		if tp == "chart" {
			charts = append(charts, payload)
		}
	}
	if err := newService(prov, th, ms, chartTool()).Stream(context.Background(), th.thread, copilot.UserMessage{Content: "gráfico"}, ownerCtx, emit); err != nil {
		t.Fatal(err)
	}
	if len(charts) != 1 {
		t.Fatalf("chart events = %d, want 1", len(charts))
	}
	var steps []toolStep
	if err := json.Unmarshal(ms.last().ToolCalls, &steps); err != nil {
		t.Fatal(err)
	}
	// Reopening the conversation must show the same chart, so it is stored with the tool step that drew it.
	if len(steps) != 1 || steps[0].Chart == nil || steps[0].Chart.Title != "Volume" {
		t.Fatalf("steps = %+v", steps)
	}
}

func TestService_EachTurnGetsItsOwnDatasets(t *testing.T) {
	th, ms := &fakeThreads{thread: testThread()}, &fakeMessages{}
	rt := &fakeTool{name: "read_x", meta: readMeta}
	prov := &scriptAI{turns: [][]ai.ToolCall{{call("read_x", nil)}, {}}, texts: []string{"", "ok"}}
	if err := newService(prov, th, ms, rt).Stream(context.Background(), th.thread, copilot.UserMessage{Content: "x"}, ownerCtx, func(string, interface{}) {}); err != nil {
		t.Fatal(err)
	}
	if rt.gotCC.Datasets == nil {
		t.Fatal("a tool must receive a dataset store for the turn")
	}
}

func TestSystemPromptDescribesTheScreen(t *testing.T) {
	cc := ownerCtx
	cc.View = copilot.View{Surface: copilot.SurfaceAttendance, DateFrom: "2026-09-01", DateTo: "2026-09-07", DepartmentID: "d1"}
	prompt := NewDriver(cc, "m", NewRegistry(), &fakeAccess{}, openFunds{}, nil).SystemPrompt()
	for _, want := range []string{"2026-09-01", "2026-09-07", "d1", "Atendimento"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt does not mention %q", want)
		}
	}
	bare := NewDriver(ownerCtx, "m", NewRegistry(), &fakeAccess{}, openFunds{}, nil).SystemPrompt()
	if strings.Contains(bare, "# Tela atual") {
		t.Fatal("a chat opened outside a page must not claim a screen")
	}
}

type orderedEvents struct{ names []string }

func (o *orderedEvents) emit(t string, payload interface{}) {
	if t == EventToolStart || t == "tool" {
		o.names = append(o.names, t)
	}
}

type slowTool struct {
	fakeTool
	seenStart *bool
	events    *orderedEvents
}

func (s *slowTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	*s.seenStart = len(s.events.names) == 1 && s.events.names[0] == EventToolStart
	return s.fakeTool.Execute(ctx, cc, args)
}

func TestDriver_AnnouncesAToolBeforeItRuns(t *testing.T) {
	events := &orderedEvents{}
	started := false
	tool := &slowTool{fakeTool: fakeTool{name: "read_x", meta: readMeta}, seenStart: &started, events: events}
	drv := NewDriver(ownerCtx, "m", NewRegistry(tool), &fakeAccess{}, openFunds{}, nil)
	drv.Dispatch(context.Background(), call("read_x", nil), events.emit)
	// A query can wait for the analytics gate; without the start event the answer looks frozen until it returns.
	if !started || len(events.names) != 2 || events.names[1] != "tool" {
		t.Fatalf("events = %v, started before execute = %v", events.names, started)
	}
}

func TestDriver_DoesNotAnnounceWhatItWillNotRun(t *testing.T) {
	events := &orderedEvents{}
	denied := NewDriver(ownerCtx, "m", NewRegistry(&fakeTool{name: "read_x", meta: readMeta}), &fakeAccess{err: errors.New("no")}, openFunds{}, nil)
	denied.Dispatch(context.Background(), call("read_x", nil), events.emit)
	proposal := NewDriver(ownerCtx, "m", NewRegistry(&fakeTool{name: "write_x", meta: writeMeta}), &fakeAccess{}, openFunds{}, nil)
	proposal.Dispatch(context.Background(), call("write_x", nil), events.emit)
	for _, name := range events.names {
		if name == EventToolStart {
			t.Fatalf("events = %v: a denied call or an approval proposal never runs, so it must not show as running", events.names)
		}
	}
}

func TestDriver_SystemAdminPassesToolChecksLikeTheRoutes(t *testing.T) {
	notMember := &fakeAccess{err: errors.New("workspace: unauthorized")}
	cc := ownerCtx
	cc.SystemAdmin = true
	read := &fakeTool{name: "read_x", meta: readMeta}
	drv := NewDriver(cc, "m", NewRegistry(read), notMember, openFunds{}, nil)
	// The routes let a platform admin into any workspace (workspace_middleware RequireAccess); the tools must agree,
	// or support staff see "no permission" on a workspace they were let into.
	if res := drv.Dispatch(context.Background(), call("read_x", nil), func(string, interface{}) {}); read.calls != 1 {
		t.Fatalf("result = %q, want the tool to run for a system admin", res.Result)
	}

	write := &fakeTool{name: "write_x", meta: writeMeta}
	drv = NewDriver(cc, "m", NewRegistry(write), notMember, openFunds{}, nil)
	step := drv.Dispatch(context.Background(), call("write_x", nil), func(string, interface{}) {})
	if step.Pause == nil || write.calls != 0 {
		t.Fatal("a system admin still approves every change; the bypass is about access, not about approval")
	}
}

func TestDriver_NonAdminsStillNeedTheirPermission(t *testing.T) {
	read := &fakeTool{name: "read_x", meta: readMeta}
	drv := NewDriver(ownerCtx, "m", NewRegistry(read), &fakeAccess{err: errors.New("no")}, openFunds{}, nil)
	drv.Dispatch(context.Background(), call("read_x", nil), func(string, interface{}) {})
	if read.calls != 0 {
		t.Fatal("without the system admin flag the permission check must still refuse")
	}
}

type describedTool struct{ fakeTool }

func (describedTool) Describe(context.Context, copilot.Context, map[string]interface{}) []copilot.Field {
	return []copilot.Field{{Key: "stage", Value: "Negociação"}}
}

func TestDriver_ProposalsCarryReadableFields(t *testing.T) {
	var proposal copilot.PendingAction
	capture := func(eventType string, payload interface{}) {
		if eventType == "tool_proposal" {
			proposal = payload.(copilot.PendingAction)
		}
	}
	plain := &fakeTool{name: "write_x", meta: writeMeta}
	NewDriver(ownerCtx, "m", NewRegistry(plain), &fakeAccess{}, openFunds{}, nil).
		Dispatch(context.Background(), call("write_x", map[string]interface{}{"name": "Bia", "id": "5f0c7c1e-1d2a-4b8e-9d11-3a2b1c0d9e8f"}), capture)
	if len(proposal.Fields) != 1 || proposal.Fields[0] != (copilot.Field{Key: "name", Value: "Bia"}) {
		t.Fatalf("fields = %+v, want the plain arguments without the id", proposal.Fields)
	}

	described := &describedTool{fakeTool{name: "move_x", meta: writeMeta}}
	NewDriver(ownerCtx, "m", NewRegistry(described), &fakeAccess{}, openFunds{}, nil).
		Dispatch(context.Background(), call("move_x", map[string]interface{}{"stage_id": "s1"}), capture)
	if len(proposal.Fields) != 1 || proposal.Fields[0].Value != "Negociação" {
		t.Fatalf("fields = %+v, want the tool's own description", proposal.Fields)
	}
}
