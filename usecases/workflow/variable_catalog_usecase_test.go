package workflow_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/workflow"
)

type secretNamesStub struct {
	names []string
	err   error
	asked []string
}

func (s *secretNamesStub) SecretNames(_ context.Context, workspaceID string) ([]string, error) {
	s.asked = append(s.asked, workspaceID)
	return s.names, s.err
}

type formCatalogStub struct {
	forms map[string]workflow.TriggerForm
	err   error
}

func (s formCatalogStub) TriggerFormChoices(string) ([]workflow.TriggerInputOption, error) {
	return nil, s.err
}

func (s formCatalogStub) TriggerForm(_ string, formID string) (workflow.TriggerForm, error) {
	if s.err != nil {
		return workflow.TriggerForm{}, s.err
	}
	return s.forms[formID], nil
}

func variableCatalogWorkflow() *workflow.Workflow {
	return &workflow.Workflow{
		ID: "wf-1", WorkspaceID: "ws1", Type: workflow.WorkflowTypeMessages,
		Graph: workflow.Graph{
			Nodes: []workflow.Node{
				{ID: "m", Type: workflow.NodeTypeTriggerMessageReceived},
				{ID: "f", Type: workflow.NodeTypeActionSendForm, Config: map[string]interface{}{workflow.FormNodeFormIDKey: "form-1"}},
				{ID: "s", Type: workflow.NodeTypeActionSendText, Config: map[string]interface{}{"text": "x"}},
				{ID: "end", Type: workflow.NodeTypeEnd},
			},
			Edges: []workflow.Edge{
				{Source: "m", Target: "f"},
				{Source: "f", Target: "s", Label: workflow.FormHandleCompleted},
				{Source: "s", Target: "end"},
			},
		},
	}
}

func catalogRef(vars []workflow.CatalogVariable, ref string) *workflow.CatalogVariable {
	for i := range vars {
		if vars[i].Ref == ref {
			return &vars[i]
		}
	}
	return nil
}

func TestTheVariableCatalogReadsSecretNamesAndFormQuestionsThroughItsPorts(t *testing.T) {
	secrets := &secretNamesStub{names: []string{"crm_token"}}
	reader := VariableCatalogReader{
		Readers: TriggerInputReaders{Forms: formCatalogStub{forms: map[string]workflow.TriggerForm{"form-1": {ID: "form-1", Questions: []workflow.TriggerFormQuestion{{Key: "nome", Prompt: "Seu nome"}}}}}},
		Secrets: secrets,
		Catalog: func() []workflow.NodeDefinition { return nil },
	}

	catalog, err := reader.At(context.Background(), variableCatalogWorkflow(), "s")

	if err != nil {
		t.Fatal(err)
	}
	if catalogRef(catalog.Variables, "secret.crm_token") == nil || len(secrets.asked) != 1 || secrets.asked[0] != "ws1" {
		t.Fatalf("variables = %+v asked = %v", catalog.Variables, secrets.asked)
	}
	if answer := catalogRef(catalog.Variables, "form.nome"); answer == nil || answer.Label != "Resposta do formulário › Seu nome" {
		t.Fatalf("form.nome = %+v", answer)
	}
}

func TestAnUnreadableFormKeepsTheCatalogWithoutItsQuestions(t *testing.T) {
	reader := VariableCatalogReader{
		Readers: TriggerInputReaders{Forms: formCatalogStub{err: errors.New("db down")}},
		Secrets: &secretNamesStub{},
		Catalog: func() []workflow.NodeDefinition { return nil },
	}
	catalog, err := reader.At(context.Background(), variableCatalogWorkflow(), "s")
	if err != nil || catalogRef(catalog.Variables, "form.nome") != nil || catalogRef(catalog.Variables, "form_id") == nil {
		t.Fatalf("catalog = %+v err = %v", catalog, err)
	}
}

func TestTheVariableCatalogFailsClosedWhenSecretsCannotBeRead(t *testing.T) {
	for name, secrets := range map[string]workflow.SecretNameReader{
		"read error": &secretNamesStub{err: errors.New("db down")},
		"not wired":  nil,
	} {
		t.Run(name, func(t *testing.T) {
			reader := VariableCatalogReader{Secrets: secrets, Catalog: func() []workflow.NodeDefinition { return nil }}
			if _, err := reader.At(context.Background(), variableCatalogWorkflow(), "s"); !errors.Is(err, workflow.ErrVariablesUnavailable) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestTheVariableCatalogOfAMissingNodeIsNotFound(t *testing.T) {
	reader := VariableCatalogReader{Secrets: &secretNamesStub{}, Catalog: func() []workflow.NodeDefinition { return nil }}
	if _, err := reader.At(context.Background(), variableCatalogWorkflow(), "nada"); !errors.Is(err, workflow.ErrNodeNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestTheNodeAnalysisCarriesTheSameVariables(t *testing.T) {
	repo := NewMockWorkflowRepository()
	_ = repo.Create(variableCatalogWorkflow())
	reader := &VariableCatalogReader{Secrets: &secretNamesStub{names: []string{"crm_token"}}, Catalog: func() []workflow.NodeDefinition { return nil }}
	uc := NewTestNodeUseCase(TestNodeDeps{WorkflowRepo: repo, Variables: reader})

	analysis, err := uc.Analyze(context.Background(), AnalyzeNodeInput{WorkflowID: "wf-1", WorkspaceID: "ws1", NodeID: "s"})

	if err != nil {
		t.Fatal(err)
	}
	ui := analysis.ToUIAnalysis()
	if catalogRef(ui.Variables, "secret.crm_token") == nil || catalogRef(ui.Variables, "message") == nil {
		t.Fatalf("variables = %+v", ui.Variables)
	}
}

func TestTheNodeAnalysisLeavesTheVariablesOutWhenTheyCannotBeRead(t *testing.T) {
	repo := NewMockWorkflowRepository()
	_ = repo.Create(variableCatalogWorkflow())
	uc := NewTestNodeUseCase(TestNodeDeps{WorkflowRepo: repo, Variables: &VariableCatalogReader{Secrets: &secretNamesStub{err: errors.New("down")}}})

	analysis, err := uc.Analyze(context.Background(), AnalyzeNodeInput{WorkflowID: "wf-1", WorkspaceID: "ws1", NodeID: "s"})

	if err != nil || analysis.ToUIAnalysis().Variables != nil {
		t.Fatalf("analysis = %+v err = %v", analysis, err)
	}
}
