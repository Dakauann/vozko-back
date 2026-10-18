package tools_usecase

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"vozko/domain/lead"
	lead_usecase "vozko/usecases/lead"
)

type leadConsentRule struct {
	lead *lead.Lead
}

func (r *leadConsentRule) Record(_ context.Context, _, _ string, change lead.ConsentChange) (lead_usecase.ConsentOutcome, error) {
	change.At = consentAskedAt.Add(5 * time.Minute)
	changed, err := r.lead.ApplyConsentChange(change)
	return lead_usecase.ConsentOutcome{Lead: r.lead, Changed: changed}, err
}

func TestRecordConsentCannotCiteTheMessageTheLeadLeftWith(t *testing.T) {
	left := &lead.Lead{ID: "lead-1"}
	if _, err := left.ApplyConsentChange(lead.ConsentChange{
		ID: "withdrawal-1", Kind: lead.ConsentWithdraw, Source: lead.ConsentKeyword, At: consentAskedAt.Add(3 * time.Minute),
		Actor:    lead.ConsentActor{Type: lead.ConsentActorLead, ID: "lead-1"},
		Evidence: lead.ConsentEvidence{Type: lead.EvidenceMessage, Ref: "m-stop", Excerpt: "não quero mais receber isso"},
	}); err != nil {
		t.Fatal(err)
	}

	result := runConsentTool(t, consentTool(t, &leadConsentRule{lead: left}, conversationMessages()), agentTurnConfig(),
		map[string]interface{}{"question_message_id": "m-question", "message_id": "m-stop"})

	said := fmt.Sprint(result.Result)
	if !result.IsError || !strings.Contains(said, "lead_consent_evidence_invalid") || !strings.Contains(said, "pedido de saída") {
		t.Fatalf("result = %+v", result)
	}
	if left.OptedOutAt == nil {
		t.Fatal("the withdrawal was undone")
	}
}
