package workflow_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
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
		if blocking := st.lastReport.Blocking(); len(blocking) != 1 || blocking[0].Code != workflow.LintValidationUnavailable {
			t.Fatalf("unavailable validation was accepted: %+v", st.lastReport)
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

func ownerSchedulingWorkflow(t *testing.T) *workflow.Workflow {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "agendamento_clinica.json"))
	if err != nil {
		t.Fatal(err)
	}
	var exported struct {
		Name  string                `json:"name"`
		Type  workflow.WorkflowType `json:"type"`
		Graph workflow.Graph        `json:"graph"`
	}
	if err := json.Unmarshal(raw, &exported); err != nil {
		t.Fatal(err)
	}
	return &workflow.Workflow{ID: "wf", WorkspaceID: "ws", Name: exported.Name, Type: exported.Type, Graph: exported.Graph}
}

func productionCatalog(t *testing.T) []workflow.NodeDefinition {
	t.Helper()
	registry := NewNodeExecutorRegistry()
	if err := RegisterProductionExecutors(registry, ExecutorDeps{ConversationEvents: silentLines{}, Forms: node_executors.NewFormSenderRelay(), ChoiceStats: &spyChoiceRecorder{}, MediaURLs: externalMediaURLs()}); err != nil {
		t.Fatal(err)
	}
	return registry.Catalog()
}

func TestTheOwnersSchedulingWorkflowIsBlockedTheSameWayByTheCopilotAndByPublish(t *testing.T) {
	sheet := &googlesheet.Sheet{ID: "c8d1bfb1-aef8-4625-81c9-15b4a31ea776", WorkspaceID: "ws", Title: "Agendamentos", Columns: []googlesheet.Column{
		{Key: "nome_do_cliente", Header: "Nome do cliente", Source: googlesheet.SourceInput, Type: googlesheet.TypeText, Required: true},
		{Key: "data_do_agendamento", Header: "Data do agendamento", Source: googlesheet.SourceInput, Type: googlesheet.TypeText, Required: true},
		{Key: "dentista_responsavel", Header: "Dentista responsavel", Source: googlesheet.SourceInput, Type: googlesheet.TypeText},
		{Key: "tipo_de_consulta", Header: "Tipo de consulta", Source: googlesheet.SourceInput, Type: googlesheet.TypeText},
	}}
	catalog := productionCatalog(t)
	repo := NewMockWorkflowRepository()
	linter := &activateWorkflowUseCase{repo: repo, catalogFn: func() []workflow.NodeDefinition { return catalog }, googleSheets: sheetLookup{sheet}, modelLookup: knownModels{}}
	owner := ownerSchedulingWorkflow(t)

	stored := *owner
	stored.Graph = owner.Graph.Clone()
	if err := repo.Create(&stored); err != nil {
		t.Fatal(err)
	}
	_, publishErr := linter.Publish(context.Background(), owner.ID)
	blocked, ok := workflow.NotPublishableOf(publishErr)
	if !ok {
		t.Fatalf("publish was not blocked: %v", publishErr)
	}

	graph := owner.Graph.Clone()
	st := &builderState{workspaceID: owner.WorkspaceID, workflowID: owner.ID, name: owner.Name, wfType: owner.Type, graph: &graph}
	uc := &aiBuilderUC{deps: AIBuilderUseCaseDeps{Linter: linter}}
	uc.relint(st)

	copilot, publish := issueCodes(st.lastReport.Issues), issueCodes(blocked.Issues)
	if !reflect.DeepEqual(copilot, publish) {
		t.Fatalf("copilot blockers %v differ from publish blockers %v", copilot, publish)
	}
	if !reflect.DeepEqual(copilot[workflow.LintSheetMappingInvalid], []string{"n5"}) || len(copilot[workflow.LintValidationUnavailable]) > 0 {
		t.Fatalf("the sheet mapping blocker is missing: %v", copilot)
	}
	for _, issue := range st.lastReport.Issues {
		if issue.Code == workflow.LintUnknownOutputKey || issue.Code == workflow.LintDanglingDataRef {
			t.Fatalf("the agent tool arguments were reported as missing variables: %+v", issue)
		}
	}
	if (&builderDriver{uc: uc, st: st}).FinishVerdict(ai.ToolCall{}).Honored {
		t.Fatal("the copilot finished while publish refuses the workflow")
	}
}

func TestASheetStepCannotPassValidationWithoutTheSheetLookup(t *testing.T) {
	catalog := []workflow.NodeDefinition{node_executors.NewAddSheetRowExecutor(nil).Definition()}
	linter := &activateWorkflowUseCase{repo: NewMockWorkflowRepository(), catalogFn: func() []workflow.NodeDefinition { return catalog }}
	draft := &workflow.Workflow{WorkspaceID: "ws", Type: workflow.WorkflowTypeMessages, Graph: workflow.Graph{Nodes: []workflow.Node{{ID: "row", Type: workflow.NodeTypeActionAddSheetRow, Config: map[string]interface{}{"sheet_id": "sheet", "values": map[string]interface{}{"nome_do_cliente": "Ana"}}}}}}
	report, err := linter.Lint(context.Background(), draft)
	if err != nil || !reflect.DeepEqual(issueCodes(report.Issues)[workflow.LintValidationUnavailable], []string{"row"}) {
		t.Fatalf("a sheet step passed validation with no way to read the sheet: %v %+v", err, report.Issues)
	}
}

type knownModels struct{}

func (knownModels) IsValidModel(context.Context, string) (bool, error) { return true, nil }
