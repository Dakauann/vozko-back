package alerting

import (
	"context"
	"log"

	billing "vozko/domain/billing"
)

type LogOpsAlerter struct{}

var _ billing.OpsAlerter = (*LogOpsAlerter)(nil)

func NewLogOpsAlerter() *LogOpsAlerter { return &LogOpsAlerter{} }

func (a *LogOpsAlerter) Alert(_ context.Context, subject, detail string) error {
	log.Printf("[OPS-ALERT] %s | %s", subject, detail)
	return nil
}
