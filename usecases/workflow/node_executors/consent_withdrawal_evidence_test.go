package node_executors

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	lead_domain "vozko/domain/lead"
	"vozko/domain/workflow"
	lead_usecase "vozko/usecases/lead"
)

type leadConsentRule struct {
	lead *lead_domain.Lead
}

func (r *leadConsentRule) Record(_ context.Context, _, _ string, change lead_domain.ConsentChange) (lead_usecase.ConsentOutcome, error) {
	change.At = time.Date(2026, 10, 16, 12, 5, 0, 0, time.UTC)
	changed, err := r.lead.ApplyConsentChange(change)
	return lead_usecase.ConsentOutcome{Lead: r.lead, Changed: changed}, err
}

func leadWhoLeftWith(messageID string) *lead_domain.Lead {
	l := &lead_domain.Lead{ID: "lead-1"}
	_, err := l.ApplyConsentChange(lead_domain.ConsentChange{
		ID: "withdrawal-1", Kind: lead_domain.ConsentWithdraw, Source: lead_domain.ConsentKeyword,
		Actor:    lead_domain.ConsentActor{Type: lead_domain.ConsentActorLead, ID: "lead-1"},
		Evidence: lead_domain.ConsentEvidence{Type: lead_domain.EvidenceMessage, Ref: messageID, Excerpt: "SAIR"},
		At:       time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		panic(err)
	}
	return l
}

func TestTheRegistrarNodeCannotGrantWithTheMessageTheLeadLeftWith(t *testing.T) {
	left := leadWhoLeftWith("msg-1")
	deps := consentNodeDeps(nil)
	deps.Consent = &leadConsentRule{lead: left}
	state := map[string]interface{}{workflow.DataKeyTriggerMessageID: "msg-1"}

	result, err := runConsentNode(t, deps, map[string]interface{}{workflow.ConsentModeKey: workflow.ConsentModeGrant}, state)

	require.NoError(t, err)
	require.Contains(t, result.Error, "lead_consent_evidence_invalid")
	require.NotNil(t, left.OptedOutAt)
}
