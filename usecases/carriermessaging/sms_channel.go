package carriermessaging_usecase

import (
	"context"

	"vozko/domain/shared"
	"vozko/domain/sms"
)

func (s *Service) prepareSMS(ctx context.Context, req DeliverRequest) (outbound, error) {
	route, err := s.Repos.Routes.FindByID(ctx, req.WorkspaceID, req.SenderID)
	if err != nil {
		return outbound{}, err
	}
	if !route.CanSend() {
		return outbound{}, sms.ErrRouteCannotSend
	}
	from := route.From(s.SMSSharedNumber)
	if from == "" {
		return outbound{}, sms.ErrNoSharedNumber
	}
	body := shared.RenderPositional(req.Text, req.Vars)
	if err := sms.Validate(body); err != nil {
		return outbound{}, err
	}
	text := route.Compose(body)
	if err := sms.Validate(text); err != nil {
		return outbound{}, err
	}
	return outbound{senderID: route.ID, from: from, text: text, segments: sms.Measure(text).Segments}, nil
}
