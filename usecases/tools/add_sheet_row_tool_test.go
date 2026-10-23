package tools_usecase

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"vozko/domain/googlesheet"
	"vozko/domain/tools"
	gsuc "vozko/usecases/googlesheet"
)

type sheetRowsStub struct {
	sheet    *googlesheet.Sheet
	findErr  error
	enqueued []gsuc.EnqueueInput
	enqErr   error
	previews int
}

func (s *sheetRowsStub) Get(_ context.Context, workspaceID, id string) (*googlesheet.Sheet, error) {
	if s.findErr != nil || s.sheet.WorkspaceID != workspaceID || s.sheet.ID != id {
		return nil, googlesheet.ErrSheetNotFound
	}
	return s.sheet, nil
}

func (s *sheetRowsStub) Enqueue(_ context.Context, in gsuc.EnqueueInput) (*gsuc.EnqueueResult, error) {
	s.enqueued = append(s.enqueued, in)
	if s.enqErr != nil {
		return nil, s.enqErr
	}
	return &gsuc.EnqueueResult{RowID: "row-1", Sheet: s.sheet}, nil
}

func (s *sheetRowsStub) Preview(_ context.Context, _, _ string, values map[string]any, _ googlesheet.Origin) (*googlesheet.Sheet, googlesheet.Cells, error) {
	s.previews++
	cells, err := s.sheet.Accept(values, googlesheet.SystemValues{}, testNow)
	return s.sheet, cells, err
}

