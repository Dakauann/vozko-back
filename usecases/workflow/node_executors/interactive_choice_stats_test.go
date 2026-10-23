package node_executors

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"vozko/domain/channel"
	"vozko/domain/choicestats"
	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

type spyChoices struct {
	sends  []choicestats.Send
	clicks []choicestats.Click
}

func (s *spyChoices) RecordSend(_ context.Context, send choicestats.Send) {
	s.sends = append(s.sends, send)
}

func (s *spyChoices) RecordClick(_ context.Context, click choicestats.Click) {
	s.clicks = append(s.clicks, click)
}

type interactiveFakeAdapter struct {
	fakeAdapter
	shown []string
}

func (a *interactiveFakeAdapter) SendInteractive(_ context.Context, _ *conversation.EntryContext, req conversation.SendInteractiveRequest) (*conversation.SendOutcome, error) {
	a.sendCalls++
	if a.sendErr != nil {
		return nil, a.sendErr
	}
	return &conversation.SendOutcome{ProviderMessageID: "ig-mid-1", ShownOptionIDs: a.shown}, nil
}

func (a *interactiveFakeAdapter) InteractiveLimits() channel.InteractiveLimits {
	return channel.InteractiveLimits{MaxOptionsButtons: 2}
}

func choiceNodeContext(wired bool) *workflow.NodeContext {
	version := 5
	run := igRun()
	run.WorkflowID = "wf-1"
	run.CurrentNodeID = "ask"
	run.WorkflowVersion = &version
	run.State = workflow.NewRunState()
	run.State.Set(workflow.StateKeyDurableSteps, 2)
	node := &workflow.Node{ID: "ask", Type: workflow.NodeTypeActionSendInteractive, Config: map[string]interface{}{
		"interactive_type": "buttons",
		"body":             "Quer continuar?",
		"buttons":          `[{"Type":"reply","ID":"sim","Title":"Sim"},{"Type":"reply","ID":"nao","Title":"Não"},{"Type":"reply","ID":"talvez","Title":"Talvez"}]`,
	}}
	graph := &workflow.Graph{Nodes: []workflow.Node{*node, {ID: "next", Type: workflow.NodeTypeActionSendText}}}
	if wired {
		graph.Edges = []workflow.Edge{{Source: "ask", Target: "next", Label: "sim"}}
	}
	return &workflow.NodeContext{Run: run, Node: node, Graph: graph, State: &run.State}
}

func choiceExecutor(adapter *interactiveFakeAdapter, choices choicestats.Recorder) *interactivePromptExecutor {
	return &interactivePromptExecutor{
		channel: &channelSender{
			adapters:   conversation.NewAdapterRegistry(adapter),
			entryLeads: &entryLeads{}, leads: &knownLeadRepo{},
			history: &recordingHistory{},
		},
		choices: choices,
	}
}

func TestAnAcceptedPromptIsRecordedWithTheOptionsTheChannelShowed(t *testing.T) {
	adapter := &interactiveFakeAdapter{fakeAdapter: fakeAdapter{entryType: shared.EntryTypeInstagram, windowOpen: true}, shown: []string{"sim", "nao"}}
	choices := &spyChoices{}
	ctx := choiceNodeContext(true)
	if _, err := choiceExecutor(adapter, choices).Execute(ctx); err != nil {
		t.Fatal(err)
	}
	if len(choices.sends) != 1 {
		t.Fatalf("sends = %+v", choices.sends)
	}
	send := choices.sends[0]
	wantKey := choicestats.WorkflowPromptKey("run-1", "ask#2")
	if send.PromptKey != wantKey || send.ProviderMessageID != "ig-mid-1" || send.WorkspaceID != "ws-1" {
		t.Fatalf("send = %+v", send)
	}
	if send.Source != (choicestats.Source{Kind: choicestats.SourceWorkflow, ID: "wf-1", NodeID: "ask", Version: 5}) {
		t.Fatalf("source = %+v", send.Source)
	}
	if !reflect.DeepEqual(send.Shown, []string{"sim", "nao"}) {
		t.Fatalf("shown = %v, the option the channel dropped must not count as shown", send.Shown)
	}
	if got := ctx.State.GetString(workflow.StateKeyChoicePrompt); got != wantKey {
		t.Fatalf("run state prompt key = %q", got)
	}
}

func TestAPromptThatIsNotASendIsNeverRecorded(t *testing.T) {
	cases := []struct {
		name    string
		adapter *interactiveFakeAdapter
		wired   bool
	}{
		{"a prompt held back by a closed window", &interactiveFakeAdapter{fakeAdapter: fakeAdapter{entryType: shared.EntryTypeInstagram}}, true},
		{"a prompt the provider refused", &interactiveFakeAdapter{fakeAdapter: fakeAdapter{entryType: shared.EntryTypeInstagram, windowOpen: true, sendErr: errors.New("refused")}}, true},
		{"a prompt without wired options is a plain send", &interactiveFakeAdapter{fakeAdapter: fakeAdapter{entryType: shared.EntryTypeInstagram, windowOpen: true}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			choices := &spyChoices{}
			ctx := choiceNodeContext(tc.wired)
			ctx.State.Set(workflow.StateKeyChoicePrompt, "wf:old:prompt#1")
			_, _ = choiceExecutor(tc.adapter, choices).Execute(ctx)
			if len(choices.sends) != 0 {
				t.Fatalf("recorded %+v", choices.sends)
			}
			if got := ctx.State.GetString(workflow.StateKeyChoicePrompt); got != "" {
				t.Fatalf("a stale prompt key %q would take the next tap", got)
			}
		})
	}
}

func TestAnExecutorWithoutARecorderStillSends(t *testing.T) {
	adapter := &interactiveFakeAdapter{fakeAdapter: fakeAdapter{entryType: shared.EntryTypeInstagram, windowOpen: true}}
	if _, err := choiceExecutor(adapter, nil).Execute(choiceNodeContext(true)); err != nil || adapter.sendCalls != 1 {
		t.Fatalf("err = %v, calls = %d", err, adapter.sendCalls)
	}
}
