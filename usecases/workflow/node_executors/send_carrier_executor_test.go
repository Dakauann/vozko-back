package node_executors

import (
	"context"
	"strings"
	"testing"
	"time"

	"vozko/domain/carriermessaging"
	"vozko/domain/conversation"
	lead_domain "vozko/domain/lead"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

type recordingCarrier struct {
	requests []CarrierDelivery
	message  *carriermessaging.Message
	err      error
}

func (c *recordingCarrier) DeliverFromWorkflow(_ context.Context, req CarrierDelivery) (*carriermessaging.Message, error) {
	c.requests = append(c.requests, req)
	if c.err != nil {
		return nil, c.err
	}
	if c.message != nil {
		return c.message, nil
	}
	return &carriermessaging.Message{ID: "msg-1", Status: carriermessaging.StatusAccepted}, nil
}

type carrierLeadBook map[string]*lead_domain.Lead

func (b carrierLeadBook) FindByID(_, id string) (*lead_domain.Lead, error) {
	if found, ok := b[id]; ok {
		return found, nil
	}
	return nil, lead_domain.ErrLeadNotFound
}

type carrierEntryLeads map[string]string

func (e carrierEntryLeads) LeadOfEntry(_ context.Context, _ string, ref shared.EntryRef) (string, error) {
	return e[ref.EntryID], nil
}

func carrierContext(run *workflow.WorkflowRun, config map[string]interface{}, labels ...string) *workflow.NodeContext {
	node := &workflow.Node{ID: "send", Type: workflow.NodeTypeActionSendCarrier, Config: config}
	graph := &workflow.Graph{Nodes: []workflow.Node{*node}}
	for _, label := range labels {
		graph.Edges = append(graph.Edges, workflow.Edge{Source: "send", Target: "to_" + label, Label: label})
	}
	state := workflow.NewRunState()
	state.Set("lead_name", "Ana")
	state.Set("event", map[string]interface{}{"cart_url": "https://loja.example/c/9"})
	return &workflow.NodeContext{Run: run, Node: node, Graph: graph, State: &state, Workflow: &workflow.Workflow{ID: "wf-1"}}
}

func leadRun() *workflow.WorkflowRun {
	return &workflow.WorkflowRun{ID: "run-1", WorkflowID: "wf-1", WorkspaceID: "ws-1", EntryID: "lead-1", EntryType: workflow.EntryTypeLead}
}

func knownCarrierLeads() carrierLeadBook {
	return carrierLeadBook{
		"lead-1": {ID: "lead-1", WorkspaceID: "ws-1", Number: "5511987654321", Name: "Ana"},
		"lead-2": {ID: "lead-2", WorkspaceID: "ws-1", Name: "Sem telefone"},
	}
}

func rcsNodeConfig() map[string]interface{} {
	return map[string]interface{}{
		workflow.CarrierChannelKey: "rcs",
		workflow.CarrierSenderKey:  "snd-1",
		workflow.CarrierContentKey: map[string]interface{}{
			"text":        "Oi {{lead_name}}, seu carrinho",
			"suggestions": []interface{}{map[string]interface{}{"kind": "open_url", "label": "Abrir", "url": "{{event.cart_url}}"}},
		},
	}
}

func TestSendCarrierDeliversRCSToTheLeadOfALeadRun(t *testing.T) {
	carrier := &recordingCarrier{}
	exec := NewSendCarrierExecutor(CarrierNodeDeps{Sender: carrier, Leads: knownCarrierLeads()})
	result, err := exec.Execute(carrierContext(leadRun(), rcsNodeConfig(), workflow.CarrierHandleSent, workflow.CarrierHandleFailed))
	if err != nil {
		t.Fatal(err)
	}
	if len(carrier.requests) != 1 {
		t.Fatalf("requests = %d", len(carrier.requests))
	}
	req := carrier.requests[0]
	if req.WorkspaceID != "ws-1" || req.Channel != carriermessaging.ChannelRCS || req.SenderID != "snd-1" || req.Phone != "5511987654321" ||
		req.Lead == nil || req.Lead.ID != "lead-1" || req.RunID != "run-1" || !req.Marketing ||
		req.SentBy != conversation.SentByWorkflow("wf-1") || req.Text != "" {
		t.Fatalf("request = %+v", req)
	}
	if req.Content.Text != "Oi Ana, seu carrinho" || req.Content.Suggestions[0].URL != "https://loja.example/c/9" {
		t.Fatalf("content = %+v", req.Content)
	}
	if result.NextNodeID != "to_sent" || result.Output["sent"] != true || result.Output["message_id"] != "msg-1" || result.Output["channel"] != "rcs" {
		t.Fatalf("result = %+v", result)
	}
}

func TestSendCarrierDeliversSMSToTheLeadOfAConversation(t *testing.T) {
	carrier := &recordingCarrier{}
	run := &workflow.WorkflowRun{ID: "run-2", WorkflowID: "wf-1", WorkspaceID: "ws-1", EntryID: "entry-9", EntryType: "whatsapp"}
	config := map[string]interface{}{
		workflow.CarrierChannelKey:   "sms",
		workflow.CarrierRouteKey:     "rt-1",
		workflow.CarrierTextKey:      "Oi {{lead_name}}",
		workflow.CarrierMarketingKey: false,
	}
	exec := NewSendCarrierExecutor(CarrierNodeDeps{Sender: carrier, Leads: knownCarrierLeads(), EntryLeads: carrierEntryLeads{"entry-9": "lead-1"}})
	result, err := exec.Execute(carrierContext(run, config, workflow.CarrierHandleSent))
	if err != nil {
		t.Fatal(err)
	}
	req := carrier.requests[0]
	if req.Channel != carriermessaging.ChannelSMS || req.SenderID != "rt-1" || req.Text != "Oi Ana" || req.Marketing || req.Lead.ID != "lead-1" {
		t.Fatalf("request = %+v", req)
	}
	if result.NextNodeID != "to_sent" {
		t.Fatalf("result = %+v", result)
	}
}

func TestSendCarrierRoutesEveryRefusalToFailedWithItsReason(t *testing.T) {
	refused := &recordingCarrier{err: carriermessaging.ErrSuppressed}
	rejected := &recordingCarrier{message: &carriermessaging.Message{ID: "msg-9", Status: carriermessaging.StatusRejected, ErrorCode: "42"}}
	cases := []struct {
		name   string
		deps   CarrierNodeDeps
		run    *workflow.WorkflowRun
		config map[string]interface{}
		reason string
	}{
		{"no carrier wired", CarrierNodeDeps{Leads: knownCarrierLeads()}, leadRun(), rcsNodeConfig(), CarrierReasonUnavailable},
		{"unreadable config", CarrierNodeDeps{Sender: &recordingCarrier{}, Leads: knownCarrierLeads()}, leadRun(), map[string]interface{}{workflow.CarrierChannelKey: "fax"}, CarrierReasonConfigUnreadable},
		{"lead gone", CarrierNodeDeps{Sender: &recordingCarrier{}, Leads: carrierLeadBook{}}, leadRun(), rcsNodeConfig(), CarrierReasonLeadNotFound},
		{"conversation without a lead", CarrierNodeDeps{Sender: &recordingCarrier{}, Leads: knownCarrierLeads(), EntryLeads: carrierEntryLeads{}},
			&workflow.WorkflowRun{ID: "r", WorkspaceID: "ws-1", EntryID: "entry-x", EntryType: "whatsapp"}, rcsNodeConfig(), CarrierReasonLeadNotFound},
		{"lead without a phone", CarrierNodeDeps{Sender: &recordingCarrier{}, Leads: knownCarrierLeads()},
			&workflow.WorkflowRun{ID: "r", WorkspaceID: "ws-1", EntryID: "lead-2", EntryType: workflow.EntryTypeLead}, rcsNodeConfig(), CarrierReasonPhoneMissing},
		{"carrier refusal", CarrierNodeDeps{Sender: refused, Leads: knownCarrierLeads()}, leadRun(), rcsNodeConfig(), CarrierReasonRefused},
		{"provider rejection", CarrierNodeDeps{Sender: rejected, Leads: knownCarrierLeads()}, leadRun(), rcsNodeConfig(), CarrierReasonRejected},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := NewSendCarrierExecutor(tc.deps).Execute(carrierContext(tc.run, tc.config, workflow.CarrierHandleSent, workflow.CarrierHandleFailed))
			if err != nil {
				t.Fatal(err)
			}
			if result.NextNodeID != "to_failed" || result.Output["sent"] != false || result.Output["reason"] != tc.reason {
				t.Fatalf("result = %+v", result)
			}
			if text, _ := result.Output["error"].(string); text == "" {
				t.Fatalf("the refusal text must be kept: %+v", result.Output)
			}
		})
	}
}

