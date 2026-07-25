// Package alerting holds the concrete OpsAlerter sinks. The high-severity operational alert (an
// unconfirmed 360dialog cancellation, a reconciliation divergence) needs a human; until a paging sink
// (Slack, Sentry, PagerDuty) is chosen, the default logs at high severity so the signal is at least in
// the logs and searchable. Swapping the sink is a wiring change, not a code change in the usecases.
package alerting

import (
	"context"
	"log"

	billing "vozko/domain/billing"
)

// LogOpsAlerter is the default OpsAlerter: it logs every alert at a distinctive high-severity prefix.
// It never returns an error (a logging sink cannot fail the caller), so a sweep or reconcile loop is
// never blocked by alerting.
type LogOpsAlerter struct{}

var _ billing.OpsAlerter = (*LogOpsAlerter)(nil)

func NewLogOpsAlerter() *LogOpsAlerter { return &LogOpsAlerter{} }

func (a *LogOpsAlerter) Alert(_ context.Context, subject, detail string) error {
	log.Printf("[OPS-ALERT] %s | %s", subject, detail)
	return nil
}
