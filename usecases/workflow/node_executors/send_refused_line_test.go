package node_executors

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/conversation"
	ce "vozko/domain/conversation_event"
	lead_domain "vozko/domain/lead"
	"vozko/domain/shared"
	wo "vozko/domain/whatsapp_outreach"
	"vozko/domain/workflow"
)

type recordedLines struct{ events []*ce.ConversationEvent }

func (r *recordedLines) Log(e *ce.ConversationEvent) { r.events = append(r.events, e) }

func refusedRunAt(nodeID string) *workflow.WorkflowRun {
	run := waMediaRun()
	run.WorkflowID, run.CurrentNodeID, run.State = "wf-1", nodeID, workflow.NewRunState()
	return run
}

func TestARefusedWorkflowSendWritesTheRefusalLineOnceForItsStep(t *testing.T) {
	lines := &recordedLines{}
	sender := guardedSender(optedOutLead(), &countingWhatsAppClient{}, &recordingHistory{})
	sender.deps.Events = lines
	run := refusedRunAt("send-1")

	for attempt := 0; attempt < 3; attempt++ {
		if _, _, err := sender.SendText(context.Background(), run, "Promoção!", conversation.MessageTypeAIResponse); !errors.Is(err, wo.ErrLeadOptedOut) {
			t.Fatalf("attempt %d: err = %v", attempt, err)
		}
	}

	if len(lines.events) != 1 {
		t.Fatalf("lines = %d, want one for the refused step", len(lines.events))
	}
	line := lines.events[0]
	details := line.DetailsMap()
	if line.EventType != ce.EventLeadSendRefused || line.WorkspaceID != "ws-1" || line.EntryID != run.EntryID || line.EntryType != run.EntryType ||
		line.CorrelationID != "run-1" || line.ActorID != "workflow:wf-1" {
		t.Fatalf("line = %+v", line)
	}
	if details["workflow_id"] != "wf-1" || details["run_id"] != "run-1" || details["node_id"] != "send-1" || details["reason"] != "lead_opted_out" ||
		details["lead_id"] != "lead-1" {
		t.Fatalf("details = %+v", details)
	}

	run.CurrentNodeID = "send-2"
	if _, _, err := sender.SendText(context.Background(), run, "Outra", conversation.MessageTypeAIResponse); !errors.Is(err, wo.ErrLeadOptedOut) {
		t.Fatalf("second step err = %v", err)
	}
	if len(lines.events) != 2 {
		t.Fatalf("a refusal on another step writes its own line, got %d", len(lines.events))
	}
}

func TestATemplateStepRefusedForABlockedLeadWritesTheLine(t *testing.T) {
	lines := &recordedLines{}
	sender := senderWithLeads(&knownLeadRepo{byID: blockedLead()}, &recordingTemplateSends{}, &recordingHistory{})
	sender.deps.Events = lines
	exec := &sendTemplateExecutor{sender: sender}
	run := refusedRunAt("tmpl-1")
	node := &workflow.Node{ID: "tmpl-1", Type: workflow.NodeTypeActionSendTemplate, Config: map[string]interface{}{"template_id": "t1"}}

	if _, err := exec.Execute(&workflow.NodeContext{Node: node, Run: run, State: &run.State}); !errors.Is(err, wo.ErrLeadBlocked) {
		t.Fatalf("err = %v", err)
	}
	if len(lines.events) != 1 || lines.events[0].DetailsMap()["reason"] != "lead_blocked" {
		t.Fatalf("lines = %+v", lines.events)
	}
}

func TestAnAdapterChannelSendRefusedForAnOptedOutLeadWritesTheLine(t *testing.T) {
	lines := &recordedLines{}
	adapter := &fakeAdapter{entryType: shared.EntryTypeInstagram, windowOpen: true}
	sender := igSender(adapter, &entryLeads{leadID: "lead-1"}, &knownLeadRepo{byID: optedOutLead()})
	sender.lines = refusalLines{events: lines}
	run := igRun()
	run.WorkflowID, run.CurrentNodeID, run.State = "wf-1", "send-ig", workflow.NewRunState()

	if _, err := sender.SendText(context.Background(), run, "Oi", conversation.MessageTypeAIResponse); !errors.Is(err, wo.ErrLeadOptedOut) {
		t.Fatalf("err = %v", err)
	}
	if len(lines.events) != 1 || lines.events[0].EntryType != string(shared.EntryTypeInstagram) || lines.events[0].DetailsMap()["node_id"] != "send-ig" {
		t.Fatalf("lines = %+v", lines.events)
	}
}

func TestNoLineIsWrittenWhenTheSendGoesOutOrTheLeadCannotBeChecked(t *testing.T) {
	lines := &recordedLines{}
	reachable := guardedSender(reachableLead(), &countingWhatsAppClient{}, &recordingHistory{})
	reachable.deps.Events = lines
	if _, _, err := reachable.SendText(context.Background(), refusedRunAt("send-1"), "Oi", conversation.MessageTypeAIResponse); err != nil {
		t.Fatal(err)
	}

	unreadable := igSender(&fakeAdapter{entryType: shared.EntryTypeInstagram, windowOpen: true}, &entryLeads{err: errors.New("db down")}, &knownLeadRepo{})
	unreadable.lines = refusalLines{events: lines}
	run := igRun()
	run.State = workflow.NewRunState()
	if _, err := unreadable.SendText(context.Background(), run, "Oi", conversation.MessageTypeAIResponse); !errors.Is(err, errRecipientRefused) {
		t.Fatalf("err = %v", err)
	}
	if len(lines.events) != 0 {
		t.Fatalf("lines = %+v, want none", lines.events)
	}
}

func TestTheChannelSenderBuiltFromTheWhatsAppSenderKeepsItsLines(t *testing.T) {
	lines := &recordedLines{}
	wa := guardedSender(&lead_domain.Lead{ID: "lead-1"}, &countingWhatsAppClient{}, &recordingHistory{})
	wa.deps.Events = lines
	if got := newChannelSenderFromWhatsApp(wa).lines.events; got != lines {
		t.Fatalf("lines = %v", got)
	}
	if got := newChannelSender(SenderDeps{Events: lines}).lines.events; got != lines {
		t.Fatalf("lines = %v", got)
	}
}
