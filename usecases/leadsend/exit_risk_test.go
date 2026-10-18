package leadsend_usecase

import (
	"errors"
	"testing"

	"vozko/domain/buttonaction"
	"vozko/domain/leadaction"
)

func TestASendOfATemplateWithoutAnExitIsRefusedUnlessTheRequesterAcknowledgedTheRisk(t *testing.T) {
	h := newHarness(nil)
	h.seed(3)
	h.world.templateWithoutExit = true

	if _, err := prepare(h, scoped(), leadaction.ActionSendTemplate, templateParams()); !errors.Is(err, buttonaction.ErrExitRiskNotAcknowledged) {
		t.Fatalf("Prepare without the acknowledgement = %v, want exit_risk_not_acknowledged", err)
	}
	if len(h.world.order) != 0 {
		t.Fatalf("campaigns were created without the acknowledgement: %v", h.world.order)
	}

	acknowledged := templateParams()
	acknowledged.ExitRiskAcknowledged = true
	if _, err := prepare(h, scoped(), leadaction.ActionSendTemplate, acknowledged); err != nil {
		t.Fatalf("Prepare with the acknowledgement: %v", err)
	}
	created := h.world.parts[h.world.order[0]].official
	if created.ExitRiskAcknowledgedBy != requester().UserID {
		t.Fatalf("acknowledged by %q, want the requester %q", created.ExitRiskAcknowledgedBy, requester().UserID)
	}
}

func TestAnUnacknowledgedSendNamesNobodyAsTheAcknowledger(t *testing.T) {
	h := newHarness(nil)
	h.seed(2)

	if _, err := prepare(h, scoped(), leadaction.ActionSendTemplate, templateParams()); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if by := h.world.parts[h.world.order[0]].official.ExitRiskAcknowledgedBy; by != "" {
		t.Fatalf("acknowledged by %q without an acknowledgement", by)
	}
}
