package workflow_usecase

import (
	"context"
	"errors"
	"strings"
	"testing"
	"vozko/domain/ai"
	"vozko/domain/googlesheet"
	"vozko/domain/shared"
	"vozko/domain/workflow"
	"vozko/usecases/workflow/node_executors"
)

type unavailableBuilderLinter struct{}

func (unavailableBuilderLinter) Lint(context.Context, *workflow.Workflow) (workflow.LintReport, error) {
	return workflow.LintReport{}, errors.New("unavailable")
}

func TestBuilderUsesPublishResourceValidationAndCannotFinishWithInvalidSheetMapping(t *testing.T) {
	sheet := &googlesheet.Sheet{ID: "sheet", WorkspaceID: "ws", Columns: []googlesheet.Column{{Key: "nome_do_cliente", Header: "Nome do cliente *", Source: googlesheet.SourceInput, Type: googlesheet.TypeText, Required: true}}}
	catalog := []workflow.NodeDefinition{node_executors.NewAddSheetRowExecutor(nil).Definition()}
	linter := &activateWorkflowUseCase{catalogFn: func() []workflow.NodeDefinition { return catalog }, googleSheets: sheetLookup{sheet}}
	st := &builderState{workspaceID: "ws", wfType: workflow.WorkflowTypeMessages, graph: &workflow.Graph{Nodes: []workflow.Node{{ID: "row", Type: workflow.NodeTypeActionAddSheetRow, Config: map[string]interface{}{"sheet_id": "sheet", "values": map[string]interface{}{"nome do cliente": "Dakauann"}}}}}}
	uc := &aiBuilderUC{deps: AIBuilderUseCaseDeps{Linter: linter}}
	uc.relint(st)
	found := false
	for _, issue := range st.lastReport.Blocking() {
		if issue.Code == workflow.LintSheetMappingInvalid {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing Sheet blocker: %+v", st.lastReport)
	}
	driver := &builderDriver{uc: uc, st: st}
	if driver.FinishVerdict(ai.ToolCall{}).Honored {
		t.Fatal("accepted an invalid mapping")
	}
}

func TestBuilderDoesNotValidateWhenResourceValidationIsUnavailable(t *testing.T) {
	for _, linter := range []workflow.WorkflowLinter{nil, unavailableBuilderLinter{}} {
		uc := &aiBuilderUC{deps: AIBuilderUseCaseDeps{Linter: linter}}
		st := &builderState{graph: &workflow.Graph{}}
		uc.relint(st)
		if st.lastReport.IsGreen() {
			t.Fatal("unavailable validation was accepted")
		}
	}
}

type builderSheetLister struct{ sheet *googlesheet.Sheet }

func (s builderSheetLister) List(context.Context, googlesheet.ListInput) (*shared.PaginatedResult[*googlesheet.Sheet], error) {
	return &shared.PaginatedResult[*googlesheet.Sheet]{Items: []*googlesheet.Sheet{s.sheet}}, nil
}

func TestBuilderSheetSearchIncludesExactInputColumnKeys(t *testing.T) {
	sheet := &googlesheet.Sheet{ID: "sheet", WorkspaceID: "ws", Title: "Consultas", Columns: []googlesheet.Column{
		{Key: "nome_do_cliente", Header: "Nome do cliente *", Source: googlesheet.SourceInput, Required: true},
		{Key: "archived", Source: googlesheet.SourceInput, Archived: true},
		{Key: "automatic", Source: googlesheet.SourceWrittenAt},
	}}
	uc := &aiBuilderUC{deps: AIBuilderUseCaseDeps{ResourceResolver: NewBuilderResourceResolver(BuilderResourceResolverDeps{Sheets: builderSheetLister{sheet}})}}
	st := &builderState{workspaceID: "ws", searchCache: map[string]string{}}
	result := uc.handleFindResource(context.Background(), func(string, interface{}) {}, st, ai.ToolCall{Arguments: map[string]interface{}{"kind": "google_sheets"}})
	if !strings.Contains(result, `"key":"nome_do_cliente"`) || !strings.Contains(result, `"required":true`) || strings.Contains(result, `"key":"archived"`) || strings.Contains(result, `"key":"automatic"`) {
		t.Fatalf("incomplete schema: %s", result)
	}
}
