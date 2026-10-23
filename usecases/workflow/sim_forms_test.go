package workflow_usecase

import (
	"context"
	"errors"
	"strings"
	"testing"

	wf "vozko/domain/whatsappform"
	"vozko/domain/workflow"
	whatsappform_usecase "vozko/usecases/whatsappform"
)

func TestTheSimulatedFormSenderShowsTheFormMessage(t *testing.T) {
	outbound := make(chan SimOutboundMessage, 1)
	sender := newSimFormSender(newSimChannel(sendInto(outbound), nil))

	send, err := sender.SendFromWorkflow(context.Background(), whatsappform_usecase.WorkflowSend{
		FormID: "form-1", RunID: "run-1", Copy: wf.SendCopy{Body: "Preencha", CTA: "Abrir"},
	})

	if err != nil || send.FormID != "form-1" || send.WorkflowRunID != "run-1" || send.MessageID == "" || send.Status != wf.SendSent {
		t.Fatalf("send = %+v, err = %v", send, err)
	}
	msg := <-outbound
	if msg.Type != simFormMessageType || msg.Text != "Preencha" || msg.MessageID != send.MessageID {
		t.Fatalf("message = %+v", msg)
	}

	if _, err := sender.SendFromWorkflow(context.Background(), whatsappform_usecase.WorkflowSend{FormID: "form-1", RunID: "run-1"}); !errors.Is(err, wf.ErrCopyInvalid) {
		t.Fatalf("an invalid copy = %v", err)
	}
	if len(outbound) != 0 {
		t.Fatal("an invalid copy still reached the chat")
	}
}

func TestASimulatedFormAnswerFillsTheRunLikeARealOne(t *testing.T) {
	cases := []struct {
		name     string
		reply    simFormReply
		answered bool
		want     map[string]interface{}
	}{
		{"typed answers", simFormReply{Answers: map[string]interface{}{"email": "ana@example.com", " cidade ": "Recife", " ": "x"}}, true,
			map[string]interface{}{"form.email": "ana@example.com", "form.cidade": "Recife"}},
		{"submitted empty", simFormReply{}, true, map[string]interface{}{}},
		{"never answered", simFormReply{Outcome: workflow.FormHandleNoReply}, false, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.reply.answered() != tc.answered {
				t.Fatalf("answered = %v", tc.reply.answered())
			}
			if !tc.answered {
				return
			}
			data := tc.reply.runData()
			response, _ := data[workflow.DataKeyFormResponseID].(string)
			if !strings.HasPrefix(response, "sim-response-") {
				t.Fatalf("response id = %v", data[workflow.DataKeyFormResponseID])
			}
			if len(data) != len(tc.want)+1 {
				t.Fatalf("data = %+v", data)
			}
			for k, v := range tc.want {
				if data[k] != v {
					t.Fatalf("data[%s] = %v, want %v", k, data[k], v)
				}
			}
		})
	}
}

func TestTheSimulationWaitsAsLongAsProduction(t *testing.T) {
	cases := []struct {
		config map[string]interface{}
		want   int
	}{
		{map[string]interface{}{"timeout_seconds": float64(90)}, 90},
		{map[string]interface{}{"timeout_minutes": float64(2)}, 120},
		{map[string]interface{}{}, 300},
	}
	for _, tc := range cases {
		if got := simTimeoutSeconds(&workflow.Node{Config: tc.config}); got != tc.want {
			t.Errorf("simTimeoutSeconds(%v) = %d, want %d", tc.config, got, tc.want)
		}
	}
}

func TestTheTimeoutOutcomeMatchesProductionForEveryParkedNode(t *testing.T) {
	cases := []struct {
		node workflow.NodeType
		want string
	}{
		{workflow.NodeTypeWaitForReply, "timeout"},
		{workflow.NodeTypeActionSendInteractive, workflow.InteractiveHandleNoReply},
		{workflow.NodeTypeActionSendForm, workflow.FormHandleNoReply},
	}
	for _, tc := range cases {
		if got := unansweredOutcome(tc.node); got != tc.want {
			t.Errorf("unansweredOutcome(%s) = %q, want %q", tc.node, got, tc.want)
		}
	}
}
