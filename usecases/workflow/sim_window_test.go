package workflow_usecase

import (
	"context"
	"testing"

	"vozko/domain/workflow"
)

func greetingWorkflow(trigger workflow.Node) *workflow.Workflow {
	return &workflow.Workflow{
		ID: "wf-window", WorkspaceID: "ws1", Name: "Saudação", Type: workflow.WorkflowTypeMessages, Status: workflow.WorkflowStatusDraft,
		Graph: workflow.Graph{
			Nodes: []workflow.Node{
				trigger,
				{ID: "ola", Type: workflow.NodeTypeActionSendText, Config: map[string]interface{}{"text": "Olá!", "display_name": "Boas-vindas"}},
				{ID: "end", Type: workflow.NodeTypeEnd},
			},
			Edges: []workflow.Edge{{Source: trigger.ID, Target: "ola"}, {Source: "ola", Target: "end"}},
		},
	}
}

type attributedSend struct {
	NodeID   string `json:"nodeId"`
	NodeName string `json:"nodeName"`
	Text     string `json:"text"`
}

func TestSimReadyTellsTheDefaultWindowOfEachTrigger(t *testing.T) {
	sim := startDebugSimulation(t, debugSimOptions{workflow: multiTriggerWorkflow(), awaitStart: true})
	ready := decode[readyFrame](t, firstFrame(t, sim, "sim_ready"))
	want := map[string]workflow.ServiceWindow{"agente": workflow.ServiceWindowOpen, "hook": workflow.ServiceWindowClosed, "msg": workflow.ServiceWindowOpen, "etapa": workflow.ServiceWindowClosed}
	for _, spec := range ready.Triggers {
		if spec.Window == nil || *spec.Window != want[spec.NodeID] {
			t.Fatalf("%s window = %v, want %s", spec.NodeID, spec.Window, want[spec.NodeID])
		}
	}
}

func TestAWebhookTestFailsAFreeFormSendLikeProductionWhenTheWindowIsClosed(t *testing.T) {
	sim := startDebugSimulation(t, debugSimOptions{workflow: greetingWorkflow(workflow.Node{ID: "hook", Type: workflow.NodeTypeTriggerWebhook}), awaitStart: true})
	readUntil(t, sim.conn, "sim_ready")
	sim.send(t, "start", map[string]interface{}{"triggerNodeId": "hook", "input": map[string]interface{}{}})

	_, frames := readUntil(t, sim.conn, "run_error")
	for _, f := range frames {
		if f.Type == "message_sent" {
			t.Fatalf("a message left the closed window: %s", f.Payload)
		}
	}
	steps := stepsIn(frames)
	last := steps[len(steps)-1]
	if last.NodeID != "ola" || last.Status != string(workflow.LogStatusFailed) || last.ErrorKind != string(workflow.ErrorKindWindowClosed) {
		t.Fatalf("step = %+v, want the send refused by the window", last)
	}
}

func TestTheTesterCanOpenTheWindowOfAWebhookStart(t *testing.T) {
	sim := startDebugSimulation(t, debugSimOptions{workflow: greetingWorkflow(workflow.Node{ID: "hook", Type: workflow.NodeTypeTriggerWebhook}), awaitStart: true})
	readUntil(t, sim.conn, "sim_ready")
	sim.send(t, "start", map[string]interface{}{"triggerNodeId": "hook", "input": map[string]interface{}{}, "windowOpen": true})
	readUntil(t, sim.conn, "run_completed")
}

func TestTheTesterCanCloseTheWindowOfAnAgentCall(t *testing.T) {
	sim := startDebugSimulation(t, debugSimOptions{workflow: greetingWorkflow(workflow.Node{ID: "agente", Type: workflow.NodeTypeTriggerAgentCall}), awaitStart: true})
	readUntil(t, sim.conn, "sim_ready")
	sim.send(t, "start", map[string]interface{}{"triggerNodeId": "agente", "windowOpen": false})
	_, frames := readUntil(t, sim.conn, "run_error")
	if steps := stepsIn(frames); steps[len(steps)-1].ErrorKind != string(workflow.ErrorKindWindowClosed) {
		t.Fatalf("steps = %+v", steps)
	}
}

