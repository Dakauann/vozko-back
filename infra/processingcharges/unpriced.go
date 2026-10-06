package processingcharges

import (
	"context"

	"vozko/domain/mediagen"
)

type Unpriced struct{}

var _ mediagen.ProcessingCharges = Unpriced{}

func (Unpriced) Charge(context.Context, *mediagen.Job) error { return nil }
