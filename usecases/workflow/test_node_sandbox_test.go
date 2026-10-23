package workflow_usecase

import (
	"context"
	"testing"

	"vozko/domain/shared"
	"vozko/domain/workflow"
)

type noRunVisible struct{}

func (noRunVisible) RunVisible(shared.Person, string, string) bool { return false }

func viewerIs(person shared.Person) func(context.Context) (shared.Person, bool) {
	return func(context.Context) (shared.Person, bool) { return person, true }
}

func sandboxNodeWorkflow(node workflow.Node) *workflow.Workflow {
	return createTestWorkflow("wf1", "ws1", []workflow.Node{
		{ID: "trigger1", Type: workflow.NodeTypeTriggerMessageReceived},
		node,
		{ID: "end", Type: workflow.NodeTypeEnd},
	}, []workflow.Edge{
		{Source: "trigger1", Target: node.ID},
		{Source: node.ID, Target: "end", Label: "sucesso"},
		{Source: node.ID, Target: "end", Label: "erro"},
	})
}

func TestTheNodeTestOfASendNodeCarriesTheMessageTheCustomerWouldSee(t *testing.T) {
	repo := NewMockWorkflowRepository()
	_ = repo.Create(sandboxNodeWorkflow(workflow.Node{ID: "n1", Type: workflow.NodeTypeActionSendText, Config: map[string]interface{}{"text": "Olá, Ana"}}))
	uc := NewTestNodeUseCase(TestNodeDeps{WorkflowRepo: repo})

	result, err := uc.Execute(context.Background(), TestNodeInput{WorkflowID: "wf1", WorkspaceID: "ws1", NodeID: "n1"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success || result.Step == nil {
		t.Fatalf("result = %+v", result)
	}
	sent, ok := result.Step.Output[workflow.StepOutputSentMessages].([]interface{})
	if !ok || len(sent) != 1 {
		t.Fatalf("step output = %v, want the sent message", result.Step.Output)
	}
	message := sent[0].(map[string]interface{})
	if message["text"] != "Olá, Ana" || message["messageType"] != "ai_response" || message["deliveryStatus"] != "sent" {
		t.Fatalf("message = %v", message)
	}
	if result.Step.Simulated {
		t.Fatal("a message send is not a simulated CRM write")
	}
}

func TestTheNodeTestOfACRMNodeIsASimulatedStepThatSaysWhatItWouldDo(t *testing.T) {
	repo := NewMockWorkflowRepository()
	_ = repo.Create(sandboxNodeWorkflow(workflow.Node{ID: "n1", Type: workflow.NodeTypeActionAssignLabel, Config: map[string]interface{}{"action": "add", "label_id": "l-vip"}}))
	labels := &crmLabels{}
	uc := NewTestNodeUseCase(TestNodeDeps{WorkflowRepo: repo, ExecutorDeps: ExecutorDeps{LabelRepo: labels}})

	result, err := uc.Execute(context.Background(), TestNodeInput{WorkflowID: "wf1", WorkspaceID: "ws1", NodeID: "n1"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Step.Simulated || result.Step.Branch != "sucesso" {
		t.Fatalf("step = %+v", result.Step)
	}
	wouldDo, ok := result.Step.Output[workflow.StepOutputWouldDo].([]interface{})
	if !ok || len(wouldDo) != 1 || wouldDo[0].(map[string]interface{})["target_name"] != "VIP" {
		t.Fatalf("would_do = %v", result.Step.Output[workflow.StepOutputWouldDo])
	}
	if labels.writes != 0 {
		t.Fatal("the node test wrote a label")
	}
}

func TestTheNodeTestWithholdsAProductionPinItsEditorCannotSee(t *testing.T) {
	pinned := func() *workflow.Workflow {
		wf := pinnedLoginWorkflow()
		wf.Graph.FindNode("login").Pinned.Source = workflow.PinSourceOfRun("run-hidden")
		return wf
	}
	cases := []struct {
		name    string
		sources PinSources
		viewer  func(context.Context) (shared.Person, bool)
		served  bool
	}{
		{"the editor sees the conversation", everyRunVisible{}, viewerIs(shared.Person{UserID: "u1"}), true},
		{"the editor cannot see the conversation", noRunVisible{}, viewerIs(shared.Person{UserID: "u1"}), false},
		{"no viewer is known", everyRunVisible{}, nil, false},
		{"no way to check the conversation", nil, viewerIs(shared.Person{UserID: "u1"}), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := NewMockWorkflowRepository()
			_ = repo.Create(pinned())
			ran := 0
			registry := NewNodeExecutorRegistry()
			registry.Register(workflow.NodeTypeActionHTTPRequest, &captureHTTPExecutor{ran: &ran})
			uc := NewTestNodeUseCase(TestNodeDeps{WorkflowRepo: repo, Steps: registry, Executors: fixedExecutors(registry), PinSources: tc.sources, ViewerOf: tc.viewer})

			result, err := uc.Execute(context.Background(), TestNodeInput{WorkflowID: "wf1", WorkspaceID: "ws1", NodeID: "login"})
			if err != nil {
				t.Fatal(err)
			}
			if tc.served {
				if !result.Pinned || result.Step.Notice != "" || ran != 0 {
					t.Fatalf("result = %+v, ran %d; want the pin served", result, ran)
				}
				return
			}
			if result.Pinned || result.Step.Pinned || result.Step.Notice != workflow.PinWithheldNotice {
				t.Fatalf("step = %+v; want the pin treated as missing and the step saying so", result.Step)
			}
			if result.ErrorKind != workflow.ErrorKindRealCallNotConfirmed || ran != 0 {
				t.Fatalf("kind = %s, ran %d; an unpinned HTTP node follows the real-call rule", result.ErrorKind, ran)
			}
			stored, _ := repo.FindByID("wf1")
			if stored.Graph.FindNode("login").Pinned.Withheld {
				t.Fatal("the node test must never change the stored pin")
			}
		})
	}
}
