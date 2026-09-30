package workflow

import (
	"context"
	"errors"
	"testing"
)

func voiceGraph() Graph {
	return Graph{
		Nodes: []Node{
			{ID: "t", Type: NodeTypeTriggerCallReceived, Config: map[string]interface{}{"trunk_id": "trunk-1"}},
			{ID: "menu", Type: NodeTypeActionPlayAudio},
			{ID: "keys", Type: NodeTypeWaitDTMF},
			{ID: "sales", Type: NodeTypeActionPlayAudio},
			{ID: "end", Type: NodeTypeEnd},
		},
		Edges: []Edge{
			{Source: "t", Target: "menu"},
			{Source: "menu", Target: "keys"},
			{Source: "keys", Target: "sales", Label: "1"},
			{Source: "keys", Target: "menu", Label: "invalid"},
			{Source: "sales", Target: "end"},
		},
	}
}

func TestVoiceIsItsOwnWorkflowType(t *testing.T) {
	if !WorkflowTypeVoice.Valid() {
		t.Fatal("voice must be a valid workflow type")
	}
	if !TriggerCallReceived.Valid() || TriggerCallReceived.WorkflowType() != WorkflowTypeVoice {
		t.Fatal("an inbound call must start voice workflows only")
	}
	if !NodeTypeTriggerCallReceived.IsTrigger() {
		t.Fatal("call received is a trigger node")
	}
	for _, n := range []NodeType{NodeTypeTriggerCallReceived, NodeTypeActionPlayAudio, NodeTypeWaitDTMF} {
		if !n.Valid() {
			t.Fatalf("%s must be a valid node type", n)
		}
	}
}

func TestVoiceNodesNeverMixWithMessaging(t *testing.T) {
	voiceNode := NodeDefinition{Type: NodeTypeActionPlayAudio, Scopes: []NodeScope{NodeScopeVoice}}
	sharedNode := NodeDefinition{Type: NodeTypeActionSetVariable, Scopes: []NodeScope{NodeScopeShared}}
	whatsappNode := NodeDefinition{Type: NodeTypeActionSendText, Scopes: []NodeScope{NodeScopeWhatsApp}}
	end := NodeDefinition{Type: NodeTypeEnd, Scopes: []NodeScope{NodeScopeShared, NodeScopeVoice}}

	cases := []struct {
		def   NodeDefinition
		wf    WorkflowType
		allow bool
	}{
		{voiceNode, WorkflowTypeVoice, true},
		{voiceNode, WorkflowTypeMessages, false},
		{sharedNode, WorkflowTypeVoice, false},
		{sharedNode, WorkflowTypeMessages, true},
		{whatsappNode, WorkflowTypeVoice, false},
		{end, WorkflowTypeVoice, true},
		{end, WorkflowTypeMessages, true},
	}
	for _, tc := range cases {
		if got := DefinitionAllowedForType(tc.def, tc.wf); got != tc.allow {
			t.Errorf("%s in %s: allowed=%v, want %v", tc.def.Type, tc.wf, got, tc.allow)
		}
	}
}

func TestEndNodeIsAvailableToVoiceWorkflows(t *testing.T) {
	for _, def := range BuiltinDefinitions() {
		if def.Type == NodeTypeEnd && !DefinitionAllowedForType(def, WorkflowTypeVoice) {
			t.Fatal("voice workflows must be able to end")
		}
		if def.Type == NodeTypeEnd && !DefinitionAllowedForType(def, WorkflowTypeMessages) {
			t.Fatal("messaging workflows must still be able to end")
		}
	}
}

func TestAMenuThatRepeatsThroughAKeyWaitIsAValidGraph(t *testing.T) {
	g := voiceGraph()
	if err := ValidateGraph(&g, WorkflowTypeVoice); err != nil {
		t.Fatalf("ValidateGraph: %v", err)
	}
}

func TestAVoiceLoopWithoutACallerWaitIsRejected(t *testing.T) {
	g := Graph{
		Nodes: []Node{
			{ID: "t", Type: NodeTypeTriggerCallReceived},
			{ID: "a", Type: NodeTypeActionPlayAudio},
			{ID: "b", Type: NodeTypeActionPlayAudio},
			{ID: "end", Type: NodeTypeEnd},
		},
		Edges: []Edge{
			{Source: "t", Target: "a"},
			{Source: "a", Target: "b"},
			{Source: "b", Target: "a", Label: "again"},
			{Source: "b", Target: "end"},
		},
	}
	if err := ValidateGraph(&g, WorkflowTypeVoice); !errors.Is(err, ErrGraphCycleDetected) {
		t.Fatalf("err = %v, want ErrGraphCycleDetected", err)
	}
}

