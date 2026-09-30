package node_executors

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/workflow"
)

type fakeVoiceCall struct {
	played      [][]byte
	interrupted bool
	keys        []rune
	waited      []time.Duration
	err         error
}

func (c *fakeVoiceCall) Play(pcm []byte, interruptible bool) (bool, error) {
	if c.err != nil {
		return false, c.err
	}
	c.played = append(c.played, pcm)
	return c.interrupted && interruptible, nil
}

func (c *fakeVoiceCall) NextKey(timeout time.Duration) (rune, bool, error) {
	c.waited = append(c.waited, timeout)
	if c.err != nil {
		return 0, false, c.err
	}
	if len(c.keys) == 0 {
		return 0, false, nil
	}
	key := c.keys[0]
	c.keys = c.keys[1:]
	return key, true, nil
}

type fakeVoiceAudio struct {
	workspace string
	media     string
	pcm       []byte
	err       error
}

func (a *fakeVoiceAudio) LoadPCM(_ context.Context, workspaceID, mediaID string) ([]byte, error) {
	a.workspace, a.media = workspaceID, mediaID
	return a.pcm, a.err
}

func voiceContext(node workflow.Node, edges []workflow.Edge, runtime interface{}) *workflow.NodeContext {
	graph := &workflow.Graph{Nodes: []workflow.Node{node}, Edges: edges}
	run := &workflow.WorkflowRun{State: workflow.NewRunState()}
	return &workflow.NodeContext{
		Run:      run,
		Node:     &graph.Nodes[0],
		Graph:    graph,
		Workflow: &workflow.Workflow{WorkspaceID: "ws-1"},
		State:    &run.State,
		Runtime:  runtime,
	}
}

func TestPlayAudioPlaysTheWorkspaceFileOnTheCall(t *testing.T) {
	audio := &fakeVoiceAudio{pcm: []byte{1, 2, 3, 4}}
	call := &fakeVoiceCall{}
	node := workflow.Node{ID: "menu", Type: workflow.NodeTypeActionPlayAudio, Config: map[string]interface{}{"media_id": "m-1"}}

	res, err := NewPlayAudioExecutor(audio).Execute(voiceContext(node, nil, call))
	if err != nil || res.Error != "" {
		t.Fatalf("Execute: %v %q", err, res.Error)
	}
	if audio.workspace != "ws-1" || audio.media != "m-1" {
		t.Fatalf("loaded %s/%s, want the workflow's workspace and media", audio.workspace, audio.media)
	}
	if len(call.played) != 1 || len(call.played[0]) != 4 {
		t.Fatalf("played %v", call.played)
	}
	if res.Output["interrupted"] != false {
		t.Fatalf("output = %v", res.Output)
	}
}

func TestPlayAudioLetsAKeyPressCutItShortUnlessToldOtherwise(t *testing.T) {
	call := &fakeVoiceCall{interrupted: true}
	node := workflow.Node{ID: "menu", Type: workflow.NodeTypeActionPlayAudio, Config: map[string]interface{}{"media_id": "m-1"}}
	res, _ := NewPlayAudioExecutor(&fakeVoiceAudio{pcm: []byte{1, 2}}).Execute(voiceContext(node, nil, call))
	if res.Output["interrupted"] != true {
		t.Fatalf("output = %v, want interrupted", res.Output)
	}

	node.Config["interruptible"] = false
	res, _ = NewPlayAudioExecutor(&fakeVoiceAudio{pcm: []byte{1, 2}}).Execute(voiceContext(node, nil, &fakeVoiceCall{interrupted: true}))
	if res.Output["interrupted"] != false {
		t.Fatalf("output = %v, want played to the end", res.Output)
	}
}

func TestVoiceNodesFailWithoutRetryingWhenTheyCannotRun(t *testing.T) {
	playNode := workflow.Node{ID: "menu", Type: workflow.NodeTypeActionPlayAudio, Config: map[string]interface{}{"media_id": "m-1"}}
	waitNode := workflow.Node{ID: "keys", Type: workflow.NodeTypeWaitDTMF, Config: map[string]interface{}{"keys": []interface{}{"1"}}}
	cases := []struct {
		name string
		run  func() (*workflow.NodeResult, error)
	}{
		{"play outside a call", func() (*workflow.NodeResult, error) {
			return NewPlayAudioExecutor(&fakeVoiceAudio{pcm: []byte{1}}).Execute(voiceContext(playNode, nil, nil))
		}},
		{"audio that cannot load", func() (*workflow.NodeResult, error) {
			return NewPlayAudioExecutor(&fakeVoiceAudio{err: errors.New("gone")}).Execute(voiceContext(playNode, nil, &fakeVoiceCall{}))
		}},
		{"wait outside a call", func() (*workflow.NodeResult, error) {
			return NewWaitDTMFExecutor().Execute(voiceContext(waitNode, nil, nil))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := tc.run()
			if err != nil || res == nil || res.Error == "" {
				t.Fatalf("got (%v, %v), want a non-retryable node error", res, err)
			}
		})
	}
}

