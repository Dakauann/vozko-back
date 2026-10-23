package crmtools

import (
	"context"
	"testing"
	"time"

	"vozko/domain/copilot"
	"vozko/domain/googlesheet"
	"vozko/domain/shared"
	gsuc "vozko/usecases/googlesheet"
)

type eloSheets struct {
	sheet   *googlesheet.Sheet
	created gsuc.CreateInput
	queued  []gsuc.EnqueueInput
}

func (e *eloSheets) List(context.Context, googlesheet.ListInput) (*shared.PaginatedResult[*googlesheet.Sheet], error) {
	return &shared.PaginatedResult[*googlesheet.Sheet]{Items: []*googlesheet.Sheet{e.sheet}, TotalItems: 1}, nil
}

func (e *eloSheets) Create(_ context.Context, in gsuc.CreateInput) (*googlesheet.Sheet, error) {
	e.created = in
	return e.sheet, nil
}

func (e *eloSheets) Enqueue(_ context.Context, in gsuc.EnqueueInput) (*gsuc.EnqueueResult, error) {
	e.queued = append(e.queued, in)
	return &gsuc.EnqueueResult{RowID: "row-1", Sheet: e.sheet}, nil
}

func (e *eloSheets) Preview(_ context.Context, _, _ string, values map[string]any, _ googlesheet.Origin) (*googlesheet.Sheet, googlesheet.Cells, error) {
	cells, err := e.sheet.Accept(values, nil, time.Now())
	return e.sheet, cells, err
}

func eloSheet(t *testing.T) *eloSheets {
	t.Helper()
	s, err := googlesheet.NewSheet(googlesheet.NewSheetInput{
		WorkspaceID: "ws-1", AccountID: "a", Title: "Leads", TabName: "Leads",
		Columns: []googlesheet.ColumnInput{{Header: "Nome", Type: googlesheet.TypeText, Required: true}, {Header: "Interesse", Type: googlesheet.TypeChoice, Options: []string{"Alto", "Baixo"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	s.ID = "sheet-1"
	return &eloSheets{sheet: s}
}

var eloContext = copilot.Context{WorkspaceID: "ws-1", UserID: "u-1", ProposalID: "proposal-7"}

func TestListGoogleSheetsShowsColumnKeysForTheModel(t *testing.T) {
	res := NewListGoogleSheetsTool(eloSheet(t)).Execute(context.Background(), eloContext, map[string]interface{}{})
	sheets := res.Data.(map[string]interface{})["sheets"].([]map[string]interface{})
	columns := sheets[0]["columns"].([]map[string]interface{})
	if res.Status != copilot.StatusOK || columns[0]["key"] != "nome" || columns[1]["options"] == nil {
		t.Fatalf("result = %+v", res)
	}
}

func TestCreateGoogleSheetRefusesABadDraftBeforeApproval(t *testing.T) {
	tool := NewCreateGoogleSheetTool(eloSheet(t)).(copilot.Validator)
	bad := map[string]interface{}{"title": "Leads", "columns": []interface{}{map[string]interface{}{"header": "Status", "type": "choice"}}}
	if err := tool.Validate(context.Background(), eloContext, bad); err == nil {
		t.Fatal("a choice column without options reached the approval card")
	}
	good := map[string]interface{}{"title": "Leads", "columns": []interface{}{map[string]interface{}{"header": "Nome", "type": "text"}}}
	if err := tool.Validate(context.Background(), eloContext, good); err != nil {
		t.Fatalf("a valid draft was refused: %v", err)
	}
}

func TestAddGoogleSheetRowValidatesThenQueuesOncePerProposal(t *testing.T) {
	sheets := eloSheet(t)
	tool := NewAddGoogleSheetRowTool(sheets)
	bad := map[string]interface{}{"sheet_id": "sheet-1", "values": []interface{}{map[string]interface{}{"column": "interesse", "value": "Médio"}}}
	if err := tool.(copilot.Validator).Validate(context.Background(), eloContext, bad); err == nil {
		t.Fatal("a row the sheet refuses reached the approval card")
	}
	good := map[string]interface{}{"sheet_id": "sheet-1", "values": []interface{}{map[string]interface{}{"column": "nome", "value": "Ana"}}}
	res := tool.Execute(context.Background(), eloContext, good)
	if res.Status != copilot.StatusOK || len(sheets.queued) != 1 {
		t.Fatalf("result = %+v", res)
	}
	in := sheets.queued[0]
	if in.IdempotencyKey != googlesheet.KeyForCopilot("proposal-7") || in.Origin.Kind != googlesheet.OriginCopilot || in.Values["nome"] != "Ana" {
		t.Fatalf("input = %+v", in)
	}
}