func TestSendCarrierFailsTheStepWhenTheFailedOutputIsNotConnected(t *testing.T) {
	result, err := NewSendCarrierExecutor(CarrierNodeDeps{Leads: knownCarrierLeads()}).Execute(carrierContext(leadRun(), rcsNodeConfig(), workflow.CarrierHandleSent))
	if err != nil {
		t.Fatal(err)
	}
	if result.NextNodeID != "" || !strings.Contains(result.Error, workflow.CarrierHandleFailed) || result.ErrorKind != workflow.ErrorKindConfig {
		t.Fatalf("result = %+v", result)
	}
}

func TestSendCarrierDefinitionExposesSentAndFailed(t *testing.T) {
	def := NewSendCarrierExecutor(CarrierNodeDeps{}).Definition()
	if def.Type != workflow.NodeTypeActionSendCarrier || def.Category != workflow.NodeCategoryMessaging || def.TestBehavior != workflow.NodeTestSendsMessage {
		t.Fatalf("def = %+v", def)
	}
	if len(def.Outputs) != 2 || def.Outputs[0].ID != workflow.CarrierHandleSent || def.Outputs[1].ID != workflow.CarrierHandleFailed {
		t.Fatalf("outputs = %+v", def.Outputs)
	}
	if def.DefaultConfig[workflow.CarrierMarketingKey] != true || def.DefaultConfig[workflow.CarrierChannelKey] != defaultCarrierChannel() {
		t.Fatalf("defaults = %+v", def.DefaultConfig)
	}
	required := map[string]string{}
	for _, f := range def.ConfigSchema {
		if f.RequiredWhen != nil {
			required[f.Key] = f.RequiredWhen.Values[0]
		}
	}
	if required[workflow.CarrierSenderKey] != "rcs" || required[workflow.CarrierRouteKey] != "sms" || required[workflow.CarrierTextKey] != "sms" {
		t.Fatalf("required when = %+v", required)
	}
}

