package billing

import "context"

// OpsAlerter raises a high-severity operational alert that needs a human, separate from any
// customer-facing notification. The motivating case is an unconfirmed 360dialog cancellation: until
// someone fixes it, the platform keeps paying the vendor for a channel the customer did not pay for, so a
// silent failure there is the worst financial outcome. Reconciliation drift uses it too.
//
// The concrete sink (Slack, Sentry, PagerDuty) is a wiring choice; a default impl logs at high
// severity and may also email. Alert must never panic and should be safe to call from a sweep loop.
type OpsAlerter interface {
	Alert(ctx context.Context, subject, detail string) error
}