func TestAKeyWaitIsACallerWaitNotAReplyPark(t *testing.T) {
	if !NodeTypeWaitDTMF.WaitsForCaller() {
		t.Fatal("wait_dtmf waits for the caller")
	}
	if NodeTypeWaitDTMF.IsWait() || NodeTypeWaitDTMF.ParksForReply() {
		t.Fatal("wait_dtmf must never park the run for a message reply")
	}
	if NodeTypeWaitDTMF.Category() != NodeCategoryWait {
		t.Fatal("wait_dtmf belongs with the waits in the palette")
	}
}

func TestAVoiceWorkflowCannotUseAMessagingTrigger(t *testing.T) {
	w := Workflow{
		WorkspaceID: "ws-1",
		Name:        "URA",
		Status:      WorkflowStatusDraft,
		Type:        WorkflowTypeVoice,
		Graph: Graph{
			Nodes: []Node{{ID: "t", Type: NodeTypeTriggerFirstMessage}, {ID: "end", Type: NodeTypeEnd}},
			Edges: []Edge{{Source: "t", Target: "end"}},
		},
	}
	if err := w.Validate(); !errors.Is(err, ErrTriggerNotAllowedForType) {
		t.Fatalf("err = %v, want ErrTriggerNotAllowedForType", err)
	}

	voice := Workflow{WorkspaceID: "ws-1", Name: "URA", Graph: voiceGraph()}
	voice.Normalize()
	if voice.Type != WorkflowTypeVoice || voice.TriggerType != TriggerCallReceived {
		t.Fatalf("normalized type=%s trigger=%s", voice.Type, voice.TriggerType)
	}
	if err := voice.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestOnlyPhoneKeysCanBranchAVoiceMenu(t *testing.T) {
	if err := ValidateDTMFKeys([]string{"1", "2", "*", "#", "0"}); err != nil {
		t.Fatalf("ValidateDTMFKeys: %v", err)
	}
	cases := map[string][]string{
		"no keys":   nil,
		"letter":    {"A"},
		"two chars": {"12"},
		"duplicate": {"1", "1"},
	}
	for name, keys := range cases {
		if err := ValidateDTMFKeys(keys); err == nil {
			t.Errorf("%s: keys %v were accepted", name, keys)
		}
	}
}

func TestAHangUpCancelsTheRun(t *testing.T) {
	if !errors.Is(ErrCallEnded, context.Canceled) {
		t.Fatal("a hang-up must read as a cancelled run, never as a retryable failure")
	}
	if _, err := VoiceCallFrom(&NodeContext{}); !errors.Is(err, ErrNotInCall) {
		t.Fatalf("err = %v, want ErrNotInCall", err)
	}
}

func TestAKeyWaitWithBadKeysBlocksActivation(t *testing.T) {
	g := voiceGraph()
	g.Nodes[2].Config = map[string]interface{}{"keys": []interface{}{"1", "1"}}
	if err := RunPureGraphRules(&g); !errors.Is(err, ErrNodeInvalidDTMFConfig) {
		t.Fatalf("err = %v, want ErrNodeInvalidDTMFConfig", err)
	}
	g.Nodes[2].Config = map[string]interface{}{"keys": []interface{}{"1", "#"}}
	if err := RunPureGraphRules(&g); err != nil {
		t.Fatalf("valid keys rejected: %v", err)
	}
}

func TestAVoiceWorkflowNamesTheTrunkItAnswers(t *testing.T) {
	w := Workflow{Graph: voiceGraph()}
	if got := w.VoiceTrunkID(); got != "trunk-1" {
		t.Fatalf("VoiceTrunkID = %q", got)
	}
	messages := Workflow{Graph: Graph{Nodes: []Node{{ID: "t", Type: NodeTypeTriggerFirstMessage}}}}
	if got := messages.VoiceTrunkID(); got != "" {
		t.Fatalf("a messaging workflow answered trunk %q", got)
	}
}
