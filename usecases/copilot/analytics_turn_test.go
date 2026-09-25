package copilot_usecase

import (
	"context"
	"encoding/json"
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
	if err := newService(prov, th, ms, NewInMemoryPendingStore(), chartTool()).Stream(context.Background(), th.thread, "gráfico", ownerCtx, emit); err != nil {
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
	if err := newService(prov, th, ms, NewInMemoryPendingStore(), rt).Stream(context.Background(), th.thread, "x", ownerCtx, func(string, interface{}) {}); err != nil {
		t.Fatal(err)
	}
	if rt.gotCC.Datasets == nil {
		t.Fatal("a tool must receive a dataset store for the turn")
	}
}

func TestSystemPromptDescribesTheScreen(t *testing.T) {
	cc := ownerCtx
	cc.View = copilot.View{Surface: copilot.SurfaceAttendance, DateFrom: "2026-09-01", DateTo: "2026-09-07", DepartmentID: "d1"}
	prompt := NewDriver(cc, "m", NewRegistry(), &fakeAccess{}, nil).SystemPrompt()
	for _, want := range []string{"2026-09-01", "2026-09-07", "d1", "Atendimento"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt does not mention %q", want)
		}
	}
	bare := NewDriver(ownerCtx, "m", NewRegistry(), &fakeAccess{}, nil).SystemPrompt()
	if strings.Contains(bare, "# Tela atual") {
		t.Fatal("a chat opened outside a page must not claim a screen")
	}
}