func TestASentMessageNamesItsNodeAndArrivesBeforeThatNodesStep(t *testing.T) {
	sim := startDebugSimulation(t, debugSimOptions{workflow: greetingWorkflow(workflow.Node{ID: "agente", Type: workflow.NodeTypeTriggerAgentCall, Config: map[string]interface{}{"display_name": "Chamado pela Bia"}}), awaitStart: true})
	readUntil(t, sim.conn, "sim_ready")
	sim.send(t, "start", map[string]interface{}{"triggerNodeId": "agente"})

	_, frames := readUntil(t, sim.conn, "run_completed")
	sentAt, stepAt := -1, -1
	for i, f := range frames {
		switch f.Type {
		case "message_sent":
			sent := decode[attributedSend](t, f)
			if sent.NodeID != "ola" || sent.NodeName != "Boas-vindas" || sent.Text != "Olá!" {
				t.Fatalf("message_sent = %+v, want it attributed to the sending node", sent)
			}
			sentAt = i
		case "node_executed":
			if stepsIn([]simFrame{f})[0].NodeID == "ola" {
				stepAt = i
			}
		}
	}
	if sentAt < 0 || stepAt < 0 || sentAt > stepAt {
		t.Fatalf("message_sent at %d, its step at %d, want the message first", sentAt, stepAt)
	}
}

func TestASentMessageFallsBackToTheCatalogNameOfAnUnnamedNode(t *testing.T) {
	wf := greetingWorkflow(workflow.Node{ID: "agente", Type: workflow.NodeTypeTriggerAgentCall})
	delete(wf.Graph.Nodes[1].Config, "display_name")
	sim := startDebugSimulation(t, debugSimOptions{workflow: wf, awaitStart: true})
	readUntil(t, sim.conn, "sim_ready")
	sim.send(t, "start", map[string]interface{}{"triggerNodeId": "agente"})
	sent, _ := readUntil(t, sim.conn, "message_sent")
	if got := decode[attributedSend](t, sent); got.NodeID != "ola" || got.NodeName == "" || got.NodeName == "agente" {
		t.Fatalf("message_sent = %+v, want the send node's catalog name", got)
	}
}

func TestTheNodeTestUsesTheWindowOfThePathTrigger(t *testing.T) {
	cases := []struct {
		name    string
		trigger workflow.NodeType
		kind    workflow.ErrorKind
	}{
		{"a webhook path refuses a free-form send", workflow.NodeTypeTriggerWebhook, workflow.ErrorKindWindowClosed},
		{"a message path sends it", workflow.NodeTypeTriggerMessageReceived, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := NewMockWorkflowRepository()
			wf := greetingWorkflow(workflow.Node{ID: "t", Type: tc.trigger})
			wf.ID = "wf1"
			_ = repo.Create(wf)
			result, err := NewTestNodeUseCase(TestNodeDeps{WorkflowRepo: repo}).Execute(context.Background(), TestNodeInput{WorkflowID: "wf1", WorkspaceID: "ws1", NodeID: "ola"})
			if err != nil {
				t.Fatal(err)
			}
			if result.ErrorKind != tc.kind || result.Success != (tc.kind == "") {
				t.Fatalf("result = %+v", result)
			}
		})
	}
}

func sendInto(ch chan SimOutboundMessage) func(SimOutboundMessage) {
	return func(msg SimOutboundMessage) { ch <- msg }
}

func sentTexts(t *testing.T, frames []simFrame) []string {
	t.Helper()
	var texts []string
	for _, f := range frames {
		if f.Type == "message_sent" {
			texts = append(texts, decode[attributedSend](t, f).Text)
		}
	}
	return texts
}

