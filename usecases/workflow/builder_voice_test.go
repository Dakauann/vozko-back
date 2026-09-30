package workflow_usecase

import (
	"context"
	"sort"
	"testing"

	"vozko/domain/ai"
	"vozko/domain/sip_trunk"
	"vozko/domain/workflow"
	"vozko/usecases/workflow/node_executors"
)

func TestTheCopilotOffersOnlyVoiceNodesInAVoiceWorkflow(t *testing.T) {
	catalog := append(voiceCatalog(), node_executors.NewTextMatchExecutor().Definition())
	uc := &aiBuilderUC{}

	voice := uc.typeEnum(&builderState{wfType: workflow.WorkflowTypeVoice, fullCatalog: catalog})
	want := []string{"action_play_audio", "end", "trigger_call_received", "wait_dtmf"}
	sort.Strings(want)
	if len(voice) != len(want) {
		t.Fatalf("voice nodes = %v, want %v", voice, want)
	}
	for i := range want {
		if voice[i] != want[i] {
			t.Fatalf("voice nodes = %v, want %v", voice, want)
		}
	}

	for _, n := range uc.typeEnum(&builderState{wfType: workflow.WorkflowTypeMessages, fullCatalog: catalog}) {
		if n == "action_play_audio" || n == "wait_dtmf" || n == "trigger_call_received" {
			t.Fatalf("messaging workflows were offered %s", n)
		}
	}
}

func TestTheCopilotCanSwitchASessionToVoice(t *testing.T) {
	uc := &aiBuilderUC{}
	st := &builderState{wfType: workflow.WorkflowTypeMessages}
	if _, err := uc.applySetMeta(st, ai.ToolCall{Name: "set_meta", Arguments: map[string]interface{}{"workflow_type": "voice"}}); err != nil {
		t.Fatalf("set_meta voice: %v", err)
	}
	if st.wfType != workflow.WorkflowTypeVoice {
		t.Fatalf("type = %s", st.wfType)
	}
	if _, err := uc.applySetMeta(st, ai.ToolCall{Name: "set_meta", Arguments: map[string]interface{}{"workflow_type": "fax"}}); err == nil {
		t.Fatal("an unknown workflow type was accepted")
	}
}

type trunkListStub []*sip_trunk.SIPTrunk

func (s trunkListStub) ListByWorkspace(context.Context, string) ([]*sip_trunk.SIPTrunk, error) {
	return s, nil
}

func TestTheCopilotFindsOnlyTrunksThatTakeCalls(t *testing.T) {
	resolver := NewBuilderResourceResolver(BuilderResourceResolverDeps{Trunks: trunkListStub{
		{ID: "t1", WorkspaceID: "ws1", Name: "Principal", TrunkType: sip_trunk.TrunkTypeBidirectional},
		{ID: "t2", WorkspaceID: "ws1", Name: "Só saída", TrunkType: sip_trunk.TrunkTypeOutbound},
		{ID: "t3", WorkspaceID: "ws2", Name: "Outro workspace", TrunkType: sip_trunk.TrunkTypeInbound},
	}})
	got, err := resolver.Search(context.Background(), "ws1", "sip_trunks", "", 10)
	if err != nil || len(got) != 1 || got[0].ID != "t1" {
		t.Fatalf("Search = %v, %v", got, err)
	}
}
