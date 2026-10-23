package carrierworkflow

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/carriermessaging"
	"vozko/domain/conversation"
	lead_domain "vozko/domain/lead"
	"vozko/domain/rcs"
	carriermessaging_usecase "vozko/usecases/carriermessaging"
	"vozko/usecases/workflow/node_executors"
)

type recordingDeliverer struct {
	requests []carriermessaging_usecase.DeliverRequest
	err      error
}

func (d *recordingDeliverer) Deliver(_ context.Context, req carriermessaging_usecase.DeliverRequest) (*carriermessaging.Message, error) {
	d.requests = append(d.requests, req)
	if d.err != nil {
		return nil, d.err
	}
	return &carriermessaging.Message{ID: "msg-1", Status: carriermessaging.StatusPending}, nil
}

func TestAWorkflowDeliveryIsAWorkflowRunOfTheCarrierEngine(t *testing.T) {
	deliverer := &recordingDeliverer{}
	contact := &lead_domain.Lead{ID: "lead-1"}
	message, err := NewSender(deliverer).DeliverFromWorkflow(context.Background(), node_executors.CarrierDelivery{
		WorkspaceID: "ws-1", Channel: carriermessaging.ChannelRCS, SenderID: "snd-1", Phone: "5511987654321", ContactName: "Ana",
		Lead: contact, Marketing: true, RunID: "run-1", Content: rcs.Content{Text: "Oi"}, SentBy: conversation.SentByWorkflow("wf-1"),
	})
	if err != nil || message.ID != "msg-1" {
		t.Fatalf("message = %+v, err = %v", message, err)
	}
	req := deliverer.requests[0]
	if req.RunKind != carriermessaging.RunWorkflow || req.RunID != "run-1" || req.WorkspaceID != "ws-1" || req.Channel != carriermessaging.ChannelRCS ||
		req.SenderID != "snd-1" || req.Phone != "5511987654321" || req.ContactName != "Ana" || req.Lead != contact || !req.Marketing ||
		req.Content.Text != "Oi" || req.SentBy != conversation.SentByWorkflow("wf-1") || len(req.Vars) != 0 || req.MessageID != "" {
		t.Fatalf("request = %+v", req)
	}
}

func TestACarrierRefusalReachesTheWorkflow(t *testing.T) {
	refused := errors.New("refused")
	if _, err := NewSender(&recordingDeliverer{err: refused}).DeliverFromWorkflow(context.Background(), node_executors.CarrierDelivery{}); !errors.Is(err, refused) {
		t.Fatalf("err = %v", err)
	}
}
