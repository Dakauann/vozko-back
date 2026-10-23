package carrierworkflow

import (
	"context"

	"vozko/domain/carriermessaging"
	carriermessaging_usecase "vozko/usecases/carriermessaging"
	"vozko/usecases/workflow/node_executors"
)

type CarrierSender interface {
	Deliver(ctx context.Context, req carriermessaging_usecase.DeliverRequest) (*carriermessaging.Message, error)
}

type sender struct {
	carrier CarrierSender
}

func NewSender(carrier CarrierSender) node_executors.CarrierSender {
	return &sender{carrier: carrier}
}

func (s *sender) DeliverFromWorkflow(ctx context.Context, d node_executors.CarrierDelivery) (*carriermessaging.Message, error) {
	return s.carrier.Deliver(ctx, carriermessaging_usecase.DeliverRequest{
		WorkspaceID: d.WorkspaceID,
		Channel:     d.Channel,
		SenderID:    d.SenderID,
		Phone:       d.Phone,
		ContactName: d.ContactName,
		Lead:        d.Lead,
		RunKind:     carriermessaging.RunWorkflow,
		Marketing:   d.Marketing,
		RunID:       d.RunID,
		Content:     d.Content,
		Text:        d.Text,
		SentBy:      d.SentBy,
	})
}
