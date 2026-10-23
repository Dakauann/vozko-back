package carriermessaging_usecase

import (
	"context"

	"vozko/domain/carriermessaging"
	"vozko/domain/rcs"
	"vozko/domain/shared"
	"vozko/domain/sms"
)

type RCSQuote struct {
	Kind          rcs.ContentKind
	Class         rcs.BillingClass
	VariableSlots int
	Issues        []rcs.Issue
	Charge        carriermessaging.ChargeKind
	UnitMicros    int64
}

func (s *Service) QuoteRCS(ctx context.Context, workspaceID string, content rcs.Content) (RCSQuote, error) {
	class := content.BillingClass()
	charge := carriermessaging.ChargeForClass(class)
	price, err := s.Charges.Price(workspaceID, charge)
	if err != nil {
		return RCSQuote{}, err
	}
	return RCSQuote{
		Kind: content.Kind(), Class: class, VariableSlots: content.VariableSlots(), Issues: content.Issues(),
		Charge: charge, UnitMicros: price.PriceMicros,
	}, nil
}

type SMSQuote struct {
	Text          string
	Measurement   sms.Measurement
	VariableSlots int
	Issues        []error
	Charge        carriermessaging.ChargeKind
	UnitMicros    int64
	TotalMicros   int64
}

func (s *Service) QuoteSMS(ctx context.Context, workspaceID, text, routeID string) (SMSQuote, error) {
	composed := text
	if routeID != "" {
		route, err := s.Repos.Routes.FindByID(ctx, workspaceID, routeID)
		if err != nil {
			return SMSQuote{}, err
		}
		composed = route.Compose(text)
	}
	price, err := s.Charges.Price(workspaceID, carriermessaging.ChargeSMSSegment)
	if err != nil {
		return SMSQuote{}, err
	}
	q := SMSQuote{
		Text: composed, Measurement: sms.Measure(composed), VariableSlots: shared.HighestPositionalParameter([]string{text}),
		Charge: carriermessaging.ChargeSMSSegment, UnitMicros: price.PriceMicros,
	}
	if err := sms.Validate(text); err != nil {
		q.Issues = append(q.Issues, err)
	} else if err := sms.Validate(composed); err != nil {
		q.Issues = append(q.Issues, err)
	}
	q.TotalMicros = price.PriceMicros * int64(q.Measurement.Segments)
	return q, nil
}
