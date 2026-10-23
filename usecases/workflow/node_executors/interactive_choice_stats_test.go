package node_executors

import (
	"context"
	"errors"
	"reflect"
	"slices"
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
	sent  []conversation.SendInteractiveRequest
}

func (a *interactiveFakeAdapter) SendInteractive(_ context.Context, _ *conversation.EntryContext, req conversation.SendInteractiveRequest) (*conversation.SendOutcome, error) {
	a.sendCalls++
	if a.sendErr != nil {
		return nil, a.sendErr
	}
	a.sent = append(a.sent, req)
	var kept []conversation.InteractiveOption
	for _, opt := range req.Options {
		if slices.Contains(a.shown, opt.ID) {
			kept = append(kept, opt)
		}
	}
	return conversation.ShowingOptions(&conversation.SendOutcome{ProviderMessageID: "ig-mid-1"}, req.ShowingAs(req.Style, kept)), nil
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

func TestTheWorkflowTranscriptKeepsTheListAsTheChannelShowedIt(t *testing.T) {
	adapter := &interactiveFakeAdapter{fakeAdapter: fakeAdapter{entryType: shared.EntryTypeInstagram, windowOpen: true}, shown: []string{"beneficios", "agendar"}}
	executor := choiceExecutor(adapter, &spyChoices{})
	history := executor.channel.history.(*recordingHistory)
	ctx := choiceNodeContext(true)
	ctx.Node.Config = map[string]interface{}{
		"interactive_type": "list",
		"body":             "Para seguir com seu atendimento, selecione alguma das opções abaixo",
		"footer":           "Atendimento",
		"list_button":      "Ver opções",
		"sections": `[{"title":"Atendimento","rows":[{"id":"beneficios","title":"Saber sobre beneficios","description":"Rede"},` +
			`{"id":"agendar","title":"Agendar consulta","description":""},{"id":"outros","title":"Outros","description":""}]}]`,
	}
	ctx.Graph.Edges = []workflow.Edge{{Source: "ask", Target: "next", Label: "agendar"}}

	if _, err := executor.Execute(ctx); err != nil {
		t.Fatal(err)
	}

	req := adapter.sent[0]
	if req.ListButton != "Ver opções" || req.Options[0].Description != "Rede" || req.Options[0].Section != "Atendimento" {
		t.Fatalf("request = %+v", req)
	}
	if len(history.records) != 1 {
		t.Fatalf("records = %+v", history.records)
	}
	shown := history.records[0].Interactive
	want := &conversation.InteractiveContent{
		Kind: conversation.InteractiveKindList, Body: "Para seguir com seu atendimento, selecione alguma das opções abaixo", Footer: "Atendimento", ButtonLabel: "Ver opções",
		Sections: []conversation.InteractiveSection{{Title: "Atendimento", Rows: []conversation.InteractiveRow{
			{ID: "beneficios", Title: "Saber sobre beneficios", Description: "Rede"}, {ID: "agendar", Title: "Agendar consulta"},
		}}},
	}
	if !reflect.DeepEqual(shown, want) {
		t.Fatalf("interactive = %+v", shown)
	}
}
