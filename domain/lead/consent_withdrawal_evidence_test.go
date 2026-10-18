package lead

import (
	"errors"
	"testing"
	"time"
)

func withdrawnBy(evidence ConsentEvidence, source ConsentSource) *Lead {
	return withdrawnByActor(evidence, source, leadActor())
}

func withdrawnByActor(evidence ConsentEvidence, source ConsentSource, by ConsentActor) *Lead {
	l := &Lead{ID: "l-1"}
	withdrawal := change(ConsentWithdraw, source, by, evidence)
	if _, err := l.ApplyConsentChange(withdrawal); err != nil {
		panic(err)
	}
	return l
}

func TestAGrantCitingTheMessageOfTheCurrentWithdrawalIsRefused(t *testing.T) {
	cases := []struct {
		name  string
		lead  *Lead
		grant ConsentChange
	}{
		{"a workflow grant citing the SAIR message", withdrawnBy(spokenMessage("row-9", "SAIR"), ConsentKeyword),
			change(ConsentGrant, ConsentWorkflow, workflowActor(), message("row-9"))},
		{"an agent grant citing the SAIR message", withdrawnBy(spokenMessage("row-9", "SAIR"), ConsentKeyword),
			change(ConsentGrant, ConsentAIAgent, agentActor(), spokenMessage("row-9", "SAIR"))},
		{"a button grant citing the tapped SAIR", withdrawnBy(tap("row-9"), ConsentButtonReply),
			change(ConsentGrant, ConsentButtonReply, leadActor(), tap("row-9"))},
		{"a grant citing the ref with spaces around it", withdrawnBy(spokenMessage("row-9", "SAIR"), ConsentKeyword),
			change(ConsentGrant, ConsentWorkflow, workflowActor(), message(" row-9 "))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := *tc.lead
			tc.grant.At = consentAt.Add(time.Minute)
			changed, err := tc.lead.ApplyConsentChange(tc.grant)
			if changed || !errors.Is(err, ErrConsentEvidenceIsTheWithdrawal) {
				t.Fatalf("got (%v, %v), want a refusal", changed, err)
			}
			if ErrorCode(err) != "lead_consent_evidence_invalid" || !IsInputRefusal(err) {
				t.Fatalf("code %q input %v, want lead_consent_evidence_invalid as an input refusal", ErrorCode(err), IsInputRefusal(err))
			}
			if tc.lead.OptedOutAt == nil || tc.lead.ConsentRecord != before.ConsentRecord {
				t.Fatalf("the withdrawal must stay: %+v", tc.lead)
			}
		})
	}
}

func TestAGrantCitingALaterMessageStillUndoesTheWithdrawal(t *testing.T) {
	l := withdrawnBy(spokenMessage("row-9", "SAIR"), ConsentKeyword)
	grant := change(ConsentGrant, ConsentWorkflow, workflowActor(), message("row-12"))
	grant.At = consentAt.Add(time.Hour)
	if changed, err := l.ApplyConsentChange(grant); err != nil || !changed || l.OptedOutAt != nil {
		t.Fatalf("got (%v, %v), lead %+v", changed, err, l)
	}
}

func TestAnOperatorWithdrawalWithoutEvidenceNeverBlocksAGrant(t *testing.T) {
	l := withdrawnByActor(ConsentEvidence{}, ConsentManual, operatorActor())
	grant := change(ConsentGrant, ConsentManual, operatorActor(), note("ligou pedindo para voltar"))
	grant.At = consentAt.Add(time.Hour)
	if changed, err := l.ApplyConsentChange(grant); err != nil || !changed {
		t.Fatalf("got (%v, %v)", changed, err)
	}
}
