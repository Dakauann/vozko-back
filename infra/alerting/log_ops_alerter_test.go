package alerting

import (
	"context"
	"testing"
)

// TestLogOpsAlerter_NeverErrors: the default sink is a logger, so it must never fail the caller (a
// sweep or reconcile loop must not be blocked by alerting).
func TestLogOpsAlerter_NeverErrors(t *testing.T) {
	a := NewLogOpsAlerter()
	if err := a.Alert(context.Background(), "billing: channel cancellation failed", "workspace ws-1"); err != nil {
		t.Fatalf("the log sink must never return an error, got %v", err)
	}
}