func TestAHangUpDuringAVoiceNodeCancelsTheRun(t *testing.T) {
	call := &fakeVoiceCall{err: workflow.ErrCallEnded}
	playNode := workflow.Node{ID: "menu", Type: workflow.NodeTypeActionPlayAudio, Config: map[string]interface{}{"media_id": "m-1"}}
	if _, err := NewPlayAudioExecutor(&fakeVoiceAudio{pcm: []byte{1}}).Execute(voiceContext(playNode, nil, call)); !errors.Is(err, context.Canceled) {
		t.Fatalf("play err = %v, want cancelled", err)
	}
	waitNode := workflow.Node{ID: "keys", Type: workflow.NodeTypeWaitDTMF, Config: map[string]interface{}{"keys": []interface{}{"1"}}}
	if _, err := NewWaitDTMFExecutor().Execute(voiceContext(waitNode, nil, call)); !errors.Is(err, context.Canceled) {
		t.Fatalf("wait err = %v, want cancelled", err)
	}
}

func TestWaitDTMFFollowsThePressedKey(t *testing.T) {
	node := workflow.Node{ID: "keys", Type: workflow.NodeTypeWaitDTMF, Config: map[string]interface{}{
		"keys":            []interface{}{"1", "2"},
		"timeout_seconds": float64(5),
	}}
	edges := []workflow.Edge{
		{Source: "keys", Target: "sales", Label: "1"},
		{Source: "keys", Target: "support", Label: "2"},
		{Source: "keys", Target: "again", Label: "invalid"},
		{Source: "keys", Target: "bye", Label: "timeout"},
	}
	cases := []struct {
		name string
		call *fakeVoiceCall
		next string
		key  string
	}{
		{"known key", &fakeVoiceCall{keys: []rune{'2'}}, "support", "2"},
		{"unknown key", &fakeVoiceCall{keys: []rune{'9'}}, "again", "9"},
		{"silence", &fakeVoiceCall{}, "bye", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := NewWaitDTMFExecutor().Execute(voiceContext(node, edges, tc.call))
			if err != nil || res.Error != "" {
				t.Fatalf("Execute: %v %q", err, res.Error)
			}
			if res.NextNodeID != tc.next || res.Output["key"] != tc.key {
				t.Fatalf("next=%q key=%v, want %q %q", res.NextNodeID, res.Output["key"], tc.next, tc.key)
			}
			if tc.call.waited[0] != 5*time.Second {
				t.Fatalf("waited %v", tc.call.waited)
			}
		})
	}
}

func TestWaitDTMFEndsTheCallWhenTimeoutOrInvalidIsNotConnected(t *testing.T) {
	node := workflow.Node{ID: "keys", Type: workflow.NodeTypeWaitDTMF, Config: map[string]interface{}{"keys": []interface{}{"1"}}}
	edges := []workflow.Edge{{Source: "keys", Target: "sales", Label: "1"}}
	for name, call := range map[string]*fakeVoiceCall{"silence": {}, "unknown key": {keys: []rune{'7'}}} {
		res, err := NewWaitDTMFExecutor().Execute(voiceContext(node, edges, call))
		if err != nil || !res.Complete || res.NextNodeID != "" {
			t.Fatalf("%s: got %+v %v, want the flow to end instead of falling into another branch", name, res, err)
		}
	}
}

func TestWaitDTMFBranchesAreTheKeysPlusTimeoutAndInvalid(t *testing.T) {
	handles := WaitDTMFOutputs(map[string]interface{}{"keys": []interface{}{"1", "#"}})
	ids := make([]string, 0, len(handles))
	for _, h := range handles {
		ids = append(ids, h.ID)
	}
	want := []string{"1", "#", "invalid", "timeout"}
	if len(ids) != len(want) {
		t.Fatalf("handles = %v", ids)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("handles = %v, want %v", ids, want)
		}
	}
	if !handles[2].Optional || !handles[3].Optional || handles[0].Optional {
		t.Fatal("key branches are required, invalid and timeout are optional")
	}
}

