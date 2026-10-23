package node_executors

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vozko/domain/googlesheet"
	"vozko/domain/workflow"
	gsuc "vozko/usecases/googlesheet"
)

type sheetSink struct {
	got []gsuc.EnqueueInput
	err error
}

func (s *sheetSink) Enqueue(_ context.Context, in gsuc.EnqueueInput) (*gsuc.EnqueueResult, error) {
	s.got = append(s.got, in)
	if s.err != nil {
		return nil, s.err
	}
	sheet := &googlesheet.Sheet{ID: in.SheetID, Title: "Pedidos", SpreadsheetID: "sp-1"}
	return &gsuc.EnqueueResult{RowID: "row-1", Sheet: sheet}, nil
}

func sheetNodeContext(values map[string]interface{}) *workflow.NodeContext {
	state := workflow.NewRunState()
	state.Set("produto", "Pizza")
	node := &workflow.Node{ID: "sheet-node", Type: workflow.NodeTypeActionAddSheetRow, Config: map[string]interface{}{"sheet_id": "sheet-1", "values": values}}
	graph := &workflow.Graph{
		Nodes: []workflow.Node{*node, {ID: "ok"}, {ID: "bad"}},
		Edges: []workflow.Edge{{Source: "sheet-node", Target: "ok", Label: "sucesso"}, {Source: "sheet-node", Target: "bad", Label: "erro"}},
	}
	run := &workflow.WorkflowRun{ID: "run-1", WorkflowID: "wf-1", WorkspaceID: "ws-1", EntryID: "entry-1", EntryType: "whatsapp", CurrentNodeID: "sheet-node", State: state}
	return &workflow.NodeContext{Run: run, Node: node, Graph: graph, Workflow: &workflow.Workflow{ID: "wf-1", WorkspaceID: "ws-1"}, State: &run.State}
}

func TestAddSheetRowNodeEnqueuesTheInterpolatedRow(t *testing.T) {
	sink := &sheetSink{}
	result, err := NewAddSheetRowExecutor(sink).Execute(sheetNodeContext(map[string]interface{}{"produto": "{{var.produto}}", "quantidade": " 2 "}))
	if err != nil || result.NextNodeID != "ok" {
		t.Fatalf("result = %+v, %v", result, err)
	}
	in := sink.got[0]
	if in.Values["produto"] != "Pizza" || in.Values["quantidade"] != "2" || in.SheetID != "sheet-1" {
		t.Fatalf("input = %+v", in)
	}
	if in.Origin.Kind != googlesheet.OriginWorkflow || in.Origin.RunID != "run-1" || in.Origin.EntryID != "entry-1" {
		t.Fatalf("origin = %+v", in.Origin)
	}
	if in.IdempotencyKey != googlesheet.KeyForNode("run-1", "sheet-node#0") {
		t.Fatalf("key = %q", in.IdempotencyKey)
	}
	if result.Output["row_id"] != "row-1" || result.Output["sheet_title"] != "Pedidos" {
		t.Fatalf("output = %+v", result.Output)
	}
}

func TestAddSheetRowNodeNeverWritesAnUnresolvedPlaceholder(t *testing.T) {
	sink := &sheetSink{}
	result, err := NewAddSheetRowExecutor(sink).Execute(sheetNodeContext(map[string]interface{}{"produto": "{{var.inexistente}}"}))
	if err != nil || result.NextNodeID != "bad" || len(sink.got) != 0 {
		t.Fatalf("result = %+v, %v, enqueued %d", result, err, len(sink.got))
	}
	if result.Output["error_code"] != googlesheet.CodeUnresolvedVariable || !strings.Contains(result.Output["error"].(string), "var.inexistente") {
		t.Fatalf("output = %+v", result.Output)
	}
}

func TestAddSheetRowNodeRoutesRefusalsAndRetriesInfrastructure(t *testing.T) {
	refused := &sheetSink{err: &googlesheet.ValueError{Column: "produto", Header: "Produto", Reason: googlesheet.ErrRequiredColumnMissing}}
	result, err := NewAddSheetRowExecutor(refused).Execute(sheetNodeContext(map[string]interface{}{"produto": ""}))
	if err != nil || result.NextNodeID != "bad" || result.Output["error_code"] != googlesheet.CodeRequiredColumnMissing {
		t.Fatalf("refusal = %+v, %v", result, err)
	}
	down := &sheetSink{err: errors.New("db down")}
	if _, err := NewAddSheetRowExecutor(down).Execute(sheetNodeContext(map[string]interface{}{"produto": "x"})); err == nil {
		t.Fatal("a database failure must be retried by the engine, not routed as a refusal")
	}
}

func TestAddSheetRowNodeDefinitionIsAnExternalCall(t *testing.T) {
	def := NewAddSheetRowExecutor(&sheetSink{}).Definition()
	if def.TestBehavior != workflow.NodeTestExternalCall || def.Type != workflow.NodeTypeActionAddSheetRow {
		t.Fatalf("definition = %+v", def)
	}
	if err := workflow.TestBehaviorConsistent(def); err != nil {
		t.Fatal(err)
	}
	fields := map[string]workflow.ConfigField{}
	for _, f := range def.ConfigSchema {
		fields[f.Key] = f
	}
	if fields["sheet_id"].OptionsSource != "google_sheets" || fields["values"].Type != "sheet_columns" {
		t.Fatalf("schema = %+v", def.ConfigSchema)
	}
}
