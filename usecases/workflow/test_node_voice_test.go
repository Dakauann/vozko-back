package workflow_usecase

import (
	"context"
	"testing"

	"vozko/domain/media"
	"vozko/domain/workflow"
)

func voiceNodeTester(t *testing.T) TestNodeUseCase {
	t.Helper()
	repo := NewMockWorkflowRepository()
	w := voiceWorkflow("wf-1", "trunk-1", "greeting")
	w.Normalize()
	_ = repo.Create(w)
	audio := newSimVoiceAudio(audioMediaStub{items: map[string]*media.Media{
		"greeting": {ID: "greeting", WorkspaceID: "ws1", Type: media.MediaTypeAudio, URL: "https://files/greeting.mp3"},
	}}, func(*media.Media) {})
	return NewTestNodeUseCase(TestNodeDeps{WorkflowRepo: repo, ExecutorDeps: ExecutorDeps{VoiceAudio: audio, MediaRepo: audioMediaStub{}}})
}

func TestTestingAKeyWaitFollowsTheMockedKeyOrTimesOut(t *testing.T) {
	tester := voiceNodeTester(t)

	out, err := tester.Execute(context.Background(), TestNodeInput{WorkflowID: "wf-1", WorkspaceID: "ws1", NodeID: "keys", MockedState: map[string]interface{}{"key": "1"}})
	if err != nil || !out.Success || out.ExecutionOutput["key"] != "1" {
		t.Fatalf("with key: %+v %v", out, err)
	}

	out, err = tester.Execute(context.Background(), TestNodeInput{WorkflowID: "wf-1", WorkspaceID: "ws1", NodeID: "keys"})
	if err != nil || !out.Success || out.ExecutionOutput["key"] != "" {
		t.Fatalf("without key: %+v %v", out, err)
	}
}

func TestTestingPlayAudioChecksTheFileCanPlay(t *testing.T) {
	out, err := voiceNodeTester(t).Execute(context.Background(), TestNodeInput{WorkflowID: "wf-1", WorkspaceID: "ws1", NodeID: "menu"})
	if err != nil || !out.Success {
		t.Fatalf("play audio test: %+v %v", out, err)
	}
}

func TestMessagingWorkflowsGetNoCallInTests(t *testing.T) {
	if testVoiceRuntime(&workflow.Workflow{Type: workflow.WorkflowTypeMessages}, map[string]interface{}{"key": "1"}) != nil {
		t.Fatal("a messaging workflow was handed a call")
	}
}

func TestTheKeyWaitTestOffersAnOptionalKeyField(t *testing.T) {
	repo := NewMockWorkflowRepository()
	w := voiceWorkflow("wf-1", "trunk-1", "greeting")
	w.Normalize()
	_ = repo.Create(w)
	tester := NewTestNodeUseCase(TestNodeDeps{WorkflowRepo: repo, Registry: NewNodeExecutorRegistry()})

	analysis, err := tester.Analyze(context.Background(), AnalyzeNodeInput{WorkflowID: "wf-1", WorkspaceID: "ws1", NodeID: "keys"})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	fields := analysis.ToUIAnalysis().MockFields
	if len(fields) != 1 || fields[0].Key != "key" || !fields[0].Optional {
		t.Fatalf("mock fields = %+v, want one optional key field", fields)
	}
}