func TestVoiceNodesAreVoiceOnly(t *testing.T) {
	for _, def := range []workflow.NodeDefinition{NewPlayAudioExecutor(nil).Definition(), NewWaitDTMFExecutor().Definition()} {
		if !workflow.DefinitionAllowedForType(def, workflow.WorkflowTypeVoice) || workflow.DefinitionAllowedForType(def, workflow.WorkflowTypeMessages) {
			t.Fatalf("%s must be allowed in voice workflows only", def.Type)
		}
	}
}

type fakeTransferCall struct {
	fakeVoiceCall
	transfers []workflow.QueueTransfer
	connected bool
	failure   error
}

func (c *fakeTransferCall) TransferToQueue(_ context.Context, transfer workflow.QueueTransfer) (bool, error) {
	c.transfers = append(c.transfers, transfer)
	return c.connected, c.failure
}

func transferNode(config map[string]interface{}) workflow.Node {
	return workflow.Node{ID: "fila", Type: workflow.NodeTypeTransferToQueue, Config: config}
}

func TestTransferToQueueHandsTheCallerToAnOperatorAndEndsTheFlow(t *testing.T) {
	call := &fakeTransferCall{connected: true}
	ctx := voiceContext(transferNode(map[string]interface{}{"queue_id": " q1 ", "notes": "Cliente escolheu {{var.opcao}}"}), nil, call)
	ctx.State.Set("opcao", "2")
	ctx.Workflow.Name = "URA principal"

	res, err := NewTransferToQueueExecutor().Execute(ctx)
	if err != nil || res.Error != "" || !res.Complete {
		t.Fatalf("result = %+v, %v", res, err)
	}
	want := workflow.QueueTransfer{QueueID: "q1", Notes: "Cliente escolheu 2", From: "URA principal"}
	if len(call.transfers) != 1 || call.transfers[0] != want {
		t.Fatalf("transfers = %+v", call.transfers)
	}
	if res.Output["transferred"] != true {
		t.Fatalf("output = %v", res.Output)
	}
}

func TestTransferToQueueFollowsTimeoutWhenNobodyAnswers(t *testing.T) {
	call := &fakeTransferCall{}
	edges := []workflow.Edge{{Source: "fila", Target: "desculpa", Label: "timeout"}}
	res, err := NewTransferToQueueExecutor().Execute(voiceContext(transferNode(map[string]interface{}{"queue_id": "q1"}), edges, call))
	if err != nil || res.NextNodeID != "desculpa" || res.Complete {
		t.Fatalf("result = %+v, %v", res, err)
	}
}

func TestTransferToQueueHangsUpWhenTimeoutIsNotConnected(t *testing.T) {
	res, _ := NewTransferToQueueExecutor().Execute(voiceContext(transferNode(map[string]interface{}{"queue_id": "q1"}), nil, &fakeTransferCall{}))
	if !res.Complete || res.NextNodeID != "" {
		t.Fatalf("result = %+v", res)
	}
}

func TestTransferToQueueRefusesWhatItCannotDo(t *testing.T) {
	cases := map[string]struct {
		runtime interface{}
		config  map[string]interface{}
	}{
		"not in a call":        {nil, map[string]interface{}{"queue_id": "q1"}},
		"call cannot transfer": {&fakeVoiceCall{}, map[string]interface{}{"queue_id": "q1"}},
		"no queue chosen":      {&fakeTransferCall{}, map[string]interface{}{"queue_id": "  "}},
		"queue refused":        {&fakeTransferCall{failure: errors.New("queue not found")}, map[string]interface{}{"queue_id": "q1"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			res, err := NewTransferToQueueExecutor().Execute(voiceContext(transferNode(tc.config), nil, tc.runtime))
			if err != nil || res.Error == "" {
				t.Fatalf("result = %+v, %v", res, err)
			}
		})
	}
}

func TestTransferToQueueStopsWhenTheCallerHangsUp(t *testing.T) {
	call := &fakeTransferCall{failure: workflow.ErrCallEnded}
	if _, err := NewTransferToQueueExecutor().Execute(voiceContext(transferNode(map[string]interface{}{"queue_id": "q1"}), nil, call)); !errors.Is(err, workflow.ErrCallEnded) {
		t.Fatalf("err = %v", err)
	}
}