func sheetForTool(t *testing.T) *googlesheet.Sheet {
	t.Helper()
	s, err := googlesheet.NewSheet(googlesheet.NewSheetInput{
		WorkspaceID: "ws-1", AccountID: "acct", Title: "Pedidos", TabName: "Pedidos", Description: "Pedidos feitos pelo WhatsApp",
		Columns: []googlesheet.ColumnInput{
			{Header: "Produto", Type: googlesheet.TypeText, Required: true, Instruction: "Nome do produto pedido"},
			{Header: "Quantidade", Type: googlesheet.TypeNumber},
			{Header: "Entrega", Type: googlesheet.TypeChoice, Options: []string{"Retirada", "Delivery"}},
			{Header: "Data", Type: googlesheet.TypeDate},
			{Header: "Telefone", Type: googlesheet.TypePhone, Source: googlesheet.SourceLeadPhone},
			{Header: "Antiga", Type: googlesheet.TypeText},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EditColumns(googlesheet.ColumnsEdit{Update: []googlesheet.ColumnUpdate{{Key: "antiga", Archived: ptrBool(true)}}}); err != nil {
		t.Fatal(err)
	}
	s.ID = "sheet-1"
	return s
}

func ptrBool(b bool) *bool { return &b }

var testNow = time.Date(2026, 10, 21, 15, 0, 0, 0, time.UTC)

func toolContext(sheetID string) tools.ToolContext {
	return tools.ToolContext{WorkspaceID: "ws-1", Config: map[string]interface{}{addSheetRowConfigKey: sheetID}}
}

func TestAddSheetRowIsOneToolPerSheet(t *testing.T) {
	def := NewAddSheetRowTool(&sheetRowsStub{sheet: sheetForTool(t)}).Definition()
	if def.BindingKey != addSheetRowConfigKey || def.ConfigSchema[addSheetRowConfigKey].OptionsSource != GoogleSheetsOptionsSource || !def.RequiresConfig {
		t.Fatalf("definition = %+v", def)
	}
	if got := def.ToolNameFor(map[string]interface{}{addSheetRowConfigKey: "sheet-1"}); got != "add_sheet_row_sheet-1" {
		t.Fatalf("bound name = %q", got)
	}
}

func TestAddSheetRowExposesOnlyInputColumns(t *testing.T) {
	handler := NewAddSheetRowTool(&sheetRowsStub{sheet: sheetForTool(t)}).(tools.ContextualHandler)
	def := handler.DefinitionWithContext(toolContext("sheet-1"))
	for _, hidden := range []string{"telefone", "antiga"} {
		if _, ok := def.Parameters[hidden]; ok {
			t.Fatalf("%s must not be filled by the model", hidden)
		}
	}
	if def.Parameters["entrega"].Enum == nil || def.Parameters["quantidade"].Type != "number" || def.Parameters["data"].Type != "date" {
		t.Fatalf("parameters = %+v", def.Parameters)
	}
	if !strings.Contains(def.Parameters["produto"].Description, "Nome do produto pedido") {
		t.Fatalf("instruction missing: %q", def.Parameters["produto"].Description)
	}
	if len(def.Required) != 1 || def.Required[0] != "produto" {
		t.Fatalf("required = %v", def.Required)
	}
	if !strings.Contains(def.Description, "Pedidos") {
		t.Fatalf("description = %q", def.Description)
	}
	if issues := tools.SchemaIssues(def.JSONSchema()); len(issues) > 0 {
		t.Fatalf("schema issues: %v", issues)
	}
}

func TestAddSheetRowIsUnavailableForAnotherWorkspacesSheet(t *testing.T) {
	handler := NewAddSheetRowTool(&sheetRowsStub{sheet: sheetForTool(t)}).(tools.ContextualHandler)
	ctx := toolContext("sheet-1")
	ctx.WorkspaceID = "ws-2"
	def := handler.DefinitionWithContext(ctx)
	if len(def.Parameters) != 0 || !strings.Contains(def.Description, "indisponível") {
		t.Fatalf("definition = %+v", def)
	}
}

func agentConfig() map[string]interface{} {
	return map[string]interface{}{
		addSheetRowConfigKey: "sheet-1", "__workspace_id": "ws-1", "__entry_id": "entry-1",
		"__entry_type": "whatsapp", "__lead_id": "lead-1", "__agent_id": "agent-1",
	}
}

func TestAddSheetRowEnqueuesWithTheCallAsIdempotencyKey(t *testing.T) {
	stub := &sheetRowsStub{sheet: sheetForTool(t)}
	ctx := tools.WithCallID(context.Background(), "call-9")
	result, err := NewAddSheetRowTool(stub).ExecuteWithConfig(ctx, agentConfig(), map[string]interface{}{"produto": "Pizza"})
	if err != nil || result.IsError {
		t.Fatalf("result = %+v, %v", result, err)
	}
	in := stub.enqueued[0]
	if in.SheetID != "sheet-1" || in.Origin.Kind != googlesheet.OriginAgent || in.Origin.LeadID != "lead-1" || in.Origin.EntryID != "entry-1" {
		t.Fatalf("input = %+v", in)
	}
	if !strings.HasPrefix(in.IdempotencyKey, "ai:entry-1:call-9:") {
		t.Fatalf("key = %q", in.IdempotencyKey)
	}
}

func TestAddSheetRowExplainsRefusalsToTheModel(t *testing.T) {
	stub := &sheetRowsStub{sheet: sheetForTool(t), enqErr: &googlesheet.ValueError{Column: "entrega", Header: "Entrega", Reason: googlesheet.ErrValueNotInOptions, Detail: "opções: Retirada, Delivery"}}
	result, err := NewAddSheetRowTool(stub).ExecuteWithConfig(context.Background(), agentConfig(), map[string]interface{}{"produto": "Pizza", "entrega": "Moto"})
	if err != nil || !result.IsError || result.ErrorCode != googlesheet.CodeValueNotInOptions {
		t.Fatalf("result = %+v, %v", result, err)
	}
	text, _ := result.Result.(string)
	if !strings.Contains(text, "Entrega") || !strings.Contains(text, "Retirada, Delivery") {
		t.Fatalf("refusal = %q", text)
	}
}

func TestAddSheetRowRefusesOutsideAConversation(t *testing.T) {
	stub := &sheetRowsStub{sheet: sheetForTool(t)}
	result, _ := NewAddSheetRowTool(stub).ExecuteWithConfig(context.Background(), map[string]interface{}{addSheetRowConfigKey: "sheet-1"}, map[string]interface{}{"produto": "x"})
	if !result.IsError || len(stub.enqueued) != 0 {
		t.Fatalf("result = %+v", result)
	}
}

func TestAddSheetRowPreviewValidatesWithoutWriting(t *testing.T) {
	stub := &sheetRowsStub{sheet: sheetForTool(t)}
	previewer := NewAddSheetRowTool(stub).(tools.Previewer)
	ok, _ := previewer.Preview(context.Background(), agentConfig(), map[string]interface{}{"produto": "Pizza", "quantidade": "2"})
	bad, _ := previewer.Preview(context.Background(), agentConfig(), map[string]interface{}{"quantidade": "2"})
	if ok.IsError || !bad.IsError || len(stub.enqueued) != 0 || stub.previews != 2 {
		t.Fatalf("ok = %+v, bad = %+v, enqueued %d", ok, bad, len(stub.enqueued))
	}
	var shown map[string]any
	if err := json.Unmarshal([]byte(ok.Result.(string)), &shown); err != nil || shown["simulacao"] != true {
		t.Fatalf("preview result = %v (%v)", ok.Result, err)
	}
}

func TestSimulationPreviewsToolsThatCanPreview(t *testing.T) {
	stub := &sheetRowsStub{sheet: sheetForTool(t)}
	sim := NewSimulatedToolService(NewService(NewAddSheetRowTool(stub)))
	if _, err := sim.ExecuteWithConfig(context.Background(), "add_sheet_row_sheet-1", agentConfig(), map[string]interface{}{"produto": "Pizza"}); err != nil {
		t.Fatal(err)
	}
	if stub.previews != 1 || len(stub.enqueued) != 0 {
		t.Fatalf("previews %d, enqueued %d; a simulation must never write", stub.previews, len(stub.enqueued))
	}
}
