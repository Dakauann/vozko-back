package unofficial_whatsapp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"vozko/domain/leadexit"
	"vozko/domain/shared"
)

type linkingLeads struct{}

func (linkingLeads) EnsureLeadForPhone(context.Context, string, string, string) (string, error) {
	return "lead-1", nil
}

type scriptedExitStep struct {
	inbound leadexit.Inbound
	runs    int
	result  leadexit.Result
	err     error
}

func (s *scriptedExitStep) Run(_ context.Context, in leadexit.Inbound, replier leadexit.Replier) (leadexit.Result, error) {
	s.inbound, s.runs = in, s.runs+1
	if replier == nil {
		return leadexit.Result{}, errors.New("no replier")
	}
	return s.result, s.err
}

func TestALeadsMessageGoesThroughTheExitStepFirst(t *testing.T) {
	h := newAutoHarness(t, nil)
	h.uc.leads = linkingLeads{}
	step := &scriptedExitStep{}
	h.uc.SetLeadExitStep(step)

	h.deliverInbound(t)

	in := step.inbound
	if step.runs != 1 || in.WorkspaceID != "ws-1" || in.LeadID != "lead-1" || in.MessageID != "msg-1" || in.Text != "oi" ||
		in.EntryType != string(shared.EntryTypeUnofficialWhatsApp) || in.EntryID == "" || in.SenderNumber == "" || in.ReceivedAt.IsZero() {
		t.Fatalf("inbound = %+v (runs %d)", in, step.runs)
	}
	if len(h.ai.all()) != 1 || len(h.workflows.all()) == 0 {
		t.Fatalf("automation did not run: ai %d, workflows %d", len(h.ai.all()), len(h.workflows.all()))
	}
}

func TestAfterAnOptOutTheAgentIsSkippedButWorkflowsStillSeeTheMessage(t *testing.T) {
	h := newAutoHarness(t, nil)
	h.uc.leads = linkingLeads{}
	h.uc.SetLeadExitStep(&scriptedExitStep{result: leadexit.Result{OptedOut: true}})

	h.deliverInbound(t)

	if len(h.ai.all()) != 0 {
		t.Fatalf("the agent answered an opted-out lead: %+v", h.ai.all())
	}
	if len(h.workflows.all()) == 0 {
		t.Fatal("workflows must still see the message")
	}
}

func TestALeadsMessageWithoutTheExitStepFailsClosed(t *testing.T) {
	h := newAutoHarness(t, nil)
	h.uc.leads = linkingLeads{}
	body := inboundBody(t)

	if err := h.uc.Execute(context.Background(), &QueuedEvent{InstanceID: "inst-1", Body: body}); !errors.Is(err, errLeadExitStepMissing) {
		t.Fatalf("got %v", err)
	}
	if len(h.ai.all()) != 0 || len(h.workflows.all()) != 0 {
		t.Fatal("automation ran without the exit check")
	}
}

func inboundBody(t *testing.T) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"event": "messages", "instance": "prov-1",
		"data": map[string]any{
			"messageid": "msg-2", "chatid": "558494409624@s.whatsapp.net", "sender": "558494409624@s.whatsapp.net",
			"sender_pn": "558494409624@s.whatsapp.net", "senderName": "Dakauann", "isGroup": false, "messageType": "text",
			"text": "sair", "messageTimestamp": time.Now().UnixMilli(),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestTheExitStepNamesTheStoredInboundRowForAWorkflowItStarts(t *testing.T) {
	h := newAutoHarness(t, nil)
	h.uc.leads = linkingLeads{}
	h.uc.messages = storedInbound{byProvider: map[string]string{"unofficial_whatsapp|msg-1": "row-1"}}
	step := &scriptedExitStep{}
	h.uc.SetLeadExitStep(step)

	h.deliverInbound(t)

	if step.inbound.TriggerMessageID != "row-1" {
		t.Fatalf("trigger message = %q", step.inbound.TriggerMessageID)
	}
}
