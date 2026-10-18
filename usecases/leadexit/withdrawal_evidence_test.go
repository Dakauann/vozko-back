package leadexit_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/buttonaction"
	"vozko/domain/lead"
	"vozko/domain/leadexit"
)

func TestTheExitStepCitesOurStoredRowAsTheWithdrawalEvidence(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   leadexit.Inbound
		f    func(t *testing.T) *fixture
	}{
		{"a shown word", afterPromo(typed("wamid.in", "/sair")), func(t *testing.T) *fixture {
			f := newFixture(t, storedLead("lead-1", leadNumber))
			f.promoShows("sair", "")
			return f
		}},
		{"a tapped SAIR", tapOn("wamid.in", "wamid.tmpl", "vz1:1", "SAIR"), sairFixture},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := tc.f(t)
			in := tc.in
			in.TriggerMessageID = "row-in"

			if _, err := f.step.Run(context.Background(), in, &fakeReplier{}); err != nil {
				t.Fatal(err)
			}

			stored := f.leads.get(t, "lead-1")
			if stored.ConsentRecord == nil || stored.ConsentRecord.Evidence.Ref != "row-in" {
				t.Fatalf("record = %+v", stored.ConsentRecord)
			}
			grant := lead.ConsentChange{
				ID: "grant-1", Kind: lead.ConsentGrant, Source: lead.ConsentWorkflow, At: stepNow.Add(time.Minute),
				Actor:    lead.ConsentActor{Type: lead.ConsentActorWorkflow, ID: "wf-1"},
				Evidence: lead.ConsentEvidence{Type: lead.EvidenceMessage, Ref: "row-in", Excerpt: "SAIR"},
			}
			if _, err := stored.ApplyConsentChange(grant); !errors.Is(err, lead.ErrConsentEvidenceIsTheWithdrawal) {
				t.Fatalf("a grant citing the withdrawal message: %v", err)
			}
		})
	}
}

func TestAnInboundWhoseRowIsUnknownStillRecordsTheWithdrawalByItsProviderID(t *testing.T) {
	f := newFixture(t, storedLead("lead-1", leadNumber))
	f.promoShows("sair", "")

	if _, err := f.step.Run(context.Background(), afterPromo(typed("wamid.in", "/sair")), &fakeReplier{}); err != nil {
		t.Fatal(err)
	}

	if record := f.leads.get(t, "lead-1").ConsentRecord; record == nil || record.Evidence.Ref != "wamid.in" || record.Kind != lead.ConsentWithdraw {
		t.Fatalf("record = %+v", record)
	}
	if run := f.runs.of("wamid.in"); run.Kind != buttonaction.KindOptOut || run.Outcome != leadexit.OutcomeChanged {
		t.Fatalf("run = %+v", run)
	}
}