func TestAMessageTriggerRunsWithTheWindowTheCustomersMessageOpenedWhateverTheStartAsked(t *testing.T) {
	sim := startDebugSimulation(t, debugSimOptions{workflow: greetingWorkflow(workflow.Node{ID: "msg", Type: workflow.NodeTypeTriggerMessageReceived}), awaitStart: true})
	readUntil(t, sim.conn, "sim_ready")
	sim.send(t, "start", map[string]interface{}{"triggerNodeId": "msg", "windowOpen": false})
	readUntil(t, sim.conn, "waiting_trigger")
	sim.send(t, "reply", map[string]string{"text": "A"})

	_, frames := readUntil(t, sim.conn, "run_completed")
	if texts := sentTexts(t, frames); len(texts) != 1 || texts[0] != "Olá!" {
		t.Fatalf("sent = %q, want the greeting inside the window the customer opened", texts)
	}
}

func TestACustomerReplyOpensAClosedWindowLikeAnInboundMessageInProduction(t *testing.T) {
	wf := &workflow.Workflow{
		ID: "wf-reply-window", WorkspaceID: "ws1", Name: "Retorno", Type: workflow.WorkflowTypeMessages, Status: workflow.WorkflowStatusDraft,
		Graph: workflow.Graph{
			Nodes: []workflow.Node{
				{ID: "hook", Type: workflow.NodeTypeTriggerWebhook},
				{ID: "wait", Type: workflow.NodeTypeWaitForReply, Config: map[string]interface{}{"timeout_seconds": float64(60)}},
				{ID: "ola", Type: workflow.NodeTypeActionSendText, Config: map[string]interface{}{"text": "Olá!"}},
				{ID: "end", Type: workflow.NodeTypeEnd},
			},
			Edges: []workflow.Edge{{Source: "hook", Target: "wait"}, {Source: "wait", Target: "ola", Label: "replied"}, {Source: "ola", Target: "end"}},
		},
	}
	sim := startDebugSimulation(t, debugSimOptions{workflow: wf, awaitStart: true})
	readUntil(t, sim.conn, "sim_ready")
	sim.send(t, "start", map[string]interface{}{"triggerNodeId": "hook", "input": map[string]interface{}{}, "windowOpen": false})
	readUntil(t, sim.conn, "waiting_reply")
	sim.send(t, "reply", map[string]string{"text": "oi"})

	_, frames := readUntil(t, sim.conn, "run_completed")
	if texts := sentTexts(t, frames); len(texts) != 1 || texts[0] != "Olá!" {
		t.Fatalf("sent = %q, want the text sent once the customer's reply opened the window", texts)
	}
}

func TestAClosedWindowFailsTheSendOnceWithoutRetries(t *testing.T) {
	sim := startDebugSimulation(t, debugSimOptions{workflow: greetingWorkflow(workflow.Node{ID: "hook", Type: workflow.NodeTypeTriggerWebhook}), awaitStart: true})
	readUntil(t, sim.conn, "sim_ready")
	sim.send(t, "start", map[string]interface{}{"triggerNodeId": "hook", "input": map[string]interface{}{}})

	_, frames := readUntil(t, sim.conn, "run_error")
	attempts := 0
	for _, step := range stepsIn(frames) {
		if step.NodeID == "ola" {
			attempts++
		}
	}
	if attempts != 1 {
		t.Fatalf("send attempts = %d, want one refusal and no retry", attempts)
	}
}

func TestOnlyTriggersWithoutACustomerMessageOfferTheWindowChoice(t *testing.T) {
	sim := startDebugSimulation(t, debugSimOptions{workflow: multiTriggerWorkflow(), awaitStart: true})
	ready := decode[readyFrame](t, firstFrame(t, sim, "sim_ready"))
	want := map[string]bool{"agente": true, "hook": true, "msg": false, "etapa": true}
	for _, spec := range ready.Triggers {
		if spec.WindowEditable != want[spec.NodeID] {
			t.Fatalf("%s windowEditable = %v, want %v", spec.NodeID, spec.WindowEditable, want[spec.NodeID])
		}
	}
}