func TestAnUnboundCarrierRelayFailsTheSendAsUnavailable(t *testing.T) {
	relay := NewCarrierSenderRelay()
	exec := NewSendCarrierExecutor(CarrierNodeDeps{Sender: relay, Leads: knownCarrierLeads()})
	result, err := exec.Execute(carrierContext(leadRun(), rcsNodeConfig(), workflow.CarrierHandleSent, workflow.CarrierHandleFailed))
	if err != nil || result.Output["reason"] != CarrierReasonUnavailable {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	carrier := &recordingCarrier{}
	relay.Bind(carrier)
	result, err = exec.Execute(carrierContext(leadRun(), rcsNodeConfig(), workflow.CarrierHandleSent, workflow.CarrierHandleFailed))
	if err != nil || result.NextNodeID != "to_sent" || len(carrier.requests) != 1 {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
}

func TestAMarketingSendOutsideTheSendHoursSaysWhenItCouldGo(t *testing.T) {
	next := time.Date(2026, 10, 22, 11, 0, 0, 0, time.UTC)
	carrier := &recordingCarrier{err: &carriermessaging.QuietHoursError{NextOpen: next}}
	result, err := NewSendCarrierExecutor(CarrierNodeDeps{Sender: carrier, Leads: knownCarrierLeads()}).
		Execute(carrierContext(leadRun(), rcsNodeConfig(), workflow.CarrierHandleSent, workflow.CarrierHandleFailed))
	if err != nil {
		t.Fatal(err)
	}
	if result.NextNodeID != "to_failed" || result.Output["reason"] != CarrierReasonQuietHours || result.Output["next_open"] != "2026-10-22T11:00:00Z" {
		t.Fatalf("result = %+v", result)
	}
}

func TestSendCarrierOffersOnlyLaunchedChannels(t *testing.T) {
	def := NewSendCarrierExecutor(CarrierNodeDeps{}).Definition()
	var offered []string
	for _, f := range def.ConfigSchema {
		if f.Key == workflow.CarrierChannelKey {
			for _, o := range f.Options {
				offered = append(offered, o.Value)
			}
		}
	}
	launched := carriermessaging.LaunchedChannels()
	if len(offered) != len(launched) {
		t.Fatalf("offered %v, launched %v", offered, launched)
	}
	for i, c := range launched {
		if offered[i] != string(c) {
			t.Fatalf("offered %v, launched %v", offered, launched)
		}
	}
}

func TestDefaultCarrierChannelIsTheFirstLaunched(t *testing.T) {
	launched := carriermessaging.LaunchedChannels()
	want := ""
	if len(launched) > 0 {
		want = string(launched[0])
	}
	if got := defaultCarrierChannel(); got != want {
		t.Fatalf("default = %q, want %q", got, want)
	}
}
